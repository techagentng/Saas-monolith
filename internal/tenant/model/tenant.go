package model

import "time"

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusDisabled Status = "DISABLED"
)

type Tenant struct {
	ID           string
	Name         string
	Slug         string
	Status       Status
	Description  *string
	ContactEmail *string
	ContactPhone *string
	Timezone     *string
	// BusinessType is nil only for tenants created before this field
	// existed; every tenant created through TenantService.Create from here
	// on has one. It is immutable — see BusinessType's own doc comment.
	BusinessType *BusinessType
	// OnboardingStatus is never empty for a persisted tenant: the
	// repository defaults it the same way it already defaults Status.
	OnboardingStatus OnboardingStatus
	// OnboardingStep is a free-form resume pointer, not a typed enum — the
	// valid values depend on BusinessType and are validated at the service
	// layer, not here. Nil until onboarding progress has been saved.
	OnboardingStep *string
	// Currency is the ISO 4217 code every price this tenant sets is
	// denominated in (Scheduling S1). It is nil until the tenant declares one
	// and is write-once thereafter — see CurrencyService.Set for why changing
	// it would silently reinterpret every stored amount. It is deliberately
	// absent from UpdateTenantProfileRequest, so no ordinary profile PATCH can
	// reach it.
	Currency  *string
	CreatedAt time.Time
	UpdatedAt time.Time
}
