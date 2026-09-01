package service

import (
	"context"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/repository"
)

const (
	googleSubject   = "112233445566778899000"
	googleEmail     = "person@example.com"
	existingUserID  = "550e8400-e29b-41d4-a716-446655440010"
	existingIdentID = "550e8400-e29b-41d4-a716-446655440011"
)

func verifiedGoogleIdentity() model.GoogleIdentity {
	return model.GoogleIdentity{Subject: googleSubject, Email: googleEmail, EmailVerified: true}
}

func newGoogleService(t *testing.T, users *fakeGoogleUserRepository, identities *fakeIdentityRepository, verifier *fakeGoogleVerifier, transactor *fakeTransactor, issuer *fakeSessionIssuer) GoogleAuthenticationService {
	t.Helper()
	return NewGoogleAuthenticationService(&fakeGoogleClient{}, verifier, users, identities, transactor, issuer)
}

func TestGoogleAuthenticateCreatesUserAndIdentityForFirstTimeSignIn(t *testing.T) {
	users := &fakeGoogleUserRepository{byEmailErr: apperrors.New(apperrors.CodeUserNotFound, "missing", nil)}
	identities := &fakeIdentityRepository{findErr: apperrors.New(apperrors.CodeResourceNotFound, "missing", nil)}
	transactor := &fakeTransactor{users: users, identities: identities}
	issuer := &fakeSessionIssuer{}

	result, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, transactor, issuer).
		Authenticate(context.Background(), "authorization-code")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("result = %#v, want a normal application session", result)
	}
	if users.created == nil || users.created.Email != googleEmail || users.created.Status != model.StatusActive {
		t.Fatalf("created user = %#v", users.created)
	}
	// No password exists for a Google-only account, and none is invented.
	if users.created.PasswordHash != "" {
		t.Fatalf("password hash = %q, want empty for a federated account", users.created.PasswordHash)
	}
	if identities.created == nil || identities.created.ProviderSubject != googleSubject || identities.created.Provider != model.ProviderGoogle {
		t.Fatalf("created identity = %#v", identities.created)
	}
	if identities.created.UserID != users.created.ID {
		t.Fatal("identity was linked to a different user than the one created")
	}
	if !transactor.used {
		t.Fatal("user and identity were created outside a transaction")
	}
}

// The linking policy's first rule: the provider subject is the identity. A
// returning user must resolve to the same account, never a second one, even
// though nothing about their email changed.
func TestGoogleAuthenticateReturningUserReusesExistingAccount(t *testing.T) {
	user := &model.User{ID: existingUserID, Email: googleEmail, Status: model.StatusActive}
	users := &fakeGoogleUserRepository{byID: user}
	identities := &fakeIdentityRepository{found: &model.UserIdentity{ID: existingIdentID, UserID: existingUserID, Provider: model.ProviderGoogle, ProviderSubject: googleSubject, ProviderEmail: googleEmail}}
	transactor := &fakeTransactor{users: users, identities: identities}
	issuer := &fakeSessionIssuer{}

	if _, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, transactor, issuer).
		Authenticate(context.Background(), "authorization-code"); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if users.created != nil {
		t.Fatal("a repeat Google sign-in created a duplicate user")
	}
	if identities.created != nil {
		t.Fatal("a repeat Google sign-in created a duplicate identity")
	}
	if transactor.used {
		t.Fatal("a repeat Google sign-in opened a transaction it did not need")
	}
	if issuer.issuedFor == nil || issuer.issuedFor.ID != existingUserID {
		t.Fatalf("session issued for = %#v, want the existing user", issuer.issuedFor)
	}
}

// Rule two: a password account with the same verified email is linked rather
// than duplicated, and its credential is left exactly as it was.
func TestGoogleAuthenticateLinksVerifiedEmailToExistingPasswordAccount(t *testing.T) {
	user := &model.User{ID: existingUserID, Email: googleEmail, PasswordHash: "existing-bcrypt-hash", Status: model.StatusActive}
	users := &fakeGoogleUserRepository{byEmail: user, byID: user}
	identities := &fakeIdentityRepository{findErr: apperrors.New(apperrors.CodeResourceNotFound, "missing", nil)}
	transactor := &fakeTransactor{users: users, identities: identities}
	issuer := &fakeSessionIssuer{}

	if _, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, transactor, issuer).
		Authenticate(context.Background(), "authorization-code"); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if users.created != nil {
		t.Fatal("linking created a second user for an email that already had an account")
	}
	if identities.created == nil || identities.created.UserID != existingUserID {
		t.Fatalf("created identity = %#v, want a link to the existing account", identities.created)
	}
	if user.PasswordHash != "existing-bcrypt-hash" {
		t.Fatalf("password hash = %q, want it untouched by linking", user.PasswordHash)
	}
}

