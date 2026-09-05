package service

import (
	"context"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// mondayDate is a Monday, so a working-hours row for MONDAY applies to it.
const mondayDate = "2026-09-07"

// fixedClock is the deterministic Clock these tests inject in place of
// SystemClock.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// year2000 is well before any date under test, so the past-slot filter is
// inert unless a test overrides the clock.
var year2000 = fixedClock{now: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}

// fakeOccupancy is an OccupancyReader over a fixed set of intervals, used only
// by the occupied-interval tests. Everywhere else NoOccupancy is wired, which
// is what S7 ships.
type fakeOccupancy struct {
	intervals []availability.OccupiedInterval
	calls     int
	from, to  time.Time
}

func (f *fakeOccupancy) OccupiedIntervals(_ context.Context, _ string, _ string, from, to time.Time) ([]availability.OccupiedInterval, error) {
	f.calls++
	f.from, f.to = from, to
	return f.intervals, nil
}

type availabilityFixture struct {
	tenants      *fakeTenantReader
	services     *fakeServiceRepository
	staff        *fakeStaffRepository
	capabilities *fakeCapabilityRepository
	hours        *fakeWorkingHoursRepository
	occupancy    OccupancyReader
	clock        Clock
	svc          AvailabilityService
}

func (f *availabilityFixture) build() {
	f.svc = NewAvailabilityService(f.tenants, f.services, f.staff, f.capabilities, f.hours, f.occupancy, f.clock)
}

// newAvailabilityFixture wires a coherent happy-path scenario: tenant A in
// Africa/Lagos, one 30-minute active service, one active bookable technician
// assigned that service, working MONDAY 09:00-12:00.
func newAvailabilityFixture() *availabilityFixture {
	lagos := "Africa/Lagos"
	f := &availabilityFixture{
		tenants:      &fakeTenantReader{tenant: &tenantmodel.Tenant{ID: tenantA, Name: "Acme Nails", Slug: "acme-nails", Status: tenantmodel.StatusActive, Timezone: &lagos}},
		services:     newFakeServiceRepository(),
		staff:        newFakeStaffRepository(),
		capabilities: newFakeCapabilityRepository(),
		hours:        newFakeWorkingHoursRepository(),
		occupancy:    NoOccupancy{},
		clock:        year2000,
	}
	f.services.services[serviceID] = &model.Service{
		ID: serviceID, TenantID: tenantA, Name: "Gel Manicure",
		DurationMinutes: 30, PriceMinor: 1999, Status: model.StatusActive,
	}
	f.staff.profiles[staffID] = activeStaffProfile(staffID, tenantA)
	f.capabilities.assignments[staffID] = []string{serviceID}
	f.hours.byStaff[staffID] = []*model.WorkingHourInterval{
		{TenantID: tenantA, StaffID: staffID, DayOfWeek: model.Monday, StartTime: "09:00", EndTime: "12:00"},
	}
	f.build()
	return f
}

func startsOf(result *AvailabilityResult) []string {
	out := make([]string, len(result.Slots))
	for i, s := range result.Slots {
		out[i] = s.Start
	}
	return out
}

func assertStarts(t *testing.T, result *AvailabilityResult, want ...string) {
	t.Helper()
	got := startsOf(result)
	if len(got) != len(want) {
		t.Fatalf("slot starts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot starts = %v, want %v", got, want)
		}
	}
}

// --- happy path -----------------------------------------------------------

func TestGetAvailabilityComputesSlotsForAnAssignedTechnician(t *testing.T) {
	f := newAvailabilityFixture()

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	if result.Timezone != "Africa/Lagos" {
		t.Fatalf("timezone = %q, want Africa/Lagos", result.Timezone)
	}
	if result.Date != mondayDate || result.ServiceID != serviceID || result.StaffID != staffID {
		t.Fatalf("echoed context wrong: %+v", result)
	}
	assertStarts(t, result, "09:00", "09:30", "10:00", "10:30", "11:00", "11:30")
}

func TestGetAvailabilityIsDeterministic(t *testing.T) {
	f := newAvailabilityFixture()
	f.hours.byStaff[staffID] = []*model.WorkingHourInterval{
		{TenantID: tenantA, StaffID: staffID, DayOfWeek: model.Monday, StartTime: "13:00", EndTime: "15:00"},
		{TenantID: tenantA, StaffID: staffID, DayOfWeek: model.Monday, StartTime: "09:00", EndTime: "10:00"},
	}

	for i := 0; i < 5; i++ {
		result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
		if err != nil {
			t.Fatalf("GetAvailability: %v", err)
		}
		assertStarts(t, result, "09:00", "09:30", "13:00", "13:30", "14:00", "14:30")
	}
}

// --- identifier / date validation --------------------------------------

