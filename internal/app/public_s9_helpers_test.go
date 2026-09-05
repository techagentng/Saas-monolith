package app

import (
	"context"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// s9TenantRepo satisfies both tenantrepository.TenantRepository (for the real
// PublicTenantService) and schedulingservice.TenantReader (for the real S7
// engine) against a single tenant — exactly the two consumers the S9 public
// adapters compose. Its FindBySlug/FindByID both match that one tenant so a
// slug->id round trip through the production code is genuinely exercised.
type s9TenantRepo struct {
	tenant *tenantmodel.Tenant
	// findBySlugCalls / findByIDCalls let a test assert the engine was (or was
	// not) reached.
	findBySlugCalls int
	findByIDCalls   int
}

func (r *s9TenantRepo) FindBySlug(_ context.Context, slug string) (*tenantmodel.Tenant, error) {
	r.findBySlugCalls++
	if r.tenant == nil || r.tenant.Slug != slug {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return r.tenant, nil
}

func (r *s9TenantRepo) FindByID(_ context.Context, id string) (*tenantmodel.Tenant, error) {
	r.findByIDCalls++
	if r.tenant == nil || r.tenant.ID != id {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return r.tenant, nil
}

func (r *s9TenantRepo) Create(context.Context, *tenantmodel.Tenant) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *s9TenantRepo) ListAccessibleByUserID(context.Context, string) ([]*tenantmodel.Tenant, error) {
	return []*tenantmodel.Tenant{}, nil
}

func (r *s9TenantRepo) UpdateProfile(context.Context, string, tenantrepository.TenantProfileUpdate) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

var _ tenantrepository.TenantRepository = (*s9TenantRepo)(nil)
