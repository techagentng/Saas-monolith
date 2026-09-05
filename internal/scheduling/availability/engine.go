// Package availability is the appointment-vertical slot engine.
//
// It answers exactly one question: given a technician's recurring working
// hours, a service duration, the intervals that technician is already
// committed for, and "now", what start times can that service be booked at on
// a given calendar date in the tenant's own timezone?
//
//	Working hours + Service duration + Occupied intervals - Past - Timezone
//	  → candidate start times
//
// It is deliberately NOT an appointment-vertical-agnostic scheduler. A hotel's
// room-night inventory, a restaurant's table capacity and a transport
// company's seat map are different resource models and get their own engines;
// nothing here is written to be reused by them.
//
// Everything in this file is pure: no database, no clock, no HTTP, no
// application-error types. "Now" and every input arrive as arguments, which is
// what makes the whole engine table-testable and what keeps DST correctness
// (see Generate) provable rather than hoped for. The orchestration that loads
// these inputs from real repositories lives in
// internal/scheduling/service/availability_service.go.
//
// Slot granularity policy (S7): the gap between consecutive candidate start
// times is the service duration itself (Query.SlotStep left zero). This is the
// minimal policy — every slot a service produces abuts the previous one, so a
// 30-minute service on 09:00-12:00 yields 09:00, 09:30, 10:00, 10:30, 11:00,
// 11:30. A future "booking interval" setting (a fixed 15-minute start grid,
// say) is introduced by populating Query.SlotStep; no other part of the engine
// changes.
package availability

import (
	"fmt"
	"sort"
	"time"
)

// Date is a civil calendar date — year, month, day, with no time-of-day and
// no zone. Availability is always requested for "a day", and that day only
// becomes a span of instants once paired with a *time.Location. Resolving that
// pairing is the engine's job (Generate), never the caller's, so a public
// caller cannot smuggle in an authoritative timezone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// dateLayout is the only accepted spelling of a requested date: 4-digit year,
// zero-padded month and day. It matches working_hours' discipline of a single
// canonical wire format.
const dateLayout = "2006-01-02"

// ParseDate accepts exactly YYYY-MM-DD. A missing zero, a two-digit year, an
// impossible day (2026-02-30), or trailing time components are all rejected —
// the same "reject rather than normalize a controlled format" rule
// model.ValidateClockTime applies to wall-clock times.
func ParseDate(value string) (Date, error) {
	parsed, err := time.ParseInLocation(dateLayout, value, time.UTC)
	if err != nil {
		return Date{}, fmt.Errorf("availability: date must be in YYYY-MM-DD form: %w", err)
	}
	if parsed.Format(dateLayout) != value {
		return Date{}, fmt.Errorf("availability: date must be in YYYY-MM-DD form")
	}
	return Date{Year: parsed.Year(), Month: parsed.Month(), Day: parsed.Day()}, nil
}

// String renders the date back in the canonical layout.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// Weekday is the day of week this civil date falls on. A calendar date is the
// same weekday everywhere on Earth, so this needs no location.
func (d Date) Weekday() time.Weekday {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Weekday()
}

// WorkingInterval is one span of a technician's working day, as local
// wall-clock "HH:MM" 24-hour times. The caller passes only the intervals that
// fall on the queried weekday; the engine does not filter by day.
type WorkingInterval struct {
	Start string
	End   string
}

// OccupiedInterval is a span the technician is already committed for,
// expressed as absolute instants. Absolute rather than wall-clock so the
// source can be persisted bookings (S10) with real timezone-resolved
// timestamps, and so overlap maths is unambiguous across a DST transition.
//
// Half-open [Start, End): a candidate that merely touches one — its end equal
// to the occupied start, or its start equal to the occupied end — is not a
// conflict, matching working_hours' own touching-boundary rule.
type OccupiedInterval struct {
	Start time.Time
	End   time.Time
}

// Slot is one bookable window, as local wall-clock "HH:MM" strings in the
// tenant's timezone — the same representation working hours are stored and
// returned in.
type Slot struct {
	Start string
	End   string
}

// Query is the complete, infrastructure-free input to Generate.
type Query struct {
	// Date is the civil date being asked about, interpreted in Location.
	Date Date
	// Location is the tenant's authoritative timezone. A nil Location yields
	// no slots rather than a panic.
	Location *time.Location
	// ServiceDuration is how long the service takes. Zero or negative yields
	// no slots.
	ServiceDuration time.Duration
	// WorkingIntervals are the technician's working spans for Date's weekday,
	// already filtered by the caller. Order does not matter; the result is
	// always sorted.
	WorkingIntervals []WorkingInterval
	// Occupied are spans the technician is already committed for. Only those
	// intersecting Date matter, but extras are harmless.
	Occupied []OccupiedInterval
	// Now is the reference instant for past-slot exclusion. A slot whose start
	// is strictly before Now is dropped.
	Now time.Time
	// SlotStep is the gap between consecutive candidate start times within an
	// interval. Zero means "use ServiceDuration" — the S7 policy.
	SlotStep time.Duration
}

