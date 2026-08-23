package model

import (
	"errors"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

func TestValidateSlugAcceptsCanonicalSlugs(t *testing.T) {
	for _, slug := range []string{
		"acme",
		"acme-salon",
		"a1b",
		"salon-2024",
		"a-b-c-d",
		strings.Repeat("a", SlugMinLength),
		strings.Repeat("a", SlugMaxLength),
	} {
		if err := ValidateSlug(slug); err != nil {
			t.Fatalf("ValidateSlug(%q) = %v, want nil", slug, err)
		}
	}
}

// Feature 5 rejects non-canonical input rather than silently rewriting it, so
// the stored slug is always exactly what the client sent.
func TestValidateSlugRejectsNonCanonicalInput(t *testing.T) {
	tests := []struct {
		name, slug string
	}{
		{"uppercase", "Acme"},
		{"mixed case", "Acme-Salon"},
		{"all caps", "ACME"},
		{"single space", "acme salon"},
		{"leading space", " acme"},
		{"trailing space", "acme "},
		{"underscore", "acme_salon"},
		{"dot", "acme.salon"},
		{"slash", "acme/salon"},
		{"at sign", "acme@salon"},
		{"percent", "acme%20salon"},
		{"leading hyphen", "-acme"},
		{"trailing hyphen", "acme-"},
		{"empty", ""},
		{"only whitespace", "   "},
		{"only hyphen", "-"},
		{"non-ascii letter", "acmé"},
		{"cyrillic", "асме"},
		{"emoji", "acme-🎉"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateSlug(test.slug)
			assertSlugInvalid(t, err)
		})
	}
}

func TestValidateSlugEnforcesLengthBounds(t *testing.T) {
	tooShort := strings.Repeat("a", SlugMinLength-1)
	if err := ValidateSlug(tooShort); err == nil {
		t.Fatalf("ValidateSlug(%q) = nil, want length rejection", tooShort)
	} else {
		assertSlugInvalid(t, err)
	}

	tooLong := strings.Repeat("a", SlugMaxLength+1)
	if err := ValidateSlug(tooLong); err == nil {
		t.Fatalf("ValidateSlug(len %d) = nil, want length rejection", len(tooLong))
	} else {
		assertSlugInvalid(t, err)
	}
}

// Reserved slugs would collide with platform routes, so they are refused even
// though they are syntactically canonical.
func TestValidateSlugRejectsReservedSlugs(t *testing.T) {
	for _, reserved := range ReservedSlugs() {
		t.Run(reserved, func(t *testing.T) {
			if err := ValidateSlug(reserved); err == nil {
				t.Fatalf("ValidateSlug(%q) = nil, want reserved rejection", reserved)
			} else {
				assertSlugInvalid(t, err)
			}
		})
	}
}

// The reserved set must actually cover the platform's own top-level routes.
func TestReservedSlugsCoverPlatformRoutes(t *testing.T) {
	required := []string{"admin", "api", "login", "dashboard", "auth", "book", "settings"}
	reserved := make(map[string]bool)
	for _, slug := range ReservedSlugs() {
		reserved[slug] = true
	}
	for _, route := range required {
		if !reserved[route] {
			t.Fatalf("platform route %q is not reserved; a tenant could claim it", route)
		}
	}
}

// Every reserved entry must itself be canonical, otherwise it could never be
// produced by a request and the reservation would be dead weight.
func TestReservedSlugsAreThemselvesCanonical(t *testing.T) {
	for _, reserved := range ReservedSlugs() {
		if reserved != strings.ToLower(reserved) || strings.TrimSpace(reserved) != reserved {
			t.Fatalf("reserved slug %q is not canonical", reserved)
		}
	}
}

// ReservedSlugs must hand out a copy; mutating the result must not weaken the
// platform-route protection for later callers.
func TestReservedSlugsCannotBeMutatedByCallers(t *testing.T) {
	first := ReservedSlugs()
	if len(first) == 0 {
		t.Fatal("ReservedSlugs() is empty")
	}
	first[0] = "hijacked"

	for _, slug := range ReservedSlugs() {
		if slug == "hijacked" {
			t.Fatal("ReservedSlugs() exposed its backing array to callers")
		}
	}
	if err := ValidateSlug("admin"); err == nil {
		t.Fatal("reserved protection weakened after caller mutated the returned slice")
	}
}

func assertSlugInvalid(t *testing.T, err error) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want *AppError", err)
	}
	if appErr.Code != apperrors.CodeTenantSlugInvalid {
		t.Fatalf("code = %q, want %q", appErr.Code, apperrors.CodeTenantSlugInvalid)
	}
}
