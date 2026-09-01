package model

import (
	"strings"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// Bounds are mirrored by the column widths in migration 000012. Validating in
// both places is intentional, the same division service.go already uses: the
// service layer produces a presentable VALIDATION_FAILED, while the schema means
// no writer can bypass the rule.
const (
	MaxDisplayNameLength = 255
	MaxBioLength         = 1000
)

// StaffProfile is a person a tenant can schedule work against.
//
// It is deliberately not an authorization concept. The STAFF role answers "what
// may this person do in the workspace"; this answers "is this person a
// schedulable resource". The two are orthogonal: a BUSINESS_OWNER who performs
// services has a profile without being demoted, and a technician who never signs
// in has a profile with no user at all.
//
// Status reuses the ACTIVE/ARCHIVED vocabulary already defined in service.go for
// the catalog — the same reasoning applies (a profile is archived, not
// "disabled", because DISABLED means "an actor is barred from acting"
// everywhere else in this codebase).
type StaffProfile struct {
	ID       string
	TenantID string
	// UserID is nil for a non-login worker. When set, it names an existing
	// platform user who holds an ACTIVE membership in this tenant — verified at
	// write time only. A link grants no permission of any kind: it is a
	// reference, never an authorization fact.
	UserID      *string
	DisplayName string
	// Bio is nil when never supplied. An empty string is a distinct, legitimate
	// state, matching how the catalog already treats a service description.
	Bio *string
	// IsBookable is independent of Status. An ACTIVE profile that is not
	// bookable is a real, useful state (a receptionist, or a technician on a
	// break from taking appointments); an ARCHIVED profile is someone who no
	// longer works here.
	IsBookable bool
	Status     Status
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ValidateDisplayName trims and bounds the name customers and schedulers see.
//
// Trimming rather than rejecting padded input follows the same rule the catalog
// uses for a service name: free-form human text is trimmed, while controlled
// identifiers (slug, business type, currency) are rejected rather than
// normalized.
//
// There is no uniqueness rule. Two technicians called "Ada" are a normal
// occurrence in a real salon, and a uniqueness constraint would force the second
// one to be misnamed.
func ValidateDisplayName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", apperrors.New(apperrors.CodeValidationFailed, "staff display name is required", nil)
	}
	if len(name) > MaxDisplayNameLength {
		return "", apperrors.New(apperrors.CodeValidationFailed, "staff display name exceeds maximum length", nil)
	}
	return name, nil
}

// ValidateBio trims and bounds an optional biography. A nil input stays nil (the
// field was not supplied); a supplied value that trims to empty is kept as an
// empty string, so "cleared" and "never set" remain distinguishable.
func ValidateBio(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	bio := strings.TrimSpace(*value)
	if len(bio) > MaxBioLength {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "staff bio exceeds maximum length", nil)
	}
	return &bio, nil
}