// The same email arriving in a different letter case is the same account. This
// runs through model.NormalizeEmail, the identical function password
// registration uses, so the two paths can never disagree about identity.
func TestGoogleAuthenticateNormalizesEmailBeforeResolving(t *testing.T) {
	user := &model.User{ID: existingUserID, Email: googleEmail, Status: model.StatusActive}
	users := &fakeGoogleUserRepository{byEmail: user, byID: user}
	identities := &fakeIdentityRepository{findErr: apperrors.New(apperrors.CodeResourceNotFound, "missing", nil)}

	if _, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: model.GoogleIdentity{Subject: googleSubject, Email: "  Person@Example.COM ", EmailVerified: true}}, &fakeTransactor{users: users, identities: identities}, &fakeSessionIssuer{}).
		Authenticate(context.Background(), "authorization-code"); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if users.lookedUpEmail != googleEmail {
		t.Fatalf("looked up %q, want the normalized address", users.lookedUpEmail)
	}
	if users.created != nil {
		t.Fatal("a differently-cased email created a duplicate account")
	}
}

// Google login authenticates a person and grants nothing. The service is
// handed no tenant, membership or role repository at all, so this asserts the
// only observable proxy: the user it creates is a plain ACTIVE account and the
// session it issues is the ordinary one.
func TestGoogleAuthenticateGrantsNoTenantMembershipOrRole(t *testing.T) {
	users := &fakeGoogleUserRepository{byEmailErr: apperrors.New(apperrors.CodeUserNotFound, "missing", nil)}
	identities := &fakeIdentityRepository{findErr: apperrors.New(apperrors.CodeResourceNotFound, "missing", nil)}
	transactor := &fakeTransactor{users: users, identities: identities}

	if _, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, transactor, &fakeSessionIssuer{}).
		Authenticate(context.Background(), "authorization-code"); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	// Exactly two writes happened: one user, one identity. Anything else would
	// mean a tenant, membership or role assignment was created.
	if transactor.userWrites != 1 || transactor.identityWrites != 1 {
		t.Fatalf("writes = %d users, %d identities, want exactly one of each", transactor.userWrites, transactor.identityWrites)
	}
	// Nothing in the identity model can carry a role: assert the created
	// identity names only the provider account, never a privilege.
	if identities.created.Provider != model.ProviderGoogle {
		t.Fatalf("identity provider = %q", identities.created.Provider)
	}
}

func TestGoogleAuthenticateRollsBackWhenIdentityInsertFails(t *testing.T) {
	users := &fakeGoogleUserRepository{byEmailErr: apperrors.New(apperrors.CodeUserNotFound, "missing", nil)}
	identities := &fakeIdentityRepository{
		findErr:   apperrors.New(apperrors.CodeResourceNotFound, "missing", nil),
		createErr: apperrors.New(apperrors.CodeExternalIdentityConflict, "already linked", nil),
	}
	transactor := &fakeTransactor{users: users, identities: identities}
	issuer := &fakeSessionIssuer{}

	_, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, transactor, issuer).
		Authenticate(context.Background(), "authorization-code")
	assertAuthCode(t, err, apperrors.CodeExternalIdentityConflict)
	if !transactor.rolledBack {
		t.Fatal("a failed identity insert did not roll the transaction back")
	}
	if issuer.issuedFor != nil {
		t.Fatal("a failed sign-in issued a session")
	}
}

// A disabled account stays disabled. Federated sign-in must not become a way
// around a revocation the application has already made.
func TestGoogleAuthenticateRefusesNonActiveUsers(t *testing.T) {
	disabled := &model.User{ID: existingUserID, Email: googleEmail, Status: model.StatusDisabled}
	for name, setup := range map[string]struct {
		users      *fakeGoogleUserRepository
		identities *fakeIdentityRepository
	}{
		"already linked": {
			users:      &fakeGoogleUserRepository{byID: disabled},
			identities: &fakeIdentityRepository{found: &model.UserIdentity{ID: existingIdentID, UserID: existingUserID, Provider: model.ProviderGoogle, ProviderSubject: googleSubject}},
		},
		"linking by email": {
			users:      &fakeGoogleUserRepository{byEmail: disabled, byID: disabled},
			identities: &fakeIdentityRepository{findErr: apperrors.New(apperrors.CodeResourceNotFound, "missing", nil)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			issuer := &fakeSessionIssuer{}
			_, err := newGoogleService(t, setup.users, setup.identities, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, &fakeTransactor{users: setup.users, identities: setup.identities}, issuer).
				Authenticate(context.Background(), "authorization-code")
			assertAuthCode(t, err, apperrors.CodeInvalidCredentials)
			if issuer.issuedFor != nil {
				t.Fatal("a disabled account was issued a session")
			}
			if setup.identities.created != nil {
				t.Fatal("a disabled account was linked to a Google identity")
			}
		})
	}
}

