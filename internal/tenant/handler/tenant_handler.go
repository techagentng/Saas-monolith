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

// PublicTenant is the authenticated/owner-facing tenant representation
// returned by Create, List, GetByID, and UpdateProfile — distinct from
// PublicTenantIdentity (public_tenant_handler.go), which is the anonymous,
// unauthenticated view and does not carry onboarding fields. Adding a field
// here makes it visible to any authenticated caller who can reach one of
// this handler's routes (membership/ownership permitting), never to an
// anonymous one.
type PublicTenant struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Slug   string       `json:"slug"`
	Status model.Status `json:"status"`
	// Optional profile fields keep a stable JSON shape and serialize as null
	// when unset, matching every other public DTO in the project (none of
	// which use omitempty for scalar fields). Frontend consumers therefore
	// see the same tenant key set whether or not a profile has been filled in.
	Description  *string `json:"description"`
	ContactEmail *string `json:"contact_email"`
	ContactPhone *string `json:"contact_phone"`
	Timezone     *string `json:"timezone"`
	// BusinessType is null only for a tenant created before this field
	// existed (see model.Tenant's own doc comment) and is immutable once
	// set — this DTO has no corresponding writable field on UpdateProfile's
	// request shape, only this read-side one.
	BusinessType *model.BusinessType `json:"business_type"`
	// OnboardingStatus/OnboardingStep are workflow state, not identity —
	// exposed here (owner-facing) but deliberately absent from
	// PublicTenantIdentity's anonymous view.
	OnboardingStatus model.OnboardingStatus `json:"onboarding_status"`
	OnboardingStep   *string                `json:"onboarding_step"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// toPublicTenant builds the response DTO from a domain tenant. A single
// conversion point keeps Create/List/GetByID/UpdateProfile's four response
// sites from repeating (and risking drift on) the same field list.
func toPublicTenant(tenant *model.Tenant) PublicTenant {
	return PublicTenant{
		ID: tenant.ID, Name: tenant.Name, Slug: tenant.Slug, Status: tenant.Status,
		Description: tenant.Description, ContactEmail: tenant.ContactEmail, ContactPhone: tenant.ContactPhone, Timezone: tenant.Timezone,
		BusinessType: tenant.BusinessType, OnboardingStatus: tenant.OnboardingStatus, OnboardingStep: tenant.OnboardingStep,
		CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt,
	}
}

// Create provisions a tenant and its initial BUSINESS_OWNER for the
// authenticated caller. The creator is always the authenticated principal —
// the request body carries only {name, slug, business_type} and is never
// consulted for ownership, onboarding_status, or onboarding_step: the
// decode target below has no fields for any of those, so a client that
// sends them has them silently discarded, the same protection the existing
// owner_id/user_id/role fields already rely on.
func (h *TenantHandler) Create(writer http.ResponseWriter, request *http.Request) {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil))
		return
	}
	var input struct {
		Name         string `json:"name"`
		Slug         string `json:"slug"`
		BusinessType string `json:"business_type"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeTenantError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}
	tenant, err := h.creationService.Create(request.Context(), service.CreateTenantInput{Name: input.Name, Slug: input.Slug, BusinessType: input.BusinessType, CreatorUserID: principal.UserID})
	if err != nil {
		writeTenantError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(writer).Encode(toPublicTenant(tenant))
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
		result[i] = toPublicTenant(t)
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
	_ = json.NewEncoder(writer).Encode(toPublicTenant(tenant))
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
	_ = json.NewEncoder(writer).Encode(toPublicTenant(tenant))
}

func writeTenantError(writer http.ResponseWriter, err error) {
	_ = applicationError(err).WriteJSON(writer)
}
