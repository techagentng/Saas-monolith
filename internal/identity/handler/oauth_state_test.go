package handler

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewOAuthStateIsUnpredictableAndCarriesItsOwnExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	first, err := NewOAuthState(10*time.Minute, now)
	if err != nil {
		t.Fatalf("NewOAuthState() error = %v", err)
	}
	second, err := NewOAuthState(10*time.Minute, now)
	if err != nil {
		t.Fatalf("NewOAuthState() error = %v", err)
	}
	if first == second {
		t.Fatal("two states were identical; the value is not random")
	}
	random, expiry, found := strings.Cut(first, ".")
	if !found {
		t.Fatalf("state = %q, want a random half and an expiry", first)
	}
	// 32 random bytes, base64url without padding.
	if len(random) < 43 {
		t.Fatalf("random half = %q (%d chars), want at least 256 bits of entropy", random, len(random))
	}
	if expiry != "1700000600" {
		t.Fatalf("expiry = %q, want the issuing instant plus the lifetime", expiry)
	}
}

func TestValidateOAuthStateAcceptsOnlyAMatchingUnexpiredValue(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	state, err := NewOAuthState(10*time.Minute, now)
	if err != nil {
		t.Fatalf("NewOAuthState() error = %v", err)
	}

	if err := ValidateOAuthState(state, state, now.Add(time.Minute)); err != nil {
		t.Fatalf("ValidateOAuthState() error = %v, want a matching state to be accepted", err)
	}
}

func TestValidateOAuthStateRejectsMissingMismatchedAndExpiredValues(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	state, _ := NewOAuthState(10*time.Minute, now)
	other, _ := NewOAuthState(10*time.Minute, now)

	tests := map[string]struct {
		cookie    string
		parameter string
		at        time.Time
	}{
		// No cookie at all: the callback did not come from a browser this API
		// started a flow for, which is exactly the CSRF case.
		"missing cookie":    {"", state, now},
		"missing parameter": {state, "", now},
		"mismatched":        {state, other, now},
		"expired":           {state, state, now.Add(11 * time.Minute)},
		// The boundary itself is expired, not valid: an off-by-one here would
		// silently widen the replay window.
		"exactly at expiry": {state, state, now.Add(10 * time.Minute)},
		"malformed":         {"no-separator", "no-separator", now},
		"unparseable expiry": {
			"random.not-a-number", "random.not-a-number", now,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateOAuthState(test.cookie, test.parameter, test.at); err == nil {
				t.Fatal("ValidateOAuthState() accepted a state it must reject")
			}
		})
	}
}

// Google's callback is a cross-site top-level navigation. A Strict cookie is
// withheld on precisely that request, so pinning the OAuth cookies to Strict
// would break every sign-in — Lax is the strictest workable policy.
func TestOAuthSameSiteDowngradesStrictButPreservesNone(t *testing.T) {
	for name, test := range map[string]struct {
		configured http.SameSite
		want       http.SameSite
	}{
		"strict becomes lax": {http.SameSiteStrictMode, http.SameSiteLaxMode},
		"lax stays lax":      {http.SameSiteLaxMode, http.SameSiteLaxMode},
		"none stays none":    {http.SameSiteNoneMode, http.SameSiteNoneMode},
	} {
		t.Run(name, func(t *testing.T) {
			if got := OAuthSameSite(test.configured); got != test.want {
				t.Fatalf("OAuthSameSite(%v) = %v, want %v", test.configured, got, test.want)
			}
		})
	}
}

func TestSafeRelativePathKeepsInternalRoutes(t *testing.T) {
	for name, test := range map[string]struct {
		candidate string
		want      string
	}{
		"plain path":      {"/dashboard/services", "/dashboard/services"},
		"path with query": {"/onboarding?step=2", "/onboarding?step=2"},
		"root":            {"/", "/"},
		"empty falls back to the normal post-login entry point": {"", DefaultPostLoginPath},
	} {
		t.Run(name, func(t *testing.T) {
			if got := SafeRelativePath(test.candidate); got != test.want {
				t.Fatalf("SafeRelativePath(%q) = %q, want %q", test.candidate, got, test.want)
			}
		})
	}
}

// Every entry here is a real open-redirect vector. A sign-in flow that honored
// any of them would hand an authenticated user straight to an attacker's page.
func TestSafeRelativePathRejectsOpenRedirects(t *testing.T) {
	for name, candidate := range map[string]string{
		"absolute https":     "https://evil.example/steal",
		"absolute http":      "http://evil.example/steal",
		"protocol relative":  "//evil.example/steal",
		"backslash relative": "/\\evil.example/steal",
		"scheme only":        "javascript:alert(1)",
		"data url":           "data:text/html,<script>alert(1)</script>",
		"no leading slash":   "evil.example",
		"embedded newline":   "/dashboard\nLocation: https://evil.example",
		"embedded return":    "/dashboard\r\nSet-Cookie: a=b",
	} {
		t.Run(name, func(t *testing.T) {
			if got := SafeRelativePath(candidate); got != DefaultPostLoginPath {
				t.Fatalf("SafeRelativePath(%q) = %q, want it refused in favor of %q", candidate, got, DefaultPostLoginPath)
			}
		})
	}
}

func TestFrontendRedirectResolvesAgainstTheConfiguredOriginOnly(t *testing.T) {
	if got := FrontendRedirect("https://app.example.com/", "/dashboard"); got != "https://app.example.com/dashboard" {
		t.Fatalf("FrontendRedirect() = %q", got)
	}
	// A hostile destination cannot escape the configured origin, because the
	// path is sanitized again on the way out.
	if got := FrontendRedirect("https://app.example.com", "https://evil.example"); got != "https://app.example.com"+DefaultPostLoginPath {
		t.Fatalf("FrontendRedirect() = %q, want the destination refused", got)
	}
}
