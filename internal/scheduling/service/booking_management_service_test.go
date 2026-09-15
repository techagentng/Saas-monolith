package service

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

const (
	bmBookingID = "550e8400-e29b-41d4-a716-4466554e0001"
	bmServiceID = "550e8400-e29b-41d4-a716-4466554e0002"
	bmStaffID   = "550e8400-e29b-41d4-a716-4466554e0003"
)

// fakeBookingStore satisfies BookingReader, BookingCanceller,
// BookingRescheduler, and (S12-BE) OccupancyReader over an in-memory slice,
// and records the filter it was handed so the view->filter translation can
// be asserted without a database.
type fakeBookingStore struct {
	rows            []*repository.BookingWithRelations
	lastFilter      repository.BookingListFilter
	lastTenant      string
	cancelErr       error
	rescheduleErr   error
	updateStatusErr error
}

func (f *fakeBookingStore) ListByTenant(_ context.Context, tenantID string, filter repository.BookingListFilter) ([]*repository.BookingWithRelations, error) {
	f.lastTenant, f.lastFilter = tenantID, filter
	var out []*repository.BookingWithRelations
	for _, r := range f.rows {
		if r.Booking.TenantID != tenantID {
			continue
		}
		if filter.Status != nil && r.Booking.Status != *filter.Status {
			continue
		}
		if filter.ExcludeStatus != nil && r.Booking.Status == *filter.ExcludeStatus {
			continue
		}
		switch filter.Window {
		case repository.BookingWindowUpcoming:
			if r.Booking.StartAt.Before(filter.Now) {
				continue
			}
		case repository.BookingWindowPast:
			if !r.Booking.StartAt.Before(filter.Now) {
				continue
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeBookingStore) FindByTenantAndID(_ context.Context, tenantID, bookingID string) (*repository.BookingWithRelations, error) {
	for _, r := range f.rows {
		if r.Booking.ID == bookingID && r.Booking.TenantID == tenantID {
			return r, nil
		}
	}
	return nil, apperrors.New(apperrors.CodeBookingNotFound, "booking not found", nil)
}

func (f *fakeBookingStore) Cancel(_ context.Context, tenantID, bookingID string) (*model.Booking, bool, error) {
	if f.cancelErr != nil {
		return nil, false, f.cancelErr
	}
	for _, r := range f.rows {
		if r.Booking.ID == bookingID && r.Booking.TenantID == tenantID && r.Booking.Status == model.BookingConfirmed {
			r.Booking.Status = model.BookingCancelled
			r.Booking.UpdatedAt = time.Now().UTC()
			copied := r.Booking
			return &copied, true, nil
		}
	}
	return nil, false, nil
}

// OccupiedIntervals (S12-BE) is fakeBookingStore's OccupancyReader half —
// backing the REAL AvailabilityService bmFixture wires, so reschedule tests
// exercise the actual S7 engine's occupancy/self-exclusion logic rather than
// a canned slot list.
func (f *fakeBookingStore) OccupiedIntervals(_ context.Context, tenantID, staffID string, from, to time.Time, excludeBookingID string) ([]availability.OccupiedInterval, error) {
	var out []availability.OccupiedInterval
	for _, r := range f.rows {
		b := r.Booking
		if b.ID == excludeBookingID {
			continue
		}
		if b.TenantID == tenantID && b.StaffID == staffID && b.Status == model.BookingConfirmed &&
			b.StartAt.Before(to) && b.EndAt.After(from) {
			out = append(out, availability.OccupiedInterval{Start: b.StartAt, End: b.EndAt})
		}
	}
	return out, nil
}

// UpdateSchedule (S12-BE) mirrors the real bookings_no_overlap exclusion
// constraint exactly like statefulBookingRepository.UpdateSchedule in the
// app package's route tests: a target interval overlapping another
// CONFIRMED booking for the same staff member is rejected, but the row's
// own prior interval is never treated as conflicting with itself.
func (f *fakeBookingStore) UpdateSchedule(_ context.Context, tenantID, bookingID string, startAt, endAt time.Time) (*model.Booking, bool, error) {
	if f.rescheduleErr != nil {
		return nil, false, f.rescheduleErr
	}
	var target *repository.BookingWithRelations
	for _, r := range f.rows {
		if r.Booking.ID == bookingID && r.Booking.TenantID == tenantID && r.Booking.Status == model.BookingConfirmed {
			target = r
			break
		}
	}
	if target == nil {
		return nil, false, nil
	}
	for _, r := range f.rows {
		if r.Booking.ID == bookingID || r.Booking.TenantID != tenantID || r.Booking.StaffID != target.Booking.StaffID || r.Booking.Status != model.BookingConfirmed {
			continue
		}
		if startAt.Before(r.Booking.EndAt) && endAt.After(r.Booking.StartAt) {
			return nil, false, apperrors.New(apperrors.CodeBookingSlotUnavailable, "the requested time is no longer available", nil)
		}
	}
	target.Booking.StartAt, target.Booking.EndAt = startAt, endAt
	target.Booking.UpdatedAt = time.Now().UTC()
	copied := target.Booking
	return &copied, true, nil
}

// UpdateStatus (S13-BE) mirrors Cancel/UpdateSchedule's own WHERE-clause
// shape: it only ever transitions a CONFIRMED row.
func (f *fakeBookingStore) UpdateStatus(_ context.Context, tenantID, bookingID string, newStatus model.BookingStatus) (*model.Booking, bool, error) {
	if f.updateStatusErr != nil {
		return nil, false, f.updateStatusErr
	}
	for _, r := range f.rows {
		if r.Booking.ID == bookingID && r.Booking.TenantID == tenantID && r.Booking.Status == model.BookingConfirmed {
			r.Booking.Status = newStatus
			r.Booking.UpdatedAt = time.Now().UTC()
			copied := r.Booking
			return &copied, true, nil
		}
	}
	return nil, false, nil
}

func bmRow(id, tenantID string, status model.BookingStatus, start time.Time, customer model.Customer) *repository.BookingWithRelations {
	return &repository.BookingWithRelations{
		Booking: model.Booking{
			ID: id, TenantID: tenantID, ServiceID: bmServiceID, StaffID: bmStaffID,
			Customer: customer, StartAt: start, EndAt: start.Add(30 * time.Minute),
			Status: status, CreatedAt: start.Add(-72 * time.Hour), UpdatedAt: start.Add(-72 * time.Hour),
		},
		ServiceName: "Gel Manicure", StaffName: "Ada Okafor", ServiceDurationMins: 30,
	}
}

func bmFixture(now time.Time, rows ...*repository.BookingWithRelations) (*fakeBookingStore, *fakeTenantReader, BookingManagementService) {
	store := &fakeBookingStore{rows: rows}
	lagos := "Africa/Lagos"
	tenants := &fakeTenantReader{tenant: &tenantmodel.Tenant{ID: tenantA, Name: "Luxe Nails", Slug: "luxe-nails", Status: tenantmodel.StatusActive, Timezone: &lagos}}
	// List/Get/Cancel never touch scheduling logic, so a nilAvailability that
	// fails loudly if reached is itself a useful assertion for THOSE tests;
	// the dedicated rescheduleFixture below wires the real engine.
	svc := NewBookingManagementService(store, store, store, store, nilAvailability{}, newFakeServiceRepository(), tenants, fixedClock{now: now})
	return store, tenants, svc
}

// nilAvailability panics if List/Get/Cancel's own tests ever accidentally
// reach scheduling logic — they should not, since none of those three
// operations validates or computes a slot.
type nilAvailability struct{}

func (nilAvailability) GetAvailability(context.Context, string, string, string, string) (*AvailabilityResult, error) {
	panic("List/Get/Cancel must never reach the availability engine")
}
func (nilAvailability) GetAvailabilityExcludingBooking(context.Context, string, string, string, string, string) (*AvailabilityResult, error) {
	panic("List/Get/Cancel must never reach the availability engine")
}

var bmNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

// --- Reschedule (S12-BE) ------------------------------------------------

// rescheduleFixture wires BookingManagementService over the REAL
// AvailabilityService (S7), backed by fakes for service/staff/capability/
// working-hours/occupancy — not a canned slot list — so these tests
// genuinely exercise working-hours validation, capability revalidation, and
// (critically) the self-conflict exclusion, reusing S7 exactly as
// CreatePublicBooking does rather than re-implementing any of its rules.
type rescheduleFixture struct {
	store        *fakeBookingStore
	tenants      *fakeTenantReader
	services     *fakeServiceRepository
	staff        *fakeStaffRepository
	capabilities *fakeCapabilityRepository
	hours        *fakeWorkingHoursRepository
	svc          BookingManagementService
}

// rsTenant/rsService/rsStaff are distinct from bmServiceID/bmStaffID/tenantA
// used by the List/Get/Cancel tests above, so the two fixtures never share
// mutable fake state.
const (
	rsTenant  = "550e8400-e29b-41d4-a716-4466554e2001"
	rsService = "550e8400-e29b-41d4-a716-4466554e2002"
	rsStaff   = "550e8400-e29b-41d4-a716-4466554e2003"
)

func newRescheduleFixture(now time.Time, rows ...*repository.BookingWithRelations) *rescheduleFixture {
	store := &fakeBookingStore{rows: rows}
	lagos := "Africa/Lagos"
	tenants := &fakeTenantReader{tenant: &tenantmodel.Tenant{ID: rsTenant, Name: "Luxe Nails", Slug: "luxe-nails", Status: tenantmodel.StatusActive, Timezone: &lagos}}

	services := newFakeServiceRepository()
	services.services[rsService] = &model.Service{ID: rsService, TenantID: rsTenant, Name: "Gel Manicure", DurationMinutes: 30, PriceMinor: 150000, Status: model.StatusActive}

	staff := newFakeStaffRepository()
	staff.profiles[rsStaff] = activeStaffProfile(rsStaff, rsTenant) // DisplayName "Ada"

	capabilities := newFakeCapabilityRepository()
	capabilities.assignments[rsStaff] = []string{rsService}

	hours := newFakeWorkingHoursRepository()
	for _, day := range []model.DayOfWeek{
		model.Monday, model.Tuesday, model.Wednesday, model.Thursday, model.Friday, model.Saturday, model.Sunday,
	} {
		hours.byStaff[rsStaff] = append(hours.byStaff[rsStaff], &model.WorkingHourInterval{
			ID: "wh-" + string(day), TenantID: rsTenant, StaffID: rsStaff, DayOfWeek: day, StartTime: "09:00", EndTime: "17:00",
		})
	}

	engine := NewAvailabilityService(tenants, services, staff, capabilities, hours, store, fixedClock{now: now})
	svc := NewBookingManagementService(store, store, store, store, engine, services, tenants, fixedClock{now: now})

	return &rescheduleFixture{store: store, tenants: tenants, services: services, staff: staff, capabilities: capabilities, hours: hours, svc: svc}
}

// rsRow seeds one CONFIRMED booking for rsService/rsStaff under rsTenant.
func rsRow(id string, status model.BookingStatus, start time.Time) *repository.BookingWithRelations {
	return &repository.BookingWithRelations{
		Booking: model.Booking{
			ID: id, TenantID: rsTenant, ServiceID: rsService, StaffID: rsStaff,
			Customer: model.Customer{Name: "Jane Doe", Phone: strPtr("+2348001112222")},
			StartAt:  start, EndAt: start.Add(30 * time.Minute),
			Status: status, CreatedAt: start.Add(-72 * time.Hour), UpdatedAt: start.Add(-72 * time.Hour),
		},
		ServiceName: "Gel Manicure", StaffName: "Ada", ServiceDurationMins: 30,
	}
}

// rsNow is a Thursday. Every reschedule target below lands on a real future
// weekday within the 09:00-17:00 Africa/Lagos (UTC+1, no DST) working
// window the fixture seeds, unless a test deliberately picks one outside it.
var rsNow = time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC) // 09:00 Africa/Lagos

const rsBookingID = "550e8400-e29b-41d4-a716-4466554e2999"

func TestRescheduleMovesAConfirmedBookingPreservingItsIdentity(t *testing.T) {
	original := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)) // day+1, 10:00 Lagos
	f := newRescheduleFixture(rsNow, original)

	detail, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{
		Date: "2026-09-14", Start: "10:00", // day+4, a Monday, 10:00 Lagos = 09:00 UTC
	})
	if err != nil {
		t.Fatalf("Reschedule() error = %v", err)
	}
	if detail.ID != rsBookingID || detail.Reference != bookingReference(rsBookingID) {
		t.Fatalf("identity not preserved: id=%q ref=%q", detail.ID, detail.Reference)
	}
	if detail.CustomerName != "Jane Doe" || detail.ServiceID != rsService || detail.StaffID != rsStaff {
		t.Fatalf("customer/service/staff changed: %+v", detail)
	}
	wantStart := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	if !detail.StartAt.Equal(wantStart) || !detail.EndAt.Equal(wantStart.Add(30*time.Minute)) {
		t.Fatalf("new time = %s - %s, want %s - %s", detail.StartAt, detail.EndAt, wantStart, wantStart.Add(30*time.Minute))
	}
	if len(f.store.rows) != 1 {
		t.Fatalf("row count = %d, want 1 (no cancel-and-recreate)", len(f.store.rows))
	}
}

