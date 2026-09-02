package service

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// fakeWorkingHoursRepository is a WorkingHoursRepository over an in-memory
// map. It exists purely to prove ReplaceWeeklySchedule's validation happens
// before ANY repository call — real atomicity is proven against a real
// transaction in working_hours_integration_test.go.
type fakeWorkingHoursRepository struct {
	byStaff map[string][]*model.WorkingHourInterval
}

func newFakeWorkingHoursRepository() *fakeWorkingHoursRepository {
	return &fakeWorkingHoursRepository{byStaff: map[string][]*model.WorkingHourInterval{}}
}

func (r *fakeWorkingHoursRepository) ListByStaff(_ context.Context, _ string, staffID string) ([]*model.WorkingHourInterval, error) {
	return append([]*model.WorkingHourInterval{}, r.byStaff[staffID]...), nil
}

func (r *fakeWorkingHoursRepository) DeleteAllForStaff(_ context.Context, _ string, staffID string) error {
	delete(r.byStaff, staffID)
	return nil
}

func (r *fakeWorkingHoursRepository) Create(_ context.Context, interval *model.WorkingHourInterval) (*model.WorkingHourInterval, error) {
	r.byStaff[interval.StaffID] = append(r.byStaff[interval.StaffID], interval)
	return interval, nil
}

// workingHoursFixture wires WorkingHoursService over fakes and nilTxBeginner
// — every test here that reaches BeginTx fails loudly, which is itself the
// property under test: validation must happen before a transaction opens.
type workingHoursUnitFixture struct {
	hours *fakeWorkingHoursRepository
	staff *fakeStaffRepository
	svc   WorkingHoursService
}

func newWorkingHoursUnitFixture() *workingHoursUnitFixture {
	staff := newFakeStaffRepository()
	hours := newFakeWorkingHoursRepository()
	return &workingHoursUnitFixture{
		hours: hours,
		staff: staff,
		svc:   NewWorkingHoursService(nilTxBeginner{}, hours, staff),
	}
}

func activeStaffProfile(id, tenantID string) *model.StaffProfile {
	return &model.StaffProfile{ID: id, TenantID: tenantID, DisplayName: "Ada", IsBookable: true, Status: model.StatusActive}
}

// --- List --------------------------------------------------------------------

func TestListRejectsAnUnknownStaffMemberRatherThanReturningEmpty(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()

	_, err := fixture.svc.List(context.Background(), tenantA, staffID)
	assertCode(t, err, apperrors.CodeStaffNotFound, "List(unknown staff)")
}

func TestListRejectsAnotherTenantsStaffMember(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)

	_, err := fixture.svc.List(context.Background(), tenantB, staffID)
	assertCode(t, err, apperrors.CodeStaffNotFound, "List(cross-tenant staff)")
}

// A staff member with no configured hours yet is a successful empty list.
func TestListReturnsAnEmptyScheduleForAKnownStaffMemberWithNoHours(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)

	result, err := fixture.svc.List(context.Background(), tenantA, staffID)
	if err != nil {
		t.Fatalf("List() error = %v, want a successful empty schedule", err)
	}
	if len(result) != 0 {
		t.Fatalf("List() = %v, want empty", result)
	}
}

// --- ReplaceWeeklySchedule: staff/tenant ownership ----------------------------

func TestReplaceWeeklyScheduleRejectsAnUnknownStaffMember(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()

	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantA, staffID, nil)
	assertCode(t, err, apperrors.CodeStaffNotFound, "ReplaceWeeklySchedule(unknown staff)")
}

func TestReplaceWeeklyScheduleRejectsAnotherTenantsStaffMember(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)

	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantB, staffID, nil)
	assertCode(t, err, apperrors.CodeStaffNotFound, "ReplaceWeeklySchedule(cross-tenant staff)")
}

func TestReplaceWeeklyScheduleRejectsAMalformedTenantOrStaffID(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()

	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), "not-a-uuid", staffID, nil)
	assertCode(t, err, apperrors.CodeInvalidRequest, "ReplaceWeeklySchedule(malformed tenant id)")

	_, err = fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantA, "not-a-uuid", nil)
	assertCode(t, err, apperrors.CodeInvalidRequest, "ReplaceWeeklySchedule(malformed staff id)")
}

