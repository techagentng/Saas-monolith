package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// SaveOnboardingProgressInput carries the transport-validated request to
// PATCH /tenants/{id}/onboarding.
type SaveOnboardingProgressInput struct {
	CurrentStep string
}

// OnboardingService owns onboarding workflow rules — step validation,
// completion prerequisites, and the save-vs-complete distinction — so
// neither the handler (transport) nor the repository (persistence) has to.
// Tenant access itself (membership, tenant.update) is verified by the
// production middleware chain before either method here is ever reached;
// this service does not re-derive or re-check authorization.
type OnboardingService interface {
	// SaveProgress records the caller's current onboarding step. It does not
	// enforce step ordering — F2 is not a workflow engine, any approved step
	// may be saved in any order — and it never changes onboarding_status:
	// calling it on an already-COMPLETED tenant updates the step and leaves
	// the tenant COMPLETED, it does not revert it to IN_PROGRESS.
	SaveProgress(ctx context.Context, tenantID string, input SaveOnboardingProgressInput) (*model.Tenant, error)
	// Complete transitions a tenant from IN_PROGRESS to COMPLETED, but only
	// after validateOnboardingCompletionPrerequisites succeeds. Calling it on
	// an already-COMPLETED tenant is idempotent — it returns the tenant
	// unchanged without re-validating or writing anything, so it never
	// resets onboarding_step as a side effect of a repeated call.
	Complete(ctx context.Context, tenantID string) (*model.Tenant, error)
}

type onboardingService struct {
	tenants repository.OnboardingRepository
}

func NewOnboardingService(tenants repository.OnboardingRepository) OnboardingService {
	return &onboardingService{tenants: tenants}
}

func (s *onboardingService) SaveProgress(ctx context.Context, tenantID string, input SaveOnboardingProgressInput) (*model.Tenant, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	// Not trimmed before validation, matching ValidateSlug/ValidateBusinessType's
	// own reject-over-normalize philosophy: a padded or malformed value is
	// refused outright rather than silently corrected.
	if err := model.ValidateOnboardingStep(input.CurrentStep); err != nil {
		return nil, err
	}
	updated, err := s.tenants.UpdateOnboardingStep(ctx, tenantID, input.CurrentStep)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *onboardingService) Complete(ctx context.Context, tenantID string) (*model.Tenant, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	current, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// Idempotent: an already-COMPLETED tenant is returned as-is. This is
	// checked before prerequisite validation runs at all, so a completed
	// tenant is never re-validated (and never fails) even if something about
	// it would no longer satisfy validateOnboardingCompletionPrerequisites —
	// completion, once granted, is not retroactively revoked by this method.
	if current.OnboardingStatus == model.OnboardingStatusCompleted {
		return current, nil
	}
	if err := validateOnboardingCompletionPrerequisites(current); err != nil {
		return nil, err
	}
	updated, err := s.tenants.CompleteOnboarding(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("completing onboarding: %w", err)
	}
	return updated, nil
}

// validateOnboardingCompletionPrerequisites is F2's minimum completion
// invariant — the smallest check that closes the "create a tenant, then
// immediately call complete with nothing configured" bypass without
// inventing requirements no approved feature collects yet:
//
//  1. business_type is present and still a valid, approved value — normally
//     guaranteed at creation (Feature 1) and immutable thereafter, re-checked
//     here defensively rather than trusted blindly.
//  2. name is non-empty — normally guaranteed at creation and never
//     clearable via UpdateProfile, re-checked here for the same reason.
//  3. slug is present and canonical — normally guaranteed at creation and
//     immutable thereafter (no endpoint in this codebase ever changes it),
//     re-checked here for the same reason.
//  4. onboarding_step is non-nil — the tenant must have gone through at
//     least one PATCH .../onboarding save, so "create, then immediately
//     complete" with zero onboarding activity stays impossible.
//
//  5. timezone is present and a real IANA identifier — added by Feature 6,
//     now that the common business-profile step actually collects it. This
//     is the condition that makes COMPLETED mean "the common profile was
//     genuinely configured" rather than merely "some step was saved once":
//     before F6 the step pointer could be set by a placeholder screen with
//     no data behind it. Timezone is the right required field because every
//     future booking, availability window, and schedule is computed against
//     it — a workspace without one cannot function, and unlike a description
//     or public phone number there is no legitimate business that wants to
//     operate without a timezone. It is validated with the same
//     time.LoadLocation check TenantService.UpdateProfile uses on write, so
//     a value that is somehow stored but unusable cannot slip through here.
//
// Description, contact email, and contact phone are deliberately NOT
// required: a real business may legitimately launch without a blurb or
// without publishing direct contact details, and forcing them would be
// inventing a product rule the approved plan does not ask for.
//
// Vertical-specific requirements (services, rooms, routes, staff...) remain
// unchecked — they are not approved concepts until a future vertical pass
// defines them. This invariant is expected to grow again then.
func validateOnboardingCompletionPrerequisites(tenant *model.Tenant) error {
	if tenant.BusinessType == nil {
		return apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed without a business type", nil)
	}
	if _, err := model.ValidateBusinessType(string(*tenant.BusinessType)); err != nil {
		return apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed without a valid business type", nil)
	}
	if strings.TrimSpace(tenant.Name) == "" {
		return apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed without a business name", nil)
	}
	if err := model.ValidateSlug(tenant.Slug); err != nil {
		return apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed without a valid slug", nil)
	}
	if tenant.OnboardingStep == nil {
		return apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed before any progress has been saved", nil)
	}
	if tenant.Timezone == nil {
		return apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed without a business timezone", nil)
	}
	timezone := strings.TrimSpace(*tenant.Timezone)
	// Checked before LoadLocation on purpose: time.LoadLocation("") returns
	// UTC with no error, so an empty stored value would otherwise be
	// silently accepted as a configured timezone.
	if timezone == "" {
		return apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed without a business timezone", nil)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed without a valid business timezone", nil)
	}
	return nil
}
