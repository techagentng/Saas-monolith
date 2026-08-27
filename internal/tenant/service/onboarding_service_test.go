package service

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

const onboardingTestTenantID = "550e8400-e29b-41d4-a716-446655440900"

// A tenant that satisfies every completion prerequisite except the
// onboarding step. Timezone is populated because F6 makes it required: it is
// the one common-profile field that must genuinely be configured before a
// workspace can be considered set up (bookings cannot be scheduled without
// it), and it is the field that proves the business-profile step was really
// completed rather than skipped.
func validInProgressTenant() *model.Tenant {
	businessType := model.BusinessTypeHotel
	timezone := "Africa/Lagos"
	return &model.Tenant{
		ID: onboardingTestTenantID, Name: "Hotel Co", Slug: "hotel-co", Status: model.StatusActive,
		BusinessType: &businessType, OnboardingStatus: model.OnboardingStatusInProgress, Timezone: &timezone,
	}
}

// --- SaveProgress ------------------------------------------------------------

func TestSaveProgressPersistsValidStep(t *testing.T) {
	fake := &onboardingRepositoryFake{tenant: validInProgressTenant()}
	svc := NewOnboardingService(fake)

	updated, err := svc.SaveProgress(context.Background(), onboardingTestTenantID, SaveOnboardingProgressInput{CurrentStep: "business_profile"})
	if err != nil {
		t.Fatalf("SaveProgress() error = %v", err)
	}
	if fake.updateStepCalls != 1 || fake.lastStep != "business_profile" {
		t.Fatalf("UpdateOnboardingStep calls = %d, lastStep = %q", fake.updateStepCalls, fake.lastStep)
	}
	if updated.OnboardingStep == nil || *updated.OnboardingStep != "business_profile" {
		t.Fatalf("OnboardingStep = %v, want business_profile", updated.OnboardingStep)
	}
}

func TestSaveProgressRejectsUnknownStep(t *testing.T) {
	fake := &onboardingRepositoryFake{tenant: validInProgressTenant()}
	svc := NewOnboardingService(fake)

	_, err := svc.SaveProgress(context.Background(), onboardingTestTenantID, SaveOnboardingProgressInput{CurrentStep: "rooms"})
	assertOnboardingCode(t, err, apperrors.CodeValidationFailed)
	if fake.updateStepCalls != 0 {
		t.Fatalf("UpdateOnboardingStep called despite an invalid step")
	}
}

func TestSaveProgressRejectsMalformedTenantID(t *testing.T) {
	fake := &onboardingRepositoryFake{tenant: validInProgressTenant()}
	svc := NewOnboardingService(fake)

	_, err := svc.SaveProgress(context.Background(), "not-a-uuid", SaveOnboardingProgressInput{CurrentStep: "business_profile"})
	assertOnboardingCode(t, err, apperrors.CodeInvalidRequest)
	if fake.updateStepCalls != 0 {
		t.Fatalf("UpdateOnboardingStep called despite a malformed tenant id")
	}
}

// F2 requirement: saving progress on a COMPLETED tenant must not silently
// revert it to IN_PROGRESS.
func TestSaveProgressOnCompletedTenantLeavesStatusCompleted(t *testing.T) {
	tenant := validInProgressTenant()
	tenant.OnboardingStatus = model.OnboardingStatusCompleted
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	updated, err := svc.SaveProgress(context.Background(), onboardingTestTenantID, SaveOnboardingProgressInput{CurrentStep: "business_profile"})
	if err != nil {
		t.Fatalf("SaveProgress() error = %v", err)
	}
	if updated.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want unchanged COMPLETED (must not silently revert to IN_PROGRESS)", updated.OnboardingStatus)
	}
	if updated.OnboardingStep == nil || *updated.OnboardingStep != "business_profile" {
		t.Fatalf("OnboardingStep = %v, want the newly saved step even on a COMPLETED tenant", updated.OnboardingStep)
	}
}

// --- Complete ----------------------------------------------------------------

func TestCompleteSucceedsWhenPrerequisitesSatisfied(t *testing.T) {
	tenant := validInProgressTenant()
	step := "business_profile"
	tenant.OnboardingStep = &step
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	updated, err := svc.Complete(context.Background(), onboardingTestTenantID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if updated.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want COMPLETED", updated.OnboardingStatus)
	}
	if fake.completeCalls != 1 {
		t.Fatalf("CompleteOnboarding calls = %d, want 1", fake.completeCalls)
	}
}

