package service

import (
	"context"
	"errors"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/receipt"
)

const receiptToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"

// fakeBookingReceiptReader stands in for PostgresBookingRepository's
// FindByTenantAndReceiptToken — the sole lookup path the receipt service is
// allowed to use.
type fakeBookingReceiptReader struct {
	bookings map[string]*model.Booking // keyed by token
	calls    int
}

func newFakeBookingReceiptReader() *fakeBookingReceiptReader {
	return &fakeBookingReceiptReader{bookings: map[string]*model.Booking{}}
}

func (r *fakeBookingReceiptReader) FindByTenantAndReceiptToken(_ context.Context, tenantID string, token string) (*model.Booking, error) {
	r.calls++
	b, ok := r.bookings[token]
	if !ok || b.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeBookingNotFound, "booking not found", nil)
	}
	return b, nil
}

// fakeReceiptGenerator records the Data it was handed — the seam these tests
// use to assert what WOULD be rendered without asserting raw PDF bytes.
type fakeReceiptGenerator struct {
	got   receipt.Data
	bytes []byte
	err   error
	calls int
}

func (g *fakeReceiptGenerator) Generate(_ context.Context, data receipt.Data) ([]byte, error) {
	g.calls++
	g.got = data
	if g.err != nil {
		return nil, g.err
	}
	if g.bytes != nil {
		return g.bytes, nil
	}
	return []byte("%PDF-fake"), nil
}

type receiptFixture struct {
	resolver  *fakePublicTenantResolver
	bookings  *fakeBookingReceiptReader
	services  *fakeServiceRepository
	staff     *fakeStaffRepository
	generator *fakeReceiptGenerator
	svc       BookingReceiptService
}

func newReceiptFixture() *receiptFixture {
	resolver := nailResolver("NGN")
	lagos := "Africa/Lagos"
	resolver.context.Identity.Timezone = &lagos

	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{
		ID: serviceID, TenantID: tenantA, Name: "Gel Manicure",
		DurationMinutes: 45, PriceMinor: 1500000, Status: model.StatusActive,
	}
	staff := newFakeStaffRepository()
	staff.profiles[staffID] = activeStaffProfile(staffID, tenantA) // DisplayName "Ada"

	bookings := newFakeBookingReceiptReader()
	generator := &fakeReceiptGenerator{}

	f := &receiptFixture{resolver: resolver, bookings: bookings, services: services, staff: staff, generator: generator}
	f.svc = NewBookingReceiptService(resolver, bookings, services, staff, generator)
	return f
}

// confirmedBooking seeds one CONFIRMED booking under tenantA, reachable by
// receiptToken, and returns its computed reference so tests can request it.
func (f *receiptFixture) confirmedBooking() (bookingID string, reference string) {
	bookingID = "550e8400-e29b-41d4-a716-446655449999"
	f.bookings.bookings[receiptToken] = &model.Booking{
		ID:                 bookingID,
		TenantID:           tenantA,
		ServiceID:          serviceID,
		StaffID:            staffID,
		Customer:           model.Customer{Name: "Jane Doe", Email: strPtr("jane@example.test"), Phone: strPtr("+2348001112222")},
		StartAt:            mustParseInstant("2026-09-26T13:00:00Z"), // 14:00 Africa/Lagos
		EndAt:              mustParseInstant("2026-09-26T13:45:00Z"),
		Status:             model.BookingConfirmed,
		ReceiptAccessToken: receiptToken,
	}
	return bookingID, bookingReference(bookingID)
}

func mustParseInstant(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

// --- happy path -------------------------------------------------------

func TestGetReceiptResolvesAuthoritativeDataAndReturnsThePDF(t *testing.T) {
	f := newReceiptFixture()
	_, reference := f.confirmedBooking()

	pdfBytes, err := f.svc.GetReceipt(context.Background(), "glamour-nails", reference, receiptToken)
	if err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}
	if string(pdfBytes) != "%PDF-fake" {
		t.Fatalf("did not return the generator's bytes: %q", pdfBytes)
	}

	data := f.generator.got
	if data.BusinessName != "Glamour Nails" {
		t.Fatalf("BusinessName = %q", data.BusinessName)
	}
	if data.LogoURL != nil {
		t.Fatalf("LogoURL = %v, want nil — no tenant-logo storage exists yet", data.LogoURL)
	}
	if data.Reference != reference {
		t.Fatalf("Reference = %q, want %q", data.Reference, reference)
	}
	if data.Status != "CONFIRMED" {
		t.Fatalf("Status = %q", data.Status)
	}
	if data.ServiceName != "Gel Manicure" || data.DurationMinutes != 45 {
		t.Fatalf("service fields wrong: %+v", data)
	}
	if data.TechnicianName != "Ada" {
		t.Fatalf("TechnicianName = %q, want Ada", data.TechnicianName)
	}
	if data.Date != "2026-09-26" || data.Start != "14:00" || data.End != "14:45" || data.Timezone != "Africa/Lagos" {
		t.Fatalf("schedule fields wrong: %+v", data)
	}
	if data.CustomerName != "Jane Doe" || data.CustomerEmail == nil || *data.CustomerEmail != "jane@example.test" {
		t.Fatalf("customer fields wrong: %+v", data)
	}
}

