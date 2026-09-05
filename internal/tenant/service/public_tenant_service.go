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
//
// BusinessType (Vertical Onboarding F3) is the one deliberate addition beyond
// Feature 5's original four fields — needed so a future public vertical
// router can pick the right customer experience for this tenant. It is
// nullable only for a tenant created before Feature 1 existed (see
// model.Tenant's own doc comment); F3 does not invent a value for it.
// OnboardingStatus/OnboardingStep are workflow internals, not identity, and
// are deliberately never added here.
type PublicTenantIdentity struct {
	Slug         string
	Name         string
	Description  *string
	Timezone     *string
	BusinessType *model.BusinessType
}

// PublicTenantContext is the internal result of resolving a public slug once
// the visibility gate (reserved / canonical / ACTIVE + COMPLETED) has passed.
//
// It carries what a downstream public feature — the S8 service catalog, the S9
// availability API — needs but PublicTenantIdentity deliberately withholds: the
// internal tenant UUID, so the feature can scope its own tenant-isolated
// queries, and the currency prices are denominated in (a tenant-level property
// since Scheduling S1). It is never serialized; a handler projects only the
// parts it is allowed to expose, exactly as GetBySlug does for the identity.
type PublicTenantContext struct {
	TenantID     string
	BusinessType *model.BusinessType
	Currency     *string
	Identity     PublicTenantIdentity
}

// PublicTenantService resolves the public identity of a tenant from its slug.
type PublicTenantService interface {
	// GetBySlug returns the public identity for a canonical slug. Tenants that
	// are not publicly visible are reported as not found.
	GetBySlug(ctx context.Context, slug string) (*PublicTenantIdentity, error)
	// ResolvePublicTenant applies the identical visibility gate GetBySlug uses
	// and returns the internal context a downstream public feature needs. It is
	// the single place that owns "is this slug publicly resolvable" — callers
	// must consume this rather than re-implementing the gate against the tenant
	// repository.
	ResolvePublicTenant(ctx context.Context, slug string) (*PublicTenantContext, error)
}

type publicTenantService struct {
	tenants repository.TenantRepository
}

func NewPublicTenantService(tenants repository.TenantRepository) PublicTenantService {
	return &publicTenantService{tenants: tenants}
}

func (s *publicTenantService) GetBySlug(ctx context.Context, slug string) (*PublicTenantIdentity, error) {
	resolved, err := s.ResolvePublicTenant(ctx, slug)
	if err != nil {
		return nil, err
	}
	identity := resolved.Identity
	return &identity, nil
}

func (s *publicTenantService) ResolvePublicTenant(ctx context.Context, slug string) (*PublicTenantContext, error) {
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

	// Public visibility (Vertical Onboarding F3): ACTIVE AND COMPLETED,
	// both required. Feature 5's original ACTIVE-only check is necessary but
	// no longer sufficient on its own — a freshly created tenant is ACTIVE
	// (not disabled/revoked) yet IN_PROGRESS (business setup unfinished),
	// and must be just as unreachable publicly as a disabled or nonexistent
	// one, or the completion gate Feature 2 enforces becomes pointless. Every
	// failing combination — ACTIVE+IN_PROGRESS, DISABLED+COMPLETED,
	// DISABLED+IN_PROGRESS — collapses to the same TENANT_NOT_FOUND,
	// preserving Feature 5's "disabled and nonexistent are indistinguishable"
	// privacy property for "still onboarding and nonexistent" too. Lifecycle
	// transitions themselves remain Feature 8; this does not add a new
	// lifecycle state, it adds a second, independent condition alongside the
	// existing one.
	if tenant.Status != model.StatusActive || tenant.OnboardingStatus != model.OnboardingStatusCompleted {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}

	return &PublicTenantContext{
		TenantID:     tenant.ID,
		BusinessType: tenant.BusinessType,
		Currency:     tenant.Currency,
		Identity: PublicTenantIdentity{
			Slug:         tenant.Slug,
			Name:         tenant.Name,
			Description:  tenant.Description,
			Timezone:     tenant.Timezone,
			BusinessType: tenant.BusinessType,
		},
	}, nil
}

// compile-time guard: the service must keep satisfying its interface.
var _ PublicTenantService = (*publicTenantService)(nil)
