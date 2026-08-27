package model

import (
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

func TestValidateOnboardingStepAcceptsApprovedStep(t *testing.T) {
	if err := ValidateOnboardingStep("business_profile"); err != nil {
		t.Fatalf("ValidateOnboardingStep(business_profile) error = %v", err)
	}
}

func TestValidateOnboardingStepRejectsUnknownStep(t *testing.T) {
	for _, step := range []string{"", "review", "rooms", "services", "BUSINESS_PROFILE", " business_profile", "business_profile "} {
		t.Run(step, func(t *testing.T) {
			err := ValidateOnboardingStep(step)
			var appErr *apperrors.AppError
			if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
				t.Fatalf("ValidateOnboardingStep(%q) error = %v, want VALIDATION_FAILED", step, err)
			}
		})
	}
}

// F2 explicitly must not introduce vertical-specific steps yet.
func TestValidateOnboardingStepRejectsVerticalSpecificSteps(t *testing.T) {
	for _, step := range []string{"services", "staff", "room_types", "tables", "routes", "vehicles"} {
		t.Run(step, func(t *testing.T) {
			if err := ValidateOnboardingStep(step); err == nil {
				t.Fatalf("ValidateOnboardingStep(%q) succeeded; vertical-specific steps are not approved at F2", step)
			}
		})
	}
}
