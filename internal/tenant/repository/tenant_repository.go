package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

type TenantRepository interface {
	Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error)
	FindByID(ctx context.Context, id string) (*model.Tenant, error)
}
