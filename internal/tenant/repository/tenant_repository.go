package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

type TenantRepository interface {
	FindByID(ctx context.Context, id string) (*model.Tenant, error)
}
