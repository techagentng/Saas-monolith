package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/identity/model"
)

// IdentityRepository persists links between local users and external identity
// providers. It is deliberately narrow: resolution is only ever by
// (provider, subject), because that is the only stable key an OIDC provider
// offers. There is intentionally no FindByProviderEmail — resolving a
// federated login by a mutable email is exactly the mistake this table exists
// to prevent.
type IdentityRepository interface {
	// FindByProviderSubject returns the identity for a provider account, or a
	// RESOURCE_NOT_FOUND application error when the provider account has never
	// signed in here before.
	FindByProviderSubject(ctx context.Context, provider model.Provider, subject string) (*model.UserIdentity, error)
	Create(ctx context.Context, identity model.UserIdentity) (*model.UserIdentity, error)
	// UpdateProviderEmail records the provider's currently asserted email. It
	// never touches users.email: the local account address is the user's to
	// change, not Google's.
	UpdateProviderEmail(ctx context.Context, id, providerEmail string) error
}
