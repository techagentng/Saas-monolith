package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env              string
	Host             string
	Port             int
	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresSSLMode  string
	MigrationsDir    string
	AccessLifetime   time.Duration
	SessionLifetime  time.Duration
	PrivateKey       ed25519.PrivateKey
	PublicKey        ed25519.PublicKey
	AllowedOrigins   []string
	// CookieSecure marks the refresh cookie Secure (HTTPS-only). Defaults to
	// true in production and false in development, where the API is served
	// over plain HTTP — a Secure cookie would simply never be stored there.
	CookieSecure bool
	// CookieSameSite is the refresh cookie's SameSite policy. "Lax" is the
	// default and is correct whenever the browser app and this API share a
	// registrable domain (including localhost:3000 -> localhost:8090, where
	// the differing port does not make the request cross-site). A genuinely
	// cross-site deployment must set "None", which browsers only honor
	// together with Secure.
	CookieSameSite string
	// Google OAuth (Sign in with Google). The whole feature is optional: with
	// none of the four variables set the API starts normally and the two
	// /api/v1/auth/google routes report SERVICE_UNAVAILABLE. Setting any one
	// of them declares intent to enable it, and startup then requires all
	// four — a half-configured OAuth deployment fails at boot rather than
	// failing per-request in front of a user.
	//
	// GoogleClientSecret is never logged, never returned by any handler, and
	// never reaches the browser: the authorization-code exchange happens
	// server-side only.
	GoogleClientID     string
	GoogleClientSecret string
	// GoogleRedirectURL is the absolute URL Google redirects back to. It must
	// exactly equal the callback route this API registers
	// (/api/v1/auth/google/callback) and must be registered verbatim in the
	// Google Cloud console.
	GoogleRedirectURL string
	// FrontendURL is the browser application's origin. It is the only
	// destination the OAuth callback will ever redirect to; every post-login
	// path is resolved relative to it, which is what makes an open redirect
	// structurally impossible (see identity/handler.SafeRedirectTarget).
	FrontendURL string
	// MediaStorageDriver selects the internal/media.MediaStorage
	// implementation. Only "local" exists today; the field exists so adding a
	// second driver (S3, Cloudinary, R2) is a config value, not a rebuild —
	// see internal/media's own package doc for why the interface is shaped
	// the way it is.
	MediaStorageDriver string
	// MediaLocalDir is the "local" driver's root directory, relative to the
	// working directory unless given absolute. Never served directly by a
	// third party — internal/app mounts it behind the API's own static route.
	MediaLocalDir string
	// MediaPublicBaseURL is the externally-reachable origin (+ optional path
	// prefix) a stored key is appended to, with no trailing slash — e.g.
	// "https://api.example.com/media". It has a same-origin, best-effort
	// default in development (built from Host/Port); production must set it
	// explicitly, because "the origin this process happens to bind to"
	// (127.0.0.1 by default — see Host) is never a real browser-reachable
	// address in production the way it can be for a local dev server.
	MediaPublicBaseURL string
}

// GoogleOAuthEnabled reports whether Sign in with Google is configured. Load
// guarantees this is all-or-nothing, so checking one field is sufficient.
func (c Config) GoogleOAuthEnabled() bool { return c.GoogleClientID != "" }

func Load() (Config, error) { return load(os.LookupEnv) }

