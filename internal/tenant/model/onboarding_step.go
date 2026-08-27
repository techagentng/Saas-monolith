package model

import apperrors "github.com/techagentng/saas-monolith/internal/errors"

// knownOnboardingSteps is F2's controlled step vocabulary. "business_profile"
// is the only common, vertical-agnostic onboarding step the approved plan
// defines so far (the business-profile step from that plan's Feature 6).
// Vertical-specific steps (nail/restaurant/hotel/transport) are deliberately
// not added here — they are not approved concepts yet, and adding them
// speculatively would let a client "complete" a step this codebase has no
// real content for. Extend this map only when a step is genuinely approved.
var knownOnboardingSteps = map[string]struct{}{
	"business_profile": {},
}

// ValidateOnboardingStep enforces the current, minimum controlled step
// vocabulary — a value outside this list is rejected, not normalized, the
// same reject-over-normalize philosophy ValidateSlug and ValidateBusinessType
// already use for their own controlled values.
func ValidateOnboardingStep(step string) error {
	if _, ok := knownOnboardingSteps[step]; !ok {
		return apperrors.New(apperrors.CodeValidationFailed, "invalid onboarding step", nil)
	}
	return nil
}
