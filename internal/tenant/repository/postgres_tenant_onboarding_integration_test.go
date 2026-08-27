package repository

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

func TestUpdateOnboardingStepPersistsStepOnly(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	businessType := model.BusinessTypeNailTechnician
	tenant, err := repo.Create(ctx, &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440400", Name: "Nail Studio", Slug: "nail-studio", BusinessType: &businessType})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updated, err := repo.UpdateOnboardingStep(ctx, tenant.ID, "business_profile")
	if err != nil {
		t.Fatalf("UpdateOnboardingStep() error = %v", err)
	}
	if updated.OnboardingStep == nil || *updated.OnboardingStep != "business_profile" {
		t.Fatalf("OnboardingStep = %v, want business_profile", updated.OnboardingStep)
	}
	// Only onboarding_step changes — business_type, status, and
	// onboarding_status must all survive unchanged.
	if updated.BusinessType == nil || *updated.BusinessType != model.BusinessTypeNailTechnician {
		t.Fatalf("BusinessType = %v, want unchanged NAIL_TECHNICIAN", updated.BusinessType)
	}
	if updated.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want unchanged IN_PROGRESS", updated.OnboardingStatus)
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want unchanged ACTIVE", updated.Status)
	}

	// Re-saving a step is allowed — no ordering enforcement at this layer.
	resaved, err := repo.UpdateOnboardingStep(ctx, tenant.ID, "business_profile")
	if err != nil {
		t.Fatalf("UpdateOnboardingStep() second call error = %v", err)
	}
	if resaved.OnboardingStep == nil || *resaved.OnboardingStep != "business_profile" {
		t.Fatalf("OnboardingStep after resave = %v, want business_profile", resaved.OnboardingStep)
	}
}

func TestUpdateOnboardingStepReturnsNotFoundForMissingTenant(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)

	_, err := repo.UpdateOnboardingStep(context.Background(), "550e8400-e29b-41d4-a716-446655440999", "business_profile")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantNotFound {
		t.Fatalf("UpdateOnboardingStep() error = %v, want TENANT_NOT_FOUND", err)
	}
}

func TestUpdateOnboardingStepOnCompletedTenantLeavesStatusCompleted(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	businessType := model.BusinessTypeRestaurant
	tenant, err := repo.Create(ctx, &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440401", Name: "Bistro", Slug: "bistro-onboarding", BusinessType: &businessType})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := repo.CompleteOnboarding(ctx, tenant.ID); err != nil {
		t.Fatalf("CompleteOnboarding() error = %v", err)
	}

	updated, err := repo.UpdateOnboardingStep(ctx, tenant.ID, "business_profile")
	if err != nil {
		t.Fatalf("UpdateOnboardingStep() on a completed tenant error = %v", err)
	}
	if updated.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want unchanged COMPLETED (must not silently revert to IN_PROGRESS)", updated.OnboardingStatus)
	}
}

func TestCompleteOnboardingTransitionsStatus(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	businessType := model.BusinessTypeTransport
	tenant, err := repo.Create(ctx, &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440402", Name: "Transit Co", Slug: "transit-onboarding", BusinessType: &businessType})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if tenant.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("precondition: OnboardingStatus = %q, want IN_PROGRESS", tenant.OnboardingStatus)
	}

	updated, err := repo.CompleteOnboarding(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("CompleteOnboarding() error = %v", err)
	}
	if updated.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want COMPLETED", updated.OnboardingStatus)
	}
	// business_type/slug/status must survive the transition unchanged — this
	// method has no SET clause for any of them.
	if updated.BusinessType == nil || *updated.BusinessType != model.BusinessTypeTransport {
		t.Fatalf("BusinessType = %v, want unchanged TRANSPORT", updated.BusinessType)
	}
	if updated.Slug != "transit-onboarding" {
		t.Fatalf("Slug = %q, want unchanged", updated.Slug)
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want unchanged ACTIVE", updated.Status)
	}

	// Applying it again at the SQL level is a harmless idempotent no-op —
	// prerequisite/idempotency decisions belong to the service layer, but the
	// repository method itself must not error on a row already COMPLETED.
	reapplied, err := repo.CompleteOnboarding(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("CompleteOnboarding() second call error = %v", err)
	}
	if reapplied.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus after reapply = %q, want COMPLETED", reapplied.OnboardingStatus)
	}
}

func TestCompleteOnboardingReturnsNotFoundForMissingTenant(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)

	_, err := repo.CompleteOnboarding(context.Background(), "550e8400-e29b-41d4-a716-446655440998")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantNotFound {
		t.Fatalf("CompleteOnboarding() error = %v, want TENANT_NOT_FOUND", err)
	}
}
