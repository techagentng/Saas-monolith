package model

import (
	"fmt"
	"sort"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

// DayOfWeek is a closed vocabulary, mirrored by the
// staff_working_hours_day_valid CHECK constraint in migration 000015 — the
// same string-enum discipline Status already uses, chosen over an integer
// 0-6 so a row is legible in a raw query without a lookup table, and over a
// locale-dependent name so no reader's timezone or language setting can
// change what a value means.
type DayOfWeek string

const (
	Monday    DayOfWeek = "MONDAY"
	Tuesday   DayOfWeek = "TUESDAY"
	Wednesday DayOfWeek = "WEDNESDAY"
	Thursday  DayOfWeek = "THURSDAY"
	Friday    DayOfWeek = "FRIDAY"
	Saturday  DayOfWeek = "SATURDAY"
	Sunday    DayOfWeek = "SUNDAY"
)

// daysInOrder is the canonical week ordering used to sort a schedule for
// display — ISO 8601's Monday-first convention, not the caller's locale.
var daysInOrder = []DayOfWeek{Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday}

// dayOrdinal maps a valid day onto its position in daysInOrder. Built once
// rather than searched linearly on every sort comparison.
var dayOrdinal = func() map[DayOfWeek]int {
	m := make(map[DayOfWeek]int, len(daysInOrder))
	for i, day := range daysInOrder {
		m[day] = i
	}
	return m
}()

// ValidateDayOfWeek accepts only the seven canonical values.
func ValidateDayOfWeek(value string) (DayOfWeek, error) {
	day := DayOfWeek(value)
	if _, ok := dayOrdinal[day]; !ok {
		return "", apperrors.New(apperrors.CodeValidationFailed, "invalid day of week", nil)
	}
	return day, nil
}

// clockTimeLayout is the only format a wall-clock time is accepted or
// produced in: 24-hour, zero-padded, no seconds — "09:00", "17:30". Seconds
// are rejected rather than silently truncated, so an interval this service
// returns is always exactly what a caller could have submitted.
const clockTimeLayout = "15:04"

// ValidateClockTime parses and re-normalizes a wall-clock time string.
// Malformed input — missing the leading zero, carrying seconds, out of
// range, or not a time at all — is rejected here rather than reaching the
// database, where a driver error would be far less presentable.
//
// The re-normalization matters: it is what makes ValidateClockTime the single
// point two differently-spelled-but-equal times (there is only one legal
// spelling under this layout, but this guards against any future loosening of
// the format) would need to agree at, so overlap comparison downstream can use
// plain string equality.
func ValidateClockTime(value string) (string, error) {
	parsed, err := time.Parse(clockTimeLayout, value)
	if err != nil {
		return "", apperrors.New(apperrors.CodeValidationFailed, "invalid time of day, expected HH:MM", err)
	}
	normalized := parsed.Format(clockTimeLayout)
	if normalized != value {
		return "", apperrors.New(apperrors.CodeValidationFailed, "invalid time of day, expected HH:MM", nil)
	}
	return normalized, nil
}

// WorkingHourInterval is one recurring interval of a staff member's weekly
// schedule.
//
// It is deliberately not an appointment slot, a break, a holiday, a one-off
// exception, or an availability calculation — S7's availability engine
// consumes this table; nothing here computes bookable time. StartTime and
// EndTime are wall-clock local business time in the tenant's own timezone,
// never a UTC instant: converting that is explicitly out of scope here.
type WorkingHourInterval struct {
	ID        string
	TenantID  string
	StaffID   string
	DayOfWeek DayOfWeek
	StartTime string
	EndTime   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ValidateInterval checks one interval in isolation: a valid day, valid
// times, and start strictly before end. Touching another interval, or
// duplicating one, is a property of the whole schedule and is checked by
// ValidateWeeklySchedule instead.
func ValidateInterval(day string, start string, end string) (DayOfWeek, string, string, error) {
	validDay, err := ValidateDayOfWeek(day)
	if err != nil {
		return "", "", "", err
	}
	validStart, err := ValidateClockTime(start)
	if err != nil {
		return "", "", "", err
	}
	validEnd, err := ValidateClockTime(end)
	if err != nil {
		return "", "", "", err
	}
	if validStart >= validEnd {
		// Safe as a plain string comparison because ValidateClockTime has
		// already forced both onto the identical zero-padded HH:MM layout,
		// under which lexicographic and chronological order coincide.
		return "", "", "", apperrors.New(apperrors.CodeValidationFailed, "start time must be before end time", nil)
	}
	return validDay, validStart, validEnd, nil
}

// ValidateWeeklySchedule validates every interval individually, then checks
// the set as a whole: no two intervals on the same day may be identical, and
// no two may overlap. Touching boundaries (one interval's end equal to the
// next one's start) are explicitly ALLOWED — 09:00-12:00 and 12:00-17:00 on
// the same day is a normal adjacent pair, not an overlap, because neither
// instant belongs to both intervals under a half-open [start, end) reading.
//
// The result is sorted by day then start time — the same deterministic
// ordering ListWorkingHours returns, so a caller validating and a caller
// reading see identical ordering.
func ValidateWeeklySchedule(intervals []WorkingHourInterval) ([]WorkingHourInterval, error) {
	validated := make([]WorkingHourInterval, 0, len(intervals))
	for _, interval := range intervals {
		day, start, end, err := ValidateInterval(string(interval.DayOfWeek), interval.StartTime, interval.EndTime)
		if err != nil {
			return nil, err
		}
		validated = append(validated, WorkingHourInterval{DayOfWeek: day, StartTime: start, EndTime: end})
	}

	sort.Slice(validated, func(i, j int) bool {
		if validated[i].DayOfWeek != validated[j].DayOfWeek {
			return dayOrdinal[validated[i].DayOfWeek] < dayOrdinal[validated[j].DayOfWeek]
		}
		return validated[i].StartTime < validated[j].StartTime
	})

	for i := 1; i < len(validated); i++ {
		previous, current := validated[i-1], validated[i]
		if previous.DayOfWeek != current.DayOfWeek {
			continue
		}
		if previous.StartTime == current.StartTime && previous.EndTime == current.EndTime {
			return nil, apperrors.New(apperrors.CodeValidationFailed,
				fmt.Sprintf("duplicate interval on %s: %s-%s", current.DayOfWeek, current.StartTime, current.EndTime), nil)
		}
		// current.StartTime < previous.EndTime is a genuine overlap. Equal is
		// the allowed touching-boundary case and falls through.
		if current.StartTime < previous.EndTime {
			return nil, apperrors.New(apperrors.CodeValidationFailed,
				fmt.Sprintf("overlapping intervals on %s: %s-%s and %s-%s",
					current.DayOfWeek, previous.StartTime, previous.EndTime, current.StartTime, current.EndTime), nil)
		}
	}

	return validated, nil
}