// This is the mandatory bypass-prevention case: creating a tenant and
// immediately calling complete, with no onboarding progress ever saved, must
// be denied — otherwise F3's public-visibility gate becomes a formality.
func TestCompleteDeniedImmediatelyAfterCreationWithNoProgressSaved(t *testing.T) {
	tenant := validInProgressTenant() // OnboardingStep is nil — exactly the state right after Feature 2 creation
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	_, err := svc.Complete(context.Background(), onboardingTestTenantID)
	assertOnboardingCode(t, err, apperrors.CodeValidationFailed)
	if fake.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding called despite no onboarding progress ever saved")
	}
	if tenant.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want unchanged IN_PROGRESS after a denied completion", tenant.OnboardingStatus)
	}
}

func TestCompleteDeniedWithoutBusinessType(t *testing.T) {
	tenant := validInProgressTenant()
	tenant.BusinessType = nil
	step := "business_profile"
	tenant.OnboardingStep = &step
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	_, err := svc.Complete(context.Background(), onboardingTestTenantID)
	assertOnboardingCode(t, err, apperrors.CodeValidationFailed)
	if fake.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding called despite a missing business type")
	}
}

func TestCompleteDeniedWithEmptyName(t *testing.T) {
	tenant := validInProgressTenant()
	tenant.Name = "   "
	step := "business_profile"
	tenant.OnboardingStep = &step
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	_, err := svc.Complete(context.Background(), onboardingTestTenantID)
	assertOnboardingCode(t, err, apperrors.CodeValidationFailed)
	if fake.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding called despite an empty name")
	}
}

func TestCompleteDeniedWithInvalidSlug(t *testing.T) {
	tenant := validInProgressTenant()
	tenant.Slug = "Not A Valid Slug"
	step := "business_profile"
	tenant.OnboardingStep = &step
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	_, err := svc.Complete(context.Background(), onboardingTestTenantID)
	assertOnboardingCode(t, err, apperrors.CodeValidationFailed)
	if fake.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding called despite an invalid slug")
	}
}

// --- F6: the common business profile must actually be configured ------------

// Timezone is the F6 addition to the completion invariant. Without it, a
// tenant could reach COMPLETED — and therefore public visibility (F3) — with
// no operating timezone, which no booking flow can work against.
func TestCompleteDeniedWithoutTimezone(t *testing.T) {
	tenant := validInProgressTenant()
	tenant.Timezone = nil
	step := "business_profile"
	tenant.OnboardingStep = &step
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	_, err := svc.Complete(context.Background(), onboardingTestTenantID)
	assertOnboardingCode(t, err, apperrors.CodeValidationFailed)
	if fake.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding called despite a missing timezone")
	}
	if tenant.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want unchanged IN_PROGRESS after a denied completion", tenant.OnboardingStatus)
	}
}

// A stored value that is not a real IANA identifier is as unusable as a
// missing one, so it is rejected rather than trusted because it is non-empty.
func TestCompleteDeniedWithInvalidTimezone(t *testing.T) {
	for _, value := range []string{"", "   ", "Not/AZone", "GMT+1"} {
		t.Run(value, func(t *testing.T) {
			tenant := validInProgressTenant()
			tz := value
			tenant.Timezone = &tz
			step := "business_profile"
			tenant.OnboardingStep = &step
			fake := &onboardingRepositoryFake{tenant: tenant}
			svc := NewOnboardingService(fake)

			_, err := svc.Complete(context.Background(), onboardingTestTenantID)
			assertOnboardingCode(t, err, apperrors.CodeValidationFailed)
			if fake.completeCalls != 0 {
				t.Fatalf("CompleteOnboarding called despite timezone %q", value)
			}
		})
	}
}

// The optional half of the invariant: a business that simply has no
// description or public contact details is still a complete workspace, so
// completion must not demand them.
func TestCompleteSucceedsWithoutOptionalProfileFields(t *testing.T) {
	tenant := validInProgressTenant()
	tenant.Description = nil
	tenant.ContactEmail = nil
	tenant.ContactPhone = nil
	step := "business_profile"
	tenant.OnboardingStep = &step
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	updated, err := svc.Complete(context.Background(), onboardingTestTenantID)
	if err != nil {
		t.Fatalf("Complete() error = %v, want success without optional profile fields", err)
	}
	if updated.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want COMPLETED", updated.OnboardingStatus)
	}
}

func TestCompleteRejectsMalformedTenantID(t *testing.T) {
	fake := &onboardingRepositoryFake{tenant: validInProgressTenant()}
	svc := NewOnboardingService(fake)

	_, err := svc.Complete(context.Background(), "not-a-uuid")
	assertOnboardingCode(t, err, apperrors.CodeInvalidRequest)
	if fake.findCalls != 0 {
		t.Fatalf("FindByID called despite a malformed tenant id")
	}
}