func TestRescheduleRejectsACancelledBooking(t *testing.T) {
	cancelled := rsRow(rsBookingID, model.BookingCancelled, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
	f := newRescheduleFixture(rsNow, cancelled)

	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "10:00"})
	assertCode(t, err, apperrors.CodeValidationFailed, "reschedule a cancelled booking")
	if !f.store.rows[0].Booking.StartAt.Equal(time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("a rejected reschedule mutated the booking")
	}
}

func TestRescheduleToTheCurrentSlotIsANoOpSuccess(t *testing.T) {
	start := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC) // 10:00 Lagos
	original := rsRow(rsBookingID, model.BookingConfirmed, start)
	f := newRescheduleFixture(rsNow, original)

	detail, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-11", Start: "10:00"})
	if err != nil {
		t.Fatalf("rescheduling to the current slot should succeed as a no-op: %v", err)
	}
	if !detail.StartAt.Equal(start) {
		t.Fatalf("StartAt = %s, want unchanged %s", detail.StartAt, start)
	}
}

// THE SELF-CONFLICT PROOF (S12-BE section 10): the target overlaps the
// booking's OWN current interval. Without excluding the booking's own row
// from occupancy, this would incorrectly report BOOKING_SLOT_UNAVAILABLE.
//
// availability.Generate steps candidates on a fixed grid (the service
// duration, from the working-hours start) — a booking's own stored interval
// is therefore only ever OFF that grid if working hours changed since it was
// made (a real, if uncommon, scenario: the owner edits the schedule after
// bookings already exist). That is simulated here by shifting the day's
// start from 09:00 to 09:15 AFTER seeding the original 10:00-10:30 booking,
// producing a new grid (09:15, 09:45, 10:15, ...) whose 10:15 candidate
// partially overlaps the old interval by 15 minutes — a genuine self-overlap
// a naive implementation would incorrectly reject.
func TestRescheduleExcludesItsOwnCurrentIntervalFromSelfConflict(t *testing.T) {
	start := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC) // 10:00-10:30 Lagos
	original := rsRow(rsBookingID, model.BookingConfirmed, start)
	f := newRescheduleFixture(rsNow, original)
	f.hours.byStaff[rsStaff] = []*model.WorkingHourInterval{
		{ID: "wh-fri", TenantID: rsTenant, StaffID: rsStaff, DayOfWeek: model.Friday, StartTime: "09:15", EndTime: "17:00"},
	}

	// 10:15-10:45 Lagos overlaps the current 10:00-10:30 by 15 minutes.
	detail, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-11", Start: "10:15"})
	if err != nil {
		t.Fatalf("a booking must not conflict with its own current interval: %v", err)
	}
	want := time.Date(2026, 9, 11, 9, 15, 0, 0, time.UTC)
	if !detail.StartAt.Equal(want) {
		t.Fatalf("StartAt = %s, want %s", detail.StartAt, want)
	}
}

