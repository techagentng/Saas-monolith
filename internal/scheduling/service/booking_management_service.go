package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
)

// BookingReader / BookingCanceller are the slices of booking persistence the
// management service needs, declared here in the consumer like every other
// narrow repository interface in this package. *PostgresBookingRepository
// satisfies both.
type BookingReader interface {
	ListByTenant(ctx context.Context, tenantID string, filter repository.BookingListFilter) ([]*repository.BookingWithRelations, error)
	FindByTenantAndID(ctx context.Context, tenantID string, bookingID string) (*repository.BookingWithRelations, error)
}

type BookingCanceller interface {
	Cancel(ctx context.Context, tenantID string, bookingID string) (*model.Booking, bool, error)
}

// BookingRescheduler (S12-BE) is the transactional write half of
// rescheduling: a single atomic UPDATE of start/end, gated on the booking
// still being CONFIRMED in this tenant, backed by the same
// bookings_no_overlap exclusion constraint Create relies on.
type BookingRescheduler interface {
	UpdateSchedule(ctx context.Context, tenantID string, bookingID string, startAt time.Time, endAt time.Time) (*model.Booking, bool, error)
}

// BookingStatusUpdater (S13-BE) is the transactional write half of the
// terminal-status transitions: a single atomic UPDATE gated on the booking
// still being CONFIRMED in this tenant.
type BookingStatusUpdater interface {
	UpdateStatus(ctx context.Context, tenantID string, bookingID string, newStatus model.BookingStatus) (*model.Booking, bool, error)
}

// BookingView is the S11 dashboard's three-way split, resolved to a
// (status, time-window) pair before it reaches the repository. It is a closed
// vocabulary — the dashboard does not offer arbitrary status/date queries.
type BookingView string

const (
	// BookingViewUpcoming is the default: CONFIRMED bookings starting now or later.
	BookingViewUpcoming BookingView = "UPCOMING"
	// BookingViewPast is CONFIRMED bookings that have already started.
	BookingViewPast BookingView = "PAST"
	// BookingViewCancelled is CANCELLED bookings, any time.
	BookingViewCancelled BookingView = "CANCELLED"
	// BookingViewAll is every booking, any status, any time.
	BookingViewAll BookingView = "ALL"
)

// ParseBookingView maps the ?view= query parameter. An empty value is the
// operational default (UPCOMING); an unrecognized value is rejected rather
// than silently defaulted, matching ParseStatusFilter's discipline.
func ParseBookingView(raw string) (BookingView, error) {
	switch BookingView(strings.ToUpper(strings.TrimSpace(raw))) {
	case "":
		return BookingViewUpcoming, nil
	case BookingViewUpcoming:
		return BookingViewUpcoming, nil
	case BookingViewPast:
		return BookingViewPast, nil
	case BookingViewCancelled:
		return BookingViewCancelled, nil
	case BookingViewAll:
		return BookingViewAll, nil
	default:
		return "", apperrors.New(apperrors.CodeValidationFailed, "invalid booking view", nil)
	}
}

// BookingListFilter is the transport-validated list query.
type BookingListFilter struct {
	View      BookingView
	StaffID   *string
	ServiceID *string
	// Date, if set, is a calendar date in YYYY-MM-DD form, interpreted in
	// the TENANT's own authoritative timezone — never the server's, and
	// never a caller-supplied zone — matching the S7 availability engine's
	// own date semantics exactly (see resolveTenantLocation).
	Date *string
}

// BookingSummary is one dashboard list row. StartAt/EndAt/CreatedAt stay
// absolute UTC instants — the backend's storage and API convention — and the
// frontend formats them in the tenant's timezone. Customer phone and email are
// included because contacting the customer about their appointment is the core
// operational reason this list exists; they are only ever returned to a caller
// the middleware has already authorized for booking.read in this tenant.
type BookingSummary struct {
	ID              string
	Reference       string
	Status          model.BookingStatus
	ServiceID       string
	ServiceName     string
	StaffID         string
	StaffName       string
	CustomerName    string
	CustomerPhone   *string
	CustomerEmail   *string
	StartAt         time.Time
	EndAt           time.Time
	DurationMinutes int
	CreatedAt       time.Time
}

// BookingDetail is BookingSummary plus the tenant timezone the times should be
// displayed in, so the detail view is self-contained.
type BookingDetail struct {
	BookingSummary
	Timezone string
}

// TenantTimezoneReader is the one tenant fact the detail view needs: the IANA
// timezone appointment times are displayed in. Reuses the existing TenantReader
// shape rather than a new interface.
type TenantTimezoneReader = TenantReader

