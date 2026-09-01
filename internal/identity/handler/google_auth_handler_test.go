package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
	"github.com/techagentng/saas-monolith/internal/identity/service"
)

const testFrontendURL = "https://app.example.com"

func testOAuthStateConfig() OAuthStateConfig {
	return OAuthStateConfig{Secure: true, SameSite: http.SameSiteLaxMode, Lifetime: 10 * time.Minute}
}

func newTestGoogleHandler(googleService service.GoogleAuthenticationService) *GoogleAuthHandler {
	return NewGoogleAuthHandler(googleService, testCookieConfig(), testOAuthStateConfig(), testFrontendURL, true)
}

func findCookie(recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestGoogleStartBindsStateToTheBrowserAndRedirectsToGoogle(t *testing.T) {
	googleService := &fakeGoogleAuthenticationService{result: googleSessionResult()}
	recorder := httptest.NewRecorder()

	newTestGoogleHandler(googleService).Start(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/google", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want a redirect to Google", recorder.Code)
	}
	cookie := findCookie(recorder, OAuthStateCookieName)
	if cookie == nil {
		t.Fatal("no state cookie was set; the callback would have nothing to compare against")
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.Path != oauthCookiePath {
		t.Fatalf("state cookie = %+v, want HttpOnly, Secure and scoped to the OAuth routes", cookie)
	}
	// The state Google is asked to echo back must be the one bound to this
	// browser, or the comparison in the callback proves nothing.
	if !strings.Contains(recorder.Header().Get("Location"), url.QueryEscape(cookie.Value)) &&
		!strings.Contains(recorder.Header().Get("Location"), cookie.Value) {
		t.Fatalf("redirect %q does not carry the bound state", recorder.Header().Get("Location"))
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the redirect is cacheable; an intermediary could serve one browser's state to another")
	}
}

func TestGoogleStartStoresOnlyAnInternalReturnPath(t *testing.T) {
	for name, test := range map[string]struct {
		returnTo string
		want     string
	}{
		"internal path is kept":     {"/dashboard/services", "/dashboard/services"},
		"open redirect refused":     {"https://evil.example/steal", DefaultPostLoginPath},
		"protocol relative refused": {"//evil.example", DefaultPostLoginPath},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google?return_to="+url.QueryEscape(test.returnTo), nil)

			newTestGoogleHandler(&fakeGoogleAuthenticationService{}).Start(recorder, request)

			cookie := findCookie(recorder, OAuthReturnCookieName)
			if cookie == nil || cookie.Value != test.want {
				t.Fatalf("return cookie = %+v, want value %q", cookie, test.want)
			}
		})
	}
}

func TestGoogleRoutesReportUnavailableWhenNotConfigured(t *testing.T) {
	handler := NewGoogleAuthHandler(nil, testCookieConfig(), testOAuthStateConfig(), "", false)

	for name, invoke := range map[string]func(http.ResponseWriter, *http.Request){
		"start":    handler.Start,
		"callback": handler.Callback,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			invoke(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/google", nil))

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), string(apperrors.CodeServiceUnavailable)) {
				t.Fatalf("body = %s, want a typed error the frontend can branch on", recorder.Body.String())
			}
		})
	}
}

func TestGoogleCallbackSetsTheExistingRefreshCookieAndRedirectsToTheFrontend(t *testing.T) {
	googleService := &fakeGoogleAuthenticationService{result: googleSessionResult()}
	recorder := httptest.NewRecorder()
	request := callbackRequest(t, "authorization-code", "/dashboard/services")

	newTestGoogleHandler(googleService).Callback(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect back to the frontend", recorder.Code)
	}
	if googleService.exchangedCode != "authorization-code" {
		t.Fatalf("exchanged %q, want the code from the callback", googleService.exchangedCode)
	}
	cookie := findCookie(recorder, RefreshCookieName)
	if cookie == nil || cookie.Value != "secret-refresh" {
		t.Fatalf("refresh cookie = %+v, want the existing cookie carrying the session credential", cookie)
	}
	if !cookie.HttpOnly {
		t.Fatal("the refresh cookie is readable by JavaScript")
	}
	if location := recorder.Header().Get("Location"); location != testFrontendURL+"/dashboard/services" {
		t.Fatalf("Location = %q, want the requested internal destination", location)
	}
}

