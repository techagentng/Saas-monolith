package config

import (
	"strings"
	"testing"
)

func baseEnvironment() map[string]string {
	return map[string]string{
		"POSTGRES_HOST":     "db",
		"POSTGRES_PORT":     "5432",
		"POSTGRES_USER":     "user",
		"POSTGRES_PASSWORD": "password",
		"POSTGRES_DB":       "booking",
	}
}

func googleEnvironment() map[string]string {
	values := baseEnvironment()
	values["GOOGLE_CLIENT_ID"] = "client-id.apps.googleusercontent.com"
	values["GOOGLE_CLIENT_SECRET"] = "top-secret"
	values["GOOGLE_REDIRECT_URL"] = "http://localhost:8090" + GoogleCallbackPath
	values["FRONTEND_URL"] = "http://localhost:3000"
	return values
}

func loadFrom(values map[string]string) (Config, error) {
	return load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
}

// Google sign-in is optional. A deployment that has never heard of it must
// still start, with the feature simply switched off.
func TestGoogleOAuthIsOptional(t *testing.T) {
	cfg, err := loadFrom(baseEnvironment())
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if cfg.GoogleOAuthEnabled() {
		t.Fatal("Google OAuth reported enabled with no credentials configured")
	}
}

func TestGoogleOAuthLoadsWhenFullyConfigured(t *testing.T) {
	cfg, err := loadFrom(googleEnvironment())
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if !cfg.GoogleOAuthEnabled() {
		t.Fatal("Google OAuth reported disabled despite a complete configuration")
	}
	if cfg.GoogleClientSecret != "top-secret" || cfg.GoogleRedirectURL == "" || cfg.FrontendURL != "http://localhost:3000" {
		t.Fatalf("config = %+v", cfg)
	}
}

// A trailing slash on FRONTEND_URL would otherwise produce "//dashboard",
// which browsers read as protocol-relative — an open redirect created by a
// typo in an environment variable.
func TestGoogleOAuthTrimsTrailingSlashFromFrontendURL(t *testing.T) {
	values := googleEnvironment()
	values["FRONTEND_URL"] = "https://app.example.com/"
	values["GOOGLE_REDIRECT_URL"] = "https://api.example.com" + GoogleCallbackPath

	cfg, err := loadFrom(values)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if cfg.FrontendURL != "https://app.example.com" {
		t.Fatalf("FrontendURL = %q", cfg.FrontendURL)
	}
}

// Half a configuration is worse than none: the deployment would boot, offer a
// Google button, and only fail after the user had already authenticated.
func TestGoogleOAuthRefusesPartialConfiguration(t *testing.T) {
	for _, missing := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_REDIRECT_URL", "FRONTEND_URL"} {
		t.Run("missing "+missing, func(t *testing.T) {
			values := googleEnvironment()
			delete(values, missing)

			_, err := loadFrom(values)
			if err == nil {
				t.Fatalf("load() accepted a configuration missing %s", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Fatalf("error = %v, want it to name %s", err, missing)
			}
		})
	}
}

func TestGoogleOAuthRejectsMalformedURLs(t *testing.T) {
	for name, mutate := range map[string]func(map[string]string){
		"relative redirect":         func(v map[string]string) { v["GOOGLE_REDIRECT_URL"] = GoogleCallbackPath },
		"frontend without a scheme": func(v map[string]string) { v["FRONTEND_URL"] = "app.example.com" },
		"redirect pointing at the wrong path": func(v map[string]string) {
			v["GOOGLE_REDIRECT_URL"] = "http://localhost:8090/oauth/callback"
		},
	} {
		t.Run(name, func(t *testing.T) {
			values := googleEnvironment()
			mutate(values)
			if _, err := loadFrom(values); err == nil {
				t.Fatal("load() accepted a malformed Google OAuth URL")
			}
		})
	}
}

// Production is HTTPS end to end. Plain HTTP would put the authorization code,
// and then the session cookie, on a plaintext hop.
func TestGoogleOAuthRequiresHTTPSInProduction(t *testing.T) {
	values := googleEnvironment()
	values["APP_ENV"] = "production"
	values["ED25519_PRIVATE_KEY"] = ""
	values["COOKIE_SECURE"] = "true"

	if _, err := loadFrom(values); err == nil {
		t.Fatal("load() accepted plain HTTP Google OAuth URLs in production")
	}
}

// The callback route the router serves and the redirect URI configuration
// validates come from the same constant, so they cannot drift apart into a
// redirect_uri_mismatch that is only visible on Google's error page.
func TestGoogleCallbackPathIsTheDocumentedRoute(t *testing.T) {
	if GoogleCallbackPath != "/api/v1/auth/google/callback" {
		t.Fatalf("GoogleCallbackPath = %q", GoogleCallbackPath)
	}
}
