package model

import (
	"regexp"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// Slug length bounds. The upper bound follows the DNS label limit so a slug
// can later appear as a hostname label without a second, conflicting rule;
// it sits well inside the tenants.slug VARCHAR(255) column.
const (
	SlugMinLength = 3
	SlugMaxLength = 63
)

// canonicalSlugPattern is the single definition of a canonical slug:
// lowercase ASCII letters and digits separated by single-position hyphens,
// with no leading or trailing hyphen. Length is checked separately so that a
// length violation and a character violation stay distinguishable in code.
var canonicalSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedSlugs are the platform's own top-level route names. A tenant that
// claimed one of these would make the corresponding platform URL ambiguous,
// so they are refused even though they are syntactically canonical.
//
// This is the single source of truth for reservations; no other layer should
// keep its own copy.
var reservedSlugs = []string{
	"admin",
	"api",
	"auth",
	"book",
	"dashboard",
	"health",
	"login",
	"logout",
	"public",
	"settings",
	"signup",
	"static",
	"support",
	"tenants",
	"users",
	"www",
}

var reservedSlugSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(reservedSlugs))
	for _, slug := range reservedSlugs {
		set[slug] = struct{}{}
	}
	return set
}()

// ReservedSlugs returns a copy of the reserved slug list. Callers get a copy
// so that reservation protection cannot be weakened from outside this package.
func ReservedSlugs() []string {
	out := make([]string, len(reservedSlugs))
	copy(out, reservedSlugs)
	return out
}

// IsReservedSlug reports whether a slug is reserved for platform use.
func IsReservedSlug(slug string) bool {
	_, reserved := reservedSlugSet[slug]
	return reserved
}

// ValidateSlug enforces the canonical slug contract.
//
// Feature 5 deliberately validates rather than normalizes: a slug that is not
// already canonical is rejected, so the persisted value is always exactly what
// the caller supplied. That keeps the public identity predictable and avoids
// two different inputs silently collapsing onto one stored slug.
//
// Callers must not trim or case-fold before calling; doing so would reintroduce
// normalization at the edges and defeat the contract.
func ValidateSlug(slug string) error {
	if len(slug) < SlugMinLength || len(slug) > SlugMaxLength {
		return apperrors.New(apperrors.CodeTenantSlugInvalid, "tenant slug is not valid", nil)
	}
	if !canonicalSlugPattern.MatchString(slug) {
		return apperrors.New(apperrors.CodeTenantSlugInvalid, "tenant slug is not valid", nil)
	}
	if IsReservedSlug(slug) {
		return apperrors.New(apperrors.CodeTenantSlugInvalid, "tenant slug is not valid", nil)
	}
	return nil
}