func TestRescheduleRejectsAConflictWithAnotherConfirmedBooking(t *testing.T) {
	moving := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
	blocking := rsRow("550e8400-e29b-41d4-a716-4466554e2998", model.BookingConfirmed, time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)) // 10:00 Lagos on the target day
	f := newRescheduleFixture(rsNow, moving, blocking)

	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "10:00"})
	assertCode(t, err, apperrors.CodeBookingSlotUnavailable, "conflicting target slot")
	if !f.store.rows[0].Booking.StartAt.Equal(time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("a rejected reschedule mutated the booking's original time")
	}
}

func TestRescheduleRejectsAPastTarget(t *testing.T) {
	original := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC))
	f := newRescheduleFixture(rsNow, original) // rsNow = 2026-09-10 09:00 Lagos

	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-09", Start: "10:00"})
	assertCode(t, err, apperrors.CodeBookingSlotUnavailable, "past target")
}

func TestRescheduleRejectsATargetOutsideWorkingHours(t *testing.T) {
	original := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
	f := newRescheduleFixture(rsNow, original)

	// 20:00 Lagos is outside the fixture's 09:00-17:00 window.
	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "20:00"})
	assertCode(t, err, apperrors.CodeBookingSlotUnavailable, "outside working hours")
}

func TestRescheduleRejectsWhenTheStaffMemberNoLongerPerformsTheService(t *testing.T) {
	original := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
	f := newRescheduleFixture(rsNow, original)
	f.capabilities.assignments[rsStaff] = nil // the assignment was removed since the original booking

	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "10:00"})
	assertCode(t, err, apperrors.CodeValidationFailed, "staff no longer capable")
}

