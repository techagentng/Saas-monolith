package handler

import (
	"net/http"

	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// PublicServiceHandler serves the anonymous, customer-facing service catalog
// for a NAIL_TECHNICIAN tenant (Catalog S8).
//
// It is deliberately not part of ServiceHandler: every ServiceHandler route
// sits behind Authentication → Tenant Context → Authorization, and mixing an
// anonymous endpoint into it invites wiring one into the wrong chain — the
// same separation PublicTenantHandler keeps from TenantHandler.
type PublicServiceHandler struct {
	catalog service.PublicCatalogService
}

func NewPublicServiceHandler(catalog service.PublicCatalogService) *PublicServiceHandler {
	return &PublicServiceHandler{catalog: catalog}
}

// PublicCatalogItem is the wire representation of one customer-facing service.
// Named to avoid colliding with ServiceHandler's own PublicService; the field
// set is the customer-safe subset — no status, no tenant id, no timestamps.
type PublicCatalogItem struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     *string `json:"description"`
	DurationMinutes int     `json:"duration_minutes"`
	PriceMinor      int64   `json:"price_minor"`
}

// PublicCatalogResponse is the whole payload: the currency prices are in
// (tenant-level, null until declared) and the bookable services.
type PublicCatalogResponse struct {
	Currency *string             `json:"currency"`
	Services []PublicCatalogItem `json:"services"`
}

// List handles GET /api/v1/public/tenants/{slug}/services.
//
// The slug comes from the route; this endpoint takes no body and reads no
// principal. A tenant that is not publicly visible, or a slug that does not
// resolve, is TENANT_NOT_FOUND — identical to a nonexistent one. A resolvable
// tenant that is not NAIL_TECHNICIAN is RESOURCE_NOT_FOUND. A nail tenant with
// no active services is a successful empty list.
func (h *PublicServiceHandler) List(writer http.ResponseWriter, request *http.Request, slug string) {
	catalog, err := h.catalog.GetCatalog(request.Context(), slug)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	// Services is built with make(..., len) so an empty catalog serializes as
	// [] rather than null — "no services available for online booking yet" is
	// an ordinary, successful response the client renders as an empty state.
	items := make([]PublicCatalogItem, len(catalog.Services))
	for i, svc := range catalog.Services {
		items[i] = PublicCatalogItem{
			ID:              svc.ID,
			Name:            svc.Name,
			Description:     svc.Description,
			DurationMinutes: svc.DurationMinutes,
			PriceMinor:      svc.PriceMinor,
		}
	}
	writeJSON(writer, http.StatusOK, PublicCatalogResponse{Currency: catalog.Currency, Services: items})
}