// RescheduleBookingInput (S12-BE) carries a transport-validated reschedule
// request. Deliberately absent: end, duration, service id, staff id,
// customer, price — every one of those is derived server-side or left
// unchanged, and a client that sends them has nowhere for them to land, the
// same structural protection CreateBookingInput already applies to public
// booking creation.
type RescheduleBookingInput struct {
	Date  string
	Start string
}

// BookingManagementService is the authenticated, tenant-scoped owner/staff view
// of persisted bookings (S11). It reads and cancels; it never creates (that is
// the public S10 path) and it never touches scheduling logic — cancellation
// works purely because the S7 occupancy query already ignores non-CONFIRMED
// bookings.
//
// Tenant access and the booking.read / booking.update permission are verified
// by the production middleware chain before any method here is reached. This
// service does not re-derive authorization; it does scope every repository call
// by tenantID, so a defect in that chain cannot become a cross-tenant read or
// write.
type BookingManagementService interface {
	// List returns the tenant's bookings for one dashboard view, filtered
	// entirely in the database, ordered by start time.
	List(ctx context.Context, tenantID string, filter BookingListFilter) ([]BookingSummary, error)
	// Get returns one booking's detail. A missing or cross-tenant id is
	// BOOKING_NOT_FOUND, identically.
	Get(ctx context.Context, tenantID string, bookingID string) (*BookingDetail, error)
	// Cancel transitions a CONFIRMED booking to CANCELLED. Cancelling an
	// already-CANCELLED booking is idempotent success (the same convention
	// CatalogService.Archive and StaffService.Archive use). The row is never
	// deleted. Once cancelled, the S7/S9 occupancy query stops counting it, so
	// the slot becomes publicly available again.
	Cancel(ctx context.Context, tenantID string, bookingID string) (*BookingDetail, error)
	// Reschedule (S12-BE) moves a CONFIRMED booking to a new date/start,
	// preserving its id, reference, customer, service and technician. Only a
	// CONFIRMED booking may be rescheduled — CANCELLED, COMPLETED and NO_SHOW
	// (S13-BE) are all rejected with the same deterministic domain error.
	// Rescheduling to the booking's own current slot is a no-op success, the
	// same idempotency convention Cancel applies to an already-cancelled
	// booking. The new time is validated through the exact same S7
	// availability engine public booking creation uses (service/staff
	// validity, capability, working hours, past-slot filtering, no-overlap),
	// with that ONE booking's own current interval excluded from occupancy so
	// it never appears to conflict with itself.
	Reschedule(ctx context.Context, tenantID string, bookingID string, input RescheduleBookingInput) (*BookingDetail, error)
	// Complete transitions a CONFIRMED booking to COMPLETED (S13-BE). Only
	// allowed once the booking's appointment window has ended, in the
	// tenant's own authoritative timezone (current tenant-local time >=
	// booking.end) — see transitionToTerminal. COMPLETED is terminal:
	// completing an already-COMPLETED booking is idempotent success (the
	// Cancel/already-cancelled convention); every other current status
	// (CANCELLED, NO_SHOW) is a rejected transition.
	Complete(ctx context.Context, tenantID string, bookingID string) (*BookingDetail, error)
	// MarkNoShow transitions a CONFIRMED booking to NO_SHOW (S13-BE). Mirrors
	// Complete exactly, with NO_SHOW as the target: same time-eligibility
	// rule, same idempotency on NO_SHOW -> NO_SHOW, same rejection of every
	// other current status.
	MarkNoShow(ctx context.Context, tenantID string, bookingID string) (*BookingDetail, error)
}

type bookingManagementService struct {
	reader        BookingReader
	canceller     BookingCanceller
	rescheduler   BookingRescheduler
	statusUpdater BookingStatusUpdater
	availability  AvailabilityService
	services      AvailabilityServiceReader
	tenants       TenantTimezoneReader
	clock         Clock
}

func NewBookingManagementService(
	reader BookingReader,
	canceller BookingCanceller,
	rescheduler BookingRescheduler,
	statusUpdater BookingStatusUpdater,
	availabilityEngine AvailabilityService,
	services AvailabilityServiceReader,
	tenants TenantTimezoneReader,
	clock Clock,
) BookingManagementService {
	return &bookingManagementService{
		reader: reader, canceller: canceller, rescheduler: rescheduler, statusUpdater: statusUpdater,
		availability: availabilityEngine, services: services, tenants: tenants, clock: clock,
	}
}