func load(lookup func(string) (string, bool)) (Config, error) {
	c := Config{
		Env:             get(lookup, "APP_ENV", "development"),
		Host:            get(lookup, "HOST", "127.0.0.1"),
		MigrationsDir:   get(lookup, "MIGRATIONS_DIR", "migrations"),
		PostgresSSLMode: get(lookup, "POSTGRES_SSLMODE", "disable"),
	}
	var err error
	if c.Port, err = integer(lookup, "PORT", 8080); err != nil {
		return Config{}, err
	}
	if c.PostgresPort, err = requiredInteger(lookup, "POSTGRES_PORT"); err != nil {
		return Config{}, err
	}
	for key, target := range map[string]*string{"POSTGRES_HOST": &c.PostgresHost, "POSTGRES_USER": &c.PostgresUser, "POSTGRES_PASSWORD": &c.PostgresPassword, "POSTGRES_DB": &c.PostgresDB} {
		*target = get(lookup, key, "")
		if *target == "" {
			return Config{}, fmt.Errorf("%s is required", key)
		}
	}
	if c.AccessLifetime, err = duration(lookup, "ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if c.SessionLifetime, err = duration(lookup, "SESSION_TTL", 24*time.Hour); err != nil {
		return Config{}, err
	}
	c.AllowedOrigins = originList(lookup, "ALLOWED_ORIGINS")
	isProduction := c.Env == "production" || c.Env == "prod"
	if c.CookieSecure, err = boolean(lookup, "COOKIE_SECURE", isProduction); err != nil {
		return Config{}, err
	}
	c.CookieSameSite = get(lookup, "COOKIE_SAMESITE", "Lax")
	switch c.CookieSameSite {
	case "Lax", "Strict", "None":
	default:
		return Config{}, fmt.Errorf("COOKIE_SAMESITE must be Lax, Strict, or None")
	}
	// Browsers reject SameSite=None without Secure, which would silently drop
	// the refresh cookie and log every user out on reload. Fail at startup
	// instead of shipping a session that cannot persist.
	if c.CookieSameSite == "None" && !c.CookieSecure {
		return Config{}, fmt.Errorf("COOKIE_SAMESITE=None requires COOKIE_SECURE=true")
	}
	if err := c.loadGoogleOAuth(lookup); err != nil {
		return Config{}, err
	}
	if err := c.loadMedia(lookup, isProduction); err != nil {
		return Config{}, err
	}
	privateValue := get(lookup, "ED25519_PRIVATE_KEY", "")
	publicValue := get(lookup, "ED25519_PUBLIC_KEY", "")
	privateSet, publicSet := privateValue != "", publicValue != ""
	if !privateSet && !publicSet && c.Env != "production" && c.Env != "prod" {
		c.PublicKey, c.PrivateKey, _ = ed25519.GenerateKey(nil)
		return c, nil
	}
	if !privateSet || !publicSet {
		return Config{}, fmt.Errorf("ED25519_PRIVATE_KEY and ED25519_PUBLIC_KEY are required")
	}
	if c.PrivateKey, err = decodeKey(privateValue, ed25519.PrivateKeySize); err != nil {
		return Config{}, fmt.Errorf("ED25519_PRIVATE_KEY: %w", err)
	}
	if c.PublicKey, err = decodeKey(publicValue, ed25519.PublicKeySize); err != nil {
		return Config{}, fmt.Errorf("ED25519_PUBLIC_KEY: %w", err)
	}
	return c, nil
}

