package model

import (
	"strings"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// MaxCategoryNameLength mirrors the column width in migration 000019.
// Validating in both places is intentional, the same division service.go uses:
// the service layer produces a presentable VALIDATION_FAILED, while the schema
// means no writer can bypass the rule.
const MaxCategoryNameLength = 120

// ServiceCategory groups a tenant's services for the owner dashboard and the
// public booking catalogue.
//
// It is tenant data. Two salons may both call a category "Pedicures"; neither
// can see the other's, and the composite foreign key on services makes filing
// a service under a foreign category impossible at the schema level.
//
// It is NOT the platform suggestion catalogue. That lives in
// internal/scheduling/suggestions, is static, and is never persisted — a
// suggestion is a template the owner copies, after which the tenant owns the
// copy outright.
//
// There is deliberately no ParentID: SC1 is one level, Category -> Service.
type ServiceCategory struct {
	ID       string
	TenantID string
	Name     string
	// SortOrder is the owner's display order. Ties break on name, so a
	// tenant that never sets one still gets a stable alphabetical listing.
	SortOrder int
	// Status reuses the ACTIVE/ARCHIVED vocabulary from service.go, for the
	// same reason staff.go does: a category is a catalog entry, not an actor,
	// so ARCHIVED (not DISABLED) is the right word for "retired". Archiving a
	// category hides it (and its grouping) from the public catalog without
	// touching the services filed under it — they keep their CategoryID and
	// stay individually bookable. This is also the escape hatch for the FK's
	// "no ON DELETE action" rule: a category with services can never be
	// deleted, but it can always be archived.
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ValidateCategoryName trims and bounds a category name.
//
// Trimming rather than rejecting padded input follows the rule the rest of the
// module uses for free-form human text (service name, staff display name),
// as opposed to controlled identifiers (slug, business type, currency) which
// are rejected rather than normalized. Case is preserved: "Add-Ons" and
// "add-ons" are different names, and the unique constraint treats them as such.
func ValidateCategoryName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", apperrors.New(apperrors.CodeValidationFailed, "category name is required", nil)
	}
	if len(name) > MaxCategoryNameLength {
		return "", apperrors.New(apperrors.CodeValidationFailed, "category name exceeds maximum length", nil)
	}
	return name, nil
}
