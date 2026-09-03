package service

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

const (
	bmBookingID = "550e8400-e29b-41d4-a716-4466554e0001"
	bmServiceID = "550e8400-e29b-41d4-a716-4466554e0002"
	bmStaffID   = "550e8400-e29b-41d4-a716-4466554e0003"
)

// fakeBookingStore satisfies BookingReader and BookingCanceller over an
// in-memory slice, and records the filter it was handed so the view->filter
// translation can be asserted without a database.
type fakeBookingStore struct {
	rows       []*repository.BookingWithRelations
	lastFilter repository.BookingListFilter
	lastTenant string
	cancelErr  error
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
	svc := NewBookingManagementService(store, store, tenants, fixedClock{now: now})
	return store, tenants, svc
}

var bmNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

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

	_, _ = svc.List(context.Background(), tenantA, BookingListFilter{View: BookingViewPast})
	if f := store.lastFilter; f.Status == nil || *f.Status != model.BookingConfirmed || f.Window != repository.BookingWindowPast {
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
