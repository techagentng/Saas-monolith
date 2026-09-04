package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// ServiceCategoryUpdate carries a partial update. Only non-nil fields are
// written; omitted fields are left alone — the same nullable-pointer diffing
// ServiceUpdate already uses.
//
// The absent fields are the enforcement: there is no Status field (archiving
// owns lifecycle and is its own operation, mirroring ServiceUpdate/StaffUpdate),
// no TenantID (a category never moves between tenants), and no ID or
// timestamps.
type ServiceCategoryUpdate struct {
	Name      *string
	SortOrder *int
}

// IsEmpty reports whether no field is set for update.
func (u *ServiceCategoryUpdate) IsEmpty() bool {
	return u.Name == nil && u.SortOrder == nil
}

// ServiceCategoryListFilter narrows a tenant's category listing. Status nil
// means every status; a concrete value restricts the listing to it — the same
// vocabulary ServiceListFilter and StaffListFilter already use.
type ServiceCategoryListFilter struct {
	Status *model.Status
}

// ServiceCategoryRepository is the persistence boundary for a tenant's service
// categories.
//
// Every method takes tenantID, and every implementation must filter on it —
// the same isolation mechanism ServiceRepository and StaffRepository use. A
// category belonging to another tenant is reported exactly as a nonexistent
// one, so the API cannot be used to discover which category IDs exist
// elsewhere on the platform.
//
// There is deliberately no Delete method: SC1's contract is archive, never a
// destructive delete (see model.ServiceCategory's own doc comment on the
// status field) — matching services and staff profiles, neither of which
// exposes one either.
type ServiceCategoryRepository interface {
	Create(ctx context.Context, category *model.ServiceCategory) (*model.ServiceCategory, error)
	// FindByID resolves a category within one tenant. A row belonging to
	// another tenant yields the identical not-found error as a missing one.
	FindByID(ctx context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error)
	// ListByTenant returns the tenant's categories in a deterministic order,
	// narrowed by filter.
	ListByTenant(ctx context.Context, tenantID string, filter ServiceCategoryListFilter) ([]*model.ServiceCategory, error)
	// Update writes the non-nil fields of update. It never touches status,
	// tenant_id, or the identity columns.
	Update(ctx context.Context, tenantID string, categoryID string, update ServiceCategoryUpdate) (*model.ServiceCategory, error)
	// Archive sets status to ARCHIVED unconditionally at the SQL level. It has
	// no awareness of whether the category was already archived — that is the
	// service layer's decision (CategoryService.Archive), mirroring
	// CatalogService.Archive.
	Archive(ctx context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error)
}
