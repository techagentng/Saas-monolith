package model

import "testing"

func TestValidateDayOfWeekAcceptsAllSevenCanonicalValues(t *testing.T) {
	for _, day := range []string{"MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY", "SUNDAY"} {
		if got, err := ValidateDayOfWeek(day); err != nil || string(got) != day {
			t.Fatalf("ValidateDayOfWeek(%q) = (%q, %v), want (%q, nil)", day, got, err, day)
		}
	}
}

func TestValidateDayOfWeekRejectsUnknownAndLocaleDependentValues(t *testing.T) {
	for _, bad := range []string{"", "Monday", "monday", "Mon", "8", "FUNDAY"} {
		if _, err := ValidateDayOfWeek(bad); err == nil {
			t.Fatalf("ValidateDayOfWeek(%q) accepted an invalid day", bad)
		} else {
			assertValidationFailed(t, err, "ValidateDayOfWeek("+bad+")")
		}
	}
}

func TestValidateClockTimeAcceptsZeroPaddedTwentyFourHour(t *testing.T) {
	for _, value := range []string{"00:00", "09:00", "17:30", "23:59"} {
		if got, err := ValidateClockTime(value); err != nil || got != value {
			t.Fatalf("ValidateClockTime(%q) = (%q, %v), want (%q, nil)", value, got, err, value)
		}
	}
}

func TestValidateClockTimeRejectsMalformedValues(t *testing.T) {
	for _, bad := range []string{"", "9:00", "9am", "24:00", "12:60", "09:00:00", "09-00", "noon"} {
		if _, err := ValidateClockTime(bad); err == nil {
			t.Fatalf("ValidateClockTime(%q) accepted a malformed time", bad)
		} else {
			assertValidationFailed(t, err, "ValidateClockTime("+bad+")")
		}
	}
}

func TestValidateIntervalAcceptsAValidInterval(t *testing.T) {
	day, start, end, err := ValidateInterval("MONDAY", "09:00", "17:00")
	if err != nil {
		t.Fatalf("ValidateInterval() error = %v", err)
	}
	if day != Monday || start != "09:00" || end != "17:00" {
		t.Fatalf("ValidateInterval() = (%q, %q, %q), want (MONDAY, 09:00, 17:00)", day, start, end)
	}
}

func TestValidateIntervalRejectsStartEqualToEnd(t *testing.T) {
	_, _, _, err := ValidateInterval("MONDAY", "09:00", "09:00")
	if err == nil {
		t.Fatal("ValidateInterval() accepted start == end")
	}
	assertValidationFailed(t, err, "ValidateInterval(start == end)")
}

func TestValidateIntervalRejectsStartAfterEnd(t *testing.T) {
	_, _, _, err := ValidateInterval("MONDAY", "17:00", "09:00")
	if err == nil {
		t.Fatal("ValidateInterval() accepted start > end")
	}
	assertValidationFailed(t, err, "ValidateInterval(start > end)")
}

func TestValidateIntervalRejectsAnInvalidDay(t *testing.T) {
	_, _, _, err := ValidateInterval("FUNDAY", "09:00", "17:00")
	if err == nil {
		t.Fatal("ValidateInterval() accepted an invalid day")
	}
	assertValidationFailed(t, err, "ValidateInterval(invalid day)")
}

func interval(day DayOfWeek, start, end string) WorkingHourInterval {
	return WorkingHourInterval{DayOfWeek: day, StartTime: start, EndTime: end}
}

func TestValidateWeeklyScheduleAcceptsASingleInterval(t *testing.T) {
	got, err := ValidateWeeklySchedule([]WorkingHourInterval{interval(Monday, "09:00", "17:00")})
	if err != nil {
		t.Fatalf("ValidateWeeklySchedule() error = %v", err)
	}
	if len(got) != 1 || got[0].DayOfWeek != Monday {
		t.Fatalf("ValidateWeeklySchedule() = %+v, want one Monday interval", got)
	}
}

func TestValidateWeeklyScheduleAcceptsMultipleDays(t *testing.T) {
	schedule := []WorkingHourInterval{
		interval(Monday, "09:00", "17:00"),
		interval(Tuesday, "09:00", "17:00"),
		interval(Thursday, "10:00", "18:00"),
		interval(Friday, "09:00", "16:00"),
		interval(Saturday, "10:00", "14:00"),
	}
	got, err := ValidateWeeklySchedule(schedule)
	if err != nil {
		t.Fatalf("ValidateWeeklySchedule() error = %v", err)
	}
	if len(got) != len(schedule) {
		t.Fatalf("ValidateWeeklySchedule() returned %d intervals, want %d", len(got), len(schedule))
	}
}

