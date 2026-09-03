package availability

import (
	"testing"
	"time"
)

// distantPast is a Now far enough back that the past-slot filter never fires
// for the 2026 dates these tests use. Tests that specifically exercise past
// filtering set their own Now.
var distantPast = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

// sept7 2026 is a Monday — the weekday is irrelevant to the engine (the caller
// pre-filters working intervals) but a real date keeps the instant maths
// honest.
func sept7() Date { return Date{Year: 2026, Month: time.September, Day: 7} }

func starts(slots []Slot) []string {
	out := make([]string, len(slots))
	for i, s := range slots {
		out[i] = s.Start
	}
	return out
}

func assertSlots(t *testing.T, got []Slot, want []string) {
	t.Helper()
	if got == nil {
		t.Fatal("Generate returned a nil slice; it must always be non-nil")
	}
	gotStarts := starts(got)
	if len(gotStarts) != len(want) {
		t.Fatalf("slot starts = %v, want %v", gotStarts, want)
	}
	for i := range want {
		if gotStarts[i] != want[i] {
			t.Fatalf("slot starts = %v, want %v", gotStarts, want)
		}
	}
}

// --- ParseDate --------------------------------------------------------------

func TestParseDateAcceptsOnlyTheCanonicalLayout(t *testing.T) {
	good, err := ParseDate("2026-09-05")
	if err != nil {
		t.Fatalf("ParseDate(valid): %v", err)
	}
	if good.String() != "2026-09-05" {
		t.Fatalf("round-trip = %q, want 2026-09-05", good.String())
	}

	for _, bad := range []string{"2026-9-5", "2026/09/05", "05-09-2026", "2026-02-30", "2026-13-01", "2026-09-05T10:00", "", "today"} {
		if _, err := ParseDate(bad); err == nil {
			t.Fatalf("ParseDate(%q) accepted an invalid date", bad)
		}
	}
}

func TestWeekdayIsTimezoneIndependent(t *testing.T) {
	if got := (Date{2026, time.September, 7}).Weekday(); got != time.Monday {
		t.Fatalf("Weekday = %v, want Monday", got)
	}
}

// --- core slot generation --------------------------------------------------

func TestThirtyMinuteServiceInsideOneInterval(t *testing.T) {
	got := Generate(Query{
		Date:             sept7(),
		Location:         time.UTC,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "12:00"}},
		Now:              distantPast,
	})
	assertSlots(t, got, []string{"09:00", "09:30", "10:00", "10:30", "11:00", "11:30"})
	if got[len(got)-1].End != "12:00" {
		t.Fatalf("last slot end = %q, want 12:00", got[len(got)-1].End)
	}
}

func TestSixtyMinuteService(t *testing.T) {
	got := Generate(Query{
		Date:             sept7(),
		Location:         time.UTC,
		ServiceDuration:  60 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "12:00"}},
		Now:              distantPast,
	})
	assertSlots(t, got, []string{"09:00", "10:00", "11:00"})
}

func TestIntervalShorterThanServiceYieldsNoSlots(t *testing.T) {
	got := Generate(Query{
		Date:             sept7(),
		Location:         time.UTC,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "09:20"}},
		Now:              distantPast,
	})
	assertSlots(t, got, nil)
}

func TestExactFitServiceYieldsOneSlot(t *testing.T) {
	got := Generate(Query{
		Date:             sept7(),
		Location:         time.UTC,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "09:30"}},
		Now:              distantPast,
	})
	assertSlots(t, got, []string{"09:00"})
	if got[0].End != "09:30" {
		t.Fatalf("slot end = %q, want 09:30", got[0].End)
	}
}

func TestCandidateEndingExactlyAtClosingIsValid(t *testing.T) {
	got := Generate(Query{
		Date:             sept7(),
		Location:         time.UTC,
		ServiceDuration:  60 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "10:00"}},
		Now:              distantPast,
	})
	assertSlots(t, got, []string{"09:00"})
}

func TestCandidateEndingAfterClosingIsInvalid(t *testing.T) {
	got := Generate(Query{
		Date:             sept7(),
		Location:         time.UTC,
		ServiceDuration:  60 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "09:59"}},
		Now:              distantPast,
	})
	assertSlots(t, got, nil)
}

func TestEmptyWorkingHoursYieldNoSlots(t *testing.T) {
	got := Generate(Query{
		Date:            sept7(),
		Location:        time.UTC,
		ServiceDuration: 30 * time.Minute,
		Now:             distantPast,
	})
	assertSlots(t, got, nil)
}

// --- split working hours --------------------------------------------------

func TestSplitWorkingHoursGenerateSlotsPerIntervalIndependently(t *testing.T) {
	got := Generate(Query{
		Date:            sept7(),
		Location:        time.UTC,
		ServiceDuration: 60 * time.Minute,
		WorkingIntervals: []WorkingInterval{
			{"09:00", "12:00"},
			{"13:00", "17:00"},
		},
		Now: distantPast,
	})
	assertSlots(t, got, []string{"09:00", "10:00", "11:00", "13:00", "14:00", "15:00", "16:00"})
}

