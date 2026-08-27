package app

import (
	"context"
	"crypto/ed25519"
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
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenanthandler "github.com/techagentng/saas-monolith/internal/tenant/handler"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires
// for Vertical Onboarding F2's two routes:
//
//	PATCH  /api/v1/tenants/{tenantID}/onboarding
//	POST   /api/v1/tenants/{tenantID}/onboarding/complete
//
// both: Authentication -> Tenant Context -> Authorization (tenant.update) -> Handler
//
// using the REAL TenantContextService, the REAL OnboardingService (so the
// completion-prerequisite check is genuinely exercised, not mocked away),
// and the REAL Authorizer, dispatched through a real http.ServeMux so the
// {tenantID} pattern is genuinely captured. The backing repository is a
// stateful fake so denial tests can additionally assert nothing was mutated.

const (
	onboardingRouteUserID    = "550e8400-e29b-41d4-a716-446655440801"
	onboardingRouteSessionID = "550e8400-e29b-41d4-a716-446655440802"
	onboardingRouteTenantA   = "550e8400-e29b-41d4-a716-446655440803"
	onboardingRouteTenantB   = "550e8400-e29b-41d4-a716-446655440804"
)

func TestOnboardingSaveRouteRequiresAuthentication(t *testing.T) {
	handler, _, store := buildOnboardingRoutes(t, activeOnboardingScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding", strings.NewReader(`{"current_step":"business_profile"}`))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for unauthenticated PATCH", recorder.Code)
	}
	if store.tenant.OnboardingStep != nil {
		t.Fatalf("onboarding_step mutated on an unauthenticated request: %v", store.tenant.OnboardingStep)
	}
}

func TestOnboardingCompleteRouteRequiresAuthentication(t *testing.T) {
	handler, _, store := buildOnboardingRoutes(t, completableOnboardingScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding/complete", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for unauthenticated POST", recorder.Code)
	}
	if store.tenant.OnboardingStatus != tenantmodel.OnboardingStatusInProgress {
		t.Fatalf("onboarding_status mutated on an unauthenticated request: %q", store.tenant.OnboardingStatus)
	}
}

