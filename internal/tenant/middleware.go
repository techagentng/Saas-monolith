package tenant

import (
	"net/http"
	"strings"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

type Middleware struct{ Resolver service.TenantContextService }

func (m Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := auth.FromContext(request.Context())
		if !ok {
			_ = apperrors.Map(apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)).WriteJSON(writer)
			return
		}
		candidate := request.PathValue("tenantID")
		if candidate == "" {
			candidate = routeTenantID(request.URL.Path)
		}
		trusted, err := m.Resolver.Resolve(request.Context(), principal, candidate)
		if err != nil {
			_ = apperrors.Map(err).WriteJSON(writer)
			return
		}
		if trusted == nil {
			_ = apperrors.Map(apperrors.New(apperrors.CodeInternalError, "tenant context unavailable", nil)).WriteJSON(writer)
			return
		}
		ctx := WithContext(request.Context(), TenantContext{TenantID: trusted.TenantID})
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// routeTenantID is a fallback for callers that invoke Middleware.Wrap outside
// of an http.ServeMux dispatch (so no {tenantID} pattern value has been
// captured onto the request). It extracts the tenant ID segment immediately
// following "/api/v1/tenants/", whether or not further sub-resource segments
// follow (e.g. both "/api/v1/tenants/{id}" and "/api/v1/tenants/{id}/members"
// resolve to {id}).
func routeTenantID(path string) string {
	const prefix = "/api/v1/tenants/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	remaining := strings.TrimPrefix(path, prefix)
	parts := strings.Split(strings.Trim(remaining, "/"), "/")
	if parts[0] == "" {
		return ""
	}
	return parts[0]
}