func TestValidateWeeklyScheduleAcceptsASplitShift(t *testing.T) {
	got, err := ValidateWeeklySchedule([]WorkingHourInterval{
		interval(Monday, "13:00", "17:00"),
		interval(Monday, "09:00", "12:00"),
	})
	if err != nil {
		t.Fatalf("ValidateWeeklySchedule() error = %v", err)
	}
	// Sorted by start time within the day, regardless of submission order.
	if got[0].StartTime != "09:00" || got[1].StartTime != "13:00" {
		t.Fatalf("ValidateWeeklySchedule() did not sort by start time: %+v", got)
	}
}

// Documents the chosen boundary rule: 09:00-12:00 followed by 12:00-17:00
// touches but does not overlap, and is accepted.
func TestValidateWeeklyScheduleAcceptsTouchingBoundaries(t *testing.T) {
	_, err := ValidateWeeklySchedule([]WorkingHourInterval{
		interval(Monday, "09:00", "12:00"),
		interval(Monday, "12:00", "17:00"),
	})
	if err != nil {
		t.Fatalf("ValidateWeeklySchedule() rejected touching boundaries: %v", err)
	}
}

func TestValidateWeeklyScheduleRejectsOverlappingIntervals(t *testing.T) {
	_, err := ValidateWeeklySchedule([]WorkingHourInterval{
		interval(Monday, "09:00", "13:00"),
		interval(Monday, "12:00", "17:00"),
	})
	if err == nil {
		t.Fatal("ValidateWeeklySchedule() accepted overlapping intervals")
	}
	assertValidationFailed(t, err, "ValidateWeeklySchedule(overlap)")
}

func TestValidateWeeklyScheduleRejectsDuplicateIdenticalIntervals(t *testing.T) {
	_, err := ValidateWeeklySchedule([]WorkingHourInterval{
		interval(Monday, "09:00", "17:00"),
		interval(Monday, "09:00", "17:00"),
	})
	if err == nil {
		t.Fatal("ValidateWeeklySchedule() accepted a duplicate interval")
	}
	assertValidationFailed(t, err, "ValidateWeeklySchedule(duplicate)")
}

func TestValidateWeeklyScheduleRejectsAnInvalidDayAnywhereInTheSet(t *testing.T) {
	_, err := ValidateWeeklySchedule([]WorkingHourInterval{
		interval(Monday, "09:00", "17:00"),
		interval("FUNDAY", "09:00", "17:00"),
	})
	if err == nil {
		t.Fatal("ValidateWeeklySchedule() accepted an invalid day")
	}
	assertValidationFailed(t, err, "ValidateWeeklySchedule(invalid day)")
}

// Overlap and duplicate checks are per-day only: identical or overlapping
// clock times on two DIFFERENT days describe two different technicians'
// hours on two different days and must not interact.
func TestValidateWeeklyScheduleDoesNotCompareAcrossDifferentDays(t *testing.T) {
	got, err := ValidateWeeklySchedule([]WorkingHourInterval{
		interval(Monday, "09:00", "17:00"),
		interval(Tuesday, "09:00", "17:00"),
	})
	if err != nil {
		t.Fatalf("ValidateWeeklySchedule() error = %v, want identical hours on different days accepted", err)
	}
	if len(got) != 2 {
		t.Fatalf("ValidateWeeklySchedule() = %+v, want both intervals kept", got)
	}
}

func TestValidateWeeklyScheduleAcceptsAnEmptySchedule(t *testing.T) {
	got, err := ValidateWeeklySchedule(nil)
	if err != nil {
		t.Fatalf("ValidateWeeklySchedule(nil) error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ValidateWeeklySchedule(nil) = %+v, want empty", got)
	}
}

func TestValidateWeeklyScheduleReturnsResultsSortedByDayThenStartTime(t *testing.T) {
	got, err := ValidateWeeklySchedule([]WorkingHourInterval{
		interval(Friday, "09:00", "16:00"),
		interval(Monday, "13:00", "17:00"),
		interval(Monday, "09:00", "12:00"),
		interval(Wednesday, "09:00", "17:00"),
	})
	if err != nil {
		t.Fatalf("ValidateWeeklySchedule() error = %v", err)
	}
	wantOrder := []struct {
		day   DayOfWeek
		start string
	}{
		{Monday, "09:00"}, {Monday, "13:00"}, {Wednesday, "09:00"}, {Friday, "09:00"},
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("ValidateWeeklySchedule() returned %d intervals, want %d", len(got), len(wantOrder))
	}
	for i, want := range wantOrder {
		if got[i].DayOfWeek != want.day || got[i].StartTime != want.start {
			t.Fatalf("position %d = %s %s, want %s %s", i, got[i].DayOfWeek, got[i].StartTime, want.day, want.start)
		}
	}
}
