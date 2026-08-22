package repository

import (
	"context"
	"time"

	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

type MembershipRepository interface {
	Create(ctx context.Context, membership model.TenantMembership) (*model.TenantMembership, error)
	FindByTenantAndUser(ctx context.Context, tenantID, userID string) (*model.TenantMembership, error)
	ListByUser(ctx context.Context, userID string) ([]model.TenantMembership, error)
	Disable(ctx context.Context, tenantID, userID string, now time.Time) error
}
