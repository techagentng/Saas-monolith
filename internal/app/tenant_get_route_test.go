package app

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization"
	authzservice "github.com/techagentng/saas-monolith/internal/authorization/service"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenanthandler "github.com/techagentng/saas-monolith/internal/tenant/handler"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires
// for Feature 3 direct tenant retrieval:
//
//	Authentication -> Tenant Context -> Authorization (tenant.read) -> Handler
//
// using the REAL TenantContextService and RetrievalService (Feature 3's
// actual security logic), backed only by fake repositories, dispatched
// through a real http.ServeMux so the {tenantID} router pattern is genuinely
// exercised. This is the exact test category whose absence let the
// GetByID routing defect (routeTenantID silently returning "" for the bare
// /api/v1/tenants/{tenantID} route) ship undetected: a handler-only test
// bypasses the middleware chain entirely and cannot catch this class of bug.

const (
	getRouteUserID       = "550e8400-e29b-41d4-a716-446655440601"
	getRouteSessionID    = "550e8400-e29b-41d4-a716-446655440602"
	getRouteTenantID     = "550e8400-e29b-41d4-a716-446655440603"
	getRouteOtherTenantB = "550e8400-e29b-41d4-a716-446655440604"
)

func TestGetTenantRouteRequiresAuthentication(t *testing.T) {
	handler, _ := buildGetTenantRoute(t, &getTenantScenario{}, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+getRouteTenantID, nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for unauthenticated request", recorder.Code)
	}
}

