package handler

import (
	"net/http"
	"time"
)

// RefreshCookieName is the only cookie this API sets. It carries the opaque
// refresh credential and nothing else — no user id, no role, no permission
// set, no tenant id, and never the access token. The server stores only a
// SHA-256 hash of this value (identity/service.hashRefreshToken), so the
// cookie is useless without the matching session row.
const RefreshCookieName = "bk_refresh"

// refreshCookiePath scopes the cookie to the auth routes that actually need
// it: /login and /refresh set it, /logout clears it. It is therefore never
// attached to tenant, onboarding, or any other API request, which keeps it
// out of the blast radius of an unrelated endpoint.
const refreshCookiePath = "/api/v1/auth"

// RefreshCookieConfig carries the deployment-dependent cookie flags. Values
// come from config.Config (COOKIE_SECURE / COOKIE_SAMESITE) rather than
// being hardcoded, because the correct answer differs between local HTTP
// development and an HTTPS production deployment.
type RefreshCookieConfig struct {
	Secure   bool
	SameSite http.SameSite
	MaxAge   time.Duration
}

// ParseSameSite maps the configured string onto net/http's enum. An
// unrecognized value falls back to Lax, the safest of the three.
func ParseSameSite(value string) http.SameSite {
	switch value {
	case "Strict":
		return http.SameSiteStrictMode
	case "None":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

func (c RefreshCookieConfig) set(writer http.ResponseWriter, token string) {
	http.SetCookie(writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: c.SameSite,
		MaxAge:   int(c.MaxAge.Seconds()),
	})
}

// clear expires the cookie immediately. Name/Path/Secure/SameSite must match
// the original for the browser to actually replace it.
func (c RefreshCookieConfig) clear(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: c.SameSite,
		MaxAge:   -1,
	})
}

func readRefreshCookie(request *http.Request) string {
	cookie, err := request.Cookie(RefreshCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
