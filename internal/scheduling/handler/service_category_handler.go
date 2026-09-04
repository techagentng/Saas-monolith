package handler

import (
	"encoding/json"
	"net/http"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// ServiceCategoryHandler serves the tenant-facing category management surface
// (SC1).
//
// Every route it backs sits behind Authentication -> Tenant Context ->
// Authorization, wired in app.New, and reuses the catalog's own service.*
// permissions rather than a parallel category.* family — see CategoryService's
// doc comment for why. This handler therefore performs no permission check of
// its own, the same discipline ServiceHandler follows.
type ServiceCategoryHandler struct {
	categories service.CategoryService
}

func NewServiceCategoryHandler(categories service.CategoryService) *ServiceCategoryHandler {
	return &ServiceCategoryHandler{categories: categories}
}

// PublicServiceCategory is the tenant-facing category representation, named
// to match ServiceHandler's PublicService for the identical reason: this is
// the authenticated owner/staff view, not the anonymous public catalog.
//
// tenant_id is absent for the same reason PublicService omits it: it is
// already the caller's own tenant, named in the route.
type PublicServiceCategory struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	SortOrder int          `json:"sort_order"`
	Status    model.Status `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// toPublicServiceCategory is the single conversion point, so the five
// response sites below cannot drift apart on the field list.
func toPublicServiceCategory(category *model.ServiceCategory) PublicServiceCategory {
	return PublicServiceCategory{
		ID: category.ID, Name: category.Name, SortOrder: category.SortOrder,
		Status: category.Status, CreatedAt: category.CreatedAt, UpdatedAt: category.UpdatedAt,
	}
}

// Create handles POST /api/v1/tenants/{tenantID}/service-categories.
//
// The decode target carries exactly two fields. There is none for tenant_id,
// status, id, created_at or updated_at, so a client that sends any of them has
// them silently discarded — the same structural protection
// ServiceHandler.Create relies on.
func (h *ServiceCategoryHandler) Create(writer http.ResponseWriter, request *http.Request, tenantID string) {
	var input struct {
		Name      string `json:"name"`
		SortOrder *int   `json:"sort_order"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	created, err := h.categories.Create(request.Context(), tenantID, service.CreateCategoryInput{
		Name:      input.Name,
		SortOrder: input.SortOrder,
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	writeJSON(writer, http.StatusCreated, toPublicServiceCategory(created))
}

// List handles GET /api/v1/tenants/{tenantID}/service-categories, optionally
// narrowed by ?status=ACTIVE|ARCHIVED|ALL. An omitted status means ACTIVE, so
// an archived category is excluded from the listing a caller gets by default.
func (h *ServiceCategoryHandler) List(writer http.ResponseWriter, request *http.Request, tenantID string) {
	categories, err := h.categories.List(request.Context(), tenantID, request.URL.Query().Get("status"))
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	// An empty list serializes as [] rather than null, the same discipline
	// ServiceHandler.List follows.
	result := make([]PublicServiceCategory, len(categories))
	for i, category := range categories {
		result[i] = toPublicServiceCategory(category)
	}
	writeJSON(writer, http.StatusOK, result)
}

// Get handles GET /api/v1/tenants/{tenantID}/service-categories/{categoryID}.
// A category belonging to another tenant is reported as CATEGORY_NOT_FOUND,
// identically to one that does not exist.
func (h *ServiceCategoryHandler) Get(writer http.ResponseWriter, request *http.Request, tenantID string, categoryID string) {
	category, err := h.categories.Get(request.Context(), tenantID, categoryID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicServiceCategory(category))
}

// Update handles PATCH /api/v1/tenants/{tenantID}/service-categories/{categoryID}.
//
// Pointer fields distinguish "omitted" from "set to a value", the same
// discipline ServiceHandler.Update follows. As on Create, the absent fields
// are the protection: status, tenant_id, id and the timestamps have nowhere
// to land.
func (h *ServiceCategoryHandler) Update(writer http.ResponseWriter, request *http.Request, tenantID string, categoryID string) {
	var input struct {
		Name      *string `json:"name"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	updated, err := h.categories.Update(request.Context(), tenantID, categoryID, service.UpdateCategoryInput{
		Name:      input.Name,
		SortOrder: input.SortOrder,
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicServiceCategory(updated))
}

// Archive handles POST /api/v1/tenants/{tenantID}/service-categories/{categoryID}/archive.
//
// The request carries no body: archiving is a server-decided state
// transition, never a client-supplied status value. It is idempotent on an
// already archived category, and never touches the services filed under it —
// they keep their category_id and stay individually bookable.
func (h *ServiceCategoryHandler) Archive(writer http.ResponseWriter, request *http.Request, tenantID string, categoryID string) {
	archived, err := h.categories.Archive(request.Context(), tenantID, categoryID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicServiceCategory(archived))
}