func TestGetAvailabilityRejectsMalformedIdentifiers(t *testing.T) {
	f := newAvailabilityFixture()

	_, err := f.svc.GetAvailability(context.Background(), "nope", serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeInvalidRequest, "malformed tenant id")

	_, err = f.svc.GetAvailability(context.Background(), tenantA, "nope", staffID, mondayDate)
	assertCode(t, err, apperrors.CodeInvalidRequest, "malformed service id")

	_, err = f.svc.GetAvailability(context.Background(), tenantA, serviceID, "nope", mondayDate)
	assertCode(t, err, apperrors.CodeInvalidRequest, "malformed staff id")
}

func TestGetAvailabilityRejectsAMalformedDate(t *testing.T) {
	f := newAvailabilityFixture()

	for _, bad := range []string{"2026-9-7", "07-09-2026", "2026-02-30", "not-a-date", ""} {
		_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, bad)
		assertCode(t, err, apperrors.CodeValidationFailed, "malformed date "+bad)
	}
}

// --- timezone: configuration faults, never caller validation -----------

func TestGetAvailabilityTreatsAMissingTenantTimezoneAsInternal(t *testing.T) {
	f := newAvailabilityFixture()
	f.tenants.tenant.Timezone = nil

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeInternalError, "missing tenant timezone")
}

func TestGetAvailabilityTreatsAnInvalidTenantTimezoneAsInternal(t *testing.T) {
	f := newAvailabilityFixture()
	bogus := "Mars/Phobos"
	f.tenants.tenant.Timezone = &bogus

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeInternalError, "invalid tenant timezone")
}

func TestGetAvailabilityTreatsAnEmptyTenantTimezoneAsInternalNotUTC(t *testing.T) {
	f := newAvailabilityFixture()
	empty := "   "
	f.tenants.tenant.Timezone = &empty

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeInternalError, "empty tenant timezone")
}

func TestGetAvailabilityHonoursANonLagosTimezone(t *testing.T) {
	f := newAvailabilityFixture()
	ny := "America/New_York"
	f.tenants.tenant.Timezone = &ny

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	if result.Timezone != "America/New_York" {
		t.Fatalf("timezone = %q, want America/New_York", result.Timezone)
	}
	assertStarts(t, result, "09:00", "09:30", "10:00", "10:30", "11:00", "11:30")
}

// --- tenant isolation ---------------------------------------------------

func TestGetAvailabilityDoesNotDiscloseAnotherTenantsService(t *testing.T) {
	f := newAvailabilityFixture()
	f.services.services[serviceID].TenantID = tenantB // now belongs to another tenant

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeServiceNotFound, "cross-tenant service")
}

func TestGetAvailabilityDoesNotDiscloseAnotherTenantsStaff(t *testing.T) {
	f := newAvailabilityFixture()
	f.staff.profiles[staffID].TenantID = tenantB

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeStaffNotFound, "cross-tenant staff")
}

func TestGetAvailabilityFailsClosedOnACrossTenantCombination(t *testing.T) {
	f := newAvailabilityFixture()
	// service belongs to A, staff belongs to A, but the caller claims tenant B
	// (which owns neither).
	f.tenants.tenant.ID = tenantB
	f.tenants.tenant.Timezone = strPtr("Africa/Lagos")

	_, err := f.svc.GetAvailability(context.Background(), tenantB, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeServiceNotFound, "cross-tenant combination")
}

// --- capability enforcement ------------------------------------------

func TestGetAvailabilityRejectsATechnicianNotAssignedTheService(t *testing.T) {
	f := newAvailabilityFixture()
	f.capabilities.assignments[staffID] = []string{} // performs nothing

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeValidationFailed, "unassigned technician")
}

func TestGetAvailabilityAllowsATechnicianAssignedTheServiceAmongOthers(t *testing.T) {
	f := newAvailabilityFixture()
	f.capabilities.assignments[staffID] = []string{"550e8400-e29b-41d4-a716-4466554aaaaa", serviceID}

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result, "09:00", "09:30", "10:00", "10:30", "11:00", "11:30")
}

// --- archived / non-bookable resources -----------------------------

func TestGetAvailabilityReportsAnArchivedServiceAsNotFound(t *testing.T) {
	f := newAvailabilityFixture()
	f.services.services[serviceID].Status = model.StatusArchived

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeServiceNotFound, "archived service")
}

func TestGetAvailabilityReportsAnArchivedTechnicianAsNotFound(t *testing.T) {
	f := newAvailabilityFixture()
	f.staff.profiles[staffID].Status = model.StatusArchived

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeStaffNotFound, "archived technician")
}

func TestGetAvailabilityReturnsNoSlotsForANonBookableTechnicianWithoutError(t *testing.T) {
	f := newAvailabilityFixture()
	f.staff.profiles[staffID].IsBookable = false

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result)
}

// --- working-hours edge cases ---------------------------------------

func TestGetAvailabilityReturnsNoSlotsWhenNoHoursAreConfiguredForThatDay(t *testing.T) {
	f := newAvailabilityFixture()
	f.hours.byStaff[staffID] = []*model.WorkingHourInterval{
		{TenantID: tenantA, StaffID: staffID, DayOfWeek: model.Tuesday, StartTime: "09:00", EndTime: "17:00"},
	}

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result)
}