func (s *bookingManagementService) List(ctx context.Context, tenantID string, filter BookingListFilter) ([]BookingSummary, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	repoFilter, err := s.toRepoFilter(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}

	rows, err := s.reader.ListByTenant(ctx, tenantID, repoFilter)
	if err != nil {
		return nil, err
	}
	summaries := make([]BookingSummary, len(rows))
	for i, row := range rows {
		summaries[i] = toSummary(row)
	}
	return summaries, nil
}

func (s *bookingManagementService) Get(ctx context.Context, tenantID string, bookingID string) (*BookingDetail, error) {
	if err := validateBookingIdentifiers(tenantID, bookingID); err != nil {
		return nil, err
	}
	row, err := s.reader.FindByTenantAndID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	return s.toDetail(ctx, tenantID, row)
}

func (s *bookingManagementService) Cancel(ctx context.Context, tenantID string, bookingID string) (*BookingDetail, error) {
	if err := validateBookingIdentifiers(tenantID, bookingID); err != nil {
		return nil, err
	}

	// Resolve first: a missing or cross-tenant id is BOOKING_NOT_FOUND, and an
	// already-CANCELLED booking is returned as-is without a write.
	current, err := s.reader.FindByTenantAndID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	switch current.Booking.Status {
	case model.BookingCancelled:
		return s.toDetail(ctx, tenantID, current)
	case model.BookingConfirmed:
		// Past confirmed bookings are cancellable too — S11 has no
		// cancellation-window policy, and cancelling a past booking is a
		// legitimate record correction with no availability effect (a past
		// slot is already gone from S7).
	default:
		// COMPLETED or NO_SHOW (S13-BE): both are terminal outcomes distinct
		// from cancellation, and neither may be reversed into CANCELLED.
		return nil, apperrors.New(apperrors.CodeBookingInvalidTransition,
			"a "+string(current.Booking.Status)+" booking cannot be cancelled", nil)
	}

	if _, _, err := s.canceller.Cancel(ctx, tenantID, bookingID); err != nil {
		return nil, err
	}

	// Re-read for the fresh status/updated_at and the joined names.
	refreshed, err := s.reader.FindByTenantAndID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	return s.toDetail(ctx, tenantID, refreshed)
}

// Reschedule moves a CONFIRMED booking to a new date/start.
//
// This deliberately mirrors BookingService.CreatePublicBooking's own shape
// (validate shape -> ask S7 for the current slots -> require the requested
// start to be among them -> derive the absolute end from the service's
// authoritative duration -> write) rather than inventing a second
// availability pipeline — the one difference is GetAvailabilityExcludingBooking
// instead of GetAvailability, which excludes this booking's OWN current
// interval from occupancy so it can never appear to conflict with itself
// (see that method's doc comment). The final write is a single UPDATE
// gated on status = 'CONFIRMED', backed by the same bookings_no_overlap
// exclusion constraint Create relies on — Postgres itself is the last word
// on concurrency, exactly as it already is for booking creation.
//
// Price, duration snapshot policy, and service/staff identity are
// unaffected by this feature: the service's CURRENT duration is what
// determines the new end, the identical "no snapshot, current data is
// authoritative" rule migration 000016's own doc comment on model.Booking
// already establishes for receipts and detail views.
func (s *bookingManagementService) Reschedule(ctx context.Context, tenantID string, bookingID string, input RescheduleBookingInput) (*BookingDetail, error) {
	if err := validateBookingIdentifiers(tenantID, bookingID); err != nil {
		return nil, err
	}

	current, err := s.reader.FindByTenantAndID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	// A deterministic domain error for every non-reschedulable state: only a
	// CONFIRMED booking may move. CANCELLED, COMPLETED and NO_SHOW (S13-BE)
	// are all rejected identically here — this check predates S13-BE and
	// already covers its two new terminal statuses with no change needed.
	if current.Booking.Status != model.BookingConfirmed {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "only a confirmed booking can be rescheduled", nil)
	}

	requestedDate, err := availability.ParseDate(strings.TrimSpace(input.Date))
	if err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "date must be a calendar date in YYYY-MM-DD form", err)
	}
	requestedStart, err := model.ValidateClockTime(input.Start)
	if err != nil {
		return nil, err
	}

	avail, err := s.availability.GetAvailabilityExcludingBooking(
		ctx, tenantID, current.Booking.ServiceID, current.Booking.StaffID, requestedDate.String(), bookingID,
	)
	if err != nil {
		return nil, err
	}
	slot, ok := findSlot(avail.Slots, requestedStart)
	if !ok {
		return nil, apperrors.New(apperrors.CodeBookingSlotUnavailable, "the requested time is not an available slot", nil)
	}

	location, err := time.LoadLocation(avail.Timezone)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "availability returned an unloadable timezone", err)
	}
	startAt, err := availability.ResolveInstant(requestedDate, slot.Start, location)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "could not resolve the slot to an instant", err)
	}
	// The service's CURRENT duration, not a snapshot — the end is derived
	// the identical way CreatePublicBooking derives it, and re-validated by
	// GetAvailabilityExcludingBooking above (which already confirmed the
	// service is active with a positive duration).
	svc, err := s.services.FindByID(ctx, tenantID, current.Booking.ServiceID)
	if err != nil {
		return nil, err
	}
	endAt := startAt.Add(time.Duration(svc.DurationMinutes) * time.Minute)

	// Rescheduling to the exact instant the booking already occupies is a
	// legitimate no-op, not an error — the same idempotency convention
	// Cancel applies to an already-cancelled booking. No write happens.
	if startAt.Equal(current.Booking.StartAt) && endAt.Equal(current.Booking.EndAt) {
		return s.toDetail(ctx, tenantID, current)
	}

	if _, _, err := s.rescheduler.UpdateSchedule(ctx, tenantID, bookingID, startAt, endAt); err != nil {
		return nil, err
	}

	refreshed, err := s.reader.FindByTenantAndID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	return s.toDetail(ctx, tenantID, refreshed)
}

