package service

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// This is the F2 -> F3 integration proof against a real database: a fresh
// IN_PROGRESS tenant is publicly unreachable, and only becomes reachable
// after going through OnboardingService's real save-then-complete flow —
// never by directly flipping onboarding_status in the test, which would
// duplicate F2's own completion logic rather than reuse it. Both services
// run against the same real PostgresTenantRepository, the same one app.go
// wires them both to in production.
func TestOnboardingCompletionMakesTenantPubliclyVisible(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	tenants := repository.NewPostgresTenantRepository(db)

	businessType := model.BusinessTypeHotel
	created, err := tenants.Create(ctx, &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655441500", Name: "Grand Hotel", Slug: "grand-hotel", BusinessType: &businessType})
	if err != nil {
		t.Fatalf("seeding tenant: %v", err)
	}
	if created.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("precondition: OnboardingStatus = %q, want IN_PROGRESS", created.OnboardingStatus)
	}

	onboarding := NewOnboardingService(tenants)
	public := NewPublicTenantService(tenants)

	// Before any onboarding progress: hidden, same as a nonexistent slug.
	if _, err := public.GetBySlug(ctx, "grand-hotel"); err == nil {
		t.Fatal("GetBySlug() succeeded for a fresh IN_PROGRESS tenant, want TENANT_NOT_FOUND")
	} else {
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantNotFound {
			t.Fatalf("GetBySlug() error = %v, want TENANT_NOT_FOUND", err)
		}
	}

	// F2's own completion invariant still applies here: completing without
	// any saved progress is denied, and the tenant stays hidden.
	if _, err := onboarding.Complete(ctx, created.ID); err == nil {
		t.Fatal("Complete() succeeded with no saved progress, want denial")
	}
	if _, err := public.GetBySlug(ctx, "grand-hotel"); err == nil {
		t.Fatal("GetBySlug() succeeded despite a denied completion")
	}

	// Real F2 flow: save progress, then complete.
	if _, err := onboarding.SaveProgress(ctx, created.ID, SaveOnboardingProgressInput{CurrentStep: "business_profile"}); err != nil {
		t.Fatalf("SaveProgress() error = %v", err)
	}
	// Vertical Onboarding F6 made a valid IANA timezone part of the completion
	// prerequisites, which this test predates. Set it through the real profile
	// path rather than writing the column directly, so the fixture keeps
	// exercising production code rather than reproducing it.
	timezone := "Africa/Lagos"
	if _, err := tenants.UpdateProfile(ctx, created.ID, repository.TenantProfileUpdate{Timezone: &timezone}); err != nil {
		t.Fatalf("setting business timezone: %v", err)
	}
	if _, err := onboarding.Complete(ctx, created.ID); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	identity, err := public.GetBySlug(ctx, "grand-hotel")
	if err != nil {
		t.Fatalf("GetBySlug() after completion error = %v, want the tenant now publicly visible", err)
	}
	if identity.Slug != "grand-hotel" || identity.Name != "Grand Hotel" {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.BusinessType == nil || *identity.BusinessType != model.BusinessTypeHotel {
		t.Fatalf("BusinessType = %v, want HOTEL", identity.BusinessType)
	}
}