func TestRescheduleRejectsAMalformedDate(t *testing.T) {
	original := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
	f := newRescheduleFixture(rsNow, original)

	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "14-09-2026", Start: "10:00"})
	assertCode(t, err, apperrors.CodeValidationFailed, "malformed date")
}

func TestRescheduleRejectsAMalformedStart(t *testing.T) {
	original := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
	f := newRescheduleFixture(rsNow, original)

	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "10am"})
	assertCode(t, err, apperrors.CodeValidationFailed, "malformed start")
}

func TestRescheduleUnknownBookingIsNotFound(t *testing.T) {
	f := newRescheduleFixture(rsNow)
	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "10:00"})
	assertCode(t, err, apperrors.CodeBookingNotFound, "unknown booking")
}

func TestRescheduleCrossTenantBookingIsNotFound(t *testing.T) {
	foreign := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
	foreign.Booking.TenantID = tenantB
	f := newRescheduleFixture(rsNow, foreign)

	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "10:00"})
	assertCode(t, err, apperrors.CodeBookingNotFound, "cross-tenant reschedule")
}

// THE ROLLBACK PROOF (S12-BE section 26): a failure in the final write step
// must leave the booking's original time completely unchanged.
func TestRescheduleLeavesTheOriginalTimeUnchangedWhenTheWriteFails(t *testing.T) {
	original := rsRow(rsBookingID, model.BookingConfirmed, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
	f := newRescheduleFixture(rsNow, original)
	f.store.rescheduleErr = apperrors.New(apperrors.CodeInternalError, "simulated write failure", nil)

	_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "10:00"})
	if err == nil {
		t.Fatal("expected the forced write failure to propagate")
	}
	if !f.store.rows[0].Booking.StartAt.Equal(time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("the original booking time changed despite the write failing")
	}
}

// --- List: view -> filter translation ---------------------------------

func TestListUpcomingIsConfirmedAndFuture(t *testing.T) {
	store, _, svc := bmFixture(bmNow)

	if _, err := svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewUpcoming}); err != nil {
		t.Fatalf("List: %v", err)
	}
	f := store.lastFilter
	if f.Status == nil || *f.Status != model.BookingConfirmed || f.Window != repository.BookingWindowUpcoming || !f.Now.Equal(bmNow) {
		t.Fatalf("upcoming filter = %+v", f)
	}
	if store.lastTenant != tenantA {
		t.Fatalf("tenant scoping lost: %q", store.lastTenant)
	}
}

