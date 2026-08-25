package model

// OnboardingStatus tracks a tenant's onboarding workflow progress. It is a
// dimension entirely separate from Status (ACTIVE/DISABLED): Status is the
// tenant's lifecycle concept (suspended/disabled vs. not), unchanged by this
// type; OnboardingStatus answers whether the tenant's business setup is
// complete. A tenant can be Status=ACTIVE and OnboardingStatus=IN_PROGRESS
// at the same time — that combination is the normal, expected state of a
// freshly created tenant.
//
// There is deliberately no NOT_STARTED value: no tenant row exists until
// business type, name, and slug are all supplied together in one creation
// call, so a tenant is always born IN_PROGRESS — a "not started" tenant is
// never an observable, persisted state.
type OnboardingStatus string

const (
	OnboardingStatusInProgress OnboardingStatus = "IN_PROGRESS"
	OnboardingStatusCompleted  OnboardingStatus = "COMPLETED"
)
