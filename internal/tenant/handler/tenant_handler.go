package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

type TenantHandler struct{ service service.TenantService }

func NewTenantHandler(tenantService service.TenantService) *TenantHandler {
	return &TenantHandler{service: tenantService}
}

type PublicTenant struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Slug      string       `json:"slug"`
	Status    model.Status `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// Create provisions a tenant and its initial BUSINESS_OWNER for the
// authenticated caller. The creator is always the authenticated principal —
// the request body carries only {name, slug} and is never consulted for
// ownership.
func (h *TenantHandler) Create(writer http.ResponseWriter, request *http.Request) {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil))
		return
	}
	var input struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}
	tenant, err := h.service.Create(request.Context(), service.CreateTenantInput{Name: input.Name, Slug: input.Slug, CreatorUserID: principal.UserID})
	if err != nil {
		writeTenantError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(writer).Encode(PublicTenant{
		ID: tenant.ID, Name: tenant.Name, Slug: tenant.Slug, Status: tenant.Status, CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt,
	})
}

func writeTenantError(writer http.ResponseWriter, err error) {
	_ = applicationError(err).WriteJSON(writer)
}
