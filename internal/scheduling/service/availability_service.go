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

// AvailabilityServiceReader is the slice of the S1 catalog this engine needs:
// resolve one service within one tenant. Declared here, in the consumer,
// rather than importing the whole ServiceRepository — the same
// interface-segregation reasoning behind TenantReader and StaffReader.
type AvailabilityServiceReader interface {
	FindByID(ctx context.Context, tenantID string, serviceID string) (*model.Service, error)
}

// AvailabilityCapabilityReader answers "which services can this staff member
// perform" — the S3 staff/service capability set.
type AvailabilityCapabilityReader interface {
	ListServiceIDs(ctx context.Context, tenantID string, staffID string) ([]string, error)
}

// AvailabilityWorkingHoursReader is the S5 recurring weekly schedule for one
// staff member.
type AvailabilityWorkingHoursReader interface {
	ListByStaff(ctx context.Context, tenantID string, staffID string) ([]*model.WorkingHourInterval, error)
}

// OccupancyReader supplies the intervals a staff member is already committed
// for within a window, so availability can exclude them.
//
// S7 ships with NoOccupancy wired in: booking persistence does not exist yet
// (S10 builds it), so there is genuinely nothing to exclude. This interface is
// the single seam S10 plugs a real, booking-backed implementation into — the
// pure slot maths in package availability does not change when it does.
//
// The window [from, to) is the requested tenant-local day already resolved to
// instants. An implementation should return every committed interval that
// intersects it, as absolute instants.
type OccupancyReader interface {
	OccupiedIntervals(ctx context.Context, tenantID string, staffID string, from time.Time, to time.Time) ([]availability.OccupiedInterval, error)
}

// NoOccupancy is the S7 OccupancyReader: it reports no commitments, because no
// booking store exists yet. It is deliberately an explicit type rather than a
// nil check scattered through the service — swapping it for the S10
// implementation is a one-line change in app.New, and tests that need real
// conflicts substitute their own OccupancyReader.
type NoOccupancy struct{}

// OccupiedIntervals always returns none.
func (NoOccupancy) OccupiedIntervals(context.Context, string, string, time.Time, time.Time) ([]availability.OccupiedInterval, error) {
	return nil, nil
}

// Clock is the availability engine's only source of "now". Injected rather
// than calling time.Now() inside the domain logic so that past-slot filtering
// is deterministic under test — the explicit requirement S7 states.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production Clock.
type SystemClock struct{}

// Now returns the current instant.
func (SystemClock) Now() time.Time { return time.Now() }

// AvailabilityResult is the service-layer result: the echoed query context
// plus the computed slots. The handler maps this to the wire DTO.
type AvailabilityResult struct {
	Date      string
	Timezone  string
	ServiceID string
	StaffID   string
	Slots     []availability.Slot
}

// AvailabilityService computes, for the appointment vertical only, the slots
// at which one service can be booked with one technician on one date.
//
// It is a read-only composition over S1 (catalog), S3 (capability) and S5
// (working hours), resolved against the tenant's own authoritative timezone.
// It writes nothing and it does not create bookings — S10 does that, plugging
// real conflicts in through OccupancyReader.
//
// Tenant access and the staff.read permission are verified by the production
// middleware chain before any method here is reached. This service does not
// re-derive authorization; it does scope every repository call by tenantID, so
// a defect in that chain cannot become a cross-tenant read.
type AvailabilityService interface {
	// GetAvailability returns the bookable slots. date is a calendar date in
	// YYYY-MM-DD form, interpreted in the tenant's timezone. A caller-supplied
	// timezone is never consulted.
	//
	// Behaviour at the edges, all documented and tested:
	//   - a date entirely in the past                → zero slots, no error
	//   - today, slots whose start has already passed → excluded
	//   - a technician who exists but is not bookable → zero slots, no error
	//   - a day with no configured working hours       → zero slots, no error
	//   - a technician not assigned the service        → VALIDATION_FAILED
	//   - an archived service or archived technician    → *_NOT_FOUND (no longer offered)
	//   - a cross-tenant service or technician          → *_NOT_FOUND (no disclosure)
	//   - a tenant with a missing/invalid stored timezone → INTERNAL_ERROR
	//     (a configuration fault, never mislabelled as caller VALIDATION_FAILED)
	GetAvailability(ctx context.Context, tenantID string, serviceID string, staffID string, date string) (*AvailabilityResult, error)
}

type availabilityService struct {
	tenants      TenantReader
	services     AvailabilityServiceReader
	staff        StaffReader
	capabilities AvailabilityCapabilityReader
	hours        AvailabilityWorkingHoursReader
	occupancy    OccupancyReader
	clock        Clock
}

// NewAvailabilityService wires the engine over the existing S1/S3/S5
// repositories plus an OccupancyReader (NoOccupancy until S10) and a Clock
// (SystemClock in production).
func NewAvailabilityService(
	tenants TenantReader,
	services AvailabilityServiceReader,
	staff StaffReader,
	capabilities AvailabilityCapabilityReader,
	hours AvailabilityWorkingHoursReader,
	occupancy OccupancyReader,
	clock Clock,
) AvailabilityService {
	return &availabilityService{
		tenants:      tenants,
		services:     services,
		staff:        staff,
		capabilities: capabilities,
		hours:        hours,
		occupancy:    occupancy,
		clock:        clock,
	}
}