func TestGetTenantRouteRejectsMalformedTenantID(t *testing.T) {
	handler, tokens := buildGetTenantRoute(t, &getTenantScenario{}, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, getTenantAuthenticatedRequest(t, tokens, "/api/v1/tenants/not-a-uuid"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 INVALID_REQUEST for malformed tenant ID", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "INVALID_REQUEST")
}

func TestGetTenantRouteDeniesUserWithNoMembership(t *testing.T) {
	scenario := &getTenantScenario{
		tenant:     &tenantmodel.Tenant{ID: getRouteTenantID, Name: "Tenant B", Slug: "tenant-b", Status: tenantmodel.StatusActive},
		membership: nil, // authenticated user has no membership in this tenant
	}
	handler, tokens := buildGetTenantRoute(t, scenario, []string{"tenant.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, getTenantAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+getRouteTenantID))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	if containsAny(recorder.Body.String(), "Tenant B", "tenant-b") {
		t.Fatalf("response leaked tenant data for an inaccessible tenant: %s", recorder.Body.String())
	}
}

func TestGetTenantRouteDeniesMembershipWithoutTenantReadPermission(t *testing.T) {
	scenario := &getTenantScenario{
		tenant:     &tenantmodel.Tenant{ID: getRouteTenantID, Name: "Tenant A", Slug: "tenant-a", Status: tenantmodel.StatusActive},
		membership: &tenantmodel.TenantMembership{TenantID: getRouteTenantID, UserID: getRouteUserID, Status: tenantmodel.MembershipStatusActive},
	}
	// Active membership present, but the resolved permission set lacks tenant.read.
	handler, tokens := buildGetTenantRoute(t, scenario, []string{"user.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, getTenantAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+getRouteTenantID))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 PERMISSION_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
}

func TestGetTenantRouteSucceedsThroughFullMiddlewareChain(t *testing.T) {
	scenario := &getTenantScenario{
		tenant:     &tenantmodel.Tenant{ID: getRouteTenantID, Name: "Acme Salon", Slug: "acme-salon", Status: tenantmodel.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		membership: &tenantmodel.TenantMembership{TenantID: getRouteTenantID, UserID: getRouteUserID, Status: tenantmodel.MembershipStatusActive},
	}
	handler, tokens := buildGetTenantRoute(t, scenario, []string{"tenant.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, getTenantAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+getRouteTenantID))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var body tenanthandler.PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v, body=%s", err, recorder.Body.String())
	}
	if body.ID != getRouteTenantID || body.Name != "Acme Salon" || body.Slug != "acme-salon" || body.Status != tenantmodel.StatusActive {
		t.Fatalf("response body = %#v, want tenant %q", body, getRouteTenantID)
	}
}

func TestGetTenantRouteNonexistentTenantIsIndistinguishableFromInaccessible(t *testing.T) {
	// scenario.tenant == nil: FindByID returns TENANT_NOT_FOUND internally,
	// which TenantContextService.Resolve converts to TENANT_ACCESS_DENIED —
	// proving the wiring, not just the unit, hides tenant existence.
	scenario := &getTenantScenario{tenant: nil, membership: nil}
	handler, tokens := buildGetTenantRoute(t, scenario, []string{"tenant.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, getTenantAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+getRouteOtherTenantB))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED for a nonexistent tenant (enumeration-safe policy)", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")

	// Same request shape against a tenant that DOES exist but the user cannot
	// access must produce byte-for-byte the same status and error code, so a
	// caller cannot distinguish "exists, no access" from "does not exist".
	existsScenario := &getTenantScenario{
		tenant:     &tenantmodel.Tenant{ID: getRouteOtherTenantB, Name: "Real Tenant", Slug: "real-tenant", Status: tenantmodel.StatusActive},
		membership: nil,
	}
	existsHandler, existsTokens := buildGetTenantRoute(t, existsScenario, []string{"tenant.read"})
	existsRecorder := httptest.NewRecorder()
	existsHandler.ServeHTTP(existsRecorder, getTenantAuthenticatedRequest(t, existsTokens, "/api/v1/tenants/"+getRouteOtherTenantB))

	if existsRecorder.Code != recorder.Code {
		t.Fatalf("nonexistent-tenant status %d != inaccessible-tenant status %d, enumeration is possible", recorder.Code, existsRecorder.Code)
	}
	if existsRecorder.Body.String() != recorder.Body.String() {
		t.Fatalf("nonexistent-tenant body %q != inaccessible-tenant body %q, enumeration is possible", recorder.Body.String(), existsRecorder.Body.String())
	}
}

func TestGetTenantRouteDeniesDisabledMembership(t *testing.T) {
	scenario := &getTenantScenario{
		tenant:     &tenantmodel.Tenant{ID: getRouteTenantID, Name: "Tenant A", Slug: "tenant-a", Status: tenantmodel.StatusActive},
		membership: &tenantmodel.TenantMembership{TenantID: getRouteTenantID, UserID: getRouteUserID, Status: tenantmodel.MembershipStatusDisabled},
	}
	handler, tokens := buildGetTenantRoute(t, scenario, []string{"tenant.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, getTenantAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+getRouteTenantID))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED for disabled membership", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

func TestGetTenantRouteDeniesDisabledTenant(t *testing.T) {
	scenario := &getTenantScenario{
		tenant:     &tenantmodel.Tenant{ID: getRouteTenantID, Name: "Tenant A", Slug: "tenant-a", Status: tenantmodel.StatusDisabled},
		membership: &tenantmodel.TenantMembership{TenantID: getRouteTenantID, UserID: getRouteUserID, Status: tenantmodel.MembershipStatusActive},
	}
	handler, tokens := buildGetTenantRoute(t, scenario, []string{"tenant.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, getTenantAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+getRouteTenantID))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED for disabled tenant", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

// --- test wiring -----------------------------------------------------------------

// getTenantScenario is shared by both the fake TenantRepository and the fake
// MembershipRepository so the REAL TenantContextService (used by the tenant
// middleware) and the REAL RetrievalService (used by the handler) observe
// mutually consistent state, exactly as they would against a real database.
type getTenantScenario struct {
	tenant     *tenantmodel.Tenant           // nil => tenant does not exist
	membership *tenantmodel.TenantMembership // nil => no membership for getRouteUserID
}

func buildGetTenantRoute(t *testing.T, scenario *getTenantScenario, tenantPermissions []string) (http.Handler, *identityservice.TokenManager) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	tenants := &fakeGetTenantRepository{scenario: scenario}
	memberships := &fakeGetTenantMembershipRepository{scenario: scenario}
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	tenantMiddleware := tenant.Middleware{Resolver: contextService}
	retrievalService := tenantservice.NewRetrievalService(tenants)
	tenantHandler := tenanthandler.NewTenantHandler(&fakeTenantService{}, retrievalService)
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})

	wrapped := authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.read"}.Wrap(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tenantHandler.GetByID(w, r, r.PathValue("tenantID"))
			}),
		),
	))

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/tenants/{tenantID}", wrapped)
	return mux, tokens
}

func getTenantAuthenticatedRequest(t *testing.T, tokens *identityservice.TokenManager, path string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(getRouteUserID, getRouteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if contains(haystack, needle) {
			return true
		}
	}
	return false
}

type fakeGetTenantRepository struct{ scenario *getTenantScenario }

func (f *fakeGetTenantRepository) Create(context.Context, *tenantmodel.Tenant) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (f *fakeGetTenantRepository) FindByID(_ context.Context, id string) (*tenantmodel.Tenant, error) {
	if f.scenario.tenant == nil || f.scenario.tenant.ID != id {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return f.scenario.tenant, nil
}

func (f *fakeGetTenantRepository) ListAccessibleByUserID(context.Context, string) ([]*tenantmodel.Tenant, error) {
	if f.scenario.tenant == nil || f.scenario.tenant.Status != tenantmodel.StatusActive {
		return []*tenantmodel.Tenant{}, nil
	}
	if f.scenario.membership == nil || f.scenario.membership.Status != tenantmodel.MembershipStatusActive {
		return []*tenantmodel.Tenant{}, nil
	}
	return []*tenantmodel.Tenant{f.scenario.tenant}, nil
}

type fakeGetTenantMembershipRepository struct{ scenario *getTenantScenario }

func (f *fakeGetTenantMembershipRepository) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (f *fakeGetTenantMembershipRepository) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	if f.scenario.membership == nil || f.scenario.membership.TenantID != tenantID || f.scenario.membership.UserID != userID {
		return nil, nil
	}
	return f.scenario.membership, nil
}

func (f *fakeGetTenantMembershipRepository) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}

func (f *fakeGetTenantMembershipRepository) Disable(context.Context, string, string, time.Time) error {
	return apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}
