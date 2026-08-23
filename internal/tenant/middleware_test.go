package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

func TestMiddlewareStoresOnlyVerifiedTenantContext(t *testing.T) {
	resolver := &contextResolverFake{context: &service.TenantContext{TenantID: "550e8400-e29b-41d4-a716-446655440000"}}
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		trusted, ok := FromContext(request.Context())
		if !ok || trusted.TenantID != resolver.context.TenantID {
			t.Fatalf("tenant context = %#v, ok=%v", trusted, ok)
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/550e8400-e29b-41d4-a716-446655440000/bookings", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "550e8400-e29b-41d4-a716-446655440001"}))
	recorder := httptest.NewRecorder()

	Middleware{Resolver: resolver}.Wrap(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent || resolver.candidate != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("status=%d candidate=%q", recorder.Code, resolver.candidate)
	}
}

func TestMiddlewareRejectsMissingPrincipal(t *testing.T) {
	recorder := httptest.NewRecorder()
	Middleware{Resolver: &contextResolverFake{}}.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler called") })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/tenants/id/items", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestRouteTenantIDUsesTheEstablishedTenantRoute(t *testing.T) {
	if got := routeTenantID("/api/v1/tenants/550e8400-e29b-41d4-a716-446655440000/context"); got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("routeTenantID() = %q", got)
	}
	if got := routeTenantID("/other/tenants/550e8400-e29b-41d4-a716-446655440000/context"); got != "" {
		t.Fatalf("routeTenantID() accepted non-API tenant route: %q", got)
	}
}

// TestRouteTenantIDExtractsBareTenantRoute proves extraction works for
// Feature 3's GET /api/v1/tenants/{tenantID} route, which has no sub-resource
// segment after the tenant ID. Regression test for the bug where routeTenantID
// required len(parts) >= 2, silently returning "" for this exact route shape
// and causing every request (authorized or not) to fail UUID validation with
// 400 INVALID_REQUEST before ever reaching membership/permission checks.
func TestRouteTenantIDExtractsBareTenantRoute(t *testing.T) {
	if got := routeTenantID("/api/v1/tenants/550e8400-e29b-41d4-a716-446655440000"); got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("routeTenantID() = %q, want the bare tenant ID", got)
	}
}

func TestRouteTenantIDStillExtractsDeeperSubResourceRoutes(t *testing.T) {
	if got := routeTenantID("/api/v1/tenants/550e8400-e29b-41d4-a716-446655440000/members/660e8400-e29b-41d4-a716-446655440000"); got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("routeTenantID() = %q, want tenant ID from a two-segment sub-resource route", got)
	}
}

func TestRouteTenantIDRejectsEmptyTenantSegment(t *testing.T) {
	if got := routeTenantID("/api/v1/tenants/"); got != "" {
		t.Fatalf("routeTenantID() = %q, want empty for a trailing-slash-only path", got)
	}
}

// TestMiddlewarePrefersRouterPathValueOverManualParsing proves that when the
// request has been routed through an http.ServeMux pattern that captures
// {tenantID}, the middleware uses that trusted, router-provided value rather
// than re-deriving it via manual URL string parsing.
func TestMiddlewarePrefersRouterPathValueOverManualParsing(t *testing.T) {
	resolver := &contextResolverFake{context: &service.TenantContext{TenantID: "550e8400-e29b-41d4-a716-446655440000"}}
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/tenants/{tenantID}", Middleware{Resolver: resolver}.Wrap(next))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/550e8400-e29b-41d4-a716-446655440000", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "550e8400-e29b-41d4-a716-446655440001"}))
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (bare tenant route resolved via router PathValue)", recorder.Code)
	}
	if resolver.candidate != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("candidate = %q, want the tenant ID captured by the router pattern", resolver.candidate)
	}
}

func TestMiddlewareRejectsNilResolvedContext(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/550e8400-e29b-41d4-a716-446655440000/context", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{UserID: "550e8400-e29b-41d4-a716-446655440001"}))

	Middleware{Resolver: &contextResolverFake{}}.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next handler called") })).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

type contextResolverFake struct {
	context   *service.TenantContext
	candidate string
}

func (r *contextResolverFake) Resolve(_ context.Context, _ auth.Principal, candidate string) (*service.TenantContext, error) {
	r.candidate = candidate
	return r.context, nil
}