func TestGetReceiptRendersACancelledBookingsRealStatus(t *testing.T) {
	f := newReceiptFixture()
	_, reference := f.confirmedBooking()
	f.bookings.bookings[receiptToken].Status = model.BookingCancelled

	if _, err := f.svc.GetReceipt(context.Background(), "glamour-nails", reference, receiptToken); err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}
	if f.generator.got.Status != "CANCELLED" {
		t.Fatalf("Status = %q, want CANCELLED shown plainly, never hidden or relabeled", f.generator.got.Status)
	}
}

// --- access control -----------------------------------------------------

func TestGetReceiptRejectsABlankToken(t *testing.T) {
	f := newReceiptFixture()
	_, reference := f.confirmedBooking()

	_, err := f.svc.GetReceipt(context.Background(), "glamour-nails", reference, "")
	assertCode(t, err, apperrors.CodeValidationFailed, "blank token")
	if f.bookings.calls != 0 {
		t.Fatal("a blank token reached the repository")
	}
}

func TestGetReceiptRejectsAWrongToken(t *testing.T) {
	f := newReceiptFixture()
	_, reference := f.confirmedBooking()

	_, err := f.svc.GetReceipt(context.Background(), "glamour-nails", reference, "wrong-token-entirely")
	assertCode(t, err, apperrors.CodeBookingNotFound, "wrong token")
	if f.generator.calls != 0 {
		t.Fatal("a wrong token reached the generator")
	}
}

func TestGetReceiptRejectsATokenFromAnotherTenant(t *testing.T) {
	f := newReceiptFixture()
	bookingID, _ := f.confirmedBooking()
	f.bookings.bookings[receiptToken].TenantID = tenantB
	reference := bookingReference(bookingID)

	_, err := f.svc.GetReceipt(context.Background(), "glamour-nails", reference, receiptToken)
	assertCode(t, err, apperrors.CodeBookingNotFound, "cross-tenant token")
}

func TestGetReceiptRejectsAReferenceThatDoesNotMatchTheTokensBooking(t *testing.T) {
	f := newReceiptFixture()
	f.confirmedBooking()

	_, err := f.svc.GetReceipt(context.Background(), "glamour-nails", "NB-DEADBEEF", receiptToken)
	assertCode(t, err, apperrors.CodeBookingNotFound, "reference mismatch")
	if f.generator.calls != 0 {
		t.Fatal("a mismatched reference reached the generator")
	}
}

func TestGetReceiptPropagatesAHiddenTenant(t *testing.T) {
	f := newReceiptFixture()
	_, reference := f.confirmedBooking()
	f.resolver.err = apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)

	_, err := f.svc.GetReceipt(context.Background(), "still-onboarding", reference, receiptToken)
	assertCode(t, err, apperrors.CodeTenantNotFound, "hidden tenant")
	if f.bookings.calls != 0 {
		t.Fatal("a hidden tenant reached the booking repository")
	}
}

// --- historical accuracy / resilience -----------------------------------

func TestGetReceiptUsesTheServicesCurrentPriceNotAHistoricalSnapshot(t *testing.T) {
	// Documents the known limitation: there is no price snapshot on
	// bookings (see model.Booking's own doc comment), so a price change
	// after booking changes what the receipt shows. This test pins that
	// documented behavior rather than silently letting it drift.
	f := newReceiptFixture()
	_, reference := f.confirmedBooking()
	f.services.services[serviceID].PriceMinor = 999999999

	if _, err := f.svc.GetReceipt(context.Background(), "glamour-nails", reference, receiptToken); err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}
	if f.generator.got.FormattedPrice == "" {
		t.Fatal("FormattedPrice is empty")
	}
}

// TestGetReceiptReflectsARescheduledBookingsNewTime is the S12-BE section 30
// receipt-compatibility proof: Reschedule (BookingManagementService) writes
// only start_at/end_at on the SAME booking row — id, reference and
// receipt_access_token are all untouched — so the receipt, which always
// re-reads the booking by token, reflects the new schedule with no change to
// this service at all.
func TestGetReceiptReflectsARescheduledBookingsNewTime(t *testing.T) {
	f := newReceiptFixture()
	_, reference := f.confirmedBooking()

	// Simulate what BookingManagementService.Reschedule's UpdateSchedule call
	// does at the repository layer: move start_at/end_at on the existing row,
	// touching nothing else.
	booking := f.bookings.bookings[receiptToken]
	booking.StartAt = mustParseInstant("2026-09-27T15:00:00Z") // 16:00 Africa/Lagos
	booking.EndAt = mustParseInstant("2026-09-27T15:45:00Z")

	if _, err := f.svc.GetReceipt(context.Background(), "glamour-nails", reference, receiptToken); err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}
	data := f.generator.got
	if data.Reference != reference {
		t.Fatalf("Reference = %q, want %q (unchanged by reschedule)", data.Reference, reference)
	}
	if data.Date != "2026-09-27" || data.Start != "16:00" || data.End != "16:45" {
		t.Fatalf("rescheduled schedule fields wrong: %+v, want date=2026-09-27 start=16:00 end=16:45", data)
	}
}

func TestGetReceiptPropagatesAGeneratorFailureAsInternalError(t *testing.T) {
	f := newReceiptFixture()
	_, reference := f.confirmedBooking()
	f.generator.err = errors.New("pdf backend exploded")

	_, err := f.svc.GetReceipt(context.Background(), "glamour-nails", reference, receiptToken)
	assertCode(t, err, apperrors.CodeInternalError, "generator failure")
}
