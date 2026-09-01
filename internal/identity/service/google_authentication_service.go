package service

import (
	"context"
	"errors"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/repository"
)

// GoogleAuthenticationService turns a Google authorization code into the
// application's ordinary session.
//
// What it deliberately does NOT do is as important as what it does. It never
// creates a tenant, never writes a membership, and never assigns a role — not
// BUSINESS_OWNER, not STAFF, and least of all SUPER_ADMIN. Signing in with
// Google authenticates a person; every authorization decision downstream still
// reads tenant_memberships and user_roles exactly as it does for a password
// login. A user who signs in with Google for the first time lands in the
// existing "no tenant yet" path, which is the frontend's job, not this
// service's.
type GoogleAuthenticationService interface {
	// AuthorizationURL is the Google consent URL for an already-bound state.
	AuthorizationURL(state string) string
	// Authenticate exchanges and verifies the callback's authorization code,
	// resolves it to a local user, and issues the same session password login
	// issues.
	Authenticate(ctx context.Context, code string) (*AuthenticationResult, error)
}

// sessionIssuer is the single capability this service needs from the existing
// AuthenticationService. Depending on the narrow interface rather than the
// whole service is what keeps "Google gets a normal session" a statement about
// reuse instead of a second implementation to keep in sync.
type sessionIssuer interface {
	IssueForUser(ctx context.Context, user *model.User) (*AuthenticationResult, error)
}

type googleAuthenticationService struct {
	client     GoogleAuthorizationClient
	verifier   GoogleIDTokenVerifier
	users      repository.UserRepository
	identities repository.IdentityRepository
	transactor repository.IdentityTransactor
	sessions   sessionIssuer
}

func NewGoogleAuthenticationService(
	client GoogleAuthorizationClient,
	verifier GoogleIDTokenVerifier,
	users repository.UserRepository,
	identities repository.IdentityRepository,
	transactor repository.IdentityTransactor,
	sessions sessionIssuer,
) GoogleAuthenticationService {
	return &googleAuthenticationService{
		client:     client,
		verifier:   verifier,
		users:      users,
		identities: identities,
		transactor: transactor,
		sessions:   sessions,
	}
}

func (s *googleAuthenticationService) AuthorizationURL(state string) string {
	return s.client.AuthorizationURL(state)
}

func (s *googleAuthenticationService) Authenticate(ctx context.Context, code string) (*AuthenticationResult, error) {
	if code == "" {
		return nil, apperrors.New(apperrors.CodeOAuthExchangeFailed, "google callback carried no authorization code", nil)
	}
	rawIDToken, err := s.client.ExchangeIDToken(ctx, code)
	if err != nil {
		return nil, err
	}
	identity, err := s.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, err
	}
	user, err := s.resolveUser(ctx, identity)
	if err != nil {
		return nil, err
	}
	return s.sessions.IssueForUser(ctx, user)
}

// resolveUser applies the account-linking policy.
//
//  1. (GOOGLE, sub) already known -> that user, always. The subject is the
//     identity; the email attached to it is free to change.
//  2. No identity, but a local account holds the same normalized email ->
//     link them. Safe only because the checks above have already established
//     that Google asserted the address AND marked it verified: Google has
//     proven control of the mailbox, which is the same proof a password-reset
//     email would give. The existing password hash is left untouched, so the
//     account keeps working exactly as it did, by either method.
//  3. Otherwise -> a brand-new user and identity, created atomically.
//
// A non-ACTIVE user is refused at every branch. Disabling an account has to
// mean disabled, or federated sign-in would be a way around it.
func (s *googleAuthenticationService) resolveUser(ctx context.Context, identity model.GoogleIdentity) (*model.User, error) {
	if identity.Subject == "" {
		return nil, apperrors.New(apperrors.CodeOAuthInvalidIdentityToken, "google identity carried no subject", nil)
	}
	// Normalized through the same function password registration uses, so an
	// address Google spells with capitals and the one typed into the register
	// form are the same account, never two.
	email, err := model.NormalizeEmail(identity.Email)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeOAuthInvalidIdentityToken, "google identity carried no usable email", err)
	}
	if !identity.EmailVerified {
		return nil, apperrors.New(apperrors.CodeOAuthEmailUnverified, "google email is not verified", nil)
	}

	existing, err := s.identities.FindByProviderSubject(ctx, model.ProviderGoogle, identity.Subject)
	switch {
	case err == nil:
		return s.activeUserByID(ctx, existing, email)
	case hasCode(err, apperrors.CodeResourceNotFound):
		// Not linked yet — fall through to link-or-create.
	default:
		return nil, err
	}

	user, err := s.users.FindByEmail(ctx, email)
	switch {
	case err == nil && user != nil:
		return s.linkExistingUser(ctx, user, identity.Subject, email)
	case err == nil, hasCode(err, apperrors.CodeUserNotFound):
		return s.createUserWithIdentity(ctx, identity.Subject, email)
	default:
		return nil, err
	}
}

func (s *googleAuthenticationService) activeUserByID(ctx context.Context, identity *model.UserIdentity, email string) (*model.User, error) {
	user, err := s.users.FindByID(ctx, identity.UserID)
	if err != nil || user == nil || user.Status != model.StatusActive {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", err)
	}
	// Best effort, and non-fatal on purpose: this column is audit data, so
	// failing to refresh it must never turn a valid sign-in into an error.
	if identity.ProviderEmail != email {
		_ = s.identities.UpdateProviderEmail(ctx, identity.ID, email)
	}
	return user, nil
}

func (s *googleAuthenticationService) linkExistingUser(ctx context.Context, user *model.User, subject, email string) (*model.User, error) {
	if user.Status != model.StatusActive {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)
	}
	// One insert, so no transaction is needed. The user row is not touched at
	// all: no password rewrite, no status change, and nothing that could alter
	// the memberships or role assignments the account already holds.
	if _, err := s.identities.Create(ctx, model.UserIdentity{
		ID:              newID(),
		UserID:          user.ID,
		Provider:        model.ProviderGoogle,
		ProviderSubject: subject,
		ProviderEmail:   email,
	}); err != nil {
		return nil, err
	}
	return user, nil
}

// createUserWithIdentity creates the user and the link atomically. A failure
// anywhere inside leaves no user row behind, so a retried sign-in starts from
// a clean slate rather than colliding with a half-created account.
//
// PasswordHash is written empty because there is no password. That is safe
// rather than sloppy: Login rejects an empty submitted password before it ever
// reaches the verifier, and bcrypt rejects an empty hash regardless — so this
// account cannot be signed into with any password, including none.
func (s *googleAuthenticationService) createUserWithIdentity(ctx context.Context, subject, email string) (*model.User, error) {
	var created *model.User
	err := s.transactor.WithinTransaction(ctx, func(users repository.UserRepository, identities repository.IdentityRepository) error {
		user, err := users.Create(ctx, model.User{ID: newID(), Email: email, PasswordHash: "", Status: model.StatusActive})
		if err != nil {
			return err
		}
		if _, err := identities.Create(ctx, model.UserIdentity{
			ID:              newID(),
			UserID:          user.ID,
			Provider:        model.ProviderGoogle,
			ProviderSubject: subject,
			ProviderEmail:   email,
		}); err != nil {
			return err
		}
		created = user
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func hasCode(err error, code apperrors.ErrorCode) bool {
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && appErr.Code == code
}