func TestOnboardingSaveRouteRejectsMalformedTenantID(t *testing.T) {
	handler, tokens, _ := buildOnboardingRoutes(t, activeOnboardingScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPatchRequest(t, tokens, "/api/v1/tenants/not-a-uuid/onboarding", `{"current_step":"business_profile"}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 for malformed tenant ID", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "INVALID_REQUEST")
}

// Cross-tenant: the authenticated user has no membership in the target tenant.
func TestOnboardingSaveRouteDeniesCrossTenantAndLeavesTargetUnchanged(t *testing.T) {
	scenario := &onboardingScenario{
		tenant:     &tenantmodel.Tenant{ID: onboardingRouteTenantB, Name: "Tenant B", Slug: "tenant-b", Status: tenantmodel.StatusActive, OnboardingStatus: tenantmodel.OnboardingStatusInProgress},
		membership: nil, // user A has no membership in tenant B
	}
	handler, tokens, store := buildOnboardingRoutes(t, scenario, []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPatchRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantB+"/onboarding", `{"current_step":"business_profile"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	if store.updateStepCalls != 0 {
		t.Fatalf("UpdateOnboardingStep reached on a cross-tenant request")
	}
	if store.tenant.OnboardingStep != nil {
		t.Fatalf("onboarding_step mutated on a cross-tenant request: %v", store.tenant.OnboardingStep)
	}
}

func TestOnboardingCompleteRouteDeniesCrossTenant(t *testing.T) {
	scenario := &onboardingScenario{
		tenant:     &tenantmodel.Tenant{ID: onboardingRouteTenantB, Name: "Tenant B", Slug: "tenant-b", Status: tenantmodel.StatusActive, OnboardingStatus: tenantmodel.OnboardingStatusInProgress},
		membership: nil,
	}
	handler, tokens, store := buildOnboardingRoutes(t, scenario, []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPostRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantB+"/onboarding/complete"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	if store.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding reached on a cross-tenant request")
	}
}

// Active member whose resolved permission set lacks tenant.update (e.g.
// STAFF, which holds tenant.read but not tenant.update).
func TestOnboardingSaveRouteDeniesMemberWithoutTenantUpdatePermission(t *testing.T) {
	handler, tokens, store := buildOnboardingRoutes(t, activeOnboardingScenario(), []string{"tenant.read", "user.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPatchRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding", `{"current_step":"business_profile"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 PERMISSION_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
	if store.updateStepCalls != 0 {
		t.Fatalf("UpdateOnboardingStep reached without tenant.update permission")
	}
}

func TestOnboardingCompleteRouteDeniesMemberWithoutTenantUpdatePermission(t *testing.T) {
	handler, tokens, store := buildOnboardingRoutes(t, completableOnboardingScenario(), []string{"tenant.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPostRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding/complete"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 PERMISSION_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
	if store.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding reached without tenant.update permission")
	}
}

func TestOnboardingSaveRouteDeniesInactiveMembership(t *testing.T) {
	scenario := activeOnboardingScenario()
	scenario.membership.Status = tenantmodel.MembershipStatusDisabled
	handler, tokens, store := buildOnboardingRoutes(t, scenario, []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPatchRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding", `{"current_step":"business_profile"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 for disabled membership", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	if store.updateStepCalls != 0 {
		t.Fatalf("UpdateOnboardingStep reached with a disabled membership")
	}
}

func TestOnboardingSaveRouteDeniesDisabledTenant(t *testing.T) {
	scenario := activeOnboardingScenario()
	scenario.tenant.Status = tenantmodel.StatusDisabled
	handler, tokens, store := buildOnboardingRoutes(t, scenario, []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPatchRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding", `{"current_step":"business_profile"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 for disabled tenant", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	if store.updateStepCalls != 0 {
		t.Fatalf("UpdateOnboardingStep reached against a disabled tenant")
	}
}

func TestOnboardingSaveRouteSucceedsForAuthorizedMember(t *testing.T) {
	handler, tokens, store := buildOnboardingRoutes(t, activeOnboardingScenario(), []string{"tenant.read", "tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPatchRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding", `{"current_step":"business_profile"}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var response tenanthandler.PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v, body=%s", err, recorder.Body.String())
	}
	if response.OnboardingStep == nil || *response.OnboardingStep != "business_profile" {
		t.Fatalf("OnboardingStep = %v, want business_profile", response.OnboardingStep)
	}
	if store.tenant.OnboardingStep == nil || *store.tenant.OnboardingStep != "business_profile" {
		t.Fatalf("persisted OnboardingStep = %v, want business_profile", store.tenant.OnboardingStep)
	}
}

// A client cannot mutate business_type or onboarding_status through the
// save-progress route: the decode target has no field for either, so a
// smuggled value is silently discarded rather than applied.
func TestOnboardingSaveRouteCannotMutateBusinessTypeOrOnboardingStatus(t *testing.T) {
	handler, tokens, store := buildOnboardingRoutes(t, activeOnboardingScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	body := `{"current_step":"business_profile","business_type":"RESTAURANT","onboarding_status":"COMPLETED","status":"DISABLED","tenant_id":"attacker","owner_id":"attacker","role":"SUPER_ADMIN"}`
	handler.ServeHTTP(recorder, onboardingPatchRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding", body))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if store.tenant.BusinessType == nil || *store.tenant.BusinessType != tenantmodel.BusinessTypeNailTechnician {
		t.Fatalf("BusinessType = %v, want unchanged NAIL_TECHNICIAN — must not be settable through this route", store.tenant.BusinessType)
	}
	if store.tenant.OnboardingStatus != tenantmodel.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want unchanged IN_PROGRESS — must not be settable through this route", store.tenant.OnboardingStatus)
	}
	if store.tenant.Status != tenantmodel.StatusActive {
		t.Fatalf("Status = %q, want unchanged ACTIVE", store.tenant.Status)
	}
}

func TestOnboardingCompleteRouteSucceedsWhenPrerequisitesSatisfied(t *testing.T) {
	handler, tokens, store := buildOnboardingRoutes(t, completableOnboardingScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPostRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding/complete"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var response tenanthandler.PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v, body=%s", err, recorder.Body.String())
	}
	if response.OnboardingStatus != tenantmodel.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want COMPLETED", response.OnboardingStatus)
	}
	if store.tenant.OnboardingStatus != tenantmodel.OnboardingStatusCompleted {
		t.Fatalf("persisted OnboardingStatus = %q, want COMPLETED", store.tenant.OnboardingStatus)
	}
}

// The mandatory bypass-prevention case, proven through the real route: a
// tenant that has never had progress saved cannot be completed by an
// otherwise fully-authorized caller.
func TestOnboardingCompleteRouteDeniesWhenNoProgressEverSaved(t *testing.T) {
	handler, tokens, store := buildOnboardingRoutes(t, activeOnboardingScenario(), []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPostRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding/complete"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 VALIDATION_FAILED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
	if store.tenant.OnboardingStatus != tenantmodel.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want unchanged IN_PROGRESS after a denied completion", store.tenant.OnboardingStatus)
	}
	if store.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding reached despite denied prerequisites")
	}
}

func TestOnboardingCompleteRouteOnAlreadyCompletedTenantIsIdempotent(t *testing.T) {
	scenario := activeOnboardingScenario()
	scenario.tenant.OnboardingStatus = tenantmodel.OnboardingStatusCompleted
	handler, tokens, store := buildOnboardingRoutes(t, scenario, []string{"tenant.update"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, onboardingPostRequest(t, tokens, "/api/v1/tenants/"+onboardingRouteTenantA+"/onboarding/complete"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 (idempotent)", recorder.Code, recorder.Body.String())
	}
	if store.completeCalls != 0 {
		t.Fatalf("CompleteOnboarding reached on an already-COMPLETED tenant — idempotent path must not re-persist")
	}
}

// --- test wiring -------------------------------------------------------------

type onboardingScenario struct {
	tenant     *tenantmodel.Tenant
	membership *tenantmodel.TenantMembership
}

func activeOnboardingScenario() *onboardingScenario {
	businessType := tenantmodel.BusinessTypeNailTechnician
	return &onboardingScenario{
		tenant: &tenantmodel.Tenant{
			ID: onboardingRouteTenantA, Name: "Acme Nails", Slug: "acme-nails", Status: tenantmodel.StatusActive,
			BusinessType: &businessType, OnboardingStatus: tenantmodel.OnboardingStatusInProgress,
		},
		membership: &tenantmodel.TenantMembership{
			TenantID: onboardingRouteTenantA, UserID: onboardingRouteUserID, Status: tenantmodel.MembershipStatusActive,
		},
	}
}

// completableOnboardingScenario is activeOnboardingScenario with an
// onboarding_step already saved AND a business timezone configured — the
// minimum state the completion prerequisites require. The timezone became
// part of that minimum in Vertical Onboarding F6, which made the common
// business profile a real requirement rather than merely "some step was
// saved once".
func completableOnboardingScenario() *onboardingScenario {
	scenario := activeOnboardingScenario()
	step := "business_profile"
	scenario.tenant.OnboardingStep = &step
	timezone := "Africa/Lagos"
	scenario.tenant.Timezone = &timezone
	return scenario
}

func buildOnboardingRoutes(t *testing.T, scenario *onboardingScenario, tenantPermissions []string) (http.Handler, *identityservice.TokenManager, *statefulOnboardingRepository) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	tenants := &statefulOnboardingRepository{tenant: scenario.tenant}
	memberships := &onboardingMembershipRepository{scenario: scenario}
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	tenantMiddleware := tenant.Middleware{Resolver: contextService}

	onboardingService := tenantservice.NewOnboardingService(tenants)
	onboardingHandler := tenanthandler.NewOnboardingHandler(onboardingService)
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})

	saveWrapped := authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.update"}.Wrap(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				onboardingHandler.SaveProgress(w, r, r.PathValue("tenantID"))
			}),
		),
	))
	completeWrapped := authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "tenant.update"}.Wrap(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				onboardingHandler.Complete(w, r, r.PathValue("tenantID"))
			}),
		),
	))

	mux := http.NewServeMux()
	mux.Handle("PATCH /api/v1/tenants/{tenantID}/onboarding", saveWrapped)
	mux.Handle("POST /api/v1/tenants/{tenantID}/onboarding/complete", completeWrapped)
	return mux, tokens, tenants
}

func onboardingPatchRequest(t *testing.T, tokens *identityservice.TokenManager, path, body string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(onboardingRouteUserID, onboardingRouteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func onboardingPostRequest(t *testing.T, tokens *identityservice.TokenManager, path string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(onboardingRouteUserID, onboardingRouteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

// statefulOnboardingRepository satisfies both repository.TenantRepository
// (so it can back TenantContextService, the same as production) and
// repository.OnboardingRepository (so it can back OnboardingService).
// Create/FindBySlug/ListAccessibleByUserID/UpdateProfile are never exercised
// by these routes and fail loudly if reached, mirroring
// tenant_profile_route_test.go's statefulProfileRepository convention.
type statefulOnboardingRepository struct {
	tenant          *tenantmodel.Tenant
	updateStepCalls int
	completeCalls   int
}

func (r *statefulOnboardingRepository) Create(context.Context, *tenantmodel.Tenant) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulOnboardingRepository) FindByID(_ context.Context, id string) (*tenantmodel.Tenant, error) {
	if r.tenant == nil || r.tenant.ID != id {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return r.tenant, nil
}

func (r *statefulOnboardingRepository) FindBySlug(context.Context, string) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulOnboardingRepository) ListAccessibleByUserID(context.Context, string) ([]*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulOnboardingRepository) UpdateProfile(context.Context, string, tenantrepository.TenantProfileUpdate) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulOnboardingRepository) UpdateOnboardingStep(_ context.Context, tenantID string, step string) (*tenantmodel.Tenant, error) {
	r.updateStepCalls++
	if r.tenant == nil || r.tenant.ID != tenantID {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	r.tenant.OnboardingStep = &step
	r.tenant.UpdatedAt = time.Now().UTC()
	return r.tenant, nil
}

func (r *statefulOnboardingRepository) CompleteOnboarding(_ context.Context, tenantID string) (*tenantmodel.Tenant, error) {
	r.completeCalls++
	if r.tenant == nil || r.tenant.ID != tenantID {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	r.tenant.OnboardingStatus = tenantmodel.OnboardingStatusCompleted
	r.tenant.UpdatedAt = time.Now().UTC()
	return r.tenant, nil
}

type onboardingMembershipRepository struct{ scenario *onboardingScenario }

func (m *onboardingMembershipRepository) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (m *onboardingMembershipRepository) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	membership := m.scenario.membership
	if membership == nil || membership.TenantID != tenantID || membership.UserID != userID {
		return nil, nil
	}
	return membership, nil
}

func (m *onboardingMembershipRepository) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}

func (m *onboardingMembershipRepository) Disable(context.Context, string, string, time.Time) error {
	return nil
}
