package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
)

// IntervalInput carries one transport-validated working-hour interval from a
// PUT request. TenantID and StaffID are absent — they come from the route and
// the trusted tenant context, never from the request body, the same
// structural protection every other write endpoint here relies on.
type IntervalInput struct {
	DayOfWeek string
	StartTime string
	EndTime   string
}

// StaffReader is the narrow slice of staff persistence this module needs to
// confirm a schedule's staff member exists in this tenant before reading or
// replacing their hours. Declared here, in the consumer, rather than
// importing the whole StaffRepository — the same interface-segregation
// reasoning behind MembershipReader.
type StaffReader interface {
	FindByID(ctx context.Context, tenantID string, staffID string) (*model.StaffProfile, error)
}

// WorkingHoursService owns the working-hours rules: staff/tenant ownership,
// interval validation, overlap and duplicate detection, and atomic weekly
// replacement.
//
// This is authentication-adjacent, never availability: it records recurring
// wall-clock hours only. Slot generation, booking conflicts, breaks,
// holidays and buffer times are later features and are not computed here.
//
// Tenant access and the staff.read / staff.update permissions are verified by
// the production middleware chain before any method here is reached. This
// service does not re-derive authorization; it does scope every repository
// call by tenantID, so a defect in that chain cannot become a cross-tenant
// read or write.
type WorkingHoursService interface {
	// List returns the staff member's complete schedule, sorted by day of
	// week then start time. A staff member with no configured hours yet
	// returns a successful empty list, never a 404 — only a nonexistent (or
	// cross-tenant) staff profile is STAFF_NOT_FOUND.
	List(ctx context.Context, tenantID string, staffID string) ([]*model.WorkingHourInterval, error)
	// ReplaceWeeklySchedule sets the complete weekly schedule atomically.
	// Either every requested interval persists or none does — a set
	// containing one invalid or overlapping interval leaves the previous
	// schedule entirely intact.
	ReplaceWeeklySchedule(ctx context.Context, tenantID string, staffID string, intervals []IntervalInput) ([]*model.WorkingHourInterval, error)
}

type workingHoursService struct {
	db    txBeginner
	hours repository.WorkingHoursRepository
	staff StaffReader
}

func NewWorkingHoursService(db txBeginner, hours repository.WorkingHoursRepository, staff StaffReader) WorkingHoursService {
	return &workingHoursService{db: db, hours: hours, staff: staff}
}

func (s *workingHoursService) List(ctx context.Context, tenantID string, staffID string) ([]*model.WorkingHourInterval, error) {
	if err := validateStaffIdentifiers(tenantID, staffID); err != nil {
		return nil, err
	}
	// Resolved through the staff repository first so an unknown or foreign
	// staff ID yields STAFF_NOT_FOUND rather than an empty schedule, which
	// would wrongly imply the profile exists with no hours configured.
	if _, err := s.staff.FindByID(ctx, tenantID, staffID); err != nil {
		return nil, err
	}
	return s.hours.ListByStaff(ctx, tenantID, staffID)
}

// ReplaceWeeklySchedule validates the ENTIRE incoming set before any write —
// staff/tenant ownership, then every interval, then overlap and duplicate
// detection across the whole set — so a rejected replacement never opens a
// transaction at all, let alone leaves it partially applied. Only once
// validation has fully succeeded does the transaction run: delete the
// previous schedule, insert every validated interval, commit. Any failure
// inside rolls back, leaving the previous schedule exactly as it was.
func (s *workingHoursService) ReplaceWeeklySchedule(ctx context.Context, tenantID string, staffID string, intervals []IntervalInput) ([]*model.WorkingHourInterval, error) {
	if err := validateStaffIdentifiers(tenantID, staffID); err != nil {
		return nil, err
	}
	if _, err := s.staff.FindByID(ctx, tenantID, staffID); err != nil {
		return nil, err
	}

	candidates := make([]model.WorkingHourInterval, 0, len(intervals))
	for _, input := range intervals {
		candidates = append(candidates, model.WorkingHourInterval{
			DayOfWeek: model.DayOfWeek(input.DayOfWeek),
			StartTime: input.StartTime,
			EndTime:   input.EndTime,
		})
	}
	validated, err := model.ValidateWeeklySchedule(candidates)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("starting working hours replacement transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	hours := repository.NewPostgresWorkingHoursRepository(tx)
	if err := hours.DeleteAllForStaff(ctx, tenantID, staffID); err != nil {
		return nil, err
	}
	for _, interval := range validated {
		if _, err := hours.Create(ctx, &model.WorkingHourInterval{
			ID:        uuid.NewString(),
			TenantID:  tenantID,
			StaffID:   staffID,
			DayOfWeek: interval.DayOfWeek,
			StartTime: interval.StartTime,
			EndTime:   interval.EndTime,
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing working hours replacement: %w", err)
	}
	committed = true

	// Read back through the non-transactional repository so the response
	// reflects committed state rather than the caller's request echoed back.
	return s.hours.ListByStaff(ctx, tenantID, staffID)
}

// compile-time guard: the implementation must keep satisfying its interface.
var _ WorkingHoursService = (*workingHoursService)(nil)
