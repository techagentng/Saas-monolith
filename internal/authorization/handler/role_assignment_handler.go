package handler

import (
	"encoding/json"
	"net/http"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization/service"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

// RoleAssignmentHandler exposes tenant role assignment. Route-level access
// (role.assign) is enforced by TenantPermissionMiddleware ahead of this
// handler; the target-role business restriction (BUSINESS_OWNER may assign
// STAFF only) is enforced by TenantRoleAssignmentService, since that
// restriction depends on the target resource and must hold even if this
// service is ever invoked from another entry point.
type RoleAssignmentHandler struct {
	service service.TenantRoleAssignmentService
}

func NewRoleAssignmentHandler(assignments service.TenantRoleAssignmentService) *RoleAssignmentHandler {
	return &RoleAssignmentHandler{service: assignments}
}

type publicRoleAssignment struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	RoleID   string `json:"role_id"`
	TenantID string `json:"tenant_id"`
}

func (h *RoleAssignmentHandler) Assign(writer http.ResponseWriter, request *http.Request, tenantID string) {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		writeAssignmentError(writer, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil))
		return
	}
	trusted, ok := tenant.FromContext(request.Context())
	if !ok || trusted.TenantID != tenantID {
		writeAssignmentError(writer, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil))
		return
	}
	var input struct {
		UserID string `json:"user_id"`
		RoleID string `json:"role_id"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeAssignmentError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}
	assignment, err := h.service.AssignTenantRole(request.Context(), principal, trusted, input.UserID, input.RoleID)
	if err != nil {
		writeAssignmentError(writer, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(writer).Encode(publicRoleAssignment{
		ID: assignment.ID, UserID: assignment.UserID, RoleID: assignment.RoleID, TenantID: assignment.TenantID,
	})
}

func writeAssignmentError(writer http.ResponseWriter, err error) {
	_ = apperrors.Map(err).WriteJSON(writer)
}
