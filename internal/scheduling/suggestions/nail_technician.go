// Package suggestions holds the platform's static starter-service catalogue.
//
// It is backend-owned reference data, not tenant data: a suggestion is a
// template the owner copies into their own catalog (a plain "create a service
// with these values pre-filled"), after which the tenant owns the copy
// outright and this package is never consulted again for that row — there is
// no live link back here, no synchronization, and nothing here is ever
// persisted to a database. See model.ServiceCategory's own doc comment for
// the corresponding statement on the tenant-owned side of that boundary.
//
// This is deliberately a Go constant, not a seeded table: the set is small,
// changes only with a deploy, and needs none of the per-tenant editing,
// ordering, or lifecycle machinery service_categories/services already carry.
package suggestions

import (
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// Suggestion is one starter service a tenant can copy into its own catalog.
//
// There is deliberately no price field of any kind. Price is salon-specific
// — the same duration/service can be priced completely differently by two
// tenants in the same city — and the SC1 contract draws that line firmly at
// the template boundary: a suggestion supplies category, name, description
// and a duration to start from; the tenant supplies the price the moment it
// actually creates a real service (CreateServiceInput.PriceMinor). Adding a
// price here, even labeled as a mere starting point, would blur exactly that
// boundary the contract exists to keep sharp.
type Suggestion struct {
	// Category is the starter category name this suggestion is grouped
	// under. It is a plain label, not a foreign key — selecting a suggestion
	// finds-or-creates a tenant ServiceCategory row with this name; there is
	// no shared "template category" entity (see model.ServiceCategory).
	Category    string
	Name        string
	Description string
	// SuggestedDurationMinutes is explicitly named "suggested" (rather than
	// reusing model.Service's own DurationMinutes) so nothing about this type
	// reads as though it already carries the authority a real service's
	// fields do — it is a starting point, not a value that lands unedited.
	SuggestedDurationMinutes int
}

// NailTechnician is the fixed starter catalogue offered to a NAIL_TECHNICIAN
// tenant, grouped under five common salon-menu categories. Durations are
// deliberately modest, round starting points an owner is expected to adjust
// to their own timing — and, per this type's own doc comment, there is no
// price here at all, suggested or otherwise.
var NailTechnician = []Suggestion{
	{Category: "Natural Nails", Name: "Classic Manicure", Description: "Nail shaping, cuticle care and polish.", SuggestedDurationMinutes: 30},
	{Category: "Natural Nails", Name: "Classic Pedicure", Description: "Soak, exfoliation, nail care and polish.", SuggestedDurationMinutes: 45},
	{Category: "Natural Nails", Name: "Gel Polish Manicure", Description: "Classic manicure finished with long-lasting gel polish.", SuggestedDurationMinutes: 45},
	{Category: "Extensions", Name: "Acrylic Full Set", Description: "Full set of acrylic nail extensions, shaped and polished.", SuggestedDurationMinutes: 90},
	{Category: "Extensions", Name: "Gel Extension Full Set", Description: "Full set of gel nail extensions, shaped and polished.", SuggestedDurationMinutes: 90},
	{Category: "Extensions", Name: "Nail Extension Infill", Description: "Refill for existing acrylic or gel extensions.", SuggestedDurationMinutes: 60},
	{Category: "Pedicures", Name: "Spa Pedicure", Description: "Extended soak, scrub, mask and massage, finished with polish.", SuggestedDurationMinutes: 60},
	{Category: "Add-Ons", Name: "Nail Art (per hand)", Description: "Custom hand-painted nail art added to any service.", SuggestedDurationMinutes: 15},
	{Category: "Add-Ons", Name: "Paraffin Wax Treatment", Description: "Moisturizing paraffin wax dip added to any manicure or pedicure.", SuggestedDurationMinutes: 15},
	{Category: "Packages", Name: "Mani-Pedi Combo", Description: "Classic manicure and classic pedicure booked together.", SuggestedDurationMinutes: 75},
}

// ForBusinessType returns the suggestion set for one business type. An
// unrecognized or not-yet-supported type returns an empty, non-nil slice, not
// an error: "nothing to suggest yet" is this package's normal, successful
// answer for a vertical it has no starter catalogue for, the same
// reasoning PublicCatalogService.GetCatalog uses for an empty active catalog.
func ForBusinessType(businessType tenantmodel.BusinessType) []Suggestion {
	switch businessType {
	case tenantmodel.BusinessTypeNailTechnician:
		return NailTechnician
	default:
		return []Suggestion{}
	}
}
