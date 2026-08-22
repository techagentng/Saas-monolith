package authorization

import (
	"net/http"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization/service"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

// TenantPermissionMiddleware enforces that a request carries a trusted
// Principal (Feature 3) and a trusted TenantContext (Feature 4), then
// delegates the permission decision to the central Authorizer. It never
// inspects role names, never reads client-supplied identity headers, and
// never executes SQL directly.
//
// Ordering requirement: this middleware must run after authentication and
// tenant-context resolution.
type TenantPermissionMiddleware struct {
	Authorizer service.Authorizer
	Permission string
}

func (m TenantPermissionMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := auth.FromContext(request.Context())
		if !ok {
			_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)).WriteJSON(writer)
			return
		}
		trusted, ok := tenant.FromContext(request.Context())
		if !ok {
			_ = apperrors.Map(apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)).WriteJSON(writer)
			return
		}
		if err := m.Authorizer.RequireTenantPermission(request.Context(), principal, trusted, m.Permission); err != nil {
			_ = apperrors.Map(err).WriteJSON(writer)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// PlatformPermissionMiddleware enforces that a request carries a trusted
// Principal and delegates the platform permission decision to the central
// Authorizer. It never requires or reads a TenantContext: platform
// authorization is independent of tenant scope.
type PlatformPermissionMiddleware struct {
	Authorizer service.Authorizer
	Permission string
}

func (m PlatformPermissionMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := auth.FromContext(request.Context())
		if !ok {
			_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)).WriteJSON(writer)
			return
		}
		if err := m.Authorizer.RequirePlatformPermission(request.Context(), principal, m.Permission); err != nil {
			_ = apperrors.Map(err).WriteJSON(writer)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