// Idempotent completion: repeated calls on an already-COMPLETED tenant
// succeed without re-validating or re-persisting anything.
func TestCompleteOnAlreadyCompletedTenantIsIdempotent(t *testing.T) {
	tenant := validInProgressTenant()
	tenant.OnboardingStatus = model.OnboardingStatusCompleted // no OnboardingStep set — would fail prerequisites if re-validated
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	updated, err := svc.Complete(context.Background(), onboardingTestTenantID)
	if err != nil {
		t.Fatalf("Complete() on an already-COMPLETED tenant error = %v, want idempotent success", err)
	}
	if updated.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want COMPLETED", updated.OnboardingStatus)
	}
	if fake.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding calls = %d, want 0 — idempotent path must not re-persist", fake.completeCalls)
	}
}

// A legacy tenant (pre-F1: business_type NULL, onboarding_status COMPLETED
// from the migration default) must remain readable and untouched — F2 must
// never force it back through onboarding validation.
func TestCompleteOnLegacyCompletedTenantWithNilBusinessTypeIsUnaffected(t *testing.T) {
	tenant := &model.Tenant{ID: onboardingTestTenantID, Name: "Legacy Tenant", Slug: "legacy-tenant", Status: model.StatusActive, OnboardingStatus: model.OnboardingStatusCompleted}
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	updated, err := svc.Complete(context.Background(), onboardingTestTenantID)
	if err != nil {
		t.Fatalf("Complete() on a legacy completed tenant error = %v, want success (no forced re-onboarding)", err)
	}
	if updated.BusinessType != nil {
		t.Fatalf("BusinessType = %v, want unchanged nil", updated.BusinessType)
	}
	if fake.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding calls = %d, want 0", fake.completeCalls)
	}
}

// Completion must not mutate anything beyond onboarding_status.
func TestCompleteDoesNotMutateBusinessTypeSlugOrStatus(t *testing.T) {
	tenant := validInProgressTenant()
	step := "business_profile"
	tenant.OnboardingStep = &step
	originalBusinessType := *tenant.BusinessType
	originalSlug := tenant.Slug
	originalStatus := tenant.Status
	fake := &onboardingRepositoryFake{tenant: tenant}
	svc := NewOnboardingService(fake)

	updated, err := svc.Complete(context.Background(), onboardingTestTenantID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if updated.BusinessType == nil || *updated.BusinessType != originalBusinessType {
		t.Fatalf("BusinessType = %v, want unchanged %v", updated.BusinessType, originalBusinessType)
	}
	if updated.Slug != originalSlug {
		t.Fatalf("Slug = %q, want unchanged %q", updated.Slug, originalSlug)
	}
	if updated.Status != originalStatus {
		t.Fatalf("Status = %q, want unchanged %q", updated.Status, originalStatus)
	}
}

func TestCompletePropagatesTenantNotFound(t *testing.T) {
	fake := &onboardingRepositoryFake{findErr: apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)}
	svc := NewOnboardingService(fake)

	_, err := svc.Complete(context.Background(), onboardingTestTenantID)
	assertOnboardingCode(t, err, apperrors.CodeTenantNotFound)
}

func assertOnboardingCode(t *testing.T, err error, expected apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != expected {
		t.Fatalf("error = %v, want %q", err, expected)
	}
}

// --- fake ----------------------------------------------------------------

type onboardingRepositoryFake struct {
	tenant *model.Tenant

	findErr       error
	updateStepErr error
	completeErr   error

	findCalls       int
	updateStepCalls int
	completeCalls   int
	lastStep        string
}

func (f *onboardingRepositoryFake) FindByID(context.Context, string) (*model.Tenant, error) {
	f.findCalls++
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.tenant, nil
}

func (f *onboardingRepositoryFake) UpdateOnboardingStep(_ context.Context, _ string, step string) (*model.Tenant, error) {
	f.updateStepCalls++
	f.lastStep = step
	if f.updateStepErr != nil {
		return nil, f.updateStepErr
	}
	f.tenant.OnboardingStep = &step
	return f.tenant, nil
}

func (f *onboardingRepositoryFake) CompleteOnboarding(context.Context, string) (*model.Tenant, error) {
	f.completeCalls++
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	f.tenant.OnboardingStatus = model.OnboardingStatusCompleted
	return f.tenant, nil
}
