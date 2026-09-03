package repository

import (
	"context"
	"time"

	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// BookingWithRelations is a booking joined to the display names of its service
// and technician, and the service's duration.
//
// The owner dashboard (S11) needs "Gel Manicure with Ada" on every list row;
// resolving that per row would be an N+1. The list and detail queries JOIN
// services and staff_profiles once (both on the composite (id, tenant_id) so
// the join is itself tenant-safe), and return this shape. Cancellation, which
// needs no names, returns a plain *model.Booking.
type BookingWithRelations struct {
	Booking             model.Booking
	ServiceName         string
	StaffName           string
	ServiceDurationMins int
}

// BookingTimeWindow narrows a booking list by when the appointment starts,
// relative to a caller-supplied "now". It is deliberately coarse — the S11
// dashboard has exactly three views and does not need arbitrary ranges.
type BookingTimeWindow string

const (
	// BookingWindowAny applies no time filter.
	BookingWindowAny BookingTimeWindow = ""
	// BookingWindowUpcoming keeps bookings whose start is at or after now.
	BookingWindowUpcoming BookingTimeWindow = "UPCOMING"
	// BookingWindowPast keeps bookings whose start is strictly before now.
	BookingWindowPast BookingTimeWindow = "PAST"
)

// BookingListFilter narrows ListByTenant. A zero-value filter lists every
// booking for the tenant, ordered by start_at ascending. Every field is
// AND-combined and every one of them is applied in SQL, never in memory.
type BookingListFilter struct {
	// Status nil lists every status; a concrete value restricts to it.
	Status *model.BookingStatus
	// Window, with Now, restricts by appointment start relative to that instant.
	Window BookingTimeWindow
	Now    time.Time
	// StaffID / ServiceID nil mean "any"; a concrete value restricts to it.
	StaffID   *string
	ServiceID *string
}

// BookingRepository is the persistence boundary for appointment bookings.
//
// Every method takes tenantID and every implementation filters on it — the
// same isolation mechanism the rest of the module uses. A booking, service or
// staff id belonging to another tenant is never addressable through it, and a
// cross-tenant booking id is reported exactly as a nonexistent one.
type BookingRepository interface {
	// Create inserts one booking. The bookings_no_overlap exclusion constraint
	// is the authority on conflicts: a concurrent or pre-existing CONFIRMED
	// booking that overlaps this staff member's requested interval makes the
	// insert fail, mapped to BOOKING_SLOT_UNAVAILABLE. A composite-FK violation
	// (service/staff not in this tenant) is likewise mapped, not surfaced raw.
	Create(ctx context.Context, booking *model.Booking) (*model.Booking, error)

	// OccupiedIntervals returns the [start, end) instants this staff member is
	// already committed for that intersect the window [from, to), as an
	// indexed, tenant- and staff-scoped query — never a load-all-and-filter.
	// It is the concrete backing for the S7 OccupancyReader seam, and only
	// CONFIRMED bookings occupy time, so a CANCELLED booking is invisible here
	// with no scheduling-layer change.
	OccupiedIntervals(ctx context.Context, tenantID string, staffID string, from time.Time, to time.Time) ([]availability.OccupiedInterval, error)

	// ListByTenant returns the tenant's bookings matching filter, joined to
	// service and technician names, ordered by start_at ascending. Filtered
	// entirely in SQL.
	ListByTenant(ctx context.Context, tenantID string, filter BookingListFilter) ([]*BookingWithRelations, error)

	// FindByTenantAndID resolves one booking within one tenant, joined to
	// service and technician names. A missing or cross-tenant id yields
	// BOOKING_NOT_FOUND, identically.
	FindByTenantAndID(ctx context.Context, tenantID string, bookingID string) (*BookingWithRelations, error)

	// Cancel transitions a CONFIRMED booking in this tenant to CANCELLED,
	// preserving the row. It returns (updated=true, booking) when it changed a
	// row, and (updated=false, nil, nil) when no CONFIRMED booking with that
	// id exists in this tenant (missing, cross-tenant, or already cancelled) —
	// the service disambiguates those. It never deletes.
	Cancel(ctx context.Context, tenantID string, bookingID string) (booking *model.Booking, updated bool, err error)
}