// Complete transitions a CONFIRMED booking to COMPLETED. See
// transitionToTerminal for the full rule set.
func (s *bookingManagementService) Complete(ctx context.Context, tenantID string, bookingID string) (*BookingDetail, error) {
	return s.transitionToTerminal(ctx, tenantID, bookingID, model.BookingCompleted)
}

// MarkNoShow transitions a CONFIRMED booking to NO_SHOW. See
// transitionToTerminal for the full rule set.
func (s *bookingManagementService) MarkNoShow(ctx context.Context, tenantID string, bookingID string) (*BookingDetail, error) {
	return s.transitionToTerminal(ctx, tenantID, bookingID, model.BookingNoShow)
}

// transitionToTerminal (S13-BE) is the shared implementation behind Complete
// and MarkNoShow — a small, closed rule set, not a general state machine:
//
//  1. Resolve the booking (tenant-scoped; missing/cross-tenant is
//     BOOKING_NOT_FOUND, identical to every other method here).
//  2. If the booking is ALREADY in the requested target status, return it
//     as-is with no write — the same idempotency convention Cancel applies
//     to an already-cancelled booking, so a duplicate owner click is
//     harmless.
//  3. Any other non-CONFIRMED status (CANCELLED, or the OTHER terminal
//     status) is a rejected transition: BOOKING_INVALID_TRANSITION (409).
//     COMPLETED <-> NO_SHOW is deliberately NOT permitted — the owner must
//     live with their first terminal choice in this version.
//  4. Otherwise the booking is CONFIRMED. It is only eligible once its
//     appointment has ended: current tenant-local time >= booking.end. Both
//     sides of that comparison are absolute instants, so the comparison
//     itself is timezone-independent, but resolving the tenant's IANA
//     timezone first (rather than comparing raw clock.Now() against EndAt
//     with no tenant context at all) keeps this method visibly authoritative
//     on tenant time, matching every other eligibility check in this
//     package (see resolveTenantLocation's other callers). A future booking
//     is BOOKING_INVALID_TRANSITION (409), not a 400 — the request is
//     syntactically fine, it is simply too early.
//  5. Write via BookingStatusUpdater.UpdateStatus (a single UPDATE gated on
//     status = 'CONFIRMED', the identical shape Cancel/UpdateSchedule use),
//     then re-read for the fresh row.
func (s *bookingManagementService) transitionToTerminal(ctx context.Context, tenantID string, bookingID string, target model.BookingStatus) (*BookingDetail, error) {
	if err := validateBookingIdentifiers(tenantID, bookingID); err != nil {
		return nil, err
	}

	current, err := s.reader.FindByTenantAndID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}

	switch current.Booking.Status {
	case target:
		return s.toDetail(ctx, tenantID, current)
	case model.BookingConfirmed:
		// proceed below
	default:
		return nil, apperrors.New(apperrors.CodeBookingInvalidTransition,
			"a "+string(current.Booking.Status)+" booking cannot be marked "+string(target), nil)
	}

	tenant, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	location, err := resolveTenantLocation(tenant)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now().In(location)
	end := current.Booking.EndAt.In(location)
	if now.Before(end) {
		return nil, apperrors.New(apperrors.CodeBookingInvalidTransition,
			"a booking cannot be marked "+string(target)+" before its appointment has ended", nil)
	}

	if _, _, err := s.statusUpdater.UpdateStatus(ctx, tenantID, bookingID, target); err != nil {
		return nil, err
	}

	refreshed, err := s.reader.FindByTenantAndID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	return s.toDetail(ctx, tenantID, refreshed)
}

