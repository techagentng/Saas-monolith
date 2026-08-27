package service

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
)

func TestLoginCreatesSessionAndCredentials(t *testing.T) {
	user := &model.User{ID: "550e8400-e29b-41d4-a716-446655440000", Email: "user@example.com", PasswordHash: "hash", Status: model.StatusActive}
	sessions := &fakeSessionRepository{}
	service := NewAuthenticationService(&fakeUserRepository{user: user}, &fakeVerifier{valid: true}, sessions, NewTokenManager(testTokenConfig()))

	result, err := service.Login(context.Background(), LoginInput{Email: " User@Example.COM ", Password: "password"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.User.ID != user.ID || result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("login result = %#v", result)
	}
	if sessions.created.UserID != user.ID || sessions.created.RefreshTokenHash == "" {
		t.Fatalf("session = %#v", sessions.created)
	}
}

func TestLoginUnknownEmailAndWrongPasswordAreGenericAndCreateNoSession(t *testing.T) {
	for name, repository := range map[string]*fakeUserRepository{
		"unknown email":  {findErr: apperrors.New(apperrors.CodeUserNotFound, "missing", nil)},
		"wrong password": {user: &model.User{ID: "550e8400-e29b-41d4-a716-446655440000", Email: "user@example.com", PasswordHash: "hash", Status: model.StatusActive}},
	} {
		t.Run(name, func(t *testing.T) {
			sessions := &fakeSessionRepository{}
			verifier := &fakeVerifier{valid: name != "wrong password"}
			service := NewAuthenticationService(repository, verifier, sessions, NewTokenManager(testTokenConfig()))
			_, err := service.Login(context.Background(), LoginInput{Email: "user@example.com", Password: "password"})
			assertAuthCode(t, err, apperrors.CodeInvalidCredentials)
			if sessions.created != nil {
				t.Fatal("failed login created a session")
			}
		})
	}
}

func TestLoginDisabledUserIsGenericAndCreatesNoSession(t *testing.T) {
	sessions := &fakeSessionRepository{}
	service := NewAuthenticationService(&fakeUserRepository{user: &model.User{ID: "550e8400-e29b-41d4-a716-446655440000", Email: "user@example.com", PasswordHash: "hash", Status: model.StatusDisabled}}, &fakeVerifier{valid: true}, sessions, NewTokenManager(testTokenConfig()))

	_, err := service.Login(context.Background(), LoginInput{Email: "user@example.com", Password: "password"})
	assertAuthCode(t, err, apperrors.CodeInvalidCredentials)
	if sessions.created != nil {
		t.Fatal("disabled login created a session")
	}
}

func TestRefreshRotatesCredentialAndLogoutRevokesSession(t *testing.T) {
	user := &model.User{ID: "550e8400-e29b-41d4-a716-446655440000", Email: "user@example.com", Status: model.StatusActive}
	session := &model.Session{ID: "550e8400-e29b-41d4-a716-446655440001", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	sessions := &fakeSessionRepository{session: session}
	service := NewAuthenticationService(&fakeUserRepository{user: user}, nil, sessions, NewTokenManager(testTokenConfig()))

	result, err := service.Refresh(context.Background(), "refresh-token")
	if err != nil || result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("Refresh() = %#v, %v", result, err)
	}
	// The browser lost all in-memory state on reload; refresh is the only
	// call that can tell it who it is signed in as.
	if result.User == nil || result.User.ID != user.ID {
		t.Fatalf("Refresh() user = %#v, want the session's user for state restoration", result.User)
	}
	if sessions.rotatedHash == "" || sessions.rotatedHash == session.RefreshTokenHash {
		t.Fatal("refresh credential was not rotated")
	}
	if err := service.Logout(context.Background(), session.ID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if !sessions.revoked {
		t.Fatal("session was not revoked")
	}
}

// A session must not outlive the account behind it: a disabled user holding a
// still-unexpired refresh cookie must not be able to mint fresh access tokens.
func TestRefreshRejectsSessionWhoseUserIsNoLongerActive(t *testing.T) {
	for name, user := range map[string]*model.User{
		"disabled user": {ID: "550e8400-e29b-41d4-a716-446655440000", Email: "user@example.com", Status: model.StatusDisabled},
		"missing user":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			session := &model.Session{ID: "550e8400-e29b-41d4-a716-446655440001", UserID: "550e8400-e29b-41d4-a716-446655440000", ExpiresAt: time.Now().Add(time.Hour)}
			service := NewAuthenticationService(&fakeUserRepository{user: user}, nil, &fakeSessionRepository{session: session}, NewTokenManager(testTokenConfig()))

			_, err := service.Refresh(context.Background(), "refresh-token")
			assertAuthCode(t, err, apperrors.CodeSessionRevoked)
		})
	}
}

func assertAuthCode(t *testing.T, err error, code apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("error = %v, want %q", err, code)
	}
}

type fakeUserRepository struct {
	user    *model.User
	findErr error
}

func (r *fakeUserRepository) Create(context.Context, model.User) (*model.User, error) {
	return nil, errors.New("not used")
}

func (r *fakeUserRepository) FindByEmail(context.Context, string) (*model.User, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.user, nil
}

func (r *fakeUserRepository) FindByID(context.Context, string) (*model.User, error) {
	return r.user, r.findErr
}

type fakeVerifier struct{ valid bool }

func (v *fakeVerifier) Verify(string, string) error {
	if !v.valid {
		return errors.New("password mismatch")
	}
	return nil
}

type fakeSessionRepository struct {
	created     *model.Session
	session     *model.Session
	rotatedHash string
	revoked     bool
}

func (r *fakeSessionRepository) Create(_ context.Context, session model.Session) (*model.Session, error) {
	r.created = &session
	return &session, nil
}

func (r *fakeSessionRepository) Rotate(_ context.Context, _ string, hash string, _ time.Time) (*model.Session, error) {
	r.rotatedHash = hash
	return r.session, nil
}

func (r *fakeSessionRepository) Revoke(context.Context, string) error {
	r.revoked = true
	return nil
}

func (r *fakeSessionRepository) FindActive(context.Context, string, string, time.Time) (*model.Session, error) {
	return r.session, nil
}

func testTokenConfig() TokenConfig {
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	return TokenConfig{AccessLifetime: 15 * time.Minute, SessionLifetime: 24 * time.Hour, PrivateKey: privateKey, PublicKey: publicKey}
}
