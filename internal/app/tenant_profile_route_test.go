package app

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization"
	authzservice "github.com/techagentng/saas-monolith/internal/authorization/service"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identitymodel "github.com/techagentng/saas-monolith/internal/identity/model"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenanthandler "github.com/techagentng/saas-monolith/internal/tenant/handler"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires for
// Feature 4 tenant profile updates:
//
//	Authentication -> Tenant Context -> Authorization (tenant.update) -> Handler
//
// using the REAL TenantContextService and the REAL tenant service validation,
// dispatched through a real http.ServeMux so the {tenantID} pattern is
// genuinely exercised. The backing repository is a stateful fake, so every
// denial test can additionally assert that the target tenant was not mutated.

const (
	patchRouteUserID    = "550e8400-e29b-41d4-a716-446655440701"
	patchRouteSessionID = "550e8400-e29b-41d4-a716-446655440702"
	patchRouteTenantA   = "550e8400-e29b-41d4-a716-446655440703"
	patchRouteTenantB   = "550e8400-e29b-41d4-a716-446655440704"
)

func TestPatchTenantProfileRouteRequiresAuthentication(t *testing.T) {
	handler, _, store := buildPatchTenantRoute(t, activeProfileScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+patchRouteTenantA, strings.NewReader(`{"name":"Hacked"}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for unauthenticated PATCH", recorder.Code)
	}
	assertProfileUnchanged(t, store)
}

func TestPatchTenantProfileRouteRejectsMalformedTenantID(t *testing.T) {
	handler, tokens, _ := buildPatchTenantRoute(t, activeProfileScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/not-a-uuid", `{"name":"Acme"}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 for malformed tenant ID", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "INVALID_REQUEST")
}

// Cross-tenant: the authenticated user has no membership in the target tenant.
func TestPatchTenantProfileRouteDeniesCrossTenantAndLeavesTargetUnchanged(t *testing.T) {
	scenario := &profileScenario{
		tenant: &tenantmodel.Tenant{
			ID: patchRouteTenantB, Name: "Tenant B", Slug: "tenant-b", Status: tenantmodel.StatusActive,
		},
		membership: nil, // user A has no membership in tenant B
	}
	handler, tokens, store := buildPatchTenantRoute(t, scenario, []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantB, `{"name":"Owned By Attacker"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	if store.updateCalls != 0 {
		t.Fatalf("repository UpdateProfile reached on a cross-tenant request")
	}
	if store.tenant.Name != "Tenant B" {
		t.Fatalf("Tenant B name = %q, want unchanged %q", store.tenant.Name, "Tenant B")
	}
}

// Active member whose resolved permission set lacks tenant.update (e.g. STAFF,
// which holds tenant.read but not tenant.update).
func TestPatchTenantProfileRouteDeniesMemberWithoutTenantUpdatePermission(t *testing.T) {
	handler, tokens, store := buildPatchTenantRoute(t, activeProfileScenario(), []string{"tenant.read", "user.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantA, `{"name":"Staff Rename"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 PERMISSION_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
	assertProfileUnchanged(t, store)
}

func TestPatchTenantProfileRouteSucceedsForAuthorizedBusinessOwner(t *testing.T) {
	handler, tokens, store := buildPatchTenantRoute(t, activeProfileScenario(), []string{"tenant.read", "tenant.update"})
	recorder := httptest.NewRecorder()

	body := `{"name":"Acme Beauty Studio","description":"Full service salon","contact_email":"hi@acme.test","contact_phone":"+2348012345678","timezone":"Africa/Lagos"}`
	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantA, body))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var response tenanthandler.PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v, body=%s", err, recorder.Body.String())
	}
	if response.Name != "Acme Beauty Studio" {
		t.Fatalf("Name = %q", response.Name)
	}
	if response.Description == nil || *response.Description != "Full service salon" {
		t.Fatalf("Description = %v", response.Description)
	}
	if response.Timezone == nil || *response.Timezone != "Africa/Lagos" {
		t.Fatalf("Timezone = %v", response.Timezone)
	}
	// Persisted state, not just the response projection.
	if store.tenant.Name != "Acme Beauty Studio" {
		t.Fatalf("persisted Name = %q", store.tenant.Name)
	}
}

// Mandatory Feature 4 invariant, proven through the real HTTP route.
func TestPatchTenantProfileRouteNameChangeDoesNotChangeSlug(t *testing.T) {
	handler, tokens, store := buildPatchTenantRoute(t, activeProfileScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantA, `{"name":"Acme Beauty Studio"}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var response tenanthandler.PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Name != "Acme Beauty Studio" {
		t.Fatalf("Name = %q, want %q", response.Name, "Acme Beauty Studio")
	}
	if response.Slug != "acme-salon" {
		t.Fatalf("Slug = %q, want unchanged %q", response.Slug, "acme-salon")
	}
	if store.tenant.Slug != "acme-salon" {
		t.Fatalf("persisted Slug = %q, want unchanged", store.tenant.Slug)
	}
}

// The decoder is lenient project-wide; protected keys must still be inert
// because they do not exist on the update DTO.
func TestPatchTenantProfileRouteIgnoresProtectedFieldsInBody(t *testing.T) {
	handler, tokens, store := buildPatchTenantRoute(t, activeProfileScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	originalCreatedAt := store.tenant.CreatedAt
	body := `{"name":"New Business Name","slug":"attacker-slug","status":"DISABLED","id":"attacker-id","created_at":"2020-01-01T00:00:00Z"}`
	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantA, body))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if store.tenant.Name != "New Business Name" {
		t.Fatalf("Name = %q, want updated", store.tenant.Name)
	}
	if store.tenant.ID != patchRouteTenantA {
		t.Fatalf("ID = %q, want unchanged %q", store.tenant.ID, patchRouteTenantA)
	}
	if store.tenant.Slug != "acme-salon" {
		t.Fatalf("Slug = %q, want unchanged", store.tenant.Slug)
	}
	if store.tenant.Status != tenantmodel.StatusActive {
		t.Fatalf("Status = %q, want unchanged ACTIVE", store.tenant.Status)
	}
	if !store.tenant.CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("CreatedAt = %v, want unchanged %v", store.tenant.CreatedAt, originalCreatedAt)
	}
}

func TestPatchTenantProfileRouteDeniesInactiveMembership(t *testing.T) {
	scenario := activeProfileScenario()
	scenario.membership.Status = tenantmodel.MembershipStatusDisabled
	handler, tokens, store := buildPatchTenantRoute(t, scenario, []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantA, `{"name":"Revoked Rename"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 for disabled membership", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	assertProfileUnchanged(t, store)
}

func TestPatchTenantProfileRouteDeniesDisabledTenant(t *testing.T) {
	scenario := activeProfileScenario()
	scenario.tenant.Status = tenantmodel.StatusDisabled
	handler, tokens, store := buildPatchTenantRoute(t, scenario, []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantA, `{"name":"Disabled Rename"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 for disabled tenant", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	assertProfileUnchanged(t, store)
}

func TestPatchTenantProfileRouteRejectsEmptyPatchThroughFullChain(t *testing.T) {
	handler, tokens, store := buildPatchTenantRoute(t, activeProfileScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantA, `{}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 for empty patch", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
	if store.updateCalls != 0 {
		t.Fatalf("empty patch reached persistence")
	}
	assertProfileUnchanged(t, store)
}

func TestPatchTenantProfileRouteRejectsInvalidTimezoneThroughFullChain(t *testing.T) {
	handler, tokens, store := buildPatchTenantRoute(t, activeProfileScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, patchRequest(t, tokens, "/api/v1/tenants/"+patchRouteTenantA, `{"timezone":"Mars/Olympus"}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 for invalid timezone", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
	assertProfileUnchanged(t, store)
}

// Feature 4 adds PATCH on the same path pattern Feature 3 already registers
// for GET. http.ServeMux panics on conflicting patterns and silently
// mis-dispatches on subtle ones, so this asserts the two production
// registrations coexist and route by method.
func TestTenantIDRoutePatternsCoexistByMethod(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/tenants/{tenantID}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("get"))
	}))
	mux.Handle("PATCH /api/v1/tenants/{tenantID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("patch:" + r.PathValue("tenantID")))
	}))

	getRecorder := httptest.NewRecorder()
	mux.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+patchRouteTenantA, nil))
	if getRecorder.Body.String() != "get" {
		t.Fatalf("GET dispatched to %q", getRecorder.Body.String())
	}

	patchRecorder := httptest.NewRecorder()
	mux.ServeHTTP(patchRecorder, httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+patchRouteTenantA, nil))
	if patchRecorder.Body.String() != "patch:"+patchRouteTenantA {
		t.Fatalf("PATCH dispatched to %q; tenantID must resolve from the path", patchRecorder.Body.String())
	}
}

// --- test wiring -------------------------------------------------------------

type profileScenario struct {
	tenant     *tenantmodel.Tenant
	membership *tenantmodel.TenantMembership
}

func activeProfileScenario() *profileScenario {
	created := time.Now().UTC().Add(-time.Hour)
	return &profileScenario{
		tenant: &tenantmodel.Tenant{
			ID: patchRouteTenantA, Name: "Acme Salon", Slug: "acme-salon", Status: tenantmodel.StatusActive,
			CreatedAt: created, UpdatedAt: created,
		},
		membership: &tenantmodel.TenantMembership{
			TenantID: patchRouteTenantA, UserID: patchRouteUserID, Status: tenantmodel.MembershipStatusActive,
		},
	}
}

func assertProfileUnchanged(t *testing.T, store *statefulProfileRepository) {
	t.Helper()
	if store.tenant.Name != "Acme Salon" {
		t.Fatalf("tenant Name = %q, want unchanged %q", store.tenant.Name, "Acme Salon")
	}
	if store.tenant.Description != nil || store.tenant.ContactEmail != nil ||
		store.tenant.ContactPhone != nil || store.tenant.Timezone != nil {
		t.Fatalf("profile mutated on a denied/rejected request: %#v", store.tenant)
	}
}

func buildPatchTenantRoute(t *testing.T, scenario *profileScenario, tenantPermissions []string) (http.Handler, *identityservice.TokenManager, *statefulProfileRepository) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	tenants := &statefulProfileRepository{tenant: scenario.tenant}
	memberships := &profileMembershipRepository{scenario: scenario}
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	tenantMiddleware := tenant.Middleware{Resolver: contextService}

	// The REAL tenant service runs Feature 4 validation. Its transaction
	// dependency belongs to Feature 2 creation and must never be touched here.
	profileService := tenantservice.NewTenantService(&rejectBeginTx{t: t}, &profileUserRepository{}, tenants)
	tenantHandler := tenanthandler.NewTenantHandler(profileService, tenantservice.NewRetrievalService(tenants))
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})

	wrapped := authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.update"}.Wrap(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tenantHandler.UpdateProfile(w, r, r.PathValue("tenantID"))
			}),
		),
	))

	mux := http.NewServeMux()
	mux.Handle("PATCH /api/v1/tenants/{tenantID}", wrapped)
	return mux, tokens, tenants
}

func patchRequest(t *testing.T, tokens *identityservice.TokenManager, path, body string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(patchRouteUserID, patchRouteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	return request
}

// statefulProfileRepository applies profile updates to its stored tenant so
// tests can assert both the response and the persisted result, and can prove
// that denied requests mutate nothing.
type statefulProfileRepository struct {
	tenant      *tenantmodel.Tenant
	updateCalls int
}

func (r *statefulProfileRepository) Create(context.Context, *tenantmodel.Tenant) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulProfileRepository) FindByID(_ context.Context, id string) (*tenantmodel.Tenant, error) {
	if r.tenant == nil || r.tenant.ID != id {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return r.tenant, nil
}

func (r *statefulProfileRepository) ListAccessibleByUserID(context.Context, string) ([]*tenantmodel.Tenant, error) {
	if r.tenant == nil || r.tenant.Status != tenantmodel.StatusActive {
		return []*tenantmodel.Tenant{}, nil
	}
	return []*tenantmodel.Tenant{r.tenant}, nil
}

func (r *statefulProfileRepository) UpdateProfile(_ context.Context, tenantID string, update tenantrepository.TenantProfileUpdate) (*tenantmodel.Tenant, error) {
	r.updateCalls++
	if r.tenant == nil || r.tenant.ID != tenantID {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	// Mirror the SQL contract: only supplied fields change; identity fields
	// are not addressable through the typed update at all.
	if update.Name != nil {
		r.tenant.Name = *update.Name
	}
	if update.Description != nil {
		r.tenant.Description = update.Description
	}
	if update.ContactEmail != nil {
		r.tenant.ContactEmail = update.ContactEmail
	}
	if update.ContactPhone != nil {
		r.tenant.ContactPhone = update.ContactPhone
	}
	if update.Timezone != nil {
		r.tenant.Timezone = update.Timezone
	}
	r.tenant.UpdatedAt = time.Now().UTC()
	return r.tenant, nil
}

type profileMembershipRepository struct{ scenario *profileScenario }

func (m *profileMembershipRepository) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (m *profileMembershipRepository) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	membership := m.scenario.membership
	if membership == nil || membership.TenantID != tenantID || membership.UserID != userID {
		return nil, nil
	}
	return membership, nil
}

func (m *profileMembershipRepository) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}

func (m *profileMembershipRepository) Disable(context.Context, string, string, time.Time) error {
	return nil
}

// rejectBeginTx fails the test if Feature 4 ever opens a transaction; profile
// updates are a single statement and must not touch Feature 2's tx path.
type rejectBeginTx struct{ t *testing.T }

func (b *rejectBeginTx) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	b.t.Fatal("BeginTx called during a profile update")
	return nil, nil
}

// profileUserRepository satisfies the identity dependency the tenant service
// carries for Feature 2 creation. Feature 4 profile updates never consult it,
// so every method fails loudly if reached.
type profileUserRepository struct{}

func (profileUserRepository) Create(context.Context, identitymodel.User) (*identitymodel.User, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (profileUserRepository) FindByID(context.Context, string) (*identitymodel.User, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (profileUserRepository) FindByEmail(context.Context, string) (*identitymodel.User, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}
