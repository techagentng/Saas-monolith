package availability

import (
	"fmt"
	"time"
)

// ResolveInstant converts a tenant-local wall-clock start ("HH:MM") on a civil
// date into an absolute instant in the given location — the same resolution
// Generate performs internally for its candidate slots (time.Date does the
// DST-aware offset lookup for that date; it does not assume a fixed UTC
// offset).
//
// S10 uses this to persist a booking's start_at once the requested start has
// been confirmed to match a slot Generate returned. Keeping it here, next to
// instantAt, is what stops the booking layer from re-deriving timezone maths
// the engine already owns.
func ResolveInstant(date Date, wallClock string, location *time.Location) (time.Time, error) {
	if location == nil {
		return time.Time{}, fmt.Errorf("availability: ResolveInstant needs a location")
	}
	minutesOfDay, ok := parseMinutesOfDay(wallClock)
	if !ok {
		return time.Time{}, fmt.Errorf("availability: %q is not an HH:MM wall-clock time", wallClock)
	}
	return instantAt(date, minutesOfDay, location), nil
}
