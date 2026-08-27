package handler

import (
	"encoding/json"
	"net/http"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

// CurrencyHandler serves the write-once tenant currency endpoint
// (Scheduling S1).
type CurrencyHandler struct {
	service service.CurrencyService
}

func NewCurrencyHandler(currencyService service.CurrencyService) *CurrencyHandler {
	return &CurrencyHandler{service: currencyService}
}

// Set handles PUT /api/v1/tenants/{tenantID}/currency. Tenant access and the
// tenant.update permission are enforced by the production middleware chain
// before this is reached; tenantID is trusted here only because that chain
// already verified it against the authenticated caller.
//
// The decode target has a field only for currency — no tenant id, no status, no
// profile field — so a client cannot smuggle anything else through this
// endpoint. PUT rather than PATCH because the operation is idempotent: sending
// the same currency again succeeds and changes nothing.
func (h *CurrencyHandler) Set(writer http.ResponseWriter, request *http.Request, tenantID string) {
	var input struct {
		Currency string `json:"currency"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	tenant, err := h.service.Set(request.Context(), tenantID, input.Currency)
	if err != nil {
		writeTenantError(writer, err)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(toPublicTenant(tenant))
}
