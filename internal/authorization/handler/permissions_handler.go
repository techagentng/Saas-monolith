package handler

import (
	"encoding/json"
	"net/http"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization/service"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

// PermissionsHandler exposes the authenticated principal's own effective
// permissions for a tenant, backed directly by Feature 5's
// PermissionResolutionService — the same resolver TenantPermissionMiddleware
// uses to make every other authorization decision. It computes no
// permissions itself and executes no authorization SQL.
type PermissionsHandler struct {
	resolution service.PermissionResolutionService
}

func NewPermissionsHandler(resolution service.PermissionResolutionService) *PermissionsHandler {
	return &PermissionsHandler{resolution: resolution}
}

type effectivePermissions struct {
	Permissions []string `json:"permissions"`
}

// GetEffective returns the caller's own effective tenant permissions.
// Identity is taken exclusively from the authenticated Principal; tenantID
// is taken from the trusted TenantContext established by tenant.Middleware,
// never from a client-supplied identifier in the request body or query.
func (h *PermissionsHandler) GetEffective(writer http.ResponseWriter, request *http.Request, tenantID string) {
	principal, ok := auth.FromContext(request.Context())
	if !ok {
		writePermissionsError(writer, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil))
		return
	}
	trusted, ok := tenant.FromContext(request.Context())
	if !ok || trusted.TenantID != tenantID {
		writePermissionsError(writer, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil))
		return
	}
	permissions, err := h.resolution.ResolveTenant(request.Context(), principal.UserID, trusted.TenantID)
	if err != nil {
		writePermissionsError(writer, err)
		return
	}
	if permissions == nil {
		permissions = []string{}
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(effectivePermissions{Permissions: permissions})
}

func writePermissionsError(writer http.ResponseWriter, err error) {
	_ = apperrors.Map(err).WriteJSON(writer)
}