func TestGetAvailabilityReturnsNoSlotsForAStaffMemberWithNoHoursAtAll(t *testing.T) {
	f := newAvailabilityFixture()
	delete(f.hours.byStaff, staffID)

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result)
}

func TestGetAvailabilitySplitsWorkingHoursWithoutBridgingTheGap(t *testing.T) {
	f := newAvailabilityFixture()
	f.services.services[serviceID].DurationMinutes = 90
	f.hours.byStaff[staffID] = []*model.WorkingHourInterval{
		{TenantID: tenantA, StaffID: staffID, DayOfWeek: model.Monday, StartTime: "09:00", EndTime: "12:00"},
		{TenantID: tenantA, StaffID: staffID, DayOfWeek: model.Monday, StartTime: "13:00", EndTime: "17:00"},
	}

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result, "09:00", "10:30", "13:00", "14:30")
}

// --- clock / past slots ---------------------------------------------

func TestGetAvailabilityExcludesElapsedSlotsForToday(t *testing.T) {
	f := newAvailabilityFixture()
	lagos, _ := time.LoadLocation("Africa/Lagos")
	f.clock = fixedClock{now: time.Date(2026, 9, 7, 10, 15, 0, 0, lagos)}
	f.build()

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result, "10:30", "11:00", "11:30")
}

func TestGetAvailabilityReturnsNoSlotsForADateBeforeToday(t *testing.T) {
	f := newAvailabilityFixture()
	lagos, _ := time.LoadLocation("Africa/Lagos")
	f.clock = fixedClock{now: time.Date(2026, 9, 8, 0, 0, 0, 0, lagos)}
	f.build()

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result)
}

func TestGetAvailabilityLeavesAFutureDateUntouched(t *testing.T) {
	f := newAvailabilityFixture()
	lagos, _ := time.LoadLocation("Africa/Lagos")
	f.clock = fixedClock{now: time.Date(2026, 9, 6, 23, 59, 0, 0, lagos)}
	f.build()

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result, "09:00", "09:30", "10:00", "10:30", "11:00", "11:30")
}

// --- occupied intervals -------------------------------------------

func TestGetAvailabilityExcludesOccupiedIntervals(t *testing.T) {
	f := newAvailabilityFixture()
	lagos, _ := time.LoadLocation("Africa/Lagos")
	occ := &fakeOccupancy{intervals: []availability.OccupiedInterval{
		{
			Start: time.Date(2026, 9, 7, 9, 30, 0, 0, lagos),
			End:   time.Date(2026, 9, 7, 10, 30, 0, 0, lagos),
		},
	}}
	f.occupancy = occ
	f.build()

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result, "09:00", "10:30", "11:00", "11:30")

	if occ.calls != 1 {
		t.Fatalf("occupancy queried %d times, want exactly 1", occ.calls)
	}
	// The window handed to the occupancy reader is the tenant-local day.
	if occ.from.Hour() != 0 || occ.to.Sub(occ.from) != 24*time.Hour {
		t.Fatalf("occupancy window = %s .. %s, want a 24h tenant-local day", occ.from, occ.to)
	}
}

func TestGetAvailabilityAllowsASlotTouchingAnOccupiedBoundary(t *testing.T) {
	f := newAvailabilityFixture()
	lagos, _ := time.LoadLocation("Africa/Lagos")
	f.occupancy = &fakeOccupancy{intervals: []availability.OccupiedInterval{
		{
			Start: time.Date(2026, 9, 7, 8, 0, 0, 0, lagos),
			End:   time.Date(2026, 9, 7, 9, 0, 0, 0, lagos), // ends exactly as 09:00 begins
		},
	}}
	f.build()

	result, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	assertStarts(t, result, "09:00", "09:30", "10:00", "10:30", "11:00", "11:30")
}

// --- upstream errors propagate -----------------------------------

func TestGetAvailabilityPropagatesAnUnknownTenant(t *testing.T) {
	f := newAvailabilityFixture()
	f.tenants.tenant = &tenantmodel.Tenant{ID: "550e8400-e29b-41d4-a716-4466554fffff", Timezone: strPtr("Africa/Lagos")}

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeTenantNotFound, "unknown tenant")
}

func TestGetAvailabilityPropagatesAnUnknownService(t *testing.T) {
	f := newAvailabilityFixture()
	delete(f.services.services, serviceID)

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeServiceNotFound, "unknown service")
}

func TestGetAvailabilityPropagatesAnUnknownStaffMember(t *testing.T) {
	f := newAvailabilityFixture()
	delete(f.staff.profiles, staffID)

	_, err := f.svc.GetAvailability(context.Background(), tenantA, serviceID, staffID, mondayDate)
	assertCode(t, err, apperrors.CodeStaffNotFound, "unknown staff")
}