// The session credential belongs in a cookie the browser cannot read and a
// server cannot log. A URL is neither: it lands in history, in referrers, and
// in every proxy log on the way.
func TestGoogleCallbackNeverPutsCredentialsInTheRedirectURL(t *testing.T) {
	recorder := httptest.NewRecorder()

	newTestGoogleHandler(&fakeGoogleAuthenticationService{result: googleSessionResult()}).
		Callback(recorder, callbackRequest(t, "authorization-code", ""))

	location := recorder.Header().Get("Location")
	for _, secret := range []string{"secret-refresh", "access-token", "refresh_token", "access_token", "id_token"} {
		if strings.Contains(location, secret) {
			t.Fatalf("redirect %q leaked %q", location, secret)
		}
	}
}

// State is single-use: both OAuth cookies are expired on every exit, so
// replaying the same callback URL finds nothing to match against.
func TestGoogleCallbackClearsOAuthStateOnSuccessAndFailure(t *testing.T) {
	for name, googleService := range map[string]*fakeGoogleAuthenticationService{
		"success": {result: googleSessionResult()},
		"failure": {err: apperrors.New(apperrors.CodeOAuthInvalidIdentityToken, "bad token", nil)},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			newTestGoogleHandler(googleService).Callback(recorder, callbackRequest(t, "authorization-code", ""))

			for _, name := range []string{OAuthStateCookieName, OAuthReturnCookieName} {
				cookie := findCookie(recorder, name)
				if cookie == nil || cookie.Value != "" || cookie.MaxAge >= 0 {
					t.Fatalf("%s = %+v, want an immediate expiry", name, cookie)
				}
			}
		})
	}
}

func TestGoogleCallbackRejectsMissingAndMismatchedState(t *testing.T) {
	for name, build := range map[string]func() *http.Request{
		"no state cookie": func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=c&state=some-state", nil)
		},
		"mismatched state": func() *http.Request {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=c&state=attacker-state", nil)
			state, _ := NewOAuthState(10*time.Minute, time.Now())
			request.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: state})
			return request
		},
		"expired state": func() *http.Request {
			state, _ := NewOAuthState(10*time.Minute, time.Now().Add(-time.Hour))
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=c&state="+url.QueryEscape(state), nil)
			request.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: state})
			return request
		},
	} {
		t.Run(name, func(t *testing.T) {
			googleService := &fakeGoogleAuthenticationService{result: googleSessionResult()}
			recorder := httptest.NewRecorder()

			newTestGoogleHandler(googleService).Callback(recorder, build())

			if googleService.exchangedCode != "" {
				t.Fatal("the authorization code was exchanged before the state was validated")
			}
			if findCookie(recorder, RefreshCookieName) != nil {
				t.Fatal("a session cookie was issued for an unvalidated callback")
			}
			assertRedirectedToLoginWithError(t, recorder, apperrors.CodeOAuthStateInvalid)
		})
	}
}

func TestGoogleCallbackHandlesGoogleErrorResponses(t *testing.T) {
	for name, test := range map[string]struct {
		googleError string
		want        apperrors.ErrorCode
	}{
		"user cancelled consent": {"access_denied", apperrors.CodeOAuthDenied},
		"anything else":          {"server_error", apperrors.CodeOAuthExchangeFailed},
	} {
		t.Run(name, func(t *testing.T) {
			googleService := &fakeGoogleAuthenticationService{result: googleSessionResult()}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?error="+test.googleError, nil)

			newTestGoogleHandler(googleService).Callback(recorder, request)

			if googleService.exchangedCode != "" {
				t.Fatal("an errored callback still attempted a code exchange")
			}
			assertRedirectedToLoginWithError(t, recorder, test.want)
		})
	}
}

