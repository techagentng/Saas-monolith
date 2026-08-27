package handler

import (
	"encoding/json"
	"net/http"

	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

// PublicTenantHandler serves the unauthenticated tenant identity lookup.
//
// This handler is deliberately not part of TenantHandler: TenantHandler's
// methods all sit behind authentication and tenant context, and mixing an
// anonymous endpoint into it invites wiring one into the wrong chain.
type PublicTenantHandler struct {
	publicService service.PublicTenantService
}

func NewPublicTenantHandler(publicService service.PublicTenantService) *PublicTenantHandler {
	return &PublicTenantHandler{publicService: publicService}
}

// PublicTenantIdentity is the anonymous-facing tenant representation.
//
// It carries no internal tenant UUID, no lifecycle status, no timestamps, and
// none of the private business contact details from Feature 4. Optional fields
// keep a stable shape and serialize as null when unset, matching the rest of
// the project's public DTOs.
//
// BusinessType (Vertical Onboarding F3) is the one deliberate addition beyond
// Feature 5's original four fields, so a future public vertical router can
// pick the right customer experience. onboarding_status/onboarding_step are
// workflow internals and are deliberately never added here — F3's public
// addition is business_type, not onboarding progress.
type PublicTenantIdentity struct {
	Slug         string              `json:"slug"`
	Name         string              `json:"name"`
	Description  *string             `json:"description"`
	Timezone     *string             `json:"timezone"`
	BusinessType *model.BusinessType `json:"business_type"`
}

// GetBySlug resolves a tenant's public identity from its slug. The slug comes
// from the route; this endpoint takes no request body and reads no principal.
func (h *PublicTenantHandler) GetBySlug(writer http.ResponseWriter, request *http.Request, slug string) {
	identity, err := h.publicService.GetBySlug(request.Context(), slug)
	if err != nil {
		writeTenantError(writer, err)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(PublicTenantIdentity{
		Slug:         identity.Slug,
		Name:         identity.Name,
		Description:  identity.Description,
		Timezone:     identity.Timezone,
		BusinessType: identity.BusinessType,
	})
}
