package money

import (
	"errors"
	"math"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

func assertValidationFailed(t *testing.T, err error, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("%s: error = %v, want VALIDATION_FAILED", context, err)
	}
}

func TestValidateCurrencyAcceptsEverySupportedCode(t *testing.T) {
	for _, currency := range SupportedCurrencies() {
		validated, err := ValidateCurrency(string(currency))
		if err != nil {
			t.Fatalf("ValidateCurrency(%q) error = %v, want accepted", currency, err)
		}
		if validated != currency {
			t.Fatalf("ValidateCurrency(%q) = %q, want the value returned unchanged", currency, validated)
		}
	}
}

// Reject-over-normalize: a differently-cased or padded variant of a supported
// code is refused rather than silently corrected, the same contract
// ValidateSlug and ValidateBusinessType hold to.
func TestValidateCurrencyRejectsRatherThanNormalizes(t *testing.T) {
	for _, candidate := range []string{"ngn", "Ngn", " NGN", "NGN ", "\tNGN"} {
		if _, err := ValidateCurrency(candidate); err == nil {
			t.Fatalf("ValidateCurrency(%q) accepted a non-canonical code — it must reject, not normalize", candidate)
		} else {
			assertValidationFailed(t, err, "ValidateCurrency("+candidate+")")
		}
	}
}

func TestValidateCurrencyRejectsUnsupportedCodes(t *testing.T) {
	// JPY and KWD are real ISO 4217 codes deliberately absent from the
	// allow-list: both have a minor-unit exponent other than 2, which Format
	// does not handle. They must be refused rather than mispriced.
	for _, candidate := range []string{"", "XXX", "JPY", "KWD", "US", "USDD", "123"} {
		if _, err := ValidateCurrency(candidate); err == nil {
			t.Fatalf("ValidateCurrency(%q) accepted an unsupported code", candidate)
		} else {
			assertValidationFailed(t, err, "ValidateCurrency("+candidate+")")
		}
	}
}

func TestNewAcceptsZeroAmount(t *testing.T) {
	// A free consultation or patch test is a real service, so zero is a
	// legitimate amount rather than a validation failure.
	amount, err := New(0, CurrencyNGN)
	if err != nil {
		t.Fatalf("New(0) error = %v, want zero accepted", err)
	}
	if !amount.IsZero() || amount.Minor() != 0 {
		t.Fatalf("New(0) = %v, want a zero amount", amount)
	}
}

func TestNewRejectsNegativeAmount(t *testing.T) {
	for _, minor := range []int64{-1, -1999, math.MinInt64} {
		if _, err := New(minor, CurrencyNGN); err == nil {
			t.Fatalf("New(%d) accepted a negative amount", minor)
		} else {
			assertValidationFailed(t, err, "New(negative)")
		}
	}
}

func TestNewRejectsUnsupportedCurrency(t *testing.T) {
	if _, err := New(1999, Currency("JPY")); err == nil {
		t.Fatal("New() accepted an unsupported currency")
	} else {
		assertValidationFailed(t, err, "New(unsupported currency)")
	}
}

func TestNewPreservesMinorUnitsExactly(t *testing.T) {
	amount, err := New(1999, CurrencyNGN)
	if err != nil {
		t.Fatal(err)
	}
	if amount.Minor() != 1999 {
		t.Fatalf("Minor() = %d, want 1999", amount.Minor())
	}
	if amount.Currency() != CurrencyNGN {
		t.Fatalf("Currency() = %q, want NGN", amount.Currency())
	}
}

func TestZeroValueAmountCarriesNoCurrency(t *testing.T) {
	// The zero value is the only Amount obtainable without New. It must not
	// masquerade as a valid amount in a real currency.
	var amount Amount
	if amount.Currency() != "" {
		t.Fatalf("zero-value Currency() = %q, want empty", amount.Currency())
	}
	if _, err := ValidateCurrency(string(amount.Currency())); err == nil {
		t.Fatal("the zero-value Amount's currency validated — a zero value must never pass for a real currency")
	}
}

func TestAddSumsSameCurrency(t *testing.T) {
	first, _ := New(1999, CurrencyNGN)
	second, _ := New(1, CurrencyNGN)

	sum, err := first.Add(second)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if sum.Minor() != 2000 {
		t.Fatalf("Add() = %d, want 2000", sum.Minor())
	}
	if sum.Currency() != CurrencyNGN {
		t.Fatalf("Add() currency = %q, want NGN", sum.Currency())
	}
	// Operands are values, not pointers — neither may be mutated.
	if first.Minor() != 1999 || second.Minor() != 1 {
		t.Fatalf("Add() mutated an operand: %d, %d", first.Minor(), second.Minor())
	}
}

func TestAddRejectsMixedCurrencies(t *testing.T) {
	naira, _ := New(1999, CurrencyNGN)
	dollars, _ := New(1999, CurrencyUSD)

	if _, err := naira.Add(dollars); err == nil {
		t.Fatal("Add() summed two different currencies — there is no exchange rate in this system to make that meaningful")
	} else {
		assertValidationFailed(t, err, "Add(mixed currencies)")
	}
}

func TestAddRejectsOverflowRatherThanWrapping(t *testing.T) {
	large, _ := New(math.MaxInt64, CurrencyNGN)
	one, _ := New(1, CurrencyNGN)

	if _, err := large.Add(one); err == nil {
		t.Fatal("Add() wrapped on overflow instead of refusing")
	} else {
		assertValidationFailed(t, err, "Add(overflow)")
	}
}

func TestFormatUsesExactIntegerArithmetic(t *testing.T) {
	tests := []struct {
		minor    int64
		currency Currency
		want     string
	}{
		{0, CurrencyNGN, "NGN 0.00"},
		{5, CurrencyNGN, "NGN 0.05"},
		{50, CurrencyNGN, "NGN 0.50"},
		{100, CurrencyUSD, "USD 1.00"},
		{1999, CurrencyNGN, "NGN 19.99"},
		{1000000, CurrencyGBP, "GBP 10000.00"},
		// Beyond 2^53: a float64 cannot represent this value exactly, so a
		// formatter that converted to floating point would print the wrong
		// number here. Exact output is the proof that it does not.
		{9007199254740993, CurrencyEUR, "EUR 90071992547409.93"},
	}

	for _, test := range tests {
		amount, err := New(test.minor, test.currency)
		if err != nil {
			t.Fatal(err)
		}
		if got := amount.Format(); got != test.want {
			t.Fatalf("Format(%d %s) = %q, want %q", test.minor, test.currency, got, test.want)
		}
		if got := amount.String(); got != test.want {
			t.Fatalf("String(%d %s) = %q, want %q", test.minor, test.currency, got, test.want)
		}
	}
}