func TestGoogleAuthenticateRejectsUnusableIdentities(t *testing.T) {
	for name, test := range map[string]struct {
		identity model.GoogleIdentity
		code     apperrors.ErrorCode
	}{
		"unverified email": {model.GoogleIdentity{Subject: googleSubject, Email: googleEmail, EmailVerified: false}, apperrors.CodeOAuthEmailUnverified},
		"missing email":    {model.GoogleIdentity{Subject: googleSubject, Email: "", EmailVerified: true}, apperrors.CodeOAuthInvalidIdentityToken},
		"malformed email":  {model.GoogleIdentity{Subject: googleSubject, Email: "not-an-address", EmailVerified: true}, apperrors.CodeOAuthInvalidIdentityToken},
		"missing subject":  {model.GoogleIdentity{Subject: "", Email: googleEmail, EmailVerified: true}, apperrors.CodeOAuthInvalidIdentityToken},
	} {
		t.Run(name, func(t *testing.T) {
			users := &fakeGoogleUserRepository{byEmailErr: apperrors.New(apperrors.CodeUserNotFound, "missing", nil)}
			identities := &fakeIdentityRepository{findErr: apperrors.New(apperrors.CodeResourceNotFound, "missing", nil)}
			issuer := &fakeSessionIssuer{}

			_, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: test.identity}, &fakeTransactor{users: users, identities: identities}, issuer).
				Authenticate(context.Background(), "authorization-code")
			assertAuthCode(t, err, test.code)
			if users.created != nil || identities.created != nil {
				t.Fatal("an unusable Google identity still wrote to the database")
			}
			if issuer.issuedFor != nil {
				t.Fatal("an unusable Google identity was issued a session")
			}
		})
	}
}

func TestGoogleAuthenticateSurfacesExchangeAndVerificationFailures(t *testing.T) {
	users := &fakeGoogleUserRepository{}
	identities := &fakeIdentityRepository{}

	t.Run("empty code", func(t *testing.T) {
		_, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, &fakeTransactor{}, &fakeSessionIssuer{}).
			Authenticate(context.Background(), "")
		assertAuthCode(t, err, apperrors.CodeOAuthExchangeFailed)
	})

	t.Run("exchange failure", func(t *testing.T) {
		client := &fakeGoogleClient{exchangeErr: apperrors.New(apperrors.CodeOAuthExchangeFailed, "exchange rejected", nil)}
		service := NewGoogleAuthenticationService(client, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, users, identities, &fakeTransactor{}, &fakeSessionIssuer{})
		_, err := service.Authenticate(context.Background(), "authorization-code")
		assertAuthCode(t, err, apperrors.CodeOAuthExchangeFailed)
	})

	t.Run("invalid id token", func(t *testing.T) {
		verifier := &fakeGoogleVerifier{err: apperrors.New(apperrors.CodeOAuthInvalidIdentityToken, "wrong audience", nil)}
		_, err := newGoogleService(t, users, identities, verifier, &fakeTransactor{}, &fakeSessionIssuer{}).
			Authenticate(context.Background(), "authorization-code")
		assertAuthCode(t, err, apperrors.CodeOAuthInvalidIdentityToken)
	})
}

// The provider's currently asserted email is refreshed for audit purposes,
// and the local account address is deliberately not.
func TestGoogleAuthenticateUpdatesProviderEmailWithoutChangingAccountEmail(t *testing.T) {
	user := &model.User{ID: existingUserID, Email: "original@example.com", Status: model.StatusActive}
	users := &fakeGoogleUserRepository{byID: user}
	identities := &fakeIdentityRepository{found: &model.UserIdentity{ID: existingIdentID, UserID: existingUserID, Provider: model.ProviderGoogle, ProviderSubject: googleSubject, ProviderEmail: "old@example.com"}}

	if _, err := newGoogleService(t, users, identities, &fakeGoogleVerifier{identity: verifiedGoogleIdentity()}, &fakeTransactor{users: users, identities: identities}, &fakeSessionIssuer{}).
		Authenticate(context.Background(), "authorization-code"); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if identities.updatedEmail != googleEmail {
		t.Fatalf("provider email updated to %q, want %q", identities.updatedEmail, googleEmail)
	}
	if user.Email != "original@example.com" {
		t.Fatalf("account email = %q, want it unchanged by Google", user.Email)
	}
}

type fakeGoogleClient struct {
	exchangeErr error
	state       string
}

func (c *fakeGoogleClient) AuthorizationURL(state string) string {
	c.state = state
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state
}

func (c *fakeGoogleClient) ExchangeIDToken(context.Context, string) (string, error) {
	if c.exchangeErr != nil {
		return "", c.exchangeErr
	}
	return "raw-id-token", nil
}

