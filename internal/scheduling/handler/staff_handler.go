package handler

import (
	"encoding/json"
	"net/http"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// StaffHandler serves the tenant-facing staff roster and capability assignment.
//
// Every route it backs sits behind Authentication -> Tenant Context ->
// Authorization, wired in app.New, so this handler performs no permission check
// of its own.
//
// There is deliberately no anonymous variant. A public technician list is S8's
// concern and will be a separate handler with its own DTO — the one below
// carries the linked user id, which must never reach an anonymous caller.
type StaffHandler struct {
	staff service.StaffService
}

func NewStaffHandler(staff service.StaffService) *StaffHandler {
	return &StaffHandler{staff: staff}
}

// PublicStaff is the tenant-facing staff representation.
//
// tenant_id is absent: it is already the caller's own tenant, named in the
// route. user_id IS present, because an owner managing the roster needs to see
// which profiles are linked to a login — but it is the only identity data here.
// No email, no password material, no role, and no membership state: none of that
// belongs to a scheduling resource, and a linked user is a reference rather than
// an authorization fact.
type PublicStaff struct {
	ID          string       `json:"id"`
	UserID      *string      `json:"user_id"`
	DisplayName string       `json:"display_name"`
	Bio         *string      `json:"bio"`
	IsBookable  bool         `json:"is_bookable"`
	Status      model.Status `json:"status"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// toPublicStaff is the single conversion point, so the response sites below
// cannot drift apart on the field list.
func toPublicStaff(profile *model.StaffProfile) PublicStaff {
	return PublicStaff{
		ID: profile.ID, UserID: profile.UserID, DisplayName: profile.DisplayName,
		Bio: profile.Bio, IsBookable: profile.IsBookable, Status: profile.Status,
		CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
	}
}

// StaffCapabilities is the capability-set response. It carries service ids only:
// resolving them to full service records is the catalog's job, and duplicating
// service fields here would create a second copy that could disagree with it.
type StaffCapabilities struct {
	ServiceIDs []string `json:"service_ids"`
}

// Create handles POST /api/v1/tenants/{tenantID}/staff.
//
// The decode target carries exactly four fields. There is none for tenant_id,
// status, id or the timestamps, so a client that sends them has them silently
// discarded — the same structural protection every other write endpoint here
// relies on.
func (h *StaffHandler) Create(writer http.ResponseWriter, request *http.Request, tenantID string) {
	var input struct {
		DisplayName string  `json:"display_name"`
		Bio         *string `json:"bio"`
		UserID      *string `json:"user_id"`
		IsBookable  *bool   `json:"is_bookable"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	created, err := h.staff.Create(request.Context(), tenantID, service.CreateStaffInput{
		DisplayName: input.DisplayName,
		Bio:         input.Bio,
		UserID:      input.UserID,
		IsBookable:  input.IsBookable,
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	writeJSON(writer, http.StatusCreated, toPublicStaff(created))
}

// List handles GET /api/v1/tenants/{tenantID}/staff, optionally narrowed by
// ?status=ACTIVE|ARCHIVED|ALL. An omitted status means ACTIVE, matching the
// catalog listing's own default.
func (h *StaffHandler) List(writer http.ResponseWriter, request *http.Request, tenantID string) {
	profiles, err := h.staff.List(request.Context(), tenantID, request.URL.Query().Get("status"))
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	// An empty roster serializes as [] rather than null.
	result := make([]PublicStaff, len(profiles))
	for i, profile := range profiles {
		result[i] = toPublicStaff(profile)
	}
	writeJSON(writer, http.StatusOK, result)
}

// Get handles GET /api/v1/tenants/{tenantID}/staff/{staffID}. A profile
// belonging to another tenant is reported as STAFF_NOT_FOUND, identically to one
// that does not exist.
func (h *StaffHandler) Get(writer http.ResponseWriter, request *http.Request, tenantID string, staffID string) {
	profile, err := h.staff.Get(request.Context(), tenantID, staffID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicStaff(profile))
}

// Update handles PATCH /api/v1/tenants/{tenantID}/staff/{staffID}.
//
// Pointer fields distinguish "omitted" from "set", so a partial patch leaves
// untouched fields alone. user_id has no field here on purpose: re-pointing a
// profile at a different person is not an edit, and status belongs to archiving.
func (h *StaffHandler) Update(writer http.ResponseWriter, request *http.Request, tenantID string, staffID string) {
	var input struct {
		DisplayName *string `json:"display_name"`
		Bio         *string `json:"bio"`
		IsBookable  *bool   `json:"is_bookable"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	updated, err := h.staff.Update(request.Context(), tenantID, staffID, service.UpdateStaffInput{
		DisplayName: input.DisplayName,
		Bio:         input.Bio,
		IsBookable:  input.IsBookable,
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicStaff(updated))
}

// Archive handles POST /api/v1/tenants/{tenantID}/staff/{staffID}/archive.
//
// The request carries no body: archiving is a server-decided state transition,
// never a client-supplied status value. It is idempotent on an already archived
// profile.
func (h *StaffHandler) Archive(writer http.ResponseWriter, request *http.Request, tenantID string, staffID string) {
	archived, err := h.staff.Archive(request.Context(), tenantID, staffID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicStaff(archived))
}

// ListCapabilities handles GET /api/v1/tenants/{tenantID}/staff/{staffID}/services.
func (h *StaffHandler) ListCapabilities(writer http.ResponseWriter, request *http.Request, tenantID string, staffID string) {
	serviceIDs, err := h.staff.ListCapabilities(request.Context(), tenantID, staffID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, StaffCapabilities{ServiceIDs: serviceIDs})
}

// ReplaceCapabilities handles PUT /api/v1/tenants/{tenantID}/staff/{staffID}/services.
//
// PUT rather than PATCH because the body is the complete set: whatever is sent
// becomes the capability set, and sending the same set twice changes nothing.
// A missing or null service_ids is treated as an explicit empty set — "this
// technician performs nothing" is a legitimate state an owner may want, and
// guessing otherwise would make the endpoint's meaning depend on JSON subtleties.
func (h *StaffHandler) ReplaceCapabilities(writer http.ResponseWriter, request *http.Request, tenantID string, staffID string) {
	var input struct {
		ServiceIDs []string `json:"service_ids"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	serviceIDs, err := h.staff.ReplaceCapabilities(request.Context(), tenantID, staffID, input.ServiceIDs)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, StaffCapabilities{ServiceIDs: serviceIDs})
}
