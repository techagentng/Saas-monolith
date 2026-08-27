package model

import (
	"strings"
	"testing"
)

func TestValidateDisplayNameTrimsSurroundingWhitespace(t *testing.T) {
	name, err := ValidateDisplayName("  Ada  ")
	if err != nil {
		t.Fatalf("ValidateDisplayName() error = %v", err)
	}
	if name != "Ada" {
		t.Fatalf("ValidateDisplayName() = %q, want %q", name, "Ada")
	}
}

func TestValidateDisplayNameRejectsEmptyAndWhitespaceOnly(t *testing.T) {
	for _, candidate := range []string{"", " ", "\t", "\n  \t"} {
		if _, err := ValidateDisplayName(candidate); err == nil {
			t.Fatalf("ValidateDisplayName(%q) accepted an empty name", candidate)
		} else {
			assertValidationFailed(t, err, "ValidateDisplayName(empty)")
		}
	}
}

func TestValidateDisplayNameBounds(t *testing.T) {
	atLimit := strings.Repeat("a", MaxDisplayNameLength)
	if _, err := ValidateDisplayName(atLimit); err != nil {
		t.Fatalf("ValidateDisplayName(%d chars) error = %v, want accepted at the limit", MaxDisplayNameLength, err)
	}
	overLimit := strings.Repeat("a", MaxDisplayNameLength+1)
	if _, err := ValidateDisplayName(overLimit); err == nil {
		t.Fatalf("ValidateDisplayName(%d chars) accepted a name over the limit", MaxDisplayNameLength+1)
	} else {
		assertValidationFailed(t, err, "ValidateDisplayName(too long)")
	}
}

// Two technicians called "Ada" is a normal occurrence in a real salon, so the
// model imposes no uniqueness of its own — proven here by validating the same
// name twice rather than by asserting the absence of a rule.
func TestValidateDisplayNameHasNoUniquenessRule(t *testing.T) {
	first, err := ValidateDisplayName("Ada")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ValidateDisplayName("Ada")
	if err != nil {
		t.Fatalf("a second identical display name was rejected: %v", err)
	}
	if first != second {
		t.Fatalf("identical names validated differently: %q vs %q", first, second)
	}
}

func TestValidateBioLeavesNilAsNil(t *testing.T) {
	bio, err := ValidateBio(nil)
	if err != nil {
		t.Fatalf("ValidateBio(nil) error = %v", err)
	}
	if bio != nil {
		t.Fatalf("ValidateBio(nil) = %v, want nil — an omitted bio is not an empty one", *bio)
	}
}

func TestValidateBioKeepsAnExplicitEmptyString(t *testing.T) {
	blank := "   "
	bio, err := ValidateBio(&blank)
	if err != nil {
		t.Fatalf("ValidateBio() error = %v", err)
	}
	if bio == nil {
		t.Fatal("ValidateBio(\"   \") returned nil, want a pointer to an empty string")
	}
	if *bio != "" {
		t.Fatalf("ValidateBio(\"   \") = %q, want %q", *bio, "")
	}
}

func TestValidateBioBounds(t *testing.T) {
	atLimit := strings.Repeat("a", MaxBioLength)
	if _, err := ValidateBio(&atLimit); err != nil {
		t.Fatalf("ValidateBio(%d chars) error = %v, want accepted at the limit", MaxBioLength, err)
	}
	overLimit := strings.Repeat("a", MaxBioLength+1)
	if _, err := ValidateBio(&overLimit); err == nil {
		t.Fatalf("ValidateBio(%d chars) accepted a bio over the limit", MaxBioLength+1)
	} else {
		assertValidationFailed(t, err, "ValidateBio(too long)")
	}
}

// A staff profile reuses the catalog's ACTIVE/ARCHIVED vocabulary rather than
// inventing INVITED, SUSPENDED or ON_LEAVE — none of which any approved feature
// produces or consumes yet.
func TestStaffProfileUsesTheSharedStatusVocabulary(t *testing.T) {
	profile := StaffProfile{Status: StatusActive}
	if profile.Status != "ACTIVE" {
		t.Fatalf("StatusActive = %q, want ACTIVE", profile.Status)
	}
	profile.Status = StatusArchived
	if profile.Status != "ARCHIVED" {
		t.Fatalf("StatusArchived = %q, want ARCHIVED", profile.Status)
	}
}

// is_bookable and status answer different questions, and every combination is
// meaningful — which is exactly why they are not collapsed into one field.
func TestBookabilityAndStatusAreIndependent(t *testing.T) {
	cases := []struct {
		name       string
		status     Status
		isBookable bool
		meaning    string
	}{
		{"active and bookable", StatusActive, true, "a technician currently taking appointments"},
		{"active but not bookable", StatusActive, false, "a receptionist, or someone not taking appointments right now"},
		{"archived", StatusArchived, false, "someone who no longer works here"},
		{"archived but still flagged bookable", StatusArchived, true, "stale flag on a departed profile; status must win downstream"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			profile := StaffProfile{Status: test.status, IsBookable: test.isBookable}
			if profile.Status != test.status || profile.IsBookable != test.isBookable {
				t.Fatalf("the model collapsed an independent field: %+v (%s)", profile, test.meaning)
			}
		})
	}
}