// --- ReplaceWeeklySchedule: interval validation, before any write ------------

func TestReplaceWeeklyScheduleRejectsAnInvalidIntervalBeforeWriting(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)
	fixture.hours.byStaff[staffID] = []*model.WorkingHourInterval{
		{TenantID: tenantA, StaffID: staffID, DayOfWeek: model.Monday, StartTime: "09:00", EndTime: "17:00"},
	}

	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantA, staffID, []IntervalInput{
		{DayOfWeek: "MONDAY", StartTime: "17:00", EndTime: "09:00"}, // start after end
	})
	if err == nil {
		t.Fatal("ReplaceWeeklySchedule() accepted start > end")
	}
	assertCode(t, err, apperrors.CodeValidationFailed, "ReplaceWeeklySchedule(start > end)")
	// nilTxBeginner would panic/error differently had a transaction opened;
	// reaching VALIDATION_FAILED here proves the check happens first. The
	// previous schedule must also be untouched.
	if len(fixture.hours.byStaff[staffID]) != 1 {
		t.Fatalf("a rejected replacement disturbed the existing schedule: %+v", fixture.hours.byStaff[staffID])
	}
}

func TestReplaceWeeklyScheduleRejectsOverlappingIntervalsBeforeWriting(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)

	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantA, staffID, []IntervalInput{
		{DayOfWeek: "MONDAY", StartTime: "09:00", EndTime: "13:00"},
		{DayOfWeek: "MONDAY", StartTime: "12:00", EndTime: "17:00"},
	})
	assertCode(t, err, apperrors.CodeValidationFailed, "ReplaceWeeklySchedule(overlap)")
}

func TestReplaceWeeklyScheduleRejectsDuplicateIntervalsBeforeWriting(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)

	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantA, staffID, []IntervalInput{
		{DayOfWeek: "MONDAY", StartTime: "09:00", EndTime: "17:00"},
		{DayOfWeek: "MONDAY", StartTime: "09:00", EndTime: "17:00"},
	})
	assertCode(t, err, apperrors.CodeValidationFailed, "ReplaceWeeklySchedule(duplicate)")
}

func TestReplaceWeeklyScheduleRejectsAnInvalidDay(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)

	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantA, staffID, []IntervalInput{
		{DayOfWeek: "FUNDAY", StartTime: "09:00", EndTime: "17:00"},
	})
	assertCode(t, err, apperrors.CodeValidationFailed, "ReplaceWeeklySchedule(invalid day)")
}

func TestReplaceWeeklyScheduleRejectsAMalformedTime(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)

	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantA, staffID, []IntervalInput{
		{DayOfWeek: "MONDAY", StartTime: "9am", EndTime: "17:00"},
	})
	assertCode(t, err, apperrors.CodeValidationFailed, "ReplaceWeeklySchedule(malformed time)")
}

// Touching boundaries are accepted at the service layer too — the same rule
// model.ValidateWeeklySchedule documents, exercised here through the public
// service entry point rather than the model function directly.
func TestReplaceWeeklyScheduleAcceptsTouchingBoundaries(t *testing.T) {
	fixture := newWorkingHoursUnitFixture()
	fixture.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)

	// nilTxBeginner intentionally makes this fail once it passes validation —
	// proving acceptance requires reaching BeginTx is itself the assertion
	// this test relies on being exercised at the integration level instead;
	// here we only assert this is NOT rejected as a validation failure.
	_, err := fixture.svc.ReplaceWeeklySchedule(context.Background(), tenantA, staffID, []IntervalInput{
		{DayOfWeek: "MONDAY", StartTime: "09:00", EndTime: "12:00"},
		{DayOfWeek: "MONDAY", StartTime: "12:00", EndTime: "17:00"},
	})
	if err == nil {
		t.Fatal("expected nilTxBeginner to fail once validation passes")
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.Code == apperrors.CodeValidationFailed {
		t.Fatalf("touching boundaries were rejected as a validation failure: %v", err)
	}
}
