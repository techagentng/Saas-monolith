package auth

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

func TestMiddlewareEstablishesPrincipalAndIgnoresSpoofedHeader(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	tokens := service.NewTokenManager(service.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	token, err := tokens.Issue("550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := FromContext(request.Context())
		if !ok || principal.UserID != "550e8400-e29b-41d4-a716-446655440000" {
			t.Fatalf("principal = %#v, ok=%v", principal, ok)
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	middleware := Middleware{Tokens: tokens, Sessions: &sessionRepository{}}
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-User-ID", "550e8400-e29b-41d4-a716-446655440099")
	recorder := httptest.NewRecorder()

	middleware.Wrap(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestMiddlewareRejectsMissingCredential(t *testing.T) {
	middleware := Middleware{}
	recorder := httptest.NewRecorder()
	middleware.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler was called") })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

type sessionRepository struct{}

func (sessionRepository) Create(_ context.Context, session model.Session) (*model.Session, error) {
	return &session, nil
}
func (sessionRepository) Rotate(context.Context, string, string, time.Time) (*model.Session, error) {
	return nil, nil
}
func (sessionRepository) Revoke(context.Context, string) error { return nil }
func (sessionRepository) FindActive(_ context.Context, userID, sessionID string, now time.Time) (*model.Session, error) {
	return &model.Session{UserID: userID, ID: sessionID, ExpiresAt: now.Add(time.Minute)}, nil
}