func TestListPastAndCancelledAndAllTranslateCorrectly(t *testing.T) {
	store, _, svc := bmFixture(bmNow)

	// S13-BE: PAST excludes CANCELLED rather than requiring CONFIRMED, so
	// CONFIRMED, COMPLETED and NO_SHOW bookings that have already started all
	// appear in the historical view.
	_, _ = svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewPast})
	if f := store.lastFilter; f.ExcludeStatus == nil || *f.ExcludeStatus != model.BookingCancelled || f.Status != nil || f.Window != repository.BookingWindowPast {
		t.Fatalf("past filter = %+v", f)
	}
	_, _ = svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewCancelled})
	if f := store.lastFilter; f.Status == nil || *f.Status != model.BookingCancelled || f.Window != repository.BookingWindowAny {
		t.Fatalf("cancelled filter = %+v", f)
	}
	_, _ = svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewAll})
	if f := store.lastFilter; f.Status != nil || f.Window != repository.BookingWindowAny {
		t.Fatalf("all filter = %+v", f)
	}
}

func TestParseBookingViewDefaultsToUpcomingAndRejectsGarbage(t *testing.T) {
	if v, err := ParseBookingView(""); err != nil || v != BookingViewUpcoming {
		t.Fatalf("empty -> %v, %v", v, err)
	}
	if v, err := ParseBookingView("past"); err != nil || v != BookingViewPast {
		t.Fatalf("past -> %v, %v", v, err)
	}
	if _, err := ParseBookingView("everything"); err == nil {
		t.Fatal("garbage view accepted")
	}
}

func TestListMapsRowsToSummariesWithReferenceAndPII(t *testing.T) {
	phone, email := "+2348001112222", "jane@example.com"
	row := bmRow(bmBookingID, tenantA, model.BookingConfirmed, bmNow.Add(48*time.Hour),
		model.Customer{Name: "Jane Doe", Phone: &phone, Email: &email})
	_, _, svc := bmFixture(bmNow, row)

	got, err := svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewUpcoming})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d", len(got))
	}
	s := got[0]
	if s.Reference != "NB-"+strings.ToUpper(strings.ReplaceAll(bmBookingID, "-", "")[:8]) {
		t.Fatalf("reference = %q", s.Reference)
	}
	if s.ServiceName != "Gel Manicure" || s.StaffName != "Ada Okafor" || s.DurationMinutes != 30 {
		t.Fatalf("relations not mapped: %+v", s)
	}
	if s.CustomerName != "Jane Doe" || s.CustomerPhone == nil || *s.CustomerPhone != phone || s.CustomerEmail == nil {
		t.Fatalf("customer PII not mapped for the authorized operator: %+v", s)
	}
	if s.StartAt.Location() != time.UTC {
		t.Fatalf("StartAt not UTC: %v", s.StartAt.Location())
	}
}

// --- List: date filter (S11) -------------------------------------------

// The date filter must mean the TENANT's calendar day, not the server's UTC
// day. The fixture tenant is Africa/Lagos (UTC+1): "2026-09-07" there is
// [2026-09-06 23:00 UTC, 2026-09-07 23:00 UTC), not [2026-09-07 00:00 UTC,
// 2026-09-08 00:00 UTC) — a bug that used the latter would be invisible in a
// UTC-only test.
func TestListDateFilterResolvesTheTenantsCalendarDayNotTheServers(t *testing.T) {
	store, _, svc := bmFixture(bmNow)
	date := "2026-09-07"

	if _, err := svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewAll, Date: &date}); err != nil {
		t.Fatalf("List: %v", err)
	}

	f := store.lastFilter
	if f.StartAtFrom == nil || f.StartAtTo == nil {
		t.Fatalf("date filter did not populate a range: %+v", f)
	}
	wantFrom := time.Date(2026, 9, 6, 23, 0, 0, 0, time.UTC) // 2026-09-07 00:00 Africa/Lagos
	wantTo := time.Date(2026, 9, 7, 23, 0, 0, 0, time.UTC)   // 2026-09-08 00:00 Africa/Lagos
	if !f.StartAtFrom.Equal(wantFrom) || !f.StartAtTo.Equal(wantTo) {
		t.Fatalf("range = [%s, %s), want [%s, %s)", f.StartAtFrom, f.StartAtTo, wantFrom, wantTo)
	}
}

func TestListRejectsAMalformedDateFilter(t *testing.T) {
	_, _, svc := bmFixture(bmNow)
	bad := "07-09-2026"
	_, err := svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewAll, Date: &bad})
	assertCode(t, err, apperrors.CodeValidationFailed, "malformed date filter")
}

func TestListWithNoDateFilterLeavesTheRangeUnset(t *testing.T) {
	store, _, svc := bmFixture(bmNow)
	if _, err := svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewAll}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if f := store.lastFilter; f.StartAtFrom != nil || f.StartAtTo != nil {
		t.Fatalf("range = [%v, %v), want unset when no date filter is given", f.StartAtFrom, f.StartAtTo)
	}
}