func (s *bookingManagementService) toRepoFilter(ctx context.Context, tenantID string, filter BookingListFilter) (repository.BookingListFilter, error) {
	out := repository.BookingListFilter{StaffID: trimmedIDOrNil(filter.StaffID), ServiceID: trimmedIDOrNil(filter.ServiceID)}
	if out.StaffID != nil {
		if _, err := uuid.Parse(*out.StaffID); err != nil {
			return repository.BookingListFilter{}, apperrors.New(apperrors.CodeValidationFailed, "invalid staff id filter", nil)
		}
	}
	if out.ServiceID != nil {
		if _, err := uuid.Parse(*out.ServiceID); err != nil {
			return repository.BookingListFilter{}, apperrors.New(apperrors.CodeValidationFailed, "invalid service id filter", nil)
		}
	}

	confirmed := model.BookingConfirmed
	cancelled := model.BookingCancelled
	switch filter.View {
	case BookingViewUpcoming:
		out.Status, out.Window, out.Now = &confirmed, repository.BookingWindowUpcoming, s.clock.Now()
	case BookingViewPast:
		// S13-BE: PAST now means every non-CANCELLED booking that has
		// already started — CONFIRMED (not yet actioned), COMPLETED and
		// NO_SHOW all belong in the historical view; CANCELLED keeps its own
		// dedicated view regardless of when it was scheduled for.
		out.ExcludeStatus, out.Window, out.Now = &cancelled, repository.BookingWindowPast, s.clock.Now()
	case BookingViewCancelled:
		out.Status = &cancelled
	case BookingViewAll:
		// no status or window filter
	default:
		return repository.BookingListFilter{}, apperrors.New(apperrors.CodeValidationFailed, "invalid booking view", nil)
	}

	if date := trimmedIDOrNil(filter.Date); date != nil {
		parsedDate, err := availability.ParseDate(*date)
		if err != nil {
			return repository.BookingListFilter{}, apperrors.New(apperrors.CodeValidationFailed, "date must be a calendar date in YYYY-MM-DD form", err)
		}
		tenant, err := s.tenants.FindByID(ctx, tenantID)
		if err != nil {
			return repository.BookingListFilter{}, err
		}
		location, err := resolveTenantLocation(tenant)
		if err != nil {
			return repository.BookingListFilter{}, err
		}
		// [dayStart, dayEnd) in the TENANT's timezone — the identical
		// day-boundary construction availability_service.go's GetAvailability
		// uses, so "date=2026-09-12" means the same calendar day here as it
		// does for the public availability query, DST included.
		dayStart := time.Date(parsedDate.Year, parsedDate.Month, parsedDate.Day, 0, 0, 0, 0, location)
		dayEnd := dayStart.AddDate(0, 0, 1)
		out.StartAtFrom, out.StartAtTo = &dayStart, &dayEnd
	}

	return out, nil
}

func (s *bookingManagementService) toDetail(ctx context.Context, tenantID string, row *repository.BookingWithRelations) (*BookingDetail, error) {
	tenant, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	timezone := ""
	if tenant.Timezone != nil {
		timezone = strings.TrimSpace(*tenant.Timezone)
	}
	return &BookingDetail{BookingSummary: toSummary(row), Timezone: timezone}, nil
}

func toSummary(row *repository.BookingWithRelations) BookingSummary {
	b := row.Booking
	return BookingSummary{
		ID:              b.ID,
		Reference:       bookingReference(b.ID),
		Status:          b.Status,
		ServiceID:       b.ServiceID,
		ServiceName:     row.ServiceName,
		StaffID:         b.StaffID,
		StaffName:       row.StaffName,
		CustomerName:    b.Customer.Name,
		CustomerPhone:   b.Customer.Phone,
		CustomerEmail:   b.Customer.Email,
		StartAt:         b.StartAt.UTC(),
		EndAt:           b.EndAt.UTC(),
		DurationMinutes: row.ServiceDurationMins,
		CreatedAt:       b.CreatedAt.UTC(),
	}
}

func validateBookingIdentifiers(tenantID string, bookingID string) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	if _, err := uuid.Parse(bookingID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid booking id", err)
	}
	return nil
}

func trimmedIDOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// compile-time guard.
var _ BookingManagementService = (*bookingManagementService)(nil)
