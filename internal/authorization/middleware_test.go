package authorization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

const (
	mwUserID   = "550e8400-e29b-41d4-a716-446655440401"
	mwTenantID = "550e8400-e29b-41d4-a716-446655440402"
)

func TestTenantPermissionMiddlewareAllowsGrantedPermission(t *testing.T) {
	authorizer := &authorizerFake{}
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.read"}.Wrap(next).ServeHTTP(recorder, requestWithContext(mwUserID, mwTenantID))

	if !handlerCalled || recorder.Code != http.StatusNoContent {
		t.Fatalf("handlerCalled=%v status=%d, want handler executed with 204", handlerCalled, recorder.Code)
	}
	if authorizer.gotPermission != "user.read" || authorizer.gotTenantID != mwTenantID || authorizer.gotUserID != mwUserID {
		t.Fatalf("authorizer received userID=%q tenantID=%q permission=%q", authorizer.gotUserID, authorizer.gotTenantID, authorizer.gotPermission)
	}
}

func TestTenantPermissionMiddlewareDeniesMissingPermission(t *testing.T) {
	authorizer := &authorizerFake{err: apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler must not run") })

	recorder := httptest.NewRecorder()
	TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.update"}.Wrap(next).ServeHTTP(recorder, requestWithContext(mwUserID, mwTenantID))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestTenantPermissionMiddlewareRequiresPrincipal(t *testing.T) {
	authorizer := &authorizerFake{}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler must not run") })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/x/users", nil)
	request = request.WithContext(tenant.WithContext(request.Context(), tenant.TenantContext{TenantID: mwTenantID}))
	TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.read"}.Wrap(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for missing principal", recorder.Code)
	}
	if authorizer.called {
		t.Fatal("authorizer must not be consulted without a principal")
	}
}

func TestTenantPermissionMiddlewareRequiresTenantContext(t *testing.T) {
	authorizer := &authorizerFake{}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler must not run") })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/x/users", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: mwUserID}))
	TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.read"}.Wrap(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 TENANT_ACCESS_DENIED for missing tenant context", recorder.Code)
	}
	if authorizer.called {
		t.Fatal("authorizer must not be consulted without a trusted tenant context")
	}
}

func TestTenantPermissionMiddlewareFailsClosedOnResolverError(t *testing.T) {
	authorizer := &authorizerFake{err: apperrors.New(apperrors.CodeServiceUnavailable, "authorization unavailable", nil)}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler must not run") })

	recorder := httptest.NewRecorder()
	TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.read"}.Wrap(next).ServeHTTP(recorder, requestWithContext(mwUserID, mwTenantID))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 on resolver failure", recorder.Code)
	}
}

func TestTenantPermissionMiddlewareTenantAPermissionCannotSatisfyTenantBRoute(t *testing.T) {
	authorizer := &authorizerFake{allowedTenantID: mwTenantID}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler must not run") })

	recorder := httptest.NewRecorder()
	TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.read"}.Wrap(next).ServeHTTP(recorder, requestWithContext(mwUserID, "550e8400-e29b-41d4-a716-446655440999"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when tenant context does not match the authorized tenant", recorder.Code)
	}
}

func TestTenantPermissionMiddlewareIgnoresSpoofedHeaders(t *testing.T) {
	authorizer := &authorizerFake{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	request := requestWithContext(mwUserID, mwTenantID)
	request.Header.Set("X-User-ID", "550e8400-e29b-41d4-a716-446655440888")
	request.Header.Set("X-Tenant-ID", "550e8400-e29b-41d4-a716-446655440777")
	request.Header.Set("X-Role", "SUPER_ADMIN")
	request.Header.Set("X-Permissions", "permission.assign")
	TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.read"}.Wrap(next).ServeHTTP(recorder, request)

	if authorizer.gotUserID != mwUserID || authorizer.gotTenantID != mwTenantID {
		t.Fatalf("middleware must use only trusted context, got userID=%q tenantID=%q", authorizer.gotUserID, authorizer.gotTenantID)
	}
}

func TestTenantPermissionMiddlewareDoesNotPanicOnEmptyContext(t *testing.T) {
	authorizer := &authorizerFake{}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler must not run") })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/x/users", nil)
	TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.read"}.Wrap(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 with completely empty context", recorder.Code)
	}
}

func TestPlatformPermissionMiddlewareAllowsGrantedPermission(t *testing.T) {
	authorizer := &authorizerFake{platform: true}
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/platform/roles", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: mwUserID}))
	PlatformPermissionMiddleware{Authorizer: authorizer, Permission: "role.read"}.Wrap(next).ServeHTTP(recorder, request)

	if !handlerCalled || recorder.Code != http.StatusNoContent {
		t.Fatalf("handlerCalled=%v status=%d, want handler executed", handlerCalled, recorder.Code)
	}
}

func TestPlatformPermissionMiddlewareRequiresPrincipal(t *testing.T) {
	authorizer := &authorizerFake{platform: true}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler must not run") })

	recorder := httptest.NewRecorder()
	PlatformPermissionMiddleware{Authorizer: authorizer, Permission: "role.read"}.Wrap(next).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/platform/roles", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for missing principal", recorder.Code)
	}
}

func TestPlatformPermissionMiddlewareDeniesTenantContextAloneWithoutPlatformPermission(t *testing.T) {
	authorizer := &authorizerFake{}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler must not run") })

	recorder := httptest.NewRecorder()
	request := requestWithContext(mwUserID, mwTenantID)
	PlatformPermissionMiddleware{Authorizer: authorizer, Permission: "role.read"}.Wrap(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; tenant context presence must not grant platform authority", recorder.Code)
	}
}

func requestWithContext(userID, tenantID string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+tenantID+"/users", nil)
	ctx := auth.WithPrincipal(request.Context(), auth.Principal{UserID: userID})
	ctx = tenant.WithContext(ctx, tenant.TenantContext{TenantID: tenantID})
	return request.WithContext(ctx)
}

type authorizerFake struct {
	err             error
	platform        bool
	allowedTenantID string
	called          bool
	gotUserID       string
	gotTenantID     string
	gotPermission   string
}

func (a *authorizerFake) RequireTenantPermission(_ context.Context, principal auth.Principal, tenantContext tenant.TenantContext, permission string) error {
	a.called = true
	a.gotUserID = principal.UserID
	a.gotTenantID = tenantContext.TenantID
	a.gotPermission = permission
	if a.err != nil {
		return a.err
	}
	if a.allowedTenantID != "" && tenantContext.TenantID != a.allowedTenantID {
		return apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)
	}
	return nil
}

func (a *authorizerFake) RequirePlatformPermission(_ context.Context, principal auth.Principal, permission string) error {
	a.called = true
	a.gotUserID = principal.UserID
	a.gotPermission = permission
	if a.err != nil {
		return a.err
	}
	if !a.platform {
		return apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)
	}
	return nil
}
