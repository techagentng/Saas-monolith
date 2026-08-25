package model

import apperrors "github.com/techagentng/saas-monolith/internal/errors"

// BusinessType identifies which vertical a tenant operates in. It is chosen
// once — at or before tenant creation — and is immutable for the tenant's
// entire lifetime: UpdateTenantProfileRequest deliberately has no field for
// it, and no endpoint in this codebase ever changes it after creation.
type BusinessType string

const (
	BusinessTypeNailTechnician BusinessType = "NAIL_TECHNICIAN"
	BusinessTypeRestaurant     BusinessType = "RESTAURANT"
	BusinessTypeHotel          BusinessType = "HOTEL"
	BusinessTypeTransport      BusinessType = "TRANSPORT"
)

var validBusinessTypes = map[BusinessType]struct{}{
	BusinessTypeNailTechnician: {},
	BusinessTypeRestaurant:     {},
	BusinessTypeHotel:          {},
	BusinessTypeTransport:      {},
}

// ValidateBusinessType enforces the canonical business-type contract: an
// exact, case-sensitive match against the approved allow-list. A value
// outside that list — including a differently-cased or padded variant of an
// approved one — is rejected rather than normalized, the same
// reject-over-normalize philosophy ValidateSlug already uses for slugs.
func ValidateBusinessType(value string) (BusinessType, error) {
	candidate := BusinessType(value)
	if _, ok := validBusinessTypes[candidate]; !ok {
		return "", apperrors.New(apperrors.CodeValidationFailed, "invalid business type", nil)
	}
	return candidate, nil
}