func TestServiceNeverCrossesAWorkingHourGap(t *testing.T) {
	got := Generate(Query{
		Date:            sept7(),
		Location:        time.UTC,
		ServiceDuration: 90 * time.Minute,
		WorkingIntervals: []WorkingInterval{
			{"09:00", "12:00"},
			{"13:00", "17:00"},
		},
		Now: distantPast,
	})
	// 09:00 morning interval: 09:00-10:30, 10:30-12:00. Nothing starting
	// 11:30 (would end 13:00, bridging the gap) or 12:00.
	assertSlots(t, got, []string{"09:00", "10:30", "13:00", "14:30"})
	for _, s := range got {
		if s.Start == "11:30" || s.Start == "12:00" {
			t.Fatalf("a slot at %q bridged the 12:00-13:00 gap", s.Start)
		}
	}
}

func TestTouchingWorkingHourBoundariesProduceContiguousSlotsWithoutDuplicates(t *testing.T) {
	got := Generate(Query{
		Date:            sept7(),
		Location:        time.UTC,
		ServiceDuration: 60 * time.Minute,
		WorkingIntervals: []WorkingInterval{
			{"09:00", "12:00"},
			{"12:00", "15:00"},
		},
		Now: distantPast,
	})
	assertSlots(t, got, []string{"09:00", "10:00", "11:00", "12:00", "13:00", "14:00"})
}

func TestResultIsDeterministicallyOrderedRegardlessOfInputOrder(t *testing.T) {
	got := Generate(Query{
		Date:            sept7(),
		Location:        time.UTC,
		ServiceDuration: 60 * time.Minute,
		WorkingIntervals: []WorkingInterval{
			{"13:00", "15:00"},
			{"09:00", "11:00"},
		},
		Now: distantPast,
	})
	assertSlots(t, got, []string{"09:00", "10:00", "13:00", "14:00"})
}

// --- past-slot exclusion -------------------------------------------------

func TestSlotsWhoseStartHasAlreadyPassedAreExcluded(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	now := time.Date(2026, 9, 7, 10, 15, 0, 0, loc)
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "12:00"}},
		Now:              now,
	})
	assertSlots(t, got, []string{"10:30", "11:00", "11:30"})
}

func TestASlotStartingExactlyNowIsStillOffered(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, loc)
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "12:00"}},
		Now:              now,
	})
	assertSlots(t, got, []string{"10:00", "10:30", "11:00", "11:30"})
}

func TestADateEntirelyInThePastYieldsNoSlots(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	got := Generate(Query{
		Date:             Date{2020, time.January, 6},
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "17:00"}},
		Now:              time.Date(2026, 9, 7, 10, 0, 0, 0, loc),
	})
	assertSlots(t, got, nil)
}

func TestAFutureDateIsUnaffectedByNow(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  60 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "12:00"}},
		Now:              time.Date(2026, 9, 6, 23, 59, 0, 0, loc),
	})
	assertSlots(t, got, []string{"09:00", "10:00", "11:00"})
}

// --- occupied intervals -------------------------------------------------

// occupiedAt builds an absolute interval from wall-clock times on sept7 in loc.
func occupiedAt(loc *time.Location, startHHMM, endHHMM string) OccupiedInterval {
	s, _ := time.ParseInLocation("2006-01-02 15:04", "2026-09-07 "+startHHMM, loc)
	e, _ := time.ParseInLocation("2006-01-02 15:04", "2026-09-07 "+endHHMM, loc)
	return OccupiedInterval{Start: s, End: e}
}

func TestOccupiedFullCollisionRemovesExactlyThatSlot(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "11:00"}},
		Occupied:         []OccupiedInterval{occupiedAt(loc, "09:30", "10:00")},
		Now:              distantPast,
	})
	assertSlots(t, got, []string{"09:00", "10:00", "10:30"})
}

func TestOccupiedPartialCollisionRemovesEveryOverlappingSlot(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "11:00"}},
		Occupied:         []OccupiedInterval{occupiedAt(loc, "09:15", "09:45")},
		Now:              distantPast,
	})
	assertSlots(t, got, []string{"10:00", "10:30"})
}

func TestOccupiedIntervalContainedInsideACandidate(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  60 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "11:00"}},
		Occupied:         []OccupiedInterval{occupiedAt(loc, "09:15", "09:30")},
		Now:              distantPast,
	})
	assertSlots(t, got, []string{"10:00"})
}

func TestCandidateContainedInsideAnOccupiedInterval(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "11:00"}},
		Occupied:         []OccupiedInterval{occupiedAt(loc, "08:00", "12:00")},
		Now:              distantPast,
	})
	assertSlots(t, got, nil)
}

func TestOccupiedBoundaryTouchIsAllowed(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "10:30"}},
		Occupied: []OccupiedInterval{
			occupiedAt(loc, "08:30", "09:00"), // ends exactly as 09:00 slot starts
			occupiedAt(loc, "10:00", "10:30"), // starts exactly as 09:30 slot ends
		},
		Now: distantPast,
	})
	assertSlots(t, got, []string{"09:00", "09:30"})
}

