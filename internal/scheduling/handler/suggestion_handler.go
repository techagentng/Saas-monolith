package handler

import (
	"net/http"

	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// SuggestionHandler serves the authenticated, tenant-scoped, read-only
// starter-service catalogue.
//
// It sits behind the identical Authentication -> Tenant Context ->
// Authorization chain every other ServiceHandler route uses, reusing
// service.read (SC1 adds no new permission for it, the same reasoning
// CategoryService documents) rather than introducing a bespoke one for a
// single read endpoint.
type SuggestionHandler struct {
	suggestions service.SuggestionService
}

func NewSuggestionHandler(suggestions service.SuggestionService) *SuggestionHandler {
	return &SuggestionHandler{suggestions: suggestions}
}

// ServiceSuggestion is the wire representation of one starter service. It has
// no id: a suggestion is never persisted and never referenced afterward (see
// the suggestions package's own doc comment), so there is nothing stable to
// key it by beyond the fields already shown.
//
// There is deliberately no price field, of any name. Price is salon-specific
// and is entered only when the tenant creates its real service — see
// suggestions.Suggestion's own doc comment for the full reasoning. This is
// the SC1 contract's hard boundary, not an oversight: a client must never be
// able to read a "suggested_price" off this endpoint and treat it as
// authoritative.
type ServiceSuggestion struct {
	Category                 string `json:"category"`
	Name                     string `json:"name"`
	Description              string `json:"description"`
	SuggestedDurationMinutes int    `json:"suggested_duration_minutes"`
}

// List handles GET /api/v1/tenants/{tenantID}/service-suggestions. A tenant
// with no business type yet, or one whose vertical has no starter catalogue
// defined, gets an empty list — a normal, successful response, not an error.
func (h *SuggestionHandler) List(writer http.ResponseWriter, request *http.Request, tenantID string) {
	list, err := h.suggestions.List(request.Context(), tenantID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	// [] rather than null for the empty case, the same discipline every other
	// listing endpoint in this module follows.
	result := make([]ServiceSuggestion, len(list))
	for i, suggestion := range list {
		result[i] = ServiceSuggestion{
			Category:                 suggestion.Category,
			Name:                     suggestion.Name,
			Description:              suggestion.Description,
			SuggestedDurationMinutes: suggestion.SuggestedDurationMinutes,
		}
	}
	writeJSON(writer, http.StatusOK, result)
}