// Generate returns the bookable slots for one service/technician/date, sorted
// ascending by start time, always non-nil.
//
// Algorithm, per working interval independently (a service never bridges the
// gap between two intervals — 11:30-13:00 is not a slot merely because there
// is working time on both sides of a 12:00-13:00 lunch break):
//
//  1. Step candidate start times from the interval start by SlotStep.
//  2. Keep a candidate only if it ends at or before the interval end
//     (a slot ending exactly at closing is valid; one ending after is not).
//  3. Resolve the candidate's wall-clock start against Location to an absolute
//     instant, and its end as that instant plus the true elapsed duration.
//  4. Drop it if that instant is strictly before Now.
//  5. Drop it if it overlaps any occupied interval (half-open; touching is
//     allowed).
//
// DST correctness: step 3 uses time.Date, which resolves a wall-clock time
// against Location's actual offset on that date — it does not assume a fixed
// UTC offset. A spring-forward day therefore has one fewer real hour and a
// fall-back day one more, and occupied-interval overlap is computed on the
// resolved instants, so a booking that spans the transition still excludes the
// right candidates. Wall-clock start times that do not exist (02:30 on a
// spring-forward morning) are normalized forward by time.Date rather than
// dropped — a booking UI still offers the label and stores the unambiguous
// instant.
func Generate(q Query) []Slot {
	slots := make([]Slot, 0)
	if q.Location == nil || q.ServiceDuration <= 0 {
		return slots
	}

	durationMinutes := int(q.ServiceDuration / time.Minute)
	if durationMinutes <= 0 {
		return slots
	}
	stepMinutes := durationMinutes
	if q.SlotStep > 0 {
		stepMinutes = int(q.SlotStep / time.Minute)
	}
	if stepMinutes <= 0 {
		return slots
	}

	seen := make(map[string]struct{})
	for _, interval := range q.WorkingIntervals {
		startOfDay, ok := parseMinutesOfDay(interval.Start)
		if !ok {
			continue
		}
		endOfDay, ok := parseMinutesOfDay(interval.End)
		if !ok || startOfDay >= endOfDay {
			continue
		}

		for candidateStart := startOfDay; candidateStart+durationMinutes <= endOfDay; candidateStart += stepMinutes {
			candidateEnd := candidateStart + durationMinutes

			startInstant := instantAt(q.Date, candidateStart, q.Location)
			endInstant := startInstant.Add(q.ServiceDuration)

			if startInstant.Before(q.Now) {
				continue
			}
			if overlapsAny(startInstant, endInstant, q.Occupied) {
				continue
			}

			start := formatMinutesOfDay(candidateStart)
			if _, duplicate := seen[start]; duplicate {
				continue
			}
			seen[start] = struct{}{}
			slots = append(slots, Slot{Start: start, End: formatMinutesOfDay(candidateEnd)})
		}
	}

	sort.Slice(slots, func(i, j int) bool { return slots[i].Start < slots[j].Start })
	return slots
}

// parseMinutesOfDay converts "HH:MM" to minutes since midnight. It reuses the
// same layout working_hours validates against, so anything that survived that
// validation parses here.
func parseMinutesOfDay(value string) (int, bool) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, false
	}
	return parsed.Hour()*60 + parsed.Minute(), true
}

// formatMinutesOfDay is parseMinutesOfDay's inverse. The maximum value it is
// ever handed is a working-hour end, at most 23:59, so no "24:00" wrap is
// possible.
func formatMinutesOfDay(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// instantAt resolves a wall-clock minute-of-day on a civil date against a
// location. time.Date does the DST-aware offset lookup.
func instantAt(date Date, minutesOfDay int, location *time.Location) time.Time {
	return time.Date(date.Year, date.Month, date.Day, minutesOfDay/60, minutesOfDay%60, 0, 0, location)
}

// overlapsAny reports whether [start, end) intersects any occupied interval,
// using the exact rule the S7 spec states: candidate.start < occupied.end &&
// candidate.end > occupied.start. Equality on either boundary is not an
// overlap.
func overlapsAny(start, end time.Time, occupied []OccupiedInterval) bool {
	for _, o := range occupied {
		if start.Before(o.End) && end.After(o.Start) {
			return true
		}
	}
	return false
}
