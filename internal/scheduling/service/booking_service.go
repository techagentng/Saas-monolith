package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// BookingCreator is the slice of booking persistence this service needs.
// Declared in the consumer, like every other narrow repository interface in
// this package. The concrete PostgresBookingRepository satisfies it.
type BookingCreator interface {
	Create(ctx context.Context, booking *model.Booking) (*model.Booking, error)
}

// CustomerInput is the transport-validated customer identity from the request
// body. Adapted to model.Customer (which owns the trimming/validation rules)
// before anything is persisted.
type CustomerInput struct {
	Name  string
	Phone *string
	Email *string
}

// CreateBookingInput carries a transport-validated public booking request.
//
// The slug (route), and therefore the tenant, is resolved separately and is
// never in this struct. There is deliberately no field for tenant_id,
// duration, end, price, currency, service name, staff name or timezone — the
// backend derives every one of those, and a client that sends them has them
// ignored because there is nowhere for them to land.
type CreateBookingInput struct {
	ServiceID string
	StaffID   string
	Date      string
	Start     string
	Customer  CustomerInput
}

// BookedAppointment is the customer-safe view of a persisted booking.
// Deliberately absent: tenant_id, customer PII echoed back, created_at,
// updated_at, internal scheduling maths. The handler maps this to the wire
// DTO.
type BookedAppointment struct {
	ID          string
	Reference   string
	Status      model.BookingStatus
	ServiceID   string
	ServiceName string
	StaffID     string
	StaffName   string
	Date        string // tenant-local calendar date
	Start       string // tenant-local "HH:MM"
	End         string // tenant-local "HH:MM"
	Timezone    string
}

// BookingService turns an S9 availability selection into a persisted
// appointment, for the NAIL_TECHNICIAN vertical only.
//
// It does not re-implement any scheduling rule. Whether the requested start is
// bookable — not past, inside working hours, not across a split-shift gap,
// performed by a capable technician, and (crucially) not already taken — is
// decided by calling the S7 AvailabilityService and checking the requested
// start against the slots it returns. The database's bookings_no_overlap
// exclusion constraint is the concurrency authority underneath that.
type BookingService interface {
	// CreatePublicBooking resolves the public slug, enforces the vertical,
	// re-validates the requested slot against S7, and persists the booking.
	//
	//   - hidden / reserved / non-canonical / unknown slug → TENANT_NOT_FOUND / TENANT_SLUG_INVALID
	//   - a resolvable non-NAIL_TECHNICIAN tenant           → RESOURCE_NOT_FOUND
	//   - archived / missing / cross-tenant service or staff → SERVICE_NOT_FOUND / STAFF_NOT_FOUND
	//   - a technician not assigned the service              → VALIDATION_FAILED
	//   - malformed date, start, or customer                 → VALIDATION_FAILED
	//   - the requested start is not a currently-available slot
	//     (past, outside hours, across a gap, or just taken)  → BOOKING_SLOT_UNAVAILABLE (409)
	//   - a concurrent booking wins the race                  → BOOKING_SLOT_UNAVAILABLE (409)
	CreatePublicBooking(ctx context.Context, slug string, input CreateBookingInput) (*BookedAppointment, error)
}

type bookingService struct {
	tenants      PublicTenantResolver
	availability AvailabilityService
	services     AvailabilityServiceReader
	staff        StaffReader
	bookings     BookingCreator
}

func NewBookingService(
	tenants PublicTenantResolver,
	availabilityEngine AvailabilityService,
	services AvailabilityServiceReader,
	staff StaffReader,
	bookings BookingCreator,
) BookingService {
	return &bookingService{
		tenants:      tenants,
		availability: availabilityEngine,
		services:     services,
		staff:        staff,
		bookings:     bookings,
	}
}

