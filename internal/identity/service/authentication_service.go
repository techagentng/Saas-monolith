package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/repository"
)

type LoginInput struct{ Email, Password string }

type AuthenticationResult struct {
	User         *model.User
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

type AuthenticationService interface {
	Login(ctx context.Context, input LoginInput) (*AuthenticationResult, error)
	Refresh(ctx context.Context, refreshToken string) (*AuthenticationResult, error)
	Logout(ctx context.Context, sessionID string) error
	// IssueForUser mints the ordinary application session for an already
	// authenticated user. It exists so a federated sign-in (Google) produces
	// the identical session, refresh credential and access token that password
	// login produces, rather than growing a second session architecture
	// alongside this one. It performs no credential check of its own — the
	// caller is responsible for having actually authenticated the user, and
	// for rejecting a non-ACTIVE one.
	IssueForUser(ctx context.Context, user *model.User) (*AuthenticationResult, error)
}

type authenticationService struct {
	users    repository.UserRepository
	verifier interface{ Verify(string, string) error }
	sessions repository.SessionRepository
	tokens   *TokenManager
}

func NewAuthenticationService(users repository.UserRepository, verifier interface{ Verify(string, string) error }, sessions repository.SessionRepository, tokens *TokenManager) AuthenticationService {
	return &authenticationService{users: users, verifier: verifier, sessions: sessions, tokens: tokens}
}

func (s *authenticationService) Login(ctx context.Context, input LoginInput) (*AuthenticationResult, error) {
	email, err := model.NormalizeEmail(input.Email)
	if err != nil || input.Password == "" {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", err)
	}
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil || user == nil {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", err)
	}
	if err := s.verifier.Verify(input.Password, user.PasswordHash); err != nil || user.Status != model.StatusActive {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", err)
	}
	return s.issue(ctx, user)
}

func (s *authenticationService) Refresh(ctx context.Context, refreshToken string) (*AuthenticationResult, error) {
	raw, err := newRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generating refresh credential: %w", err)
	}
	hash := hashRefreshToken(refreshToken)
	session, err := s.sessions.Rotate(ctx, hash, hashRefreshToken(raw), time.Now())
	if err != nil {
		return nil, fmt.Errorf("rotating session: %w", err)
	}
	if session.Expired(time.Now()) {
		return nil, apperrors.New(apperrors.CodeSessionExpired, "session expired", nil)
	}
	if session.Revoked() {
		return nil, apperrors.New(apperrors.CodeSessionRevoked, "session revoked", nil)
	}
	access, err := s.tokens.Issue(session.UserID, session.ID)
	if err != nil {
		return nil, fmt.Errorf("issuing access token: %w", err)
	}
	// The user is resolved and returned so a browser that refreshed (losing
	// all in-memory state) can rebuild its identity from this one call,
	// rather than needing a separate "who am I" endpoint. A session whose
	// user has since been removed or deactivated must not refresh: the
	// session is treated as no longer valid rather than half-restored.
	user, err := s.users.FindByID(ctx, session.UserID)
	if err != nil || user == nil || user.Status != model.StatusActive {
		return nil, apperrors.New(apperrors.CodeSessionRevoked, "session revoked", err)
	}
	return &AuthenticationResult{User: user, AccessToken: access, RefreshToken: raw, ExpiresIn: int64(s.tokens.config.AccessLifetime.Seconds())}, nil
}

func (s *authenticationService) IssueForUser(ctx context.Context, user *model.User) (*AuthenticationResult, error) {
	if user == nil || user.Status != model.StatusActive {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)
	}
	return s.issue(ctx, user)
}

func (s *authenticationService) Logout(ctx context.Context, sessionID string) error {
	return s.sessions.Revoke(ctx, sessionID)
}

func (s *authenticationService) issue(ctx context.Context, user *model.User) (*AuthenticationResult, error) {
	raw, err := newRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generating refresh credential: %w", err)
	}
	now := time.Now()
	session := model.Session{ID: uuid.NewString(), UserID: user.ID, RefreshTokenHash: hashRefreshToken(raw), CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(s.tokens.config.SessionLifetime)}
	if _, err := s.sessions.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	access, err := s.tokens.Issue(user.ID, session.ID)
	if err != nil {
		return nil, fmt.Errorf("issuing access token: %w", err)
	}
	return &AuthenticationResult{User: user, AccessToken: access, RefreshToken: raw, ExpiresIn: int64(s.tokens.config.AccessLifetime.Seconds())}, nil
}

func newRefreshToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
func hashRefreshToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
