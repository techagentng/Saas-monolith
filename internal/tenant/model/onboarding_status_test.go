package model

import "testing"

func TestOnboardingStatuses(t *testing.T) {
	if OnboardingStatusInProgress != "IN_PROGRESS" || OnboardingStatusCompleted != "COMPLETED" {
		t.Fatalf("onboarding statuses = %q, %q", OnboardingStatusInProgress, OnboardingStatusCompleted)
	}
}
