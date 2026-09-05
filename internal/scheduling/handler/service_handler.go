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
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     *string `json:"description"`
	DurationMinutes int     `json:"duration_minutes"`
	PriceMinor      int64   `json:"price_minor"`
	// CategoryID is the raw internal identifier, unlike the anonymous public
	// catalog's "category" name field (public_service_handler.go): the owner
	// dashboard is the authenticated party managing the assignment itself, so
	// the id — not just its display name — is exactly what it needs back.
	CategoryID *string      `json:"category_id"`
	Status     model.Status `json:"status"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

// toPublicService is the single conversion point, so the five response sites
// below cannot drift apart on the field list.
func toPublicService(svc *model.Service) PublicService {
	return PublicService{
		ID: svc.ID, Name: svc.Name, Description: svc.Description,
		DurationMinutes: svc.DurationMinutes, PriceMinor: svc.PriceMinor,
		CategoryID: svc.CategoryID,
		Status:     svc.Status, CreatedAt: svc.CreatedAt, UpdatedAt: svc.UpdatedAt,
	}
}

// nullableCategoryID captures whether category_id was present in a PATCH body
// at all — a distinction a plain *string cannot make, since encoding/json
// leaves an omitted key and an explicit null at the identical zero value.
//
// The field on the decode target MUST be this named type by value
// (`CategoryID nullableCategoryID`), never `*nullableCategoryID`. That is not
// a style preference: encoding/json's own indirect() short-circuits a
// settable *pointer* struct field on a null literal — it zeroes the pointer
// directly and never calls UnmarshalJSON at all, which would make null and
// "omitted" indistinguishable again, exactly the bug this type exists to
// avoid. A named non-pointer field is instead auto-addressed by
// encoding/json, which DOES reach UnmarshalJSON for both null and a string —
// and, critically, is left completely untouched (kept at its Go zero value,
// present == false) when the key is absent from the JSON altogether. That is
// what makes `present` a reliable presence flag.
type nullableCategoryID struct {
	present bool
	value   *string
}

func (n *nullableCategoryID) UnmarshalJSON(data []byte) error {
	n.present = true
	if string(data) == "null" {
		n.value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	n.value = &value
	return nil
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
		CategoryID      *string `json:"category_id"`
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
		CategoryID:      input.CategoryID,
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
		Name            *string            `json:"name"`
		Description     *string            `json:"description"`
		DurationMinutes *int               `json:"duration_minutes"`
		PriceMinor      *int64             `json:"price_minor"`
		CategoryID      nullableCategoryID `json:"category_id"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	serviceUpdate := service.UpdateServiceInput{
		Name:            input.Name,
		Description:     input.Description,
		DurationMinutes: input.DurationMinutes,
		PriceMinor:      input.PriceMinor,
	}
	// input.CategoryID.present is false when the key was omitted entirely —
	// left unwired, so UpdateServiceInput.CategoryID stays nil ("leave
	// unchanged"). When the key was present (null or a string),
	// input.CategoryID.value already carries the tri-state result and is
	// wired straight through.
	if input.CategoryID.present {
		serviceUpdate.CategoryID = &input.CategoryID.value
	}

	updated, err := h.catalog.Update(request.Context(), tenantID, serviceID, serviceUpdate)
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