func TestMultipleOccupiedIntervals(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos")
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "12:00"}},
		Occupied: []OccupiedInterval{
			occupiedAt(loc, "09:00", "09:30"),
			occupiedAt(loc, "10:30", "11:00"),
		},
		Now: distantPast,
	})
	assertSlots(t, got, []string{"09:30", "10:00", "11:00", "11:30"})
}

// --- timezone / DST ----------------------------------------------------

func TestAfricaLagosSlotsResolveAgainstTheZoneNotUTC(t *testing.T) {
	loc := mustLoad(t, "Africa/Lagos") // UTC+1, no DST
	// Now is 08:30 UTC == 09:30 Lagos. The 09:00 slot has passed; 09:30 has
	// not. If the engine wrongly treated the wall-clock times as UTC, 09:00
	// (== 09:00 UTC) would still be in the future and would survive.
	now := time.Date(2026, 9, 7, 8, 30, 0, 0, time.UTC)
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "11:00"}},
		Now:              now,
	})
	assertSlots(t, got, []string{"09:30", "10:00", "10:30"})
}

func TestAnotherIANATimezone(t *testing.T) {
	loc := mustLoad(t, "Asia/Kolkata")                  // UTC+5:30, no DST
	now := time.Date(2026, 9, 7, 4, 15, 0, 0, time.UTC) // 09:45 Kolkata
	got := Generate(Query{
		Date:             sept7(),
		Location:         loc,
		ServiceDuration:  30 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"09:00", "11:00"}},
		Now:              now,
	})
	assertSlots(t, got, []string{"10:00", "10:30"})
}

// On the US spring-forward day, 02:00-03:00 wall-clock does not exist. The
// engine still offers wall-clock labels stepping by the hour; because conflict
// detection is on resolved instants (not wall strings), a booking that lands
// wholly inside real working time still excludes exactly the right labels even
// across the transition.
func TestDaylightSavingSpringForwardTransition(t *testing.T) {
	loc := mustLoad(t, "America/New_York")
	springForward := Date{2026, time.March, 8}

	base := Generate(Query{
		Date:             springForward,
		Location:         loc,
		ServiceDuration:  60 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"00:00", "06:00"}},
		Now:              distantPast,
	})
	assertSlots(t, base, []string{"00:00", "01:00", "02:00", "03:00", "04:00", "05:00"})

	// A booking on the real instants 04:00-06:00 EDT (08:00-10:00 UTC), well
	// clear of the skipped hour, so overlap is unambiguous.
	booking := OccupiedInterval{
		Start: time.Date(2026, 3, 8, 4, 0, 0, 0, loc),
		End:   time.Date(2026, 3, 8, 6, 0, 0, 0, loc),
	}
	withBooking := Generate(Query{
		Date:             springForward,
		Location:         loc,
		ServiceDuration:  60 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"00:00", "06:00"}},
		Occupied:         []OccupiedInterval{booking},
		Now:              distantPast,
	})
	assertSlots(t, withBooking, []string{"00:00", "01:00", "02:00", "03:00"})
}

func TestDaylightSavingFallBackTransitionKeepsSlotMathsConsistent(t *testing.T) {
	loc := mustLoad(t, "America/New_York")
	fallBack := Date{2026, time.November, 1} // 02:00 EDT -> 01:00 EST repeats

	got := Generate(Query{
		Date:             fallBack,
		Location:         loc,
		ServiceDuration:  60 * time.Minute,
		WorkingIntervals: []WorkingInterval{{"00:00", "05:00"}},
		Now:              distantPast,
	})
	// Wall-clock labels remain a clean hourly walk; the engine does not
	// duplicate or drop the repeated 01:00 hour at the label level.
	assertSlots(t, got, []string{"00:00", "01:00", "02:00", "03:00", "04:00"})
}

// --- defensive inputs ------------------------------------------------

func TestNilLocationOrNonPositiveDurationYieldNoSlots(t *testing.T) {
	if got := Generate(Query{Date: sept7(), ServiceDuration: 30 * time.Minute, WorkingIntervals: []WorkingInterval{{"09:00", "17:00"}}, Now: distantPast}); len(got) != 0 {
		t.Fatalf("nil Location produced %v", got)
	}
	if got := Generate(Query{Date: sept7(), Location: time.UTC, ServiceDuration: 0, WorkingIntervals: []WorkingInterval{{"09:00", "17:00"}}, Now: distantPast}); len(got) != 0 {
		t.Fatalf("zero duration produced %v", got)
	}
}

func TestMalformedWorkingIntervalIsSkippedNotFatal(t *testing.T) {
	got := Generate(Query{
		Date:            sept7(),
		Location:        time.UTC,
		ServiceDuration: 30 * time.Minute,
		WorkingIntervals: []WorkingInterval{
			{"09:00", "9am"},   // malformed end — skipped
			{"14:00", "13:00"}, // inverted — skipped
			{"10:00", "11:00"}, // valid
		},
		Now: distantPast,
	})
	assertSlots(t, got, []string{"10:00", "10:30"})
}
