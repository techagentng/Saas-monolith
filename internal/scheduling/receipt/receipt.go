// Package receipt renders a booking's public PDF receipt (S12-BE).
//
// It knows nothing about HTTP, tenant resolution, or persistence — Data is
// the complete, already-resolved, server-authoritative input, assembled by
// internal/scheduling/service.BookingReceiptService. This mirrors how
// internal/media.MediaStorage is a standalone infrastructure package the
// scheduling service layer depends on directly, rather than a
// locally-redeclared narrow interface.
package receipt

import "context"

// Data is everything one receipt is rendered from. Every field is resolved
// from persisted state by BookingReceiptService — nothing here is ever taken
// from a frontend-supplied request field.
type Data struct {
	BusinessName string
	// LogoURL is nil whenever the tenant has no logo configured — which, as
	// of S12-BE, is every tenant: no tenant-branding/logo storage exists
	// anywhere in the schema (confirmed by audit). The field exists so this
	// package is ready the day that storage is added, without a second
	// change here.
	LogoURL *string

	Reference string
	// Status is the verbatim booking status ("CONFIRMED" or "CANCELLED").
	// S12-BE's spec is explicit: a cancelled booking's receipt still renders,
	// showing its real status — never pretending it is still confirmed.
	Status string

	ServiceName     string
	DurationMinutes int
	TechnicianName  string

	Date     string // YYYY-MM-DD, tenant-local
	Start    string // HH:MM, tenant-local
	End      string // HH:MM, tenant-local
	Timezone string // IANA name, e.g. "Africa/Lagos"

	// FormattedPrice is already rendered for display (e.g. "₦15,000.00").
	// This package never touches a raw minor-unit integer or a currency
	// code directly, so it cannot itself misformat money — see
	// internal/scheduling/service's formatReceiptPrice.
	FormattedPrice string

	CustomerName  string
	CustomerEmail *string
	CustomerPhone *string
}

// Generator renders one receipt's PDF bytes from already-resolved Data. This
// is the seam BookingReceiptService depends on and the seam a test
// substitutes to assert what data WOULD be rendered without asserting raw
// PDF byte content.
type Generator interface {
	Generate(ctx context.Context, data Data) ([]byte, error)
}