// Google's own error text is attacker-influenceable and never forwarded; the
// browser only ever sees one of this application's own codes.
func TestGoogleCallbackDoesNotReflectGoogleErrorText(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?error="+url.QueryEscape("<script>alert(1)</script>"), nil)

	newTestGoogleHandler(&fakeGoogleAuthenticationService{}).Callback(recorder, request)

	if strings.Contains(recorder.Header().Get("Location"), "script") {
		t.Fatalf("Location = %q reflected Google's error text", recorder.Header().Get("Location"))
	}
}

func TestGoogleCallbackSurfacesAuthenticationFailuresAsSafeCodes(t *testing.T) {
	for name, code := range map[string]apperrors.ErrorCode{
		"exchange failed":   apperrors.CodeOAuthExchangeFailed,
		"invalid id token":  apperrors.CodeOAuthInvalidIdentityToken,
		"unverified email":  apperrors.CodeOAuthEmailUnverified,
		"identity conflict": apperrors.CodeExternalIdentityConflict,
		"disabled account":  apperrors.CodeInvalidCredentials,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			googleService := &fakeGoogleAuthenticationService{err: apperrors.New(code, "internal detail", nil)}

			newTestGoogleHandler(googleService).Callback(recorder, callbackRequest(t, "authorization-code", ""))

			if findCookie(recorder, RefreshCookieName) != nil {
				t.Fatal("a failed sign-in still issued a session cookie")
			}
			assertRedirectedToLoginWithError(t, recorder, code)
			if strings.Contains(recorder.Header().Get("Location"), "internal detail") {
				t.Fatal("an internal error message leaked into the redirect")
			}
		})
	}
}

// A destination smuggled in through the return cookie is sanitized on the way
// out too, so a tampered cookie cannot turn a successful sign-in into an open
// redirect.
func TestGoogleCallbackRefusesAnExternalReturnDestination(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=c&state=s", nil)
	state, _ := NewOAuthState(10*time.Minute, time.Now())
	request.URL.RawQuery = "code=authorization-code&state=" + url.QueryEscape(state)
	request.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: state})
	request.AddCookie(&http.Cookie{Name: OAuthReturnCookieName, Value: "https://evil.example/steal"})

	newTestGoogleHandler(&fakeGoogleAuthenticationService{result: googleSessionResult()}).Callback(recorder, request)

	if location := recorder.Header().Get("Location"); location != testFrontendURL+DefaultPostLoginPath {
		t.Fatalf("Location = %q, want the external destination refused", location)
	}
}

func callbackRequest(t *testing.T, code, returnPath string) *http.Request {
	t.Helper()
	state, err := NewOAuthState(10*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("NewOAuthState() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
	request.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: state})
	if returnPath != "" {
		request.AddCookie(&http.Cookie{Name: OAuthReturnCookieName, Value: returnPath})
	}
	return request
}

func assertRedirectedToLoginWithError(t *testing.T, recorder *httptest.ResponseRecorder, want apperrors.ErrorCode) {
	t.Helper()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect back to the login page", recorder.Code)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", recorder.Header().Get("Location"), err)
	}
	if location.Scheme+"://"+location.Host != testFrontendURL || location.Path != loginPath {
		t.Fatalf("Location = %q, want the frontend login page", location)
	}
	if got := location.Query().Get(AuthErrorQueryParameter); got != string(want) {
		t.Fatalf("%s = %q, want %q", AuthErrorQueryParameter, got, want)
	}
}

func googleSessionResult() *service.AuthenticationResult {
	return &service.AuthenticationResult{
		User:         &model.User{ID: "550e8400-e29b-41d4-a716-446655440020", Email: "person@example.com", Status: model.StatusActive},
		AccessToken:  "access-token",
		RefreshToken: "secret-refresh",
		ExpiresIn:    900,
	}
}

type fakeGoogleAuthenticationService struct {
	result        *service.AuthenticationResult
	err           error
	exchangedCode string
}

func (s *fakeGoogleAuthenticationService) AuthorizationURL(state string) string {
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + url.QueryEscape(state)
}

func (s *fakeGoogleAuthenticationService) Authenticate(_ context.Context, code string) (*service.AuthenticationResult, error) {
	s.exchangedCode = code
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}
