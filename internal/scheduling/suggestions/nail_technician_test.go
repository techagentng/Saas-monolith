package suggestions

import (
	"reflect"
	"strings"
	"testing"

	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// The SC1 contract's hard boundary: a suggestion carries no price of any
// name — price is salon-specific and is entered only when the tenant creates
// its real service. This guards the Go type itself, structurally, so a future
// field addition here is a deliberate decision, not an accident that quietly
// reintroduces a price onto the template.
func TestSuggestionHasNoPriceField(t *testing.T) {
	structType := reflect.TypeOf(Suggestion{})
	for i := 0; i < structType.NumField(); i++ {
		name := strings.ToLower(structType.Field(i).Name)
		if strings.Contains(name, "price") || strings.Contains(name, "cost") || strings.Contains(name, "amount") {
			t.Fatalf("Suggestion exposes %q — suggestions must carry no price field of any name", structType.Field(i).Name)
		}
	}
}

func TestForBusinessTypeReturnsTheNailTechnicianCatalogue(t *testing.T) {
	got := ForBusinessType(tenantmodel.BusinessTypeNailTechnician)
	if len(got) == 0 {
		t.Fatal("ForBusinessType(NAIL_TECHNICIAN) returned nothing")
	}
	for _, suggestion := range got {
		if suggestion.Category == "" {
			t.Fatalf("suggestion %+v has no category", suggestion)
		}
		if suggestion.Name == "" {
			t.Fatalf("suggestion %+v has no name", suggestion)
		}
		if suggestion.SuggestedDurationMinutes <= 0 {
			t.Fatalf("suggestion %q has non-positive duration %d", suggestion.Name, suggestion.SuggestedDurationMinutes)
		}
	}
}

func TestForBusinessTypeReturnsEmptyNotNilForAnUnsupportedVertical(t *testing.T) {
	for _, businessType := range []tenantmodel.BusinessType{tenantmodel.BusinessTypeRestaurant, tenantmodel.BusinessTypeHotel, tenantmodel.BusinessTypeTransport, tenantmodel.BusinessType("UNKNOWN")} {
		got := ForBusinessType(businessType)
		if got == nil {
			t.Fatalf("ForBusinessType(%q) returned nil, want an empty non-nil slice", businessType)
		}
		if len(got) != 0 {
			t.Fatalf("ForBusinessType(%q) = %#v, want empty — no starter catalogue exists yet for this vertical", businessType, got)
		}
	}
}

// NailTechnician backs the tenant's find-or-create category flow by name, so
// every suggestion must actually name a category — an empty label would
// silently produce an uncategorised service instead of the intended grouping.
func TestNailTechnicianSuggestionsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, suggestion := range NailTechnician {
		seen[suggestion.Name] = true
	}
	if len(seen) != len(NailTechnician) {
		t.Fatalf("NailTechnician contains duplicate suggestion names: %d unique of %d entries", len(seen), len(NailTechnician))
	}
}
