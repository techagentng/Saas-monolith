package service

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// fakeAvailabilityEngine stands in for the S7 AvailabilityService. The booking
// service must go through it for every scheduling decision, so this records
// what it was asked and returns a canned result or error.
type fakeAvailabilityEngine struct {
	result    *AvailabilityResult
	err       error
	gotTenant string
	gotSvc    string
	gotStaff  string
	gotDate   string
	calls     int
}

func (f *fakeAvailabilityEngine) GetAvailability(_ context.Context, tenantID, serviceID, staffID, date string) (*AvailabilityResult, error) {
	f.calls++
	f.gotTenant, f.gotSvc, f.gotStaff, f.gotDate = tenantID, serviceID, staffID, date
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// fakeBookingCreator records the booking it was handed and can be told to fail
// as though the exclusion constraint fired.
type fakeBookingCreator struct {
	created *model.Booking
	err     error
	calls   int
}

func (f *fakeBookingCreator) Create(_ context.Context, booking *model.Booking) (*model.Booking, error) {
	f.calls++
	f.created = booking
	if f.err != nil {
		return nil, f.err
	}
	stored := *booking
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	if stored.Status == "" {
		stored.Status = model.BookingConfirmed
	}
	return &stored, nil
}

type bookingFixture struct {
	resolver *fakePublicTenantResolver
	engine   *fakeAvailabilityEngine
	services *fakeServiceRepository
	staff    *fakeStaffRepository
	creator  *fakeBookingCreator
	svc      BookingService
}

func newBookingFixture() *bookingFixture {
	resolver := nailResolver("NGN") // TenantID = tenantA, BusinessType = NAIL_TECHNICIAN
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{
		ID: serviceID, TenantID: tenantA, Name: "Gel Manicure",
		DurationMinutes: 30, PriceMinor: 1999, Status: model.StatusActive,
	}
	staff := newFakeStaffRepository()
	staff.profiles[staffID] = activeStaffProfile(staffID, tenantA) // DisplayName "Ada"

	engine := &fakeAvailabilityEngine{result: &AvailabilityResult{
		Date:      "2026-09-07",
		Timezone:  "Africa/Lagos",
		ServiceID: serviceID,
		StaffID:   staffID,
		Slots: []availability.Slot{
			{Start: "09:00", End: "09:30"},
			{Start: "09:30", End: "10:00"},
			{Start: "10:00", End: "10:30"},
		},
	}}
	creator := &fakeBookingCreator{}

	f := &bookingFixture{resolver: resolver, engine: engine, services: services, staff: staff, creator: creator}
	f.svc = NewBookingService(resolver, engine, services, staff, creator)
	return f
}

func validBookingInput() CreateBookingInput {
	return CreateBookingInput{
		ServiceID: serviceID,
		StaffID:   staffID,
		Date:      "2026-09-07",
		Start:     "09:30",
		Customer:  CustomerInput{Name: "Jane Doe", Phone: strPtr("+2348001112222")},
	}
}

// --- happy path -----------------------------------------------------------

func TestCreatePublicBookingPersistsAndReturnsACustomerSafeBooking(t *testing.T) {
	f := newBookingFixture()

	result, err := f.svc.CreatePublicBooking(context.Background(), "glamour-nails", validBookingInput())
	if err != nil {
		t.Fatalf("CreatePublicBooking() error = %v", err)
	}

	if result.Status != model.BookingConfirmed {
		t.Fatalf("status = %q, want CONFIRMED", result.Status)
	}
	if result.ServiceName != "Gel Manicure" || result.StaffName != "Ada" {
		t.Fatalf("names = %q / %q", result.ServiceName, result.StaffName)
	}
	if result.Date != "2026-09-07" || result.Start != "09:30" || result.End != "10:00" || result.Timezone != "Africa/Lagos" {
		t.Fatalf("schedule fields wrong: %+v", result)
	}
	if !strings.HasPrefix(result.Reference, "NB-") || len(result.Reference) != 11 {
		t.Fatalf("reference = %q, want NB-XXXXXXXX", result.Reference)
	}
	if result.ID == "" || result.Reference == result.ID {
		t.Fatalf("id/reference wrong: id=%q ref=%q", result.ID, result.Reference)
	}

	// The engine was consulted with the RESOLVED tenant id, never a client value.
	if f.engine.gotTenant != tenantA || f.engine.gotSvc != serviceID || f.engine.gotStaff != staffID || f.engine.gotDate != "2026-09-07" {
		t.Fatalf("engine call = %+v", f.engine)
	}
}

func TestCreatePublicBookingDerivesEndFromServiceDurationNotTheSlot(t *testing.T) {
	f := newBookingFixture()
	f.services.services[serviceID].DurationMinutes = 30

	if _, err := f.svc.CreatePublicBooking(context.Background(), "glamour-nails", validBookingInput()); err != nil {
		t.Fatalf("CreatePublicBooking() error = %v", err)
	}

	b := f.creator.created
	if b == nil {
		t.Fatal("no booking reached the creator")
	}
	if got := b.EndAt.Sub(b.StartAt); got != 30*time.Minute {
		t.Fatalf("end - start = %s, want exactly the 30-minute service duration", got)
	}
	// 09:30 Africa/Lagos (UTC+1) == 08:30 UTC.
	if b.StartAt.UTC().Hour() != 8 || b.StartAt.UTC().Minute() != 30 {
		t.Fatalf("start instant = %s, want 08:30 UTC (09:30 Africa/Lagos)", b.StartAt.UTC())
	}
	if b.TenantID != tenantA {
		t.Fatalf("persisted tenant = %q, want the resolved %q", b.TenantID, tenantA)
	}
}

// --- vertical + slug gate ------------------------------------------------

func TestCreatePublicBookingRefusesNonNailVerticals(t *testing.T) {
	f := newBookingFixture()
	hotel := tenantmodel.BusinessTypeHotel
	f.resolver.context.BusinessType = &hotel

	_, err := f.svc.CreatePublicBooking(context.Background(), "grand-hotel", validBookingInput())
	assertCode(t, err, apperrors.CodeResourceNotFound, "hotel vertical")
	if f.engine.calls != 0 || f.creator.calls != 0 {
		t.Fatal("a non-nail booking reached the engine or the creator")
	}
}

func TestCreatePublicBookingPropagatesAHiddenTenant(t *testing.T) {
	f := newBookingFixture()
	f.resolver.err = apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)

	_, err := f.svc.CreatePublicBooking(context.Background(), "still-onboarding", validBookingInput())
	assertCode(t, err, apperrors.CodeTenantNotFound, "hidden tenant")
	if f.creator.calls != 0 {
		t.Fatal("a hidden tenant reached the creator")
	}
}

