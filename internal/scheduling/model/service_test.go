package model

import (
	"errors"
	"strings"
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

func TestValidateNameTrimsSurroundingWhitespace(t *testing.T) {
	// A service name is free-form human text, so it is trimmed rather than
	// rejected — unlike slug, business type and currency, which are controlled
	// identifiers this codebase refuses to normalize.
	name, err := ValidateName("  Gel Manicure  ")
	if err != nil {
		t.Fatalf("ValidateName() error = %v", err)
	}
	if name != "Gel Manicure" {
		t.Fatalf("ValidateName() = %q, want %q", name, "Gel Manicure")
	}
}

func TestValidateNameRejectsEmptyAndWhitespaceOnly(t *testing.T) {
	for _, candidate := range []string{"", " ", "\t", "\n  \t"} {
		if _, err := ValidateName(candidate); err == nil {
			t.Fatalf("ValidateName(%q) accepted an empty name", candidate)
		} else {
			assertValidationFailed(t, err, "ValidateName(empty)")
		}
	}
}

func TestValidateNameBounds(t *testing.T) {
	atLimit := strings.Repeat("a", MaxNameLength)
	if _, err := ValidateName(atLimit); err != nil {
		t.Fatalf("ValidateName(%d chars) error = %v, want accepted at the limit", MaxNameLength, err)
	}

	overLimit := strings.Repeat("a", MaxNameLength+1)
	if _, err := ValidateName(overLimit); err == nil {
		t.Fatalf("ValidateName(%d chars) accepted a name over the limit", MaxNameLength+1)
	} else {
		assertValidationFailed(t, err, "ValidateName(too long)")
	}
}

func TestValidateDescriptionLeavesNilAsNil(t *testing.T) {
	description, err := ValidateDescription(nil)
	if err != nil {
		t.Fatalf("ValidateDescription(nil) error = %v", err)
	}
	if description != nil {
		t.Fatalf("ValidateDescription(nil) = %v, want nil — an omitted description is not the same as an empty one", *description)
	}
}

func TestValidateDescriptionKeepsAnExplicitEmptyString(t *testing.T) {
	// "cleared to empty" and "never set" stay distinguishable: an explicitly
	// supplied blank description becomes "", not nil.
	blank := "   "
	description, err := ValidateDescription(&blank)
	if err != nil {
		t.Fatalf("ValidateDescription() error = %v", err)
	}
	if description == nil {
		t.Fatal("ValidateDescription(\"   \") returned nil, want a pointer to an empty string")
	}
	if *description != "" {
		t.Fatalf("ValidateDescription(\"   \") = %q, want %q", *description, "")
	}
}

func TestValidateDescriptionBounds(t *testing.T) {
	atLimit := strings.Repeat("a", MaxDescriptionLength)
	if _, err := ValidateDescription(&atLimit); err != nil {
		t.Fatalf("ValidateDescription(%d chars) error = %v, want accepted at the limit", MaxDescriptionLength, err)
	}

	overLimit := strings.Repeat("a", MaxDescriptionLength+1)
	if _, err := ValidateDescription(&overLimit); err == nil {
		t.Fatalf("ValidateDescription(%d chars) accepted a description over the limit", MaxDescriptionLength+1)
	} else {
		assertValidationFailed(t, err, "ValidateDescription(too long)")
	}
}

func TestValidateDurationMinutesRejectsZeroAndNegative(t *testing.T) {
	// Zero is refused deliberately: a zero-duration service produces an empty
	// time range, which the exclusion constraint planned for S10 would never
	// detect a conflict on.
	for _, minutes := range []int{0, -1, -60} {
		if err := ValidateDurationMinutes(minutes); err == nil {
			t.Fatalf("ValidateDurationMinutes(%d) accepted a non-positive duration", minutes)
		} else {
			assertValidationFailed(t, err, "ValidateDurationMinutes(non-positive)")
		}
	}
}

func TestValidateDurationMinutesBounds(t *testing.T) {
	for _, minutes := range []int{MinDurationMinutes, 30, 90, MaxDurationMinutes} {
		if err := ValidateDurationMinutes(minutes); err != nil {
			t.Fatalf("ValidateDurationMinutes(%d) error = %v, want accepted", minutes, err)
		}
	}
	if err := ValidateDurationMinutes(MaxDurationMinutes + 1); err == nil {
		t.Fatalf("ValidateDurationMinutes(%d) accepted a duration over the limit", MaxDurationMinutes+1)
	} else {
		assertValidationFailed(t, err, "ValidateDurationMinutes(too long)")
	}
}

func TestValidatePriceMinorAllowsZero(t *testing.T) {
	// A free consultation or patch test is a real, offered service.
	if err := ValidatePriceMinor(0); err != nil {
		t.Fatalf("ValidatePriceMinor(0) error = %v, want a free service accepted", err)
	}
}

func TestValidatePriceMinorRejectsNegative(t *testing.T) {
	for _, price := range []int64{-1, -1999} {
		if err := ValidatePriceMinor(price); err == nil {
			t.Fatalf("ValidatePriceMinor(%d) accepted a negative price", price)
		} else {
			assertValidationFailed(t, err, "ValidatePriceMinor(negative)")
		}
	}
}

func TestValidatePriceMinorBounds(t *testing.T) {
	if err := ValidatePriceMinor(MaxPriceMinor); err != nil {
		t.Fatalf("ValidatePriceMinor(%d) error = %v, want accepted at the limit", MaxPriceMinor, err)
	}
	if err := ValidatePriceMinor(MaxPriceMinor + 1); err == nil {
		t.Fatalf("ValidatePriceMinor(%d) accepted a price over the limit", MaxPriceMinor+1)
	} else {
		assertValidationFailed(t, err, "ValidatePriceMinor(too large)")
	}
}

func TestStatusVocabularyIsActiveAndArchived(t *testing.T) {
	// ARCHIVED rather than DISABLED is a deliberate choice: DISABLED means "an
	// actor is barred from acting" everywhere else in this codebase, and a
	// service is not an actor.
	if StatusActive != "ACTIVE" {
		t.Fatalf("StatusActive = %q, want ACTIVE", StatusActive)
	}
	if StatusArchived != "ARCHIVED" {
		t.Fatalf("StatusArchived = %q, want ARCHIVED", StatusArchived)
	}
}