func get(lookup func(string) (string, bool), key, fallback string) string {
	if value, ok := lookup(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func integer(lookup func(string) (string, bool), key string, fallback int) (int, error) {
	value := get(lookup, key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func requiredInteger(lookup func(string) (string, bool), key string) (int, error) {
	if get(lookup, key, "") == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	return integer(lookup, key, 0)
}

func boolean(lookup func(string) (string, bool), key string, fallback bool) (bool, error) {
	raw := get(lookup, key, "")
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return parsed, nil
}

func duration(lookup func(string) (string, bool), key string, fallback time.Duration) (time.Duration, error) {
	value := get(lookup, key, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}

func originList(lookup func(string) (string, bool), key string) []string {
	raw := get(lookup, key, "")
	if raw == "" {
		return nil
	}
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

func decodeKey(value string, size int) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil || len(decoded) != size {
		return nil, fmt.Errorf("must be base64 encoded and %d bytes", size)
	}
	return decoded, nil
}

// loadGoogleOAuth reads the optional Sign in with Google settings.
//
// The feature is all-or-nothing on purpose. A deployment that sets, say,
// GOOGLE_CLIENT_ID and GOOGLE_REDIRECT_URL but forgets GOOGLE_CLIENT_SECRET
// would otherwise boot happily and then fail the code exchange after the user
// has already authenticated at Google — a confusing dead end that is far
// harder to diagnose than a refused startup.
func (c *Config) loadGoogleOAuth(lookup func(string) (string, bool)) error {
	c.GoogleClientID = get(lookup, "GOOGLE_CLIENT_ID", "")
	c.GoogleClientSecret = get(lookup, "GOOGLE_CLIENT_SECRET", "")
	c.GoogleRedirectURL = get(lookup, "GOOGLE_REDIRECT_URL", "")
	c.FrontendURL = strings.TrimRight(get(lookup, "FRONTEND_URL", ""), "/")

	present := 0
	for _, value := range []string{c.GoogleClientID, c.GoogleClientSecret, c.GoogleRedirectURL, c.FrontendURL} {
		if value != "" {
			present++
		}
	}
	if present == 0 {
		return nil
	}
	for key, value := range map[string]string{
		"GOOGLE_CLIENT_ID":     c.GoogleClientID,
		"GOOGLE_CLIENT_SECRET": c.GoogleClientSecret,
		"GOOGLE_REDIRECT_URL":  c.GoogleRedirectURL,
		"FRONTEND_URL":         c.FrontendURL,
	} {
		if value == "" {
			return fmt.Errorf("%s is required when Google OAuth is configured", key)
		}
	}
	if err := requireAbsoluteURL("GOOGLE_REDIRECT_URL", c.GoogleRedirectURL); err != nil {
		return err
	}
	if err := requireAbsoluteURL("FRONTEND_URL", c.FrontendURL); err != nil {
		return err
	}
	// Google will not redirect to a path other than the one registered in the
	// console, but a mismatch between this value and the route this binary
	// actually serves produces a redirect_uri_mismatch that is only visible
	// on Google's own error page. Catching it here names the real problem.
	if !strings.HasSuffix(c.GoogleRedirectURL, GoogleCallbackPath) {
		return fmt.Errorf("GOOGLE_REDIRECT_URL must end with %s", GoogleCallbackPath)
	}
	// Production is HTTPS end to end. Allowing http:// there would put the
	// authorization code, and then the session cookie, on a plaintext hop.
	if c.Env == "production" || c.Env == "prod" {
		for key, value := range map[string]string{"GOOGLE_REDIRECT_URL": c.GoogleRedirectURL, "FRONTEND_URL": c.FrontendURL} {
			if !strings.HasPrefix(value, "https://") {
				return fmt.Errorf("%s must use https in production", key)
			}
		}
	}
	return nil
}

// loadMedia reads the service-image storage settings. Unlike Google OAuth,
// this feature is never optional — service images are a standing part of the
// scheduling module — so every field always has a value: sensible defaults
// for local development, with production required to state its own public
// origin explicitly rather than inherit a meaningless one.
func (c *Config) loadMedia(lookup func(string) (string, bool), isProduction bool) error {
	c.MediaStorageDriver = get(lookup, "MEDIA_STORAGE_DRIVER", "local")
	if c.MediaStorageDriver != "local" {
		return fmt.Errorf("MEDIA_STORAGE_DRIVER %q is not supported (only \"local\" exists today)", c.MediaStorageDriver)
	}
	c.MediaLocalDir = get(lookup, "MEDIA_LOCAL_DIR", "uploads")

	defaultPublicBaseURL := ""
	if !isProduction {
		// Reads c.Host/c.Port, not the raw env again: both are already parsed
		// onto c by the time load() reaches this call, and re-deriving them
		// here would be a second, driftable copy of PORT's own fallback logic.
		defaultPublicBaseURL = fmt.Sprintf("http://%s:%d/media", c.Host, c.Port)
	}
	c.MediaPublicBaseURL = strings.TrimRight(get(lookup, "MEDIA_PUBLIC_BASE_URL", defaultPublicBaseURL), "/")
	if c.MediaPublicBaseURL == "" {
		return fmt.Errorf("MEDIA_PUBLIC_BASE_URL is required in production")
	}
	return requireAbsoluteURL("MEDIA_PUBLIC_BASE_URL", c.MediaPublicBaseURL)
}

// GoogleCallbackPath is the path segment of the OAuth callback route. It is
// declared here rather than in the router so configuration validation and
// route registration can never drift apart; internal/app asserts the route it
// registers against it.
const GoogleCallbackPath = "/api/v1/auth/google/callback"

func requireAbsoluteURL(key, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute http(s) URL", key)
	}
	return nil
}
