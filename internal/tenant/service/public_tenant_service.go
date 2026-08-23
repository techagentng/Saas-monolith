package service

import (
	"context"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// PublicTenantIdentity is the entire unauthenticated view of a tenant.
//
// It deliberately omits the internal tenant UUID, lifecycle status, timestamps,
// membership/role data, and the private business contact details captured in
// Feature 4. Anything added to this struct becomes readable by anonymous
// callers, so additions are a security decision rather than a convenience one.
type PublicTenantIdentity struct {
	Slug        string
	Name        string
	Description *string
	Timezone    *string
}

// PublicTenantService resolves the public identity of a tenant from its slug.
type PublicTenantService interface {
	// GetBySlug returns the public identity for a canonical slug. Tenants that
	// are not publicly visible are reported as not found.
	GetBySlug(ctx context.Context, slug string) (*PublicTenantIdentity, error)
}

type publicTenantService struct {
	tenants repository.TenantRepository
}

func NewPublicTenantService(tenants repository.TenantRepository) PublicTenantService {
	return &publicTenantService{tenants: tenants}
}

func (s *publicTenantService) GetBySlug(ctx context.Context, slug string) (*PublicTenantIdentity, error) {
	// A reserved slug belongs to the platform and can never name a tenant.
	// It is reported as simply absent so the public surface does not disclose
	// which names the platform holds back.
	if model.IsReservedSlug(slug) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	// A non-canonical slug cannot match any stored value, so it is refused
	// before a query is issued rather than being normalized into one.
	if err := model.ValidateSlug(slug); err != nil {
		return nil, err
	}

	tenant, err := s.tenants.FindBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Public visibility is an ACTIVE-only policy. A disabled tenant is
	// indistinguishable from an unregistered slug, so lifecycle state is never
	// disclosed publicly. Lifecycle transitions themselves remain Feature 8.
	if tenant.Status != model.StatusActive {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}

	return &PublicTenantIdentity{
		Slug:        tenant.Slug,
		Name:        tenant.Name,
		Description: tenant.Description,
		Timezone:    tenant.Timezone,
	}, nil
}

// compile-time guard: the service must keep satisfying its interface.
var _ PublicTenantService = (*publicTenantService)(nil)
