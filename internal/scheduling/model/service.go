// Package model holds the scheduling domain's entities and their validation.
//
// Scheduling is the appointment-style booking model: a named service of a
// known duration, performed by a person, occupying a window on that person's
// calendar. NAIL_TECHNICIAN is the first business_type to use it, but nothing
// in this package is nail-specific — the same model serves barbers, spas,
// tattoo studios and consultants. Verticals whose bookable unit is not a
// duration on a calendar (a restaurant's floor capacity, a hotel's room-night
// inventory, a transport company's scheduled departure) get their own modules
// rather than nullable columns here.
package model

import (
	"strings"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// Status is a service's catalog lifecycle. It is deliberately ARCHIVED rather
// than the DISABLED used by tenants, users and memberships: in this codebase
// DISABLED consistently means "an actor is barred from acting", and a service
// is not an actor. It is a catalog entry that stops being offered.
type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusArchived Status = "ARCHIVED"
)

// Bounds are mirrored by CHECK constraints in migration 000010. Validating in
// both places is intentional: the service layer produces a presentable
// VALIDATION_FAILED, while the constraint means no writer — a future admin
// tool, a manual psql session — can bypass the rule.
const (
	MaxNameLength        = 255
	MaxDescriptionLength = 1000
	// MinDurationMinutes excludes zero: a zero-duration service produces
	// degenerate slots and an empty time range, which the exclusion constraint
	// planned for S10 would then never detect a conflict on.
	MinDurationMinutes = 1
	// MaxDurationMinutes (8 hours) bounds future slot-generation cost and
	// catches the classic unit error of entering seconds.
	MaxDurationMinutes = 480
	// MaxPriceMinor is 1,000,000 major units. It catches the same class of
	// unit error for money; it is not a product pricing rule.
	MaxPriceMinor = 100000000
)

// Service is one entry in a tenant's catalog.
//
// There is deliberately no currency field: currency is a property of the
// tenant (one business trades in one currency), so storing it per row would
// permit a catalog that mixes currencies with no exchange logic in this system
// to resolve it. A booking will snapshot the currency alongside the price when
// S10 lands, which is what keeps historical rows correct.
type Service struct {
	ID       string
	TenantID string
	Name     string
	// Description is nil when never supplied. An empty string is a distinct,
	// legitimate state ("this service has no description"), matching how the
	// tenant profile already treats its own description.
	Description     *string
	DurationMinutes int
	PriceMinor      int64
	// CategoryID is nil for an uncategorised service — the permanent state of
	// every service created before SC1, and a legitimate ongoing one for a
	// tenant that never files its catalogue. The composite foreign key in
	// migration 000019 guarantees that a non-nil value names a category
	// belonging to THIS tenant.
	CategoryID *string
	Status     Status
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ValidateName trims and bounds a service name, returning the value to store.
// Trimming here (rather than rejecting padded input outright) follows
// TenantService.UpdateProfile's treatment of tenant name: a name is free-form
// human text, unlike the controlled identifiers — slug, business type,
// currency — where this codebase rejects rather than normalizes.
//
// There is no uniqueness rule. Two services may legitimately share a name: an
// archived "Gel Manicure" and its replacement is the common case, and a
// uniqueness constraint over active names would block re-creating a name the
// owner had archived.
func ValidateName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", apperrors.New(apperrors.CodeValidationFailed, "service name is required", nil)
	}
	if len(name) > MaxNameLength {
		return "", apperrors.New(apperrors.CodeValidationFailed, "service name exceeds maximum length", nil)
	}
	return name, nil
}

// ValidateDescription trims and bounds an optional description. A nil input
// stays nil (the field was not supplied); a supplied value that trims to empty
// is kept as an empty string rather than being converted back to nil, so
// "cleared to empty" and "never set" remain distinguishable.
func ValidateDescription(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	description := strings.TrimSpace(*value)
	if len(description) > MaxDescriptionLength {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "service description exceeds maximum length", nil)
	}
	return &description, nil
}

// ValidateDurationMinutes bounds a service's duration.
func ValidateDurationMinutes(minutes int) error {
	if minutes < MinDurationMinutes {
		return apperrors.New(apperrors.CodeValidationFailed, "service duration must be greater than zero", nil)
	}
	if minutes > MaxDurationMinutes {
		return apperrors.New(apperrors.CodeValidationFailed, "service duration exceeds maximum", nil)
	}
	return nil
}

// ValidatePriceMinor bounds a service's price in minor units.
//
// Zero is allowed: a free consultation or patch test is a real service offered
// by real salons. Negative is refused — internal/money refuses it too, and this
// is the catalog-layer half of the same rule.
func ValidatePriceMinor(priceMinor int64) error {
	if priceMinor < 0 {
		return apperrors.New(apperrors.CodeValidationFailed, "service price cannot be negative", nil)
	}
	if priceMinor > MaxPriceMinor {
		return apperrors.New(apperrors.CodeValidationFailed, "service price exceeds maximum", nil)
	}
	return nil
}