type fakeGoogleVerifier struct {
	identity model.GoogleIdentity
	err      error
}

func (v *fakeGoogleVerifier) Verify(context.Context, string) (model.GoogleIdentity, error) {
	return v.identity, v.err
}

type fakeGoogleUserRepository struct {
	byEmail       *model.User
	byEmailErr    error
	byID          *model.User
	byIDErr       error
	created       *model.User
	createErr     error
	lookedUpEmail string
}

func (r *fakeGoogleUserRepository) Create(_ context.Context, user model.User) (*model.User, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	r.created = &user
	return &user, nil
}

func (r *fakeGoogleUserRepository) FindByEmail(_ context.Context, email string) (*model.User, error) {
	r.lookedUpEmail = email
	if r.byEmailErr != nil {
		return nil, r.byEmailErr
	}
	if r.byEmail == nil {
		return nil, apperrors.New(apperrors.CodeUserNotFound, "user not found", nil)
	}
	return r.byEmail, nil
}

func (r *fakeGoogleUserRepository) FindByID(context.Context, string) (*model.User, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	if r.byID == nil {
		return nil, apperrors.New(apperrors.CodeUserNotFound, "user not found", nil)
	}
	return r.byID, nil
}

type fakeIdentityRepository struct {
	found        *model.UserIdentity
	findErr      error
	created      *model.UserIdentity
	createErr    error
	updatedEmail string
}

func (r *fakeIdentityRepository) FindByProviderSubject(context.Context, model.Provider, string) (*model.UserIdentity, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.found == nil {
		return nil, apperrors.New(apperrors.CodeResourceNotFound, "external identity not found", nil)
	}
	return r.found, nil
}

func (r *fakeIdentityRepository) Create(_ context.Context, identity model.UserIdentity) (*model.UserIdentity, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	r.created = &identity
	return &identity, nil
}

func (r *fakeIdentityRepository) UpdateProviderEmail(_ context.Context, _, providerEmail string) error {
	r.updatedEmail = providerEmail
	return nil
}

// fakeTransactor mimics the real commit/rollback contract: work that returns
// an error must leave nothing behind, which is what `rolledBack` records.
type fakeTransactor struct {
	users          *fakeGoogleUserRepository
	identities     *fakeIdentityRepository
	used           bool
	rolledBack     bool
	userWrites     int
	identityWrites int
}

func (t *fakeTransactor) WithinTransaction(ctx context.Context, work func(repository.UserRepository, repository.IdentityRepository) error) error {
	t.used = true
	users := &countingUserRepository{inner: t.users, counter: &t.userWrites}
	identities := &countingIdentityRepository{inner: t.identities, counter: &t.identityWrites}
	if err := work(users, identities); err != nil {
		t.rolledBack = true
		// Rollback discards everything the work recorded, exactly as the real
		// transaction would.
		if t.users != nil {
			t.users.created = nil
		}
		if t.identities != nil {
			t.identities.created = nil
		}
		return err
	}
	return nil
}

type countingUserRepository struct {
	inner   *fakeGoogleUserRepository
	counter *int
}

func (r *countingUserRepository) Create(ctx context.Context, user model.User) (*model.User, error) {
	*r.counter++
	return r.inner.Create(ctx, user)
}
func (r *countingUserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	return r.inner.FindByEmail(ctx, email)
}
func (r *countingUserRepository) FindByID(ctx context.Context, id string) (*model.User, error) {
	return r.inner.FindByID(ctx, id)
}

type countingIdentityRepository struct {
	inner   *fakeIdentityRepository
	counter *int
}

func (r *countingIdentityRepository) FindByProviderSubject(ctx context.Context, provider model.Provider, subject string) (*model.UserIdentity, error) {
	return r.inner.FindByProviderSubject(ctx, provider, subject)
}
func (r *countingIdentityRepository) Create(ctx context.Context, identity model.UserIdentity) (*model.UserIdentity, error) {
	*r.counter++
	return r.inner.Create(ctx, identity)
}
func (r *countingIdentityRepository) UpdateProviderEmail(ctx context.Context, id, providerEmail string) error {
	return r.inner.UpdateProviderEmail(ctx, id, providerEmail)
}

// fakeSessionIssuer stands in for the real AuthenticationService. It returns a
// credential-shaped result so callers can assert a normal session was issued,
// and records who it was issued for.
type fakeSessionIssuer struct {
	issuedFor *model.User
	err       error
}

func (i *fakeSessionIssuer) IssueForUser(_ context.Context, user *model.User) (*AuthenticationResult, error) {
	if i.err != nil {
		return nil, i.err
	}
	i.issuedFor = user
	return &AuthenticationResult{User: user, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresIn: 900}, nil
}
