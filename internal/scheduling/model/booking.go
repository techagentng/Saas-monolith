package model

import (
	"net/mail"
	"strings"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// BookingStatus is a booking's lifecycle. It is its own closed vocabulary
// rather than reusing service.go's ACTIVE/ARCHIVED: a booking is neither a
// catalog entry nor an actor. S10 only ever writes CONFIRMED; CANCELLED exists
// (mirrored by the CHECK and the partial exclusion constraint in migration
// 000016) so S11 can add the transition without a lock-heavy constraint
// rebuild.
type BookingStatus string

const (
	BookingConfirmed BookingStatus = "CONFIRMED"
	BookingCancelled BookingStatus = "CANCELLED"
)

// Bounds mirror sensible free-text limits. There is no column width on the
// TEXT fields in 000016 — these are the service-layer guard that keeps a
// hostile payload from being persisted, the same "validate in both places"
// discipline the rest of the module uses.
const (
	MaxCustomerNameLength  = 255
	MaxCustomerPhoneLength = 40
	MaxCustomerEmailLength = 320
)

// Customer is the anonymous public booking identity: the smallest set that
// lets a salon actually contact whoever booked. Name is required; phone and
// email are optional. There is deliberately no address, no date of birth, no
// account — S10 collects a way to reach the customer, not a profile, and
// introduces no customer authentication.
type Customer struct {
	Name string
	// Phone and Email are nil when not supplied. A supplied value that trims
	// to empty is treated as not supplied (nil), so "" is never persisted.
	Phone *string
	Email *string
}

// Booking is one persisted appointment.
//
// StartAt/EndAt are absolute instants (stored TIMESTAMPTZ). They are derived
// server-side: the tenant-local (date + start) resolved through the tenant's
// authoritative IANA timezone, and EndAt = StartAt + the service's
// authoritative DurationMinutes. A client-supplied end, duration, price or
// name is never trusted.
//
// There is no price snapshot. S1 keeps price on the service and currency on
// the tenant (nullable under the accepted S8/S9 contract); S10 introduces no
// payment or accounting behaviour, so copying a price onto the booking would
// be inventing a contract that nothing consumes. S11+ can add a snapshot when
// a real money flow needs one.
type Booking struct {
	ID        string
	TenantID  string
	ServiceID string
	StaffID   string
	Customer  Customer
	StartAt   time.Time
	EndAt     time.Time
	Status    BookingStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ValidateCustomer trims and bounds the public booking identity.
//
// It rejects an absent or blank name (a booking nobody can be reached about is
// not useful), and lightly checks a supplied email — net/mail.ParseAddress,
// not a bespoke regex — because an obviously malformed address is worth
// catching before it is stored, while exhaustive validation is a rabbit hole
// the backend does not need to enter. Phone is accepted as free text: this
// system has no international-normalization infrastructure, and imposing a
// format would reject legitimate local numbers.
func ValidateCustomer(input Customer) (Customer, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Customer{}, apperrors.New(apperrors.CodeValidationFailed, "customer name is required", nil)
	}
	if len(name) > MaxCustomerNameLength {
		return Customer{}, apperrors.New(apperrors.CodeValidationFailed, "customer name exceeds maximum length", nil)
	}

	result := Customer{Name: name}

	if phone := trimToNil(input.Phone); phone != nil {
		if len(*phone) > MaxCustomerPhoneLength {
			return Customer{}, apperrors.New(apperrors.CodeValidationFailed, "customer phone exceeds maximum length", nil)
		}
		result.Phone = phone
	}

	if email := trimToNil(input.Email); email != nil {
		if len(*email) > MaxCustomerEmailLength {
			return Customer{}, apperrors.New(apperrors.CodeValidationFailed, "customer email exceeds maximum length", nil)
		}
		if _, err := mail.ParseAddress(*email); err != nil {
			return Customer{}, apperrors.New(apperrors.CodeValidationFailed, "customer email is not a valid address", nil)
		}
		result.Email = email
	}

	return result, nil
}

// trimToNil collapses a nil-or-blank optional string to nil and trims the
// rest, so "never supplied" and "supplied as whitespace" persist identically
// as NULL.
func trimToNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
