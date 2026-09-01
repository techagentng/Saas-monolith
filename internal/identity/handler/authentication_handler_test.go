package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

func testCookieConfig() RefreshCookieConfig {
	return RefreshCookieConfig{Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 24 * time.Hour}
}

func findRefreshCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == RefreshCookieName {
			return cookie
		}
	}
	return nil
}

func TestLoginHandlerReturnsCredentialsWithoutPassword(t *testing.T) {
	handler := NewAuthenticationHandler(&fakeAuthenticationService{result: &service.AuthenticationResult{User: &model.User{ID: "id", Email: "user@example.com", PasswordHash: "hash"}, AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900}}, testCookieConfig())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"user@example.com","password":"password"}`))

	handler.Login(recorder, request)

	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "hash") || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("status/body = %d/%s", recorder.Code, recorder.Body.String())
	}
}

// The whole point of the cookie architecture: the refresh credential must be
// unreadable by frontend JavaScript, which means it must not appear in the
// response body at all.
func TestLoginPutsRefreshTokenInHttpOnlyCookieAndNotInBody(t *testing.T) {
	handler := NewAuthenticationHandler(&fakeAuthenticationService{result: &service.AuthenticationResult{User: &model.User{ID: "id", Email: "user@example.com"}, AccessToken: "access", RefreshToken: "secret-refresh", ExpiresIn: 900}}, testCookieConfig())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"user@example.com","password":"password"}`))

	handler.Login(recorder, request)

	if strings.Contains(recorder.Body.String(), "secret-refresh") || strings.Contains(recorder.Body.String(), "refresh_token") {
		t.Fatalf("refresh credential leaked into response body: %s", recorder.Body.String())
	}
	cookie := findRefreshCookie(t, recorder)
	if cookie == nil {
		t.Fatal("no refresh cookie was set on login")
	}
	if cookie.Value != "secret-refresh" {
		t.Fatalf("cookie value = %q, want the issued refresh token", cookie.Value)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie flags = HttpOnly:%v Secure:%v SameSite:%v", cookie.HttpOnly, cookie.Secure, cookie.SameSite)
	}
	if cookie.Path != refreshCookiePath {
		t.Fatalf("cookie path = %q, want %q", cookie.Path, refreshCookiePath)
	}
}

func TestRefreshReadsCookieAndRotatesIt(t *testing.T) {
	serviceMock := &fakeAuthenticationService{result: &service.AuthenticationResult{User: &model.User{ID: "id", Email: "user@example.com"}, AccessToken: "new-access", RefreshToken: "rotated-refresh", ExpiresIn: 900}}
	handler := NewAuthenticationHandler(serviceMock, testCookieConfig())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: RefreshCookieName, Value: "presented-refresh"})
	recorder := httptest.NewRecorder()

	handler.Refresh(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if serviceMock.refreshedWith != "presented-refresh" {
		t.Fatalf("service received %q, want the cookie value", serviceMock.refreshedWith)
	}
	cookie := findRefreshCookie(t, recorder)
	if cookie == nil || cookie.Value != "rotated-refresh" {
		t.Fatalf("rotated cookie = %+v, want the replacement credential", cookie)
	}
	if !strings.Contains(recorder.Body.String(), "new-access") {
		t.Fatalf("body missing new access token: %s", recorder.Body.String())
	}
}

// A body-supplied refresh token must not work — accepting one would reopen
// the JavaScript-readable path the cookie exists to close.
func TestRefreshIgnoresRequestBodyAndRequiresCookie(t *testing.T) {
	serviceMock := &fakeAuthenticationService{result: &service.AuthenticationResult{AccessToken: "access"}}
	handler := NewAuthenticationHandler(serviceMock, testCookieConfig())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"from-body"}`))
	recorder := httptest.NewRecorder()

	handler.Refresh(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when no cookie is present", recorder.Code)
	}
	if serviceMock.refreshedWith != "" {
		t.Fatalf("service was called with %q; a body token must never be honored", serviceMock.refreshedWith)
	}
}

// A credential that fails to rotate is dead forever; leaving it in the browser
// would make every later request replay a guaranteed failure.
func TestRefreshClearsCookieWhenSessionIsNoLongerValid(t *testing.T) {
	serviceMock := &fakeAuthenticationService{createErr: apperrors.New(apperrors.CodeSessionRevoked, "session revoked", nil)}
	handler := NewAuthenticationHandler(serviceMock, testCookieConfig())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: RefreshCookieName, Value: "stale"})
	recorder := httptest.NewRecorder()

	handler.Refresh(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	cookie := findRefreshCookie(t, recorder)
	if cookie == nil || cookie.MaxAge >= 0 || cookie.Value != "" {
		t.Fatalf("cookie = %+v, want an immediate expiry", cookie)
	}
}

func TestLogoutHandlerUsesAuthenticatedPrincipal(t *testing.T) {
	serviceMock := &fakeAuthenticationService{}
	handler := NewAuthenticationHandler(serviceMock, testCookieConfig())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil).WithContext(auth.WithPrincipal(context.Background(), auth.Principal{UserID: "user", SessionID: "session"}))
	recorder := httptest.NewRecorder()

	handler.Logout(recorder, request)

	if recorder.Code != http.StatusNoContent || serviceMock.loggedOut != "session" {
		t.Fatalf("status/session = %d/%q", recorder.Code, serviceMock.loggedOut)
	}
}

// Without this, a browser refresh after logout would present a cookie whose
// session is revoked — the user would appear signed out, then get a confusing
// 401 on the next call instead of a clean unauthenticated state.
func TestLogoutExpiresRefreshCookie(t *testing.T) {
	handler := NewAuthenticationHandler(&fakeAuthenticationService{}, testCookieConfig())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil).WithContext(auth.WithPrincipal(context.Background(), auth.Principal{UserID: "user", SessionID: "session"}))
	recorder := httptest.NewRecorder()

	handler.Logout(recorder, request)

	cookie := findRefreshCookie(t, recorder)
	if cookie == nil {
		t.Fatal("logout did not clear the refresh cookie")
	}
	if cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("cookie = %+v, want an immediate expiry", cookie)
	}
}

type fakeAuthenticationService struct {
	result        *service.AuthenticationResult
	createErr     error
	loggedOut     string
	refreshedWith string
}

func (s *fakeAuthenticationService) Login(context.Context, service.LoginInput) (*service.AuthenticationResult, error) {
	return s.result, s.createErr
}
func (s *fakeAuthenticationService) Refresh(_ context.Context, token string) (*service.AuthenticationResult, error) {
	s.refreshedWith = token
	return s.result, s.createErr
}
func (s *fakeAuthenticationService) Logout(_ context.Context, sessionID string) error {
	s.loggedOut = sessionID
	return s.createErr
}
func (s *fakeAuthenticationService) IssueForUser(context.Context, *model.User) (*service.AuthenticationResult, error) {
	return s.result, s.createErr
}
