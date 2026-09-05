package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// ServiceUpdate carries a partial update. Only non-nil fields are written;
// omitted fields are left alone — the same nullable-pointer diffing
// TenantProfileUpdate already uses.
//
// The absent fields are the enforcement: there is no Status field (archiving
// owns lifecycle, and it is its own operation), no TenantID (a service never
// moves between tenants), no ID, and no timestamps. A client cannot reach any
// of them because there is nothing here to carry them.
type ServiceUpdate struct {
	Name            *string
	Description     *string
	DurationMinutes *int
	PriceMinor      *int64
	// CategoryID is a pointer-to-pointer: nil means "leave the field alone",
	// a non-nil pointer to nil means "clear it to uncategorised", and a
	// non-nil pointer to a non-nil string means "file it under this category".
	// A plain *string could express "set" and "leave alone" but never
	// "clear" — the same reason ServiceUpdate as a whole is all pointers.
	CategoryID **string
}

// IsEmpty reports whether no field is set for update.
func (u *ServiceUpdate) IsEmpty() bool {
	return u.Name == nil && u.Description == nil && u.DurationMinutes == nil && u.PriceMinor == nil && u.CategoryID == nil
}

// ServiceListFilter narrows a catalog listing.
type ServiceListFilter struct {
	// Status nil means every status. A concrete value restricts the listing
	// to it.
	Status *model.Status
}

// ServiceRepository is the scheduling catalog's persistence boundary.
//
// Every method takes tenantID, and every implementation must filter on it.
// This is the tenant-isolation mechanism for the whole module: there is no
// method that can address a service without naming the tenant it must belong
// to, so a cross-tenant read or write is not something a caller can express —
// it is not merely something the service layer remembers to check.
//
// A service belonging to a different tenant is reported exactly as a
// nonexistent one, so the API cannot be used to discover which service IDs
// exist elsewhere on the platform.
type ServiceRepository interface {
	Create(ctx context.Context, service *model.Service) (*model.Service, error)
	// FindByID resolves a service within one tenant. A row belonging to
	// another tenant yields SERVICE_NOT_FOUND, identically to a missing one.
	FindByID(ctx context.Context, tenantID string, serviceID string) (*model.Service, error)
	// ListByTenant returns the tenant's catalog in a deterministic order,
	// narrowed by filter.
	ListByTenant(ctx context.Context, tenantID string, filter ServiceListFilter) ([]*model.Service, error)
	// Update writes the non-nil fields of update. It never touches status,
	// tenant_id, or the identity columns.
	Update(ctx context.Context, tenantID string, serviceID string, update ServiceUpdate) (*model.Service, error)
	// Archive sets status to ARCHIVED unconditionally at the SQL level. It has
	// no awareness of whether the service was already archived — that is the
	// service layer's decision (CatalogService.Archive), mirroring how
	// CompleteOnboarding and OnboardingService.Complete already divide that
	// responsibility.
	Archive(ctx context.Context, tenantID string, serviceID string) (*model.Service, error)
}
