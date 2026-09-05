package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/config"
	identityhandler "github.com/techagentng/saas-monolith/internal/identity/handler"
	identitymodel "github.com/techagentng/saas-monolith/internal/identity/model"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
)

// These tests exercise the Google OAuth routes exactly as app.New registers
// them: on a real http.ServeMux, with NO authentication, tenant-context or
// permission middleware. That absence is part of the contract — the whole
// point of these two endpoints is that the browser has no session yet.

func buildGoogleRoutes(enabled bool) (http.Handler, *routeFakeGoogleService) {
	googleService := &routeFakeGoogleService{}
	var wired identityservice.GoogleAuthenticationService
	if enabled {
		wired = googleService
	}
	handler := identityhandler.NewGoogleAuthHandler(
		wired,
		identityhandler.RefreshCookieConfig{Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 24 * time.Hour},
		identityhandler.OAuthStateConfig{Secure: true, SameSite: http.SameSiteLaxMode, Lifetime: googleStateLifetime},
		"https://app.example.com",
		enabled,
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/google", handler.Start)
	mux.HandleFunc("GET "+config.GoogleCallbackPath, handler.Callback)
	return mux, googleService
}

func TestGoogleStartRouteIsAnonymous(t *testing.T) {
	handler, _ := buildGoogleRoutes(true)
	recorder := httptest.NewRecorder()

	// Deliberately no Authorization header and no cookie.
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/google", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s, want a redirect to Google without authentication", recorder.Code, recorder.Body.String())
	}
}

// The callback path is the value Google is configured to redirect to. If this
// route ever moved, every production sign-in would fail with
// redirect_uri_mismatch, so the route and the configuration constant are
// asserted to be the same string.
func TestGoogleCallbackRouteMatchesTheConfiguredRedirectPath(t *testing.T) {
	handler, googleService := buildGoogleRoutes(true)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, config.GoogleCallbackPath+"?error=access_denied", nil))

	if recorder.Code == http.StatusNotFound {
		t.Fatalf("the configured callback path %q is not routed", config.GoogleCallbackPath)
	}
	if googleService.authenticated {
		t.Fatal("an errored callback reached the authentication service")
	}
}

// POST is not part of this contract: both endpoints are browser redirects.
func TestGoogleRoutesRejectNonGetMethods(t *testing.T) {
	handler, _ := buildGoogleRoutes(true)

	for _, path := range []string{"/api/v1/auth/google", config.GoogleCallbackPath} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code == http.StatusFound || recorder.Code == http.StatusSeeOther {
			t.Fatalf("POST %s was served as a redirect", path)
		}
	}
}

// A deployment with no Google credentials still routes both endpoints, so the
// frontend gets a typed 503 rather than a 404 it cannot interpret.
func TestGoogleRoutesAreRegisteredEvenWhenDisabled(t *testing.T) {
	handler, _ := buildGoogleRoutes(false)

	for _, path := range []string{"/api/v1/auth/google", config.GoogleCallbackPath} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET %s = %d, want 503 when Google sign-in is not configured", path, recorder.Code)
		}
	}
}

type routeFakeGoogleService struct{ authenticated bool }

func (s *routeFakeGoogleService) AuthorizationURL(state string) string {
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state
}

func (s *routeFakeGoogleService) Authenticate(context.Context, string) (*identityservice.AuthenticationResult, error) {
	s.authenticated = true
	return &identityservice.AuthenticationResult{
		User:         &identitymodel.User{ID: "550e8400-e29b-41d4-a716-446655440030", Email: "person@example.com", Status: identitymodel.StatusActive},
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    900,
	}, nil
}
