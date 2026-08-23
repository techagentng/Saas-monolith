package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

type TenantRepository interface {
	Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error)
	FindByID(ctx context.Context, id string) (*model.Tenant, error)
	// ListAccessibleByUserID returns all ACTIVE tenants where the user has an ACTIVE membership,
	// ordered deterministically. Uses a single bounded query to avoid N+1.
	ListAccessibleByUserID(ctx context.Context, userID string) ([]*model.Tenant, error)
}
