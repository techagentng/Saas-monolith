package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

type TenantProfileUpdate struct {
	Name         *string
	Description  *string
	ContactEmail *string
	ContactPhone *string
	Timezone     *string
}

// IsEmpty returns true if no fields are set for update.
func (u *TenantProfileUpdate) IsEmpty() bool {
	return u.Name == nil && u.Description == nil && u.ContactEmail == nil && u.ContactPhone == nil && u.Timezone == nil
}

type TenantRepository interface {
	Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error)
	FindByID(ctx context.Context, id string) (*model.Tenant, error)
	// FindBySlug looks a tenant up by its exact canonical slug. It performs no
	// normalization or fuzzy matching: the caller must supply a canonical slug.
	FindBySlug(ctx context.Context, slug string) (*model.Tenant, error)
	// ListAccessibleByUserID returns all ACTIVE tenants where the user has an ACTIVE membership,
	// ordered deterministically. Uses a single bounded query to avoid N+1.
	ListAccessibleByUserID(ctx context.Context, userID string) ([]*model.Tenant, error)
	// UpdateProfile updates the profile fields of a tenant.
	UpdateProfile(ctx context.Context, tenantID string, update TenantProfileUpdate) (*model.Tenant, error)
}

// OnboardingRepository is deliberately separate from TenantRepository:
// onboarding-progress and completion persistence (Vertical Onboarding F2)
// is a distinct concern from tenant creation/retrieval/profile management,
// and keeping it on its own narrow interface means the many existing
// TenantRepository fakes across this package's tests never need to grow
// onboarding-shaped methods they don't use. PostgresTenantRepository
// implements both interfaces on the same underlying struct.
type OnboardingRepository interface {
	FindByID(ctx context.Context, id string) (*model.Tenant, error)
	// UpdateOnboardingStep persists only onboarding_step — it never touches
	// onboarding_status, business_type, or any other column. There is no
	// ordering/sequence enforcement at this layer; that is a service-layer
	// decision (currently: none — F2 allows saving any approved step value
	// in any order).
	UpdateOnboardingStep(ctx context.Context, tenantID string, step string) (*model.Tenant, error)
	// CompleteOnboarding transitions onboarding_status to COMPLETED
	// unconditionally at the SQL level. It has no awareness of completion
	// prerequisites or of whether the tenant is already COMPLETED — both are
	// the service layer's responsibility (OnboardingService.Complete).
	CompleteOnboarding(ctx context.Context, tenantID string) (*model.Tenant, error)
}

// CurrencyRepository is deliberately separate from TenantRepository for the
// same reason OnboardingRepository is: tenant currency (Scheduling S1) is a
// distinct concern from tenant creation/retrieval/profile management, and
// keeping it on its own narrow interface means the many existing
// TenantRepository fakes across these tests never need to grow a currency
// method they don't use. PostgresTenantRepository implements all three
// interfaces on the same underlying struct.
type CurrencyRepository interface {
	FindByID(ctx context.Context, id string) (*model.Tenant, error)
	// SetCurrency writes only the currency column, unconditionally at the SQL
	// level. It has no awareness of whether the tenant already has one — the
	// write-once rule belongs to the service layer (CurrencyService.Set).
	SetCurrency(ctx context.Context, tenantID string, currency string) (*model.Tenant, error)
}