// --- shape validation ---------------------------------------------------

func TestCreatePublicBookingRejectsMalformedInput(t *testing.T) {
	cases := map[string]func(*CreateBookingInput){
		"service id":     func(in *CreateBookingInput) { in.ServiceID = "nope" },
		"staff id":       func(in *CreateBookingInput) { in.StaffID = "nope" },
		"date":           func(in *CreateBookingInput) { in.Date = "07-09-2026" },
		"start":          func(in *CreateBookingInput) { in.Start = "9am" },
		"customer name":  func(in *CreateBookingInput) { in.Customer.Name = "   " },
		"customer email": func(in *CreateBookingInput) { in.Customer.Email = strPtr("bad") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newBookingFixture()
			input := validBookingInput()
			mutate(&input)

			_, err := f.svc.CreatePublicBooking(context.Background(), "glamour-nails", input)
			assertCode(t, err, apperrors.CodeValidationFailed, name)
			if f.creator.calls != 0 {
				t.Fatalf("%s: malformed input reached the creator", name)
			}
		})
	}
}

// --- S7 errors propagate ----------------------------------------------

func TestCreatePublicBookingPropagatesEngineErrors(t *testing.T) {
	for _, code := range []apperrors.ErrorCode{
		apperrors.CodeServiceNotFound,
		apperrors.CodeStaffNotFound,
		apperrors.CodeValidationFailed, // incapable technician
		apperrors.CodeInternalError,    // broken tenant timezone
	} {
		f := newBookingFixture()
		f.engine.err = apperrors.New(code, "boom", nil)

		_, err := f.svc.CreatePublicBooking(context.Background(), "glamour-nails", validBookingInput())
		assertCode(t, err, code, string(code))
		if f.creator.calls != 0 {
			t.Fatalf("%s: reached the creator despite an engine error", code)
		}
	}
}

// --- the slot must currently be available ------------------------------

func TestCreatePublicBookingRejectsAStartThatIsNotACurrentSlot(t *testing.T) {
	f := newBookingFixture()
	input := validBookingInput()
	input.Start = "12:00" // not among the engine's slots (past / taken / outside hours)

	_, err := f.svc.CreatePublicBooking(context.Background(), "glamour-nails", input)
	assertCode(t, err, apperrors.CodeBookingSlotUnavailable, "start not in slots")
	if f.creator.calls != 0 {
		t.Fatal("an unavailable start reached the creator")
	}
}

func TestCreatePublicBookingReturns409WhenTheInsertLosesTheRace(t *testing.T) {
	f := newBookingFixture()
	// The repository maps a 23P01 exclusion violation to this before the
	// service ever sees it.
	f.creator.err = apperrors.New(apperrors.CodeBookingSlotUnavailable, "the requested time is no longer available", nil)

	_, err := f.svc.CreatePublicBooking(context.Background(), "glamour-nails", validBookingInput())
	assertCode(t, err, apperrors.CodeBookingSlotUnavailable, "lost the race")
}

// --- PII safety -------------------------------------------------------

func TestCreatePublicBookingNeverPutsCustomerPIIInAnError(t *testing.T) {
	secret := "Top Secret Customer"
	secretPhone := "+2348009998888"
	secretEmail := "secret.customer@example.com"

	// Force every error path we can and scan the message.
	for name, setup := range map[string]func(*bookingFixture, *CreateBookingInput){
		"engine error": func(f *bookingFixture, _ *CreateBookingInput) {
			f.engine.err = apperrors.New(apperrors.CodeServiceNotFound, "x", nil)
		},
		"slot unavailable": func(_ *bookingFixture, in *CreateBookingInput) { in.Start = "23:00" },
		"insert conflict": func(f *bookingFixture, _ *CreateBookingInput) {
			f.creator.err = apperrors.New(apperrors.CodeBookingSlotUnavailable, "x", nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newBookingFixture()
			input := validBookingInput()
			input.Customer = CustomerInput{Name: secret, Phone: strPtr(secretPhone), Email: strPtr(secretEmail)}
			setup(f, &input)

			_, err := f.svc.CreatePublicBooking(context.Background(), "glamour-nails", input)
			if err == nil {
				t.Fatal("expected an error")
			}
			msg := err.Error()
			for _, pii := range []string{secret, secretPhone, secretEmail} {
				if strings.Contains(msg, pii) {
					t.Fatalf("error message leaked PII %q: %s", pii, msg)
				}
			}
		})
	}
}
