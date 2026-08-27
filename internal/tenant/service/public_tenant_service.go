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

	return &PublicTenantIdentity{
		Slug:         tenant.Slug,
		Name:         tenant.Name,
		Description:  tenant.Description,
		Timezone:     tenant.Timezone,
		BusinessType: tenant.BusinessType,
	}, nil
}

// compile-time guard: the service must keep satisfying its interface.
var _ PublicTenantService = (*publicTenantService)(nil)
