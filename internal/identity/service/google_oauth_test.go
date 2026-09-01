package service

import (
	"net/url"
	"strings"
	"testing"
)

func testGoogleOAuthConfig() GoogleOAuthConfig {
	return GoogleOAuthConfig{
		ClientID:     "client-id.apps.googleusercontent.com",
		ClientSecret: "top-secret",
		RedirectURL:  "https://api.example.com/api/v1/auth/google/callback",
	}
}

func TestAuthorizationURLTargetsGoogleWithTheConfiguredClientAndState(t *testing.T) {
	client := NewGoogleAuthorizationClient(testGoogleOAuthConfig())

	raw := client.AuthorizationURL("state-value")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	if parsed.Host != "accounts.google.com" {
		t.Fatalf("host = %q, want accounts.google.com", parsed.Host)
	}
	query := parsed.Query()
	if query.Get("client_id") != "client-id.apps.googleusercontent.com" {
		t.Fatalf("client_id = %q", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != "https://api.example.com/api/v1/auth/google/callback" {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("state") != "state-value" {
		t.Fatalf("state = %q, want the value the caller bound to the browser", query.Get("state"))
	}
	if query.Get("response_type") != "code" {
		t.Fatalf("response_type = %q, want the authorization code flow", query.Get("response_type"))
	}
}

// The client secret is a server-only credential. It must never appear in
// anything the browser is handed, and the authorization URL is the one place
// where a careless implementation would leak it.
func TestAuthorizationURLNeverCarriesTheClientSecret(t *testing.T) {
	raw := NewGoogleAuthorizationClient(testGoogleOAuthConfig()).AuthorizationURL("state-value")

	if strings.Contains(raw, "top-secret") || strings.Contains(raw, "client_secret") {
		t.Fatalf("authorization URL leaked the client secret: %s", raw)
	}
}

// Authentication only. A scope beyond these three would request access to a
// user's Google data this application has no business holding, and consent
// screens are where over-broad scopes become permanent.
func TestAuthorizationURLRequestsOnlyIdentityScopes(t *testing.T) {
	raw := NewGoogleAuthorizationClient(testGoogleOAuthConfig()).AuthorizationURL("state-value")
	parsed, _ := url.Parse(raw)

	scopes := strings.Fields(parsed.Query().Get("scope"))
	if len(scopes) != 3 {
		t.Fatalf("scopes = %v, want exactly openid, email and profile", scopes)
	}
	allowed := map[string]bool{"openid": true, "email": true, "profile": true}
	for _, scope := range scopes {
		if !allowed[scope] {
			t.Fatalf("scope %q is not an identity scope", scope)
		}
	}
	for _, forbidden := range []string{"gmail", "drive", "calendar", "contacts"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("authorization URL requests %s access", forbidden)
		}
	}
}

// No offline access: a Google refresh token is a long-lived credential this
// application would then have to store and protect for no benefit, because it
// never calls a Google API.
func TestAuthorizationURLDoesNotRequestOfflineAccess(t *testing.T) {
	raw := NewGoogleAuthorizationClient(testGoogleOAuthConfig()).AuthorizationURL("state-value")
	parsed, _ := url.Parse(raw)

	if parsed.Query().Get("access_type") == "offline" {
		t.Fatal("authorization URL requested offline access")
	}
}
