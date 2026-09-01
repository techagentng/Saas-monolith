package model

import "time"

// Provider names an external identity provider. It is a closed set mirrored
// by the user_identities_provider_valid CHECK constraint in migration 000014,
// so a value that reaches the database is always one this code knows.
type Provider string

const ProviderGoogle Provider = "GOOGLE"

// UserIdentity links a local platform user to one account at an external
// provider.
//
// ProviderSubject is the provider's stable subject identifier (Google's
// `sub`). ProviderEmail is recorded for support and audit only and is never
// used to resolve an identity — see the comment on migration 000014 for why
// a mutable email must not be the identity key.
//
// Nothing here carries authorization: an identity says who signed in, never
// what they may do. Roles remain tenant-scoped memberships.
type UserIdentity struct {
	ID              string
	UserID          string
	Provider        Provider
	ProviderSubject string
	ProviderEmail   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// GoogleIdentity is the verified subset of a Google ID token this application
// consumes. It is produced only by a verifier that has already checked the
// token's signature, issuer, audience and expiry — an unverified token never
// takes this shape.
//
// Deliberately minimal: no name, no picture, no access or refresh token. This
// feature is authentication only, so nothing that would let the application
// call a Google API is captured or stored.
type GoogleIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
}
