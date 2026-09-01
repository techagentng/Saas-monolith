package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// StaffUpdate carries a partial update. Only non-nil fields are written.
//
// The absent fields are the enforcement: there is no Status (archiving owns
// lifecycle and is its own operation), no TenantID (a profile never moves
// between tenants), no UserID (re-pointing a profile at a different person is a
// re-identification, not an edit — it is deliberately not part of S3's contract),
// no ID and no timestamps.
type StaffUpdate struct {
	DisplayName *string
	Bio         *string
	IsBookable  *bool
}

// IsEmpty reports whether no field is set for update.
func (u *StaffUpdate) IsEmpty() bool {
	return u.DisplayName == nil && u.Bio == nil && u.IsBookable == nil
}

// StaffListFilter narrows a roster listing.
type StaffListFilter struct {
	// Status nil means every status. A concrete value restricts the listing to
	// it.
	Status *model.Status
}

// StaffRepository is the persistence boundary for staff profiles.
//
// Every method takes tenantID, and every implementation must filter on it — the
// same isolation mechanism ServiceRepository uses. A profile belonging to
// another tenant is reported exactly as a nonexistent one, so the API cannot be
// used to discover which staff IDs exist elsewhere on the platform.
type StaffRepository interface {
	Create(ctx context.Context, profile *model.StaffProfile) (*model.StaffProfile, error)
	FindByID(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error)
	ListByTenant(ctx context.Context, tenantID string, filter StaffListFilter) ([]*model.StaffProfile, error)
	Update(ctx context.Context, tenantID string, staffID string, update StaffUpdate) (*model.StaffProfile, error)
	// Archive sets status to ARCHIVED unconditionally at the SQL level. Whether
	// the profile was already archived is the service layer's decision, the same
	// division CatalogService.Archive already uses.
	Archive(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error)
}

// CapabilityRepository persists which services a staff member can perform.
//
// There is no Unassign method: capability is replaced as a whole set
// (DeleteAll then Assign, inside one caller-owned transaction), because the
// owner-facing operation is "these are the services this technician performs",
// not a stream of individual toggles. Partial reconciliation would be a second
// way to express the same thing, with its own ordering bugs.
type CapabilityRepository interface {
	// ListServiceIDs returns the service IDs this staff member can perform, in
	// a deterministic order.
	ListServiceIDs(ctx context.Context, tenantID string, staffID string) ([]string, error)
	// DeleteAll removes every capability row for one staff member.
	DeleteAll(ctx context.Context, tenantID string, staffID string) error
	// Assign records one capability. The composite foreign keys on the table
	// mean a cross-tenant pairing is refused by the database itself, whatever
	// the caller believes.
	Assign(ctx context.Context, tenantID string, staffID string, serviceID string) error
}
