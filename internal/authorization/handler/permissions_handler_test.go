package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

func TestGetEffectivePermissionsReturnsResolvedPermissionsAsIs(t *testing.T) {
	// PermissionResolutionService.ResolveTenant already guarantees a sorted,
	// deduplicated result (see permission_resolution_service.go); the handler
	// is a pure passthrough and must not reorder or otherwise transform it.
	handler := NewPermissionsHandler(&permissionResolutionFake{tenantPermissions: []string{"tenant.read", "tenant.update"}})
	recorder := httptest.NewRecorder()
	request := requestWithActorContext(http.MethodGet, "/api/v1/tenants/tenant/permissions", "")

	handler.GetEffective(recorder, request, "tenant")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	want := []string{"tenant.read", "tenant.update"}
	if len(body.Permissions) != len(want) {
		t.Fatalf("permissions = %v, want %v", body.Permissions, want)
	}
	for i, code := range want {
		if body.Permissions[i] != code {
			t.Fatalf("permissions = %v, want %v", body.Permissions, want)
		}
	}
}

func TestGetEffectivePermissionsReturnsEmptyArrayNotNull(t *testing.T) {
	handler := NewPermissionsHandler(&permissionResolutionFake{tenantPermissions: nil})
	recorder := httptest.NewRecorder()
	request := requestWithActorContext(http.MethodGet, "/api/v1/tenants/tenant/permissions", "")

	handler.GetEffective(recorder, request, "tenant")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); !jsonContains(got, `"permissions":[]`) {
		t.Fatalf("body = %s, want permissions to serialize as [] not null", got)
	}
}

func TestGetEffectivePermissionsRequiresAuthentication(t *testing.T) {
	handler := NewPermissionsHandler(&permissionResolutionFake{tenantPermissions: []string{"tenant.read"}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant/permissions", nil)
	ctx := tenant.WithContext(request.Context(), tenant.TenantContext{TenantID: "tenant"})

	handler.GetEffective(recorder, request.WithContext(ctx), "tenant")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestGetEffectivePermissionsRequiresTenantContext(t *testing.T) {
	handler := NewPermissionsHandler(&permissionResolutionFake{tenantPermissions: []string{"tenant.read"}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant/permissions", nil)
	ctx := auth.WithPrincipal(request.Context(), auth.Principal{UserID: "actor"})

	handler.GetEffective(recorder, request.WithContext(ctx), "tenant")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 TENANT_ACCESS_DENIED", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

func TestGetEffectivePermissionsRejectsTenantContextMismatch(t *testing.T) {
	handler := NewPermissionsHandler(&permissionResolutionFake{tenantPermissions: []string{"tenant.read"}})
	recorder := httptest.NewRecorder()
	request := requestWithActorContext(http.MethodGet, "/api/v1/tenants/other/permissions", "")

	handler.GetEffective(recorder, request, "other-tenant-from-route")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 TENANT_ACCESS_DENIED", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

func TestGetEffectivePermissionsPropagatesResolverError(t *testing.T) {
	handler := NewPermissionsHandler(&permissionResolutionFake{err: apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)})
	recorder := httptest.NewRecorder()
	request := requestWithActorContext(http.MethodGet, "/api/v1/tenants/tenant/permissions", "")

	handler.GetEffective(recorder, request, "tenant")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 TENANT_ACCESS_DENIED", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

func TestGetEffectivePermissionsNeverReadsUserIDFromRequest(t *testing.T) {
	resolver := &permissionResolutionFake{tenantPermissions: []string{"tenant.read"}}
	handler := NewPermissionsHandler(resolver)
	recorder := httptest.NewRecorder()
	// user_id in the query string must never override the authenticated principal.
	request := requestWithActorContext(http.MethodGet, "/api/v1/tenants/tenant/permissions?user_id=someone-else", "")

	handler.GetEffective(recorder, request, "tenant")

	if resolver.sawUserID != "actor" {
		t.Fatalf("resolver saw userID = %q, want the authenticated principal %q", resolver.sawUserID, "actor")
	}
}

func jsonContains(haystack, needle string) bool { return contains(haystack, needle) }

func assertBodyCode(t *testing.T, recorder *httptest.ResponseRecorder, code string) {
	t.Helper()
	if got := recorder.Body.String(); !contains(got, code) {
		t.Fatalf("response body = %s, want to contain %q", got, code)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

type permissionResolutionFake struct {
	tenantPermissions []string
	err               error
	sawUserID         string
}

func (r *permissionResolutionFake) ResolveTenant(_ context.Context, userID, _ string) ([]string, error) {
	r.sawUserID = userID
	if r.err != nil {
		return nil, r.err
	}
	return r.tenantPermissions, nil
}

func (r *permissionResolutionFake) ResolvePlatform(context.Context, string) ([]string, error) {
	return nil, nil
}
