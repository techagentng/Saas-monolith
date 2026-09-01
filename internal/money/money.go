// Package money is the platform's single authoritative representation of a
// monetary value. It exists for one reason: a bare int64 in this codebase is
// indistinguishable from the token-expiry-seconds int64s that already live in
// internal/identity, so an amount that is only ever passed around as an int64
// can be misread — or worse, silently mixed with a value in a different unit.
//
// Deliberately absent, and not to be added without a plan that calls for them:
// floating-point arithmetic of any kind, currency conversion, FX rates,
// rounding strategies, tax, and discount arithmetic. Every operation here is
// exact integer arithmetic on minor units.
package money

import (
	"fmt"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// Currency is an ISO 4217 alphabetic code restricted to the platform's
// explicit allow-list below. It is deliberately not "any three uppercase
// letters": a code the platform cannot actually price in must be refused at
// the boundary rather than stored and discovered later.
type Currency string

const (
	CurrencyNGN Currency = "NGN"
	CurrencyUSD Currency = "USD"
	CurrencyEUR Currency = "EUR"
	CurrencyGBP Currency = "GBP"
	CurrencyGHS Currency = "GHS"
	CurrencyKES Currency = "KES"
	CurrencyZAR Currency = "ZAR"
)

// supportedCurrencies is the allow-list. Every entry has an ISO 4217 minor
// unit exponent of 2, which is what minorUnitExponent below relies on —
// adding a currency with a different exponent (JPY at 0, KWD at 3) requires
// changing Format, not just adding a map entry.
var supportedCurrencies = map[Currency]struct{}{
	CurrencyNGN: {},
	CurrencyUSD: {},
	CurrencyEUR: {},
	CurrencyGBP: {},
	CurrencyGHS: {},
	CurrencyKES: {},
	CurrencyZAR: {},
}

// minorUnitExponent is 2 for every currency on the allow-list. It is named
// rather than inlined so the assumption is visible at the one place Format
// depends on it.
const minorUnitExponent = 2

// ValidateCurrency enforces the canonical currency contract: an exact,
// case-sensitive match against the allow-list. A differently-cased or padded
// variant of a supported code — "ngn", " NGN" — is rejected rather than
// normalized, the same reject-over-normalize philosophy ValidateSlug and
// ValidateBusinessType already use for their own controlled values.
func ValidateCurrency(value string) (Currency, error) {
	candidate := Currency(value)
	if _, ok := supportedCurrencies[candidate]; !ok {
		return "", apperrors.New(apperrors.CodeValidationFailed, "unsupported currency", nil)
	}
	return candidate, nil
}

// SupportedCurrencies returns the allow-list in a deterministic order. It
// exists for tests and diagnostics; it is not part of any HTTP response.
func SupportedCurrencies() []Currency {
	return []Currency{CurrencyNGN, CurrencyUSD, CurrencyEUR, CurrencyGBP, CurrencyGHS, CurrencyKES, CurrencyZAR}
}

// Amount is a non-negative quantity of a supported currency, held in minor
// units (kobo, cents, pence). Its fields are unexported so the only way to
// obtain one is New, which validates — a zero-value Amount is therefore the
// only unvalidated instance obtainable, and it carries an empty currency that
// every consumer rejects.
//
// Amounts are non-negative by construction. This system has no concept of a
// negative price or a negative total; a refund, when one is ever modelled,
// will be its own signed concept rather than a negative Amount.
type Amount struct {
	minor    int64
	currency Currency
}

// New builds a validated Amount. Both failure modes are presentable business
// outcomes rather than system failures: an unsupported currency and a
// negative amount are things a caller can correct.
func New(minor int64, currency Currency) (Amount, error) {
	if _, err := ValidateCurrency(string(currency)); err != nil {
		return Amount{}, err
	}
	if minor < 0 {
		return Amount{}, apperrors.New(apperrors.CodeValidationFailed, "amount cannot be negative", nil)
	}
	return Amount{minor: minor, currency: currency}, nil
}

// Minor returns the amount in minor units — the exact value persisted to a
// BIGINT column. There is deliberately no Major()/Float() accessor: converting
// to a fractional type is precisely the operation this package exists to
// prevent from happening anywhere authoritative.
func (a Amount) Minor() int64 { return a.minor }

// Currency returns the amount's currency, or "" for a zero-value Amount.
func (a Amount) Currency() Currency { return a.currency }

// IsZero reports whether the amount is zero. A zero amount is legitimate —
// a free consultation or patch test is a real service — so this is
// informational, never a validation failure.
func (a Amount) IsZero() bool { return a.minor == 0 }

// Add returns the sum of two amounts of the same currency. Mixing currencies
// is refused rather than coerced: there is no exchange rate in this system to
// resolve it with. Overflow is refused rather than wrapping silently.
func (a Amount) Add(other Amount) (Amount, error) {
	if a.currency != other.currency {
		return Amount{}, apperrors.New(apperrors.CodeValidationFailed, "cannot add amounts of different currencies", nil)
	}
	sum := a.minor + other.minor
	// Both operands are non-negative by construction, so the only possible
	// overflow is upward, and it is detectable by the sum having gone
	// backwards.
	if sum < a.minor {
		return Amount{}, apperrors.New(apperrors.CodeValidationFailed, "amount overflow", nil)
	}
	return Amount{minor: sum, currency: a.currency}, nil
}

// Format renders the amount for display using integer arithmetic only — the
// division and modulus below are exact, and no float is constructed at any
// point. It assumes minorUnitExponent, which holds for every currency on the
// allow-list.
func (a Amount) Format() string {
	divisor := int64(1)
	for i := 0; i < minorUnitExponent; i++ {
		divisor *= 10
	}
	return fmt.Sprintf("%s %d.%0*d", a.currency, a.minor/divisor, minorUnitExponent, a.minor%divisor)
}

// String makes Amount presentable in logs and test failures without a caller
// reaching for the unexported fields.
func (a Amount) String() string { return a.Format() }