func (s *availabilityService) GetAvailability(ctx context.Context, tenantID string, serviceID string, staffID string, date string) (*AvailabilityResult, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	if _, err := uuid.Parse(serviceID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid service id", err)
	}
	if _, err := uuid.Parse(staffID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid staff id", err)
	}

	requestedDate, err := availability.ParseDate(date)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "date must be a calendar date in YYYY-MM-DD form", err)
	}

	// The tenant's own timezone is authoritative. A broken stored value is a
	// configuration fault, not a bad request — reported as an internal error
	// (and sanitized by errors.Map), never as VALIDATION_FAILED against the
	// caller, who supplied nothing wrong.
	tenant, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	location, err := resolveTenantLocation(tenant)
	if err != nil {
		return nil, err
	}

	svc, err := s.services.FindByID(ctx, tenantID, serviceID)
	if err != nil {
		return nil, err
	}
	// An archived service is no longer offered: there is nothing to book, so
	// it is reported exactly as a missing one.
	if svc.Status == model.StatusArchived {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}
	// The catalog and its CHECK constraint both guarantee a positive duration;
	// a non-positive value here means the row is corrupt, which is an internal
	// fault rather than caller error.
	if svc.DurationMinutes < model.MinDurationMinutes {
		return nil, apperrors.New(apperrors.CodeInternalError, "service duration is not a positive value", nil)
	}

	staffProfile, err := s.staff.FindByID(ctx, tenantID, staffID)
	if err != nil {
		return nil, err
	}
	// An archived technician no longer works here — reported as missing, the
	// same treatment an archived service gets.
	if staffProfile.Status == model.StatusArchived {
		return nil, apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)
	}

	// Capability: the technician must be assigned this service. Both the
	// service and the staff profile are already confirmed to belong to this
	// tenant, so returning a concrete "not performed" here cannot leak
	// cross-tenant existence — a foreign service or staff id already failed
	// above with *_NOT_FOUND.
	capableServiceIDs, err := s.capabilities.ListServiceIDs(ctx, tenantID, staffID)
	if err != nil {
		return nil, err
	}
	if !containsID(capableServiceIDs, serviceID) {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "the requested staff member does not perform the requested service", nil)
	}

	result := &AvailabilityResult{
		Date:      requestedDate.String(),
		Timezone:  location.String(),
		ServiceID: serviceID,
		StaffID:   staffID,
		Slots:     []availability.Slot{},
	}

	// A profile that exists but is not currently taking appointments has a
	// real, empty availability — not an error, and not a disclosure concern.
	if !staffProfile.IsBookable {
		return result, nil
	}

	allHours, err := s.hours.ListByStaff(ctx, tenantID, staffID)
	if err != nil {
		return nil, err
	}
	weekday := requestedDate.Weekday()
	daysIntervals := make([]availability.WorkingInterval, 0, len(allHours))
	for _, interval := range allHours {
		if weekdayOf(interval.DayOfWeek) == weekday {
			daysIntervals = append(daysIntervals, availability.WorkingInterval{
				Start: interval.StartTime,
				End:   interval.EndTime,
			})
		}
	}

	dayStart := time.Date(requestedDate.Year, requestedDate.Month, requestedDate.Day, 0, 0, 0, 0, location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	occupied, err := s.occupancy.OccupiedIntervals(ctx, tenantID, staffID, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}

	result.Slots = availability.Generate(availability.Query{
		Date:             requestedDate,
		Location:         location,
		ServiceDuration:  time.Duration(svc.DurationMinutes) * time.Minute,
		WorkingIntervals: daysIntervals,
		Occupied:         occupied,
		Now:              s.clock.Now(),
	})
	return result, nil
}

// resolveTenantLocation loads the tenant's IANA timezone. The empty-string
// check comes before time.LoadLocation on purpose: LoadLocation("") returns
// UTC, which would silently accept an unconfigured tenant — the same trap
// OnboardingService.validateOnboardingCompletionPrerequisites documents.
func resolveTenantLocation(tenant *tenantmodel.Tenant) (*time.Location, error) {
	if tenant.Timezone == nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "tenant has no configured timezone", nil)
	}
	name := strings.TrimSpace(*tenant.Timezone)
	if name == "" {
		return nil, apperrors.New(apperrors.CodeInternalError, "tenant has no configured timezone", nil)
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "tenant timezone is not a loadable IANA identifier", err)
	}
	return location, nil
}

// weekdayByDayOfWeek maps S5's string enum onto time.Weekday. S5 validates the
// stored value, so a lookup miss is not reachable through normal writes; the
// zero value (time.Sunday) is a safe-enough fallback if one ever occurs.
var weekdayByDayOfWeek = map[model.DayOfWeek]time.Weekday{
	model.Sunday:    time.Sunday,
	model.Monday:    time.Monday,
	model.Tuesday:   time.Tuesday,
	model.Wednesday: time.Wednesday,
	model.Thursday:  time.Thursday,
	model.Friday:    time.Friday,
	model.Saturday:  time.Saturday,
}

func weekdayOf(day model.DayOfWeek) time.Weekday { return weekdayByDayOfWeek[day] }

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// compile-time guards.
var (
	_ AvailabilityService = (*availabilityService)(nil)
	_ OccupancyReader     = NoOccupancy{}
	_ Clock               = SystemClock{}
)
