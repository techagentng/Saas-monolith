package handler

import (
	"encoding/json"
	"net/http"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// ServiceHandler serves the tenant-facing service catalog.
//
// Every route it backs sits behind Authentication -> Tenant Context ->
// Authorization, wired in app.New. This handler therefore performs no
// permission check of its own: duplicating one here would create a second
// place for the authorization rules to drift from the route table.
//
// There is deliberately no anonymous variant of any of these endpoints. The
// public catalog is S8 and will be a separate handler with its own visibility
// gate, for the same reason PublicTenantHandler is separate from TenantHandler:
// mixing an anonymous endpoint into an authenticated handler invites wiring one
// into the wrong chain.
type ServiceHandler struct {
	catalog service.CatalogService
}

func NewServiceHandler(catalog service.CatalogService) *ServiceHandler {
	return &ServiceHandler{catalog: catalog}
}

// PublicService is the tenant-facing service representation.
//
// tenant_id is absent: it is already the caller's own tenant, named in the
// route, and echoing an internal identifier buys nothing. currency is absent
// too — it is a property of the tenant, which the client already holds, so
// repeating it on every row of a catalog listing would be duplicated state
// that could appear to disagree with its source.
type PublicService struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Description     *string      `json:"description"`
	DurationMinutes int          `json:"duration_minutes"`
	PriceMinor      int64        `json:"price_minor"`
	Status          model.Status `json:"status"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// toPublicService is the single conversion point, so the five response sites
// below cannot drift apart on the field list.
func toPublicService(svc *model.Service) PublicService {
	return PublicService{
		ID: svc.ID, Name: svc.Name, Description: svc.Description,
		DurationMinutes: svc.DurationMinutes, PriceMinor: svc.PriceMinor,
		Status: svc.Status, CreatedAt: svc.CreatedAt, UpdatedAt: svc.UpdatedAt,
	}
}

// Create handles POST /api/v1/tenants/{tenantID}/services.
//
// The decode target carries exactly four fields. There is none for tenant_id,
// status, currency, id, created_at or updated_at, so a client that sends any of
// them has them silently discarded — the same structural protection
// TenantHandler.Create and OnboardingHandler.SaveProgress already rely on,
// rather than a validation rule someone could forget to write.
func (h *ServiceHandler) Create(writer http.ResponseWriter, request *http.Request, tenantID string) {
	var input struct {
		Name            string  `json:"name"`
		Description     *string `json:"description"`
		DurationMinutes int     `json:"duration_minutes"`
		PriceMinor      int64   `json:"price_minor"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	created, err := h.catalog.Create(request.Context(), tenantID, service.CreateServiceInput{
		Name:            input.Name,
		Description:     input.Description,
		DurationMinutes: input.DurationMinutes,
		PriceMinor:      input.PriceMinor,
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	writeJSON(writer, http.StatusCreated, toPublicService(created))
}

// List handles GET /api/v1/tenants/{tenantID}/services, optionally narrowed by
// ?status=ACTIVE|ARCHIVED|ALL. An omitted status means ACTIVE.
func (h *ServiceHandler) List(writer http.ResponseWriter, request *http.Request, tenantID string) {
	services, err := h.catalog.List(request.Context(), tenantID, request.URL.Query().Get("status"))
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	// An empty catalog serializes as [] rather than null: a client iterating
	// the response should not have to special-case "no services yet".
	result := make([]PublicService, len(services))
	for i, svc := range services {
		result[i] = toPublicService(svc)
	}
	writeJSON(writer, http.StatusOK, result)
}

// Get handles GET /api/v1/tenants/{tenantID}/services/{serviceID}. A service
// belonging to another tenant is reported as SERVICE_NOT_FOUND, identically to
// one that does not exist.
func (h *ServiceHandler) Get(writer http.ResponseWriter, request *http.Request, tenantID string, serviceID string) {
	svc, err := h.catalog.Get(request.Context(), tenantID, serviceID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicService(svc))
}

// Update handles PATCH /api/v1/tenants/{tenantID}/services/{serviceID}.
//
// Pointer fields distinguish "omitted" from "set to a value", so a partial
// patch leaves untouched fields alone. As on Create, the absent fields are the
// protection: status, tenant_id, id and currency have nowhere to land.
func (h *ServiceHandler) Update(writer http.ResponseWriter, request *http.Request, tenantID string, serviceID string) {
	var input struct {
		Name            *string `json:"name"`
		Description     *string `json:"description"`
		DurationMinutes *int    `json:"duration_minutes"`
		PriceMinor      *int64  `json:"price_minor"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	updated, err := h.catalog.Update(request.Context(), tenantID, serviceID, service.UpdateServiceInput{
		Name:            input.Name,
		Description:     input.Description,
		DurationMinutes: input.DurationMinutes,
		PriceMinor:      input.PriceMinor,
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicService(updated))
}

// Archive handles POST /api/v1/tenants/{tenantID}/services/{serviceID}/archive.
//
// The request carries no body: archiving is a server-decided state transition,
// never a client-supplied status value. It is idempotent on an already archived
// service.
func (h *ServiceHandler) Archive(writer http.ResponseWriter, request *http.Request, tenantID string, serviceID string) {
	archived, err := h.catalog.Archive(request.Context(), tenantID, serviceID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicService(archived))
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

// writeSchedulingError routes every failure through the shared sanitizer, so an
// unmapped or infrastructure error becomes a generic INTERNAL_ERROR rather than
// leaking a driver message.
func writeSchedulingError(writer http.ResponseWriter, err error) {
	_ = apperrors.Map(err).WriteJSON(writer)
}