func (s *bookingService) CreatePublicBooking(ctx context.Context, slug string, input CreateBookingInput) (*BookedAppointment, error) {
	resolved, err := s.tenants.ResolvePublicTenant(ctx, slug)
	if err != nil {
		return nil, err
	}
	if resolved.BusinessType == nil || *resolved.BusinessType != tenantmodel.BusinessTypeNailTechnician {
		return nil, apperrors.New(apperrors.CodeResourceNotFound, "online booking is not available for this business type", nil)
	}
	tenantID := resolved.TenantID

	// Shape validation of client-supplied identifiers and the customer, before
	// any scheduling work. The customer's name/phone/email never appear in an
	// error message — model.ValidateCustomer only ever returns static text.
	if _, err := uuid.Parse(input.ServiceID); err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid service id", nil)
	}
	if _, err := uuid.Parse(input.StaffID); err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid staff id", nil)
	}
	requestedDate, err := availability.ParseDate(input.Date)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "date must be a calendar date in YYYY-MM-DD form", nil)
	}
	requestedStart, err := model.ValidateClockTime(input.Start)
	if err != nil {
		return nil, err
	}
	customer, err := model.ValidateCustomer(model.Customer{
		Name:  input.Customer.Name,
		Phone: input.Customer.Phone,
		Email: input.Customer.Email,
	})
	if err != nil {
		return nil, err
	}

	// S7 is authoritative for what is bookable. Its errors (service/staff
	// missing or archived, cross-tenant, incapable technician, broken tenant
	// timezone) are exactly the errors this endpoint should return, so they
	// propagate unchanged.
	avail, err := s.availability.GetAvailability(ctx, tenantID, input.ServiceID, input.StaffID, requestedDate.String())
	if err != nil {
		return nil, err
	}

	slot, ok := findSlot(avail.Slots, requestedStart)
	if !ok {
		// The requested start is not among the slots S7 currently considers
		// bookable — it has passed, falls outside working hours, bridges a
		// split-shift gap, or another booking already covers it. The customer
		// action is identical in every case: pick another time.
		return nil, apperrors.New(apperrors.CodeBookingSlotUnavailable, "the requested time is not an available slot", nil)
	}

	// Authoritative service (name + duration) and staff (display name) for the
	// response and the derived end. GetAvailability already proved both exist
	// in this tenant and are bookable; these reads cannot leak anything new.
	svc, err := s.services.FindByID(ctx, tenantID, input.ServiceID)
	if err != nil {
		return nil, err
	}
	staffProfile, err := s.staff.FindByID(ctx, tenantID, input.StaffID)
	if err != nil {
		return nil, err
	}

	// The tenant timezone S7 resolved is authoritative; re-parse the name it
	// returned rather than re-resolving from the tenant record.
	location, err := time.LoadLocation(avail.Timezone)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "availability returned an unloadable timezone", err)
	}
	startAt, err := availability.ResolveInstant(requestedDate, slot.Start, location)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "could not resolve the slot to an instant", err)
	}
	// end is derived, never taken from the client: start + the service's own
	// duration. slot.End is the same value expressed as wall-clock and is used
	// for display only.
	endAt := startAt.Add(time.Duration(svc.DurationMinutes) * time.Minute)

	bookingID := uuid.NewString()
	persisted, err := s.bookings.Create(ctx, &model.Booking{
		ID:        bookingID,
		TenantID:  tenantID,
		ServiceID: input.ServiceID,
		StaffID:   input.StaffID,
		Customer:  customer,
		StartAt:   startAt,
		EndAt:     endAt,
		Status:    model.BookingConfirmed,
	})
	if err != nil {
		// A 23P01 from the exclusion constraint has already been mapped to
		// BOOKING_SLOT_UNAVAILABLE by the repository — the deterministic loser
		// of a concurrent race. It propagates unchanged.
		return nil, err
	}

	return &BookedAppointment{
		ID:          persisted.ID,
		Reference:   bookingReference(persisted.ID),
		Status:      persisted.Status,
		ServiceID:   svc.ID,
		ServiceName: svc.Name,
		StaffID:     staffProfile.ID,
		StaffName:   staffProfile.DisplayName,
		Date:        requestedDate.String(),
		Start:       slot.Start,
		End:         slot.End,
		Timezone:    avail.Timezone,
	}, nil
}

// findSlot looks for a slot whose start matches the requested wall-clock time.
// Both sides are the normalized HH:MM form, so plain equality is correct.
func findSlot(slots []availability.Slot, start string) (availability.Slot, bool) {
	for _, slot := range slots {
		if slot.Start == start {
			return slot, true
		}
	}
	return availability.Slot{}, false
}

// bookingReference is a short, human-readable tag for a customer to quote —
// "NB-1A2B3C4D". It is display-only and NOT unique: the booking UUID is the
// canonical identifier, and nothing in S10 looks a booking up by reference.
// Derived from the UUID's own random bytes, so it is not sequential and not
// enumerable.
func bookingReference(bookingID string) string {
	hex := strings.ReplaceAll(bookingID, "-", "")
	if len(hex) < 8 {
		return "NB-" + strings.ToUpper(hex)
	}
	return "NB-" + strings.ToUpper(hex[:8])
}

// compile-time guard.
var _ BookingService = (*bookingService)(nil)
