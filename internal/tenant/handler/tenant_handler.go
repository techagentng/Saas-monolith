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

type TenantHandler struct {
	creationService  service.TenantService
	retrievalService service.RetrievalService
}

func NewTenantHandler(creationService service.TenantService, retrievalService service.RetrievalService) *TenantHandler {
	return &TenantHandler{creationService: creationService, retrievalService: retrievalService}
}

type PublicTenant struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Slug   string       `json:"slug"`
	Status model.Status `json:"status"`
	// Optional profile fields keep a stable JSON shape and serialize as null
	// when unset, matching every other public DTO in the project (none of
	// which use omitempty for scalar fields). Frontend consumers therefore
	// see the same tenant key set whether or not a profile has been filled in.
	Description  *string   `json:"description"`
	ContactEmail *string   `json:"contact_email"`
	ContactPhone *string   `json:"contact_phone"`
	Timezone     *string   `json:"timezone"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
	tenant, err := h.creationService.Create(request.Context(), service.CreateTenantInput{Name: input.Name, Slug: input.Slug, CreatorUserID: principal.UserID})
	if err != nil {
		writeTenantError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(writer).Encode(PublicTenant{
		ID: tenant.ID, Name: tenant.Name, Slug: tenant.Slug, Status: tenant.Status,
		Description: tenant.Description, ContactEmail: tenant.ContactEmail, ContactPhone: tenant.ContactPhone, Timezone: tenant.Timezone,
		CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt,
	})
}

// List returns all accessible tenants for the authenticated user,
// ordered by created_at, id.
func (h *TenantHandler) List(writer http.ResponseWriter, request *http.Request) {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil))
		return
	}
	tenants, err := h.retrievalService.ListAccessible(request.Context(), principal.UserID)
	if err != nil {
		writeTenantError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	result := make([]PublicTenant, len(tenants))
	for i, t := range tenants {
		result[i] = PublicTenant{
			ID: t.ID, Name: t.Name, Slug: t.Slug, Status: t.Status,
			Description: t.Description, ContactEmail: t.ContactEmail, ContactPhone: t.ContactPhone, Timezone: t.Timezone,
			CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		}
	}
	_ = json.NewEncoder(writer).Encode(result)
}

// GetByID returns a single accessible tenant, or 403 if the user lacks access.
func (h *TenantHandler) GetByID(writer http.ResponseWriter, request *http.Request, tenantID string) {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil))
		return
	}
	tenant, err := h.retrievalService.GetAccessible(request.Context(), principal.UserID, tenantID)
	if err != nil {
		writeTenantError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(PublicTenant{
		ID: tenant.ID, Name: tenant.Name, Slug: tenant.Slug, Status: tenant.Status,
		Description: tenant.Description, ContactEmail: tenant.ContactEmail, ContactPhone: tenant.ContactPhone, Timezone: tenant.Timezone,
		CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt,
	})
}

// UpdateProfile updates the profile fields of an accessible tenant.
// The authenticated user must have tenant.update permission.
func (h *TenantHandler) UpdateProfile(writer http.ResponseWriter, request *http.Request, tenantID string) {
	var input service.UpdateTenantProfileRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	tenant, err := h.creationService.UpdateProfile(request.Context(), tenantID, input)
	if err != nil {
		writeTenantError(writer, err)
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(PublicTenant{
		ID: tenant.ID, Name: tenant.Name, Slug: tenant.Slug, Status: tenant.Status,
		Description: tenant.Description, ContactEmail: tenant.ContactEmail, ContactPhone: tenant.ContactPhone, Timezone: tenant.Timezone,
		CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt,
	})
}

func writeTenantError(writer http.ResponseWriter, err error) {
	_ = applicationError(err).WriteJSON(writer)
}