func TestListDateFilterPropagatesATenantTimezoneLookupFailure(t *testing.T) {
	store, tenants, svc := bmFixture(bmNow)
	tenants.findErr = apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	date := "2026-09-07"

	_, err := svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewAll, Date: &date})
	assertCode(t, err, apperrors.CodeTenantNotFound, "tenant lookup failure during date resolution")
	if store.lastFilter.StartAtFrom != nil {
		t.Fatal("a failed tenant lookup must not reach the repository with a half-built filter")
	}
}

func TestListRejectsAMalformedStaffFilter(t *testing.T) {
	_, _, svc := bmFixture(bmNow)
	bad := "not-a-uuid"
	_, err := svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewAll, StaffID: &bad})
	assertCode(t, err, apperrors.CodeValidationFailed, "malformed staff filter")
}

// --- Get -------------------------------------------------------------

func TestGetReturnsDetailWithTenantTimezone(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingConfirmed, bmNow.Add(24*time.Hour), model.Customer{Name: "Jane"})
	_, _, svc := bmFixture(bmNow, row)

	d, err := svc.Get(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.Timezone != "Africa/Lagos" || d.ID != bmBookingID || d.Status != model.BookingConfirmed {
		t.Fatalf("detail = %+v", d)
	}
}

func TestGetHidesCrossTenantBookingAsNotFound(t *testing.T) {
	row := bmRow(bmBookingID, tenantB, model.BookingConfirmed, bmNow, model.Customer{Name: "Jane"})
	_, _, svc := bmFixture(bmNow, row) // fixture tenant is tenantA

	_, err := svc.Get(context.Background(), tenantA, bmBookingID)
	assertCode(t, err, apperrors.CodeBookingNotFound, "cross-tenant booking")
}

func TestGetRejectsMalformedIDs(t *testing.T) {
	_, _, svc := bmFixture(bmNow)
	_, err := svc.Get(context.Background(), tenantA, "nope")
	assertCode(t, err, apperrors.CodeInvalidRequest, "malformed booking id")
}

// --- Cancel --------------------------------------------------------

func TestCancelConfirmedBookingTransitionsToCancelled(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingConfirmed, bmNow.Add(24*time.Hour), model.Customer{Name: "Jane"})
	store, _, svc := bmFixture(bmNow, row)

	d, err := svc.Cancel(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if d.Status != model.BookingCancelled {
		t.Fatalf("status = %q, want CANCELLED", d.Status)
	}
	// row still present (not deleted).
	if len(store.rows) != 1 || store.rows[0].Booking.Status != model.BookingCancelled {
		t.Fatalf("row not preserved-and-cancelled: %+v", store.rows)
	}
}

func TestCancelIsIdempotentOnAnAlreadyCancelledBooking(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingCancelled, bmNow.Add(-24*time.Hour), model.Customer{Name: "Jane"})
	store, _, svc := bmFixture(bmNow, row)
	store.cancelErr = apperrors.New(apperrors.CodeInternalError, "Cancel must not be called for an already-cancelled booking", nil)

	d, err := svc.Cancel(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("idempotent cancel returned error: %v", err)
	}
	if d.Status != model.BookingCancelled {
		t.Fatalf("status = %q", d.Status)
	}
}

func TestCancelPastConfirmedBookingIsAllowed(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingConfirmed, bmNow.Add(-72*time.Hour), model.Customer{Name: "Jane"})
	_, _, svc := bmFixture(bmNow, row)

	d, err := svc.Cancel(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("cancelling a past confirmed booking should be allowed: %v", err)
	}
	if d.Status != model.BookingCancelled {
		t.Fatalf("status = %q", d.Status)
	}
}

func TestCancelCrossTenantBookingIsNotFound(t *testing.T) {
	row := bmRow(bmBookingID, tenantB, model.BookingConfirmed, bmNow, model.Customer{Name: "Jane"})
	_, _, svc := bmFixture(bmNow, row)

	_, err := svc.Cancel(context.Background(), tenantA, bmBookingID)
	assertCode(t, err, apperrors.CodeBookingNotFound, "cross-tenant cancel")
}

func TestCancelNonexistentBookingIsNotFound(t *testing.T) {
	_, _, svc := bmFixture(bmNow)
	_, err := svc.Cancel(context.Background(), tenantA, "550e8400-e29b-41d4-a716-4466554e9999")
	assertCode(t, err, apperrors.CodeBookingNotFound, "nonexistent cancel")
}

// S13-BE section 14/26: once a booking is COMPLETED or NO_SHOW, cancellation
// must be rejected, not silently accepted as a no-op or misreported as
// idempotent success (the pre-S13 "the only other status is CONFIRMED"
// assumption in Cancel would otherwise attempt-and-silently-no-op here).
func TestCancelRejectsACompletedBooking(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingCompleted, bmNow.Add(-72*time.Hour), model.Customer{Name: "Jane"})
	store, _, svc := bmFixture(bmNow, row)

	_, err := svc.Cancel(context.Background(), tenantA, bmBookingID)
	assertCode(t, err, apperrors.CodeBookingInvalidTransition, "cancel a completed booking")
	if store.rows[0].Booking.Status != model.BookingCompleted {
		t.Fatal("a rejected cancel mutated the booking")
	}
}

