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
	// ListAccessibleByUserID returns all ACTIVE tenants where the user has an ACTIVE membership,
	// ordered deterministically. Uses a single bounded query to avoid N+1.
	ListAccessibleByUserID(ctx context.Context, userID string) ([]*model.Tenant, error)
	// UpdateProfile updates the profile fields of a tenant.
	UpdateProfile(ctx context.Context, tenantID string, update TenantProfileUpdate) (*model.Tenant, error)
}
