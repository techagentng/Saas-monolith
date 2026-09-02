package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// WorkingHoursRepository is the persistence boundary for a staff member's
// recurring weekly working hours.
//
// There is no Update: an interval is replaced as a whole schedule (DeleteAll
// then Create for each surviving interval, inside one caller-owned
// transaction), the same "the owner-facing operation is the complete set, not
// a stream of individual toggles" reasoning CapabilityRepository already
// applies to staff_services. Every method takes tenantID, and every
// implementation must filter on it — a schedule belonging to another tenant
// is reported exactly as an empty one, never disclosed.
type WorkingHoursRepository interface {
	// ListByStaff returns every interval for one staff member, ordered by day
	// of week then start time — the same deterministic ordering the API
	// response guarantees.
	ListByStaff(ctx context.Context, tenantID string, staffID string) ([]*model.WorkingHourInterval, error)
	// DeleteAllForStaff removes every interval for one staff member. Scoped by
	// both staffID and tenantID, so it can never affect another staff member's
	// schedule even if called with a mismatched pair.
	DeleteAllForStaff(ctx context.Context, tenantID string, staffID string) error
	// Create inserts one interval. The composite foreign key on
	// staff_working_hours means a staffID/tenantID pair naming a different
	// tenant's staff profile is refused by the database itself.
	Create(ctx context.Context, interval *model.WorkingHourInterval) (*model.WorkingHourInterval, error)
}