func TestCancelRejectsANoShowBooking(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingNoShow, bmNow.Add(-72*time.Hour), model.Customer{Name: "Jane"})
	store, _, svc := bmFixture(bmNow, row)

	_, err := svc.Cancel(context.Background(), tenantA, bmBookingID)
	assertCode(t, err, apperrors.CodeBookingInvalidTransition, "cancel a no-show booking")
	if store.rows[0].Booking.Status != model.BookingNoShow {
		t.Fatal("a rejected cancel mutated the booking")
	}
}

// --- Complete / NoShow (S13-BE) -----------------------------------------

func TestCompletePastConfirmedBookingTransitionsToCompleted(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingConfirmed, bmNow.Add(-2*time.Hour), model.Customer{Name: "Jane"}) // ended 90m ago
	store, _, svc := bmFixture(bmNow, row)

	d, err := svc.Complete(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if d.Status != model.BookingCompleted {
		t.Fatalf("status = %q, want COMPLETED", d.Status)
	}
	if len(store.rows) != 1 || store.rows[0].Booking.Status != model.BookingCompleted {
		t.Fatalf("row not preserved-and-completed: %+v", store.rows)
	}
}

func TestCompleteIsIdempotentOnAnAlreadyCompletedBooking(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingCompleted, bmNow.Add(-2*time.Hour), model.Customer{Name: "Jane"})
	store, _, svc := bmFixture(bmNow, row)
	store.updateStatusErr = apperrors.New(apperrors.CodeInternalError, "UpdateStatus must not be called for an already-completed booking", nil)

	d, err := svc.Complete(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("idempotent complete returned error: %v", err)
	}
	if d.Status != model.BookingCompleted {
		t.Fatalf("status = %q", d.Status)
	}
}

func TestMarkNoShowOnPastConfirmedBookingTransitionsToNoShow(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingConfirmed, bmNow.Add(-2*time.Hour), model.Customer{Name: "Jane"})
	store, _, svc := bmFixture(bmNow, row)

	d, err := svc.MarkNoShow(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("MarkNoShow: %v", err)
	}
	if d.Status != model.BookingNoShow {
		t.Fatalf("status = %q, want NO_SHOW", d.Status)
	}
	if len(store.rows) != 1 || store.rows[0].Booking.Status != model.BookingNoShow {
		t.Fatalf("row not preserved-and-marked: %+v", store.rows)
	}
}

func TestMarkNoShowIsIdempotentOnAnAlreadyNoShowBooking(t *testing.T) {
	row := bmRow(bmBookingID, tenantA, model.BookingNoShow, bmNow.Add(-2*time.Hour), model.Customer{Name: "Jane"})
	store, _, svc := bmFixture(bmNow, row)
	store.updateStatusErr = apperrors.New(apperrors.CodeInternalError, "UpdateStatus must not be called for an already-no-show booking", nil)

	d, err := svc.MarkNoShow(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("idempotent no-show returned error: %v", err)
	}
	if d.Status != model.BookingNoShow {
		t.Fatalf("status = %q", d.Status)
	}
}

