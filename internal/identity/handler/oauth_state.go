package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// OAuthStateCookieName holds the CSRF state for an in-flight Google sign-in.
// It is HttpOnly and short-lived, and it is the only thing that binds the
// callback Google sends back to the browser that actually started the flow —
// without it, anyone could feed this API a callback URL and have the resulting
// session cookie land in a victim's browser.
const OAuthStateCookieName = "bk_oauth_state"

// OAuthReturnCookieName carries the in-app path to land on after sign-in.
// It is kept out of the `state` parameter deliberately: state exists to be
// compared, and mixing a caller-influenced value into it would mean the thing
// being compared is partly attacker-chosen. Round-tripping it through Google
// is also unnecessary — the browser is the same browser either way.
const OAuthReturnCookieName = "bk_oauth_return"

// oauthCookiePath scopes both cookies to the two Google routes. They are
// therefore never attached to the refresh endpoint, tenant reads, or anything
// else, and they disappear from every request the moment the flow ends.
const oauthCookiePath = "/api/v1/auth/google"

// DefaultPostLoginPath is where a sign-in with no requested destination lands.
// It is the same entry point password login uses, so the existing
// ProtectedRoute -> TenantGate chain performs all onboarding/tenant routing
// from here; nothing about that logic is duplicated on the backend.
const DefaultPostLoginPath = "/dashboard"

// oauthStateSeparator splits the random half of a state value from its
// expiry. The format is "<base64url random>.<unix expiry>".
const oauthStateSeparator = "."

// OAuthStateConfig carries the state cookie's deployment-dependent flags.
//
// Lifetime is short by design: the value only has to survive the round trip
// through Google's consent screen. Anything longer widens the window in which
// a captured authorization URL could be replayed.
type OAuthStateConfig struct {
	Secure   bool
	SameSite http.SameSite
	Lifetime time.Duration
}

// OAuthSameSite adapts the application's configured SameSite policy for the
// OAuth cookies.
//
// Strict is downgraded to Lax, and this is a correctness fix rather than a
// weakening: Google's callback is a cross-site top-level GET navigation, and a
// Strict cookie is withheld on exactly that request — the state cookie would
// never arrive, so every single Google sign-in would fail as "invalid state".
// Lax is the strictest policy under which the flow can work at all, and it
// still withholds the cookie from cross-site subresource requests, which is
// the CSRF exposure that matters here. None is passed through unchanged, since
// a genuinely cross-site deployment already needs it (with Secure) for the
// refresh cookie.
func OAuthSameSite(configured http.SameSite) http.SameSite {
	if configured == http.SameSiteNoneMode {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

// NewOAuthState mints a state value: 32 bytes from crypto/rand, plus the
// absolute instant it stops being acceptable.
//
// The expiry travels inside the value rather than relying on the cookie's
// Max-Age alone. A cookie's lifetime is enforced by the browser, which is the
// party this value exists to distrust; an expiry the server re-reads on every
// callback is enforced by the server.
func NewOAuthState(lifetime time.Duration, now time.Time) (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data) + oauthStateSeparator + strconv.FormatInt(now.Add(lifetime).Unix(), 10), nil
}

// ValidateOAuthState accepts a callback only when the state Google echoed back
// is byte-identical to the one this browser was issued, and that value has not
// expired.
//
// The comparison is constant-time. State is not a secret in the way a password
// is, but a timing oracle on it would let an attacker reconstruct a victim's
// in-flight state one byte at a time, which is precisely the CSRF window this
// closes.
func ValidateOAuthState(cookieValue, parameterValue string, now time.Time) error {
	invalid := apperrors.New(apperrors.CodeOAuthStateInvalid, "oauth state is missing, mismatched, or expired", nil)
	if cookieValue == "" || parameterValue == "" {
		return invalid
	}
	if subtle.ConstantTimeCompare([]byte(cookieValue), []byte(parameterValue)) != 1 {
		return invalid
	}
	_, expiry, found := strings.Cut(cookieValue, oauthStateSeparator)
	if !found {
		return invalid
	}
	seconds, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || !now.Before(time.Unix(seconds, 0)) {
		return invalid
	}
	return nil
}

func (c OAuthStateConfig) setState(writer http.ResponseWriter, state string) {
	c.setCookie(writer, OAuthStateCookieName, state, int(c.Lifetime.Seconds()))
}

func (c OAuthStateConfig) setReturnPath(writer http.ResponseWriter, path string) {
	c.setCookie(writer, OAuthReturnCookieName, path, int(c.Lifetime.Seconds()))
}

// clear expires both OAuth cookies. The callback calls this on every exit,
// success or failure, so a state value is single-use: replaying the same
// callback URL a second time finds no cookie to match against.
func (c OAuthStateConfig) clear(writer http.ResponseWriter) {
	c.setCookie(writer, OAuthStateCookieName, "", -1)
	c.setCookie(writer, OAuthReturnCookieName, "", -1)
}

func (c OAuthStateConfig) setCookie(writer http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     oauthCookiePath,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: c.SameSite,
		MaxAge:   maxAge,
	})
}

func readCookie(request *http.Request, name string) string {
	cookie, err := request.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SafeRelativePath reduces a caller-supplied destination to something that can
// only ever point back into this application, falling back to
// DefaultPostLoginPath when it cannot.
//
// Every rejection below is a real open-redirect vector, not defensive
// paranoia: "https://evil.example" is absolute; "//evil.example" is
// protocol-relative and browsers treat it as absolute; "/\evil.example" is
// normalized to a protocol-relative URL by several browsers; and a value
// carrying a scheme or host at all is by definition not internal. What
// survives is a path (optionally with a query) that resolves against the
// configured frontend origin and nowhere else.
func SafeRelativePath(candidate string) string {
	if candidate == "" {
		return DefaultPostLoginPath
	}
	if strings.ContainsAny(candidate, "\\\r\n\t") {
		return DefaultPostLoginPath
	}
	if !strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "//") {
		return DefaultPostLoginPath
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Scheme != "" || parsed.Opaque != "" {
		return DefaultPostLoginPath
	}
	target := parsed.EscapedPath()
	if !strings.HasPrefix(target, "/") {
		return DefaultPostLoginPath
	}
	if parsed.RawQuery != "" {
		target += "?" + parsed.RawQuery
	}
	return target
}

// FrontendRedirect composes the absolute destination the browser is sent to.
// frontendURL comes from configuration and is the only origin this API will
// ever redirect a signed-in browser to; path has already been through
// SafeRelativePath.
func FrontendRedirect(frontendURL, path string) string {
	return strings.TrimRight(frontendURL, "/") + SafeRelativePath(path)
}
