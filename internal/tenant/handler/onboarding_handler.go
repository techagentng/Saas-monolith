package handler

import (
	"encoding/json"
	"net/http"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

type OnboardingHandler struct {
	service service.OnboardingService
}

func NewOnboardingHandler(onboardingService service.OnboardingService) *OnboardingHandler {
	return &OnboardingHandler{service: onboardingService}
}

// SaveProgress handles PATCH /api/v1/tenants/{tenantID}/onboarding. Tenant
// access and tenant.update permission are already enforced by the
// production middleware chain (Authentication -> Tenant Context ->
// Authorization) before this is ever reached; tenantID is trusted here only
// because that chain already verified it against the authenticated caller.
// The decode target has a field only for current_step — there is no field
// for business_type, onboarding_status, tenant status, owner, role, or
// permission, so a client attempting to smuggle any of them in has them
// silently discarded, the same protection TenantHandler.Create/UpdateProfile
// already rely on.
func (h *OnboardingHandler) SaveProgress(writer http.ResponseWriter, request *http.Request, tenantID string) {
	var input struct {
		CurrentStep string `json:"current_step"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}
	tenant, err := h.service.SaveProgress(request.Context(), tenantID, service.SaveOnboardingProgressInput{CurrentStep: input.CurrentStep})
	if err != nil {
		writeTenantError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(toPublicTenant(tenant))
}

// Complete handles POST /api/v1/tenants/{tenantID}/onboarding/complete. The
// request carries no body fields — completion is a validated state
// transition the server alone decides (service.OnboardingService.Complete),
// never a client-supplied flag.
func (h *OnboardingHandler) Complete(writer http.ResponseWriter, request *http.Request, tenantID string) {
	tenant, err := h.service.Complete(request.Context(), tenantID)
	if err != nil {
		writeTenantError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(toPublicTenant(tenant))
}