// S13-BE section 26: every disallowed transition, both directions.
func TestCompleteAndNoShowRejectInvalidCurrentStatuses(t *testing.T) {
	past := bmNow.Add(-2 * time.Hour)
	for _, tc := range []struct {
		name   string
		status model.BookingStatus
		action func(svc BookingManagementService) (*BookingDetail, error)
	}{
		{"cancelled -> complete", model.BookingCancelled, func(svc BookingManagementService) (*BookingDetail, error) {
			return svc.Complete(context.Background(), tenantA, bmBookingID)
		}},
		{"cancelled -> no-show", model.BookingCancelled, func(svc BookingManagementService) (*BookingDetail, error) {
			return svc.MarkNoShow(context.Background(), tenantA, bmBookingID)
		}},
		{"completed -> no-show", model.BookingCompleted, func(svc BookingManagementService) (*BookingDetail, error) {
			return svc.MarkNoShow(context.Background(), tenantA, bmBookingID)
		}},
		{"no-show -> complete", model.BookingNoShow, func(svc BookingManagementService) (*BookingDetail, error) {
			return svc.Complete(context.Background(), tenantA, bmBookingID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := bmRow(bmBookingID, tenantA, tc.status, past, model.Customer{Name: "Jane"})
			store, _, svc := bmFixture(bmNow, row)
			store.updateStatusErr = apperrors.New(apperrors.CodeInternalError, "UpdateStatus must not be called for an invalid transition", nil)

			_, err := tc.action(svc)
			assertCode(t, err, apperrors.CodeBookingInvalidTransition, tc.name)
			if store.rows[0].Booking.Status != tc.status {
				t.Fatalf("a rejected transition mutated the booking: got %q, want unchanged %q", store.rows[0].Booking.Status, tc.status)
			}
		})
	}
}

// S13-BE section 27: an owner should not be able to complete or no-show a
// booking whose appointment has not happened yet.
func TestCompleteAndNoShowRejectAFutureConfirmedBooking(t *testing.T) {
	future := bmNow.Add(2 * time.Hour) // ends 2h30m from now
	for _, tc := range []struct {
		name   string
		action func(svc BookingManagementService) (*BookingDetail, error)
	}{
		{"complete", func(svc BookingManagementService) (*BookingDetail, error) {
			return svc.Complete(context.Background(), tenantA, bmBookingID)
		}},
		{"no-show", func(svc BookingManagementService) (*BookingDetail, error) {
			return svc.MarkNoShow(context.Background(), tenantA, bmBookingID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := bmRow(bmBookingID, tenantA, model.BookingConfirmed, future, model.Customer{Name: "Jane"})
			store, _, svc := bmFixture(bmNow, row)
			store.updateStatusErr = apperrors.New(apperrors.CodeInternalError, "UpdateStatus must not be called before the booking has ended", nil)

			_, err := tc.action(svc)
			assertCode(t, err, apperrors.CodeBookingInvalidTransition, tc.name+" a future booking")
			if store.rows[0].Booking.Status != model.BookingConfirmed {
				t.Fatalf("a rejected transition mutated the booking: %q", store.rows[0].Booking.Status)
			}
		})
	}
}

// S13-BE section 28: the exact boundary "now == booking.end" is eligible
// (>=, not >). bmRow gives every booking a 30-minute duration, so starting it
// exactly 30 minutes before bmNow makes EndAt land exactly on bmNow.
func TestCompleteAtExactlyTheBookingsEndTimeIsEligible(t *testing.T) {
	start := bmNow.Add(-30 * time.Minute) // EndAt == bmNow exactly
	row := bmRow(bmBookingID, tenantA, model.BookingConfirmed, start, model.Customer{Name: "Jane"})
	_, _, svc := bmFixture(bmNow, row)

	d, err := svc.Complete(context.Background(), tenantA, bmBookingID)
	if err != nil {
		t.Fatalf("Complete at the exact boundary should be eligible: %v", err)
	}
	if d.Status != model.BookingCompleted {
		t.Fatalf("status = %q", d.Status)
	}
}

func TestCompleteAndNoShowAreCrossTenantNotFound(t *testing.T) {
	row := bmRow(bmBookingID, tenantB, model.BookingConfirmed, bmNow.Add(-2*time.Hour), model.Customer{Name: "Jane"})
	_, _, svc := bmFixture(bmNow, row) // fixture tenant is tenantA

	_, err := svc.Complete(context.Background(), tenantA, bmBookingID)
	assertCode(t, err, apperrors.CodeBookingNotFound, "cross-tenant complete")

	_, err = svc.MarkNoShow(context.Background(), tenantA, bmBookingID)
	assertCode(t, err, apperrors.CodeBookingNotFound, "cross-tenant no-show")
}

func TestCompleteAndNoShowRejectMalformedIDs(t *testing.T) {
	_, _, svc := bmFixture(bmNow)

	_, err := svc.Complete(context.Background(), tenantA, "nope")
	assertCode(t, err, apperrors.CodeInvalidRequest, "malformed complete id")

	_, err = svc.MarkNoShow(context.Background(), tenantA, "nope")
	assertCode(t, err, apperrors.CodeInvalidRequest, "malformed no-show id")
}

func TestCompleteAndNoShowNonexistentBookingIsNotFound(t *testing.T) {
	_, _, svc := bmFixture(bmNow)
	_, err := svc.Complete(context.Background(), tenantA, "550e8400-e29b-41d4-a716-4466554e9999")
	assertCode(t, err, apperrors.CodeBookingNotFound, "nonexistent complete")
	_, err = svc.MarkNoShow(context.Background(), tenantA, "550e8400-e29b-41d4-a716-4466554e9999")
	assertCode(t, err, apperrors.CodeBookingNotFound, "nonexistent no-show")
}

// S13-BE section 31: rescheduling must remain rejected for both new terminal
// statuses.
func TestRescheduleRejectsCompletedAndNoShowBookings(t *testing.T) {
	for _, status := range []model.BookingStatus{model.BookingCompleted, model.BookingNoShow} {
		t.Run(string(status), func(t *testing.T) {
			row := rsRow(rsBookingID, status, time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC))
			f := newRescheduleFixture(rsNow, row)

			_, err := f.svc.Reschedule(context.Background(), rsTenant, rsBookingID, RescheduleBookingInput{Date: "2026-09-14", Start: "10:00"})
			assertCode(t, err, apperrors.CodeValidationFailed, "reschedule a "+string(status)+" booking")
		})
	}
}

// PII must never appear in an error the service produces.
func TestManagementErrorsCarryNoCustomerPII(t *testing.T) {
	secret := "Very Secret Person"
	row := bmRow(bmBookingID, tenantB, model.BookingConfirmed, bmNow, model.Customer{Name: secret, Phone: strPtr("+2348000000000")})
	_, _, svc := bmFixture(bmNow, row)

	_, err := svc.Cancel(context.Background(), tenantA, bmBookingID) // cross-tenant -> BOOKING_NOT_FOUND
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "2348000000000") {
		t.Fatalf("error leaked PII: %v", err)
	}
}
