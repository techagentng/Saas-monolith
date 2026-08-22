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
