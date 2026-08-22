package app

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	tenanthandler "github.com/techagentng/saas-monolith/internal/tenant/handler"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires
// for Feature 2 tenant creation:
//
//	Authentication -> Handler
//
// with no tenant-context or tenant-permission middleware, since no tenant
// exists yet for either to evaluate against. The service dependency is
// faked (Feature 2's transaction atomicity is proven separately by real
// Postgres integration tests in internal/tenant/service).

func TestCreateTenantRouteRequiresAuthentication(t *testing.T) {
	handler, _ := buildTenantCreateRoute(t, nil, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/tenants", requestBody(`{"name":"Salon","slug":"salon"}`)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for unauthenticated request", recorder.Code)
	}
}

func TestCreateTenantRouteExecutesHandlerForAnyAuthenticatedUser(t *testing.T) {
	tenant := &tenantmodel.Tenant{ID: "tenant-1", Name: "Salon", Slug: "salon", Status: tenantmodel.StatusActive}
	handler, tokens := buildTenantCreateRoute(t, tenant, nil)

	request := tenantRouteAuthenticatedRequest(t, tokens, `{"name":"Salon","slug":"salon"}`)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201 (no tenant permission should be required)", recorder.Code, recorder.Body.String())
	}
}

func TestCreateTenantRoutePropagatesServiceError(t *testing.T) {
	handler, tokens := buildTenantCreateRoute(t, nil, apperrors.New(apperrors.CodeTenantSlugTaken, "tenant slug is already taken", nil))

	request := tenantRouteAuthenticatedRequest(t, tokens, `{"name":"Salon","slug":"salon"}`)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 TENANT_SLUG_TAKEN", recorder.Code)
	}
}

func buildTenantCreateRoute(t *testing.T, result *tenantmodel.Tenant, err error) (http.Handler, *identityservice.TokenManager) {
	t.Helper()
	publicKey, privateKey, genErr := ed25519.GenerateKey(nil)
	if genErr != nil {
		t.Fatal(genErr)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: fakeSessionRepository{}}
	tenantHandler := tenanthandler.NewTenantHandler(&fakeTenantService{tenant: result, err: err})

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tenants", authMiddleware.Wrap(http.HandlerFunc(tenantHandler.Create)))
	return mux, tokens
}

func tenantRouteAuthenticatedRequest(t *testing.T, tokens *identityservice.TokenManager, body string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(routeUserID, routeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", requestBody(body))
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

type fakeTenantService struct {
	tenant *tenantmodel.Tenant
	err    error
}

func (f *fakeTenantService) Create(context.Context, tenantservice.CreateTenantInput) (*tenantmodel.Tenant, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tenant, nil
}
