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
	authzhandler "github.com/techagentng/saas-monolith/internal/authorization/handler"
	authzmodel "github.com/techagentng/saas-monolith/internal/authorization/model"
	authzservice "github.com/techagentng/saas-monolith/internal/authorization/service"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires
// for the F11 effective-permissions endpoint:
//
//	Authentication -> Tenant Context -> Handler
//
// using the REAL TenantContextService and REAL PermissionResolutionService
// (Feature 5's actual authorization resolver — the same one
// TenantPermissionMiddleware relies on for every other route), backed only
// by fake repositories, dispatched through a real http.ServeMux.

const (
	permsRouteUserID    = "550e8400-e29b-41d4-a716-446655440701"
	permsRouteSessionID = "550e8400-e29b-41d4-a716-446655440702"
	permsRouteTenantID  = "550e8400-e29b-41d4-a716-446655440703"
	permsOwnerRoleID    = "650e8400-e29b-41d4-a716-446655440102"
)

func TestPermissionsRouteRequiresAuthentication(t *testing.T) {
	handler, _ := buildPermissionsRoute(t, &permissionsScenario{})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+permsRouteTenantID+"/permissions", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for unauthenticated request", recorder.Code)
	}
}

func TestPermissionsRouteRejectsMalformedTenantID(t *testing.T) {
	handler, tokens := buildPermissionsRoute(t, &permissionsScenario{})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, permissionsAuthenticatedRequest(t, tokens, "/api/v1/tenants/not-a-uuid/permissions"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 INVALID_REQUEST for malformed tenant ID", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "INVALID_REQUEST")
}

func TestPermissionsRouteDeniesUserWithNoMembership(t *testing.T) {
	scenario := &permissionsScenario{
		tenant:     &tenantmodel.Tenant{ID: permsRouteTenantID, Status: tenantmodel.StatusActive},
		membership: nil,
	}
	handler, tokens := buildPermissionsRoute(t, scenario)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, permissionsAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+permsRouteTenantID+"/permissions"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

func TestPermissionsRouteDeniesDisabledMembership(t *testing.T) {
	scenario := &permissionsScenario{
		tenant:     &tenantmodel.Tenant{ID: permsRouteTenantID, Status: tenantmodel.StatusActive},
		membership: &tenantmodel.TenantMembership{TenantID: permsRouteTenantID, UserID: permsRouteUserID, Status: tenantmodel.MembershipStatusDisabled},
	}
	handler, tokens := buildPermissionsRoute(t, scenario)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, permissionsAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+permsRouteTenantID+"/permissions"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED for disabled membership", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

func TestPermissionsRouteDeniesDisabledTenant(t *testing.T) {
	scenario := &permissionsScenario{
		tenant:     &tenantmodel.Tenant{ID: permsRouteTenantID, Status: tenantmodel.StatusDisabled},
		membership: &tenantmodel.TenantMembership{TenantID: permsRouteTenantID, UserID: permsRouteUserID, Status: tenantmodel.MembershipStatusActive},
	}
	handler, tokens := buildPermissionsRoute(t, scenario)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, permissionsAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+permsRouteTenantID+"/permissions"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 TENANT_ACCESS_DENIED for disabled tenant", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

func TestPermissionsRouteReturnsEffectivePermissionsForMember(t *testing.T) {
	scenario := &permissionsScenario{
		tenant:     &tenantmodel.Tenant{ID: permsRouteTenantID, Status: tenantmodel.StatusActive},
		membership: &tenantmodel.TenantMembership{TenantID: permsRouteTenantID, UserID: permsRouteUserID, Status: tenantmodel.MembershipStatusActive},
		roles:      []authzmodel.Role{{ID: permsOwnerRoleID, Name: "BUSINESS_OWNER", Scope: authzmodel.ScopeTenant}},
		rolePermissions: map[string][]authzmodel.Permission{
			permsOwnerRoleID: {{Code: "tenant.update"}, {Code: "tenant.read"}, {Code: "tenant.read"}},
		},
	}
	handler, tokens := buildPermissionsRoute(t, scenario)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, permissionsAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+permsRouteTenantID+"/permissions"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v, body=%s", err, recorder.Body.String())
	}
	want := []string{"tenant.read", "tenant.update"}
	if len(body.Permissions) != len(want) || body.Permissions[0] != want[0] || body.Permissions[1] != want[1] {
		t.Fatalf("permissions = %v, want deterministic deduplicated %v", body.Permissions, want)
	}
}

func TestPermissionsRouteReturnsEmptyPermissionsForMemberWithNoRoles(t *testing.T) {
	scenario := &permissionsScenario{
		tenant:     &tenantmodel.Tenant{ID: permsRouteTenantID, Status: tenantmodel.StatusActive},
		membership: &tenantmodel.TenantMembership{TenantID: permsRouteTenantID, UserID: permsRouteUserID, Status: tenantmodel.MembershipStatusActive},
	}
	handler, tokens := buildPermissionsRoute(t, scenario)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, permissionsAuthenticatedRequest(t, tokens, "/api/v1/tenants/"+permsRouteTenantID+"/permissions"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 with empty permission set (default deny, not an error)", recorder.Code, recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"permissions":[]`) {
		t.Fatalf("body = %s, want permissions to serialize as [] for a member with no roles", recorder.Body.String())
	}
}

// --- test wiring -----------------------------------------------------------------

type permissionsScenario struct {
	tenant          *tenantmodel.Tenant
	membership      *tenantmodel.TenantMembership
	roles           []authzmodel.Role
	rolePermissions map[string][]authzmodel.Permission
}

func buildPermissionsRoute(t *testing.T, scenario *permissionsScenario) (http.Handler, *identityservice.TokenManager) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	tenants := &fakePermissionsTenantRepository{scenario: scenario}
	memberships := &fakePermissionsMembershipRepository{scenario: scenario}
	userRoles := &fakePermissionsUserRoleRepository{scenario: scenario}
	rolePermissions := &fakePermissionsRolePermissionRepository{scenario: scenario}

	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	tenantMiddleware := tenant.Middleware{Resolver: contextService}
	resolution := authzservice.NewPermissionResolutionService(userRoles, rolePermissions, memberships, tenants)
	permissionsHandler := authzhandler.NewPermissionsHandler(resolution)

	wrapped := authMiddleware.Wrap(tenantMiddleware.Wrap(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			permissionsHandler.GetEffective(w, r, r.PathValue("tenantID"))
		}),
	))

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/tenants/{tenantID}/permissions", wrapped)
	return mux, tokens
}

func permissionsAuthenticatedRequest(t *testing.T, tokens *identityservice.TokenManager, path string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(permsRouteUserID, permsRouteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

type fakePermissionsTenantRepository struct{ scenario *permissionsScenario }

func (f *fakePermissionsTenantRepository) Create(context.Context, *tenantmodel.Tenant) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (f *fakePermissionsTenantRepository) FindByID(_ context.Context, id string) (*tenantmodel.Tenant, error) {
	if f.scenario.tenant == nil || f.scenario.tenant.ID != id {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return f.scenario.tenant, nil
}

func (f *fakePermissionsTenantRepository) ListAccessibleByUserID(context.Context, string) ([]*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (f *fakePermissionsTenantRepository) UpdateProfile(context.Context, string, tenantrepository.TenantProfileUpdate) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (f *fakePermissionsTenantRepository) FindBySlug(context.Context, string) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
}

type fakePermissionsMembershipRepository struct{ scenario *permissionsScenario }

func (f *fakePermissionsMembershipRepository) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (f *fakePermissionsMembershipRepository) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	if f.scenario.membership == nil || f.scenario.membership.TenantID != tenantID || f.scenario.membership.UserID != userID {
		return nil, nil
	}
	return f.scenario.membership, nil
}

func (f *fakePermissionsMembershipRepository) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}

func (f *fakePermissionsMembershipRepository) Disable(context.Context, string, string, time.Time) error {
	return apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

type fakePermissionsUserRoleRepository struct{ scenario *permissionsScenario }

func (f *fakePermissionsUserRoleRepository) Assign(_ context.Context, assignment authzmodel.UserRole) (*authzmodel.UserRole, error) {
	return &assignment, nil
}

func (f *fakePermissionsUserRoleRepository) Remove(context.Context, string) error { return nil }

func (f *fakePermissionsUserRoleRepository) ListByUserScope(_ context.Context, userID string, scope authzmodel.Scope, tenantID string) ([]authzmodel.Role, error) {
	if userID != permsRouteUserID || scope != authzmodel.ScopeTenant || tenantID != permsRouteTenantID {
		return nil, nil
	}
	return f.scenario.roles, nil
}

type fakePermissionsRolePermissionRepository struct{ scenario *permissionsScenario }

func (f *fakePermissionsRolePermissionRepository) Assign(context.Context, authzmodel.RolePermission) error {
	return nil
}
func (f *fakePermissionsRolePermissionRepository) Remove(context.Context, string, string) error {
	return nil
}
func (f *fakePermissionsRolePermissionRepository) ListByRole(_ context.Context, roleID string) ([]authzmodel.Permission, error) {
	return f.scenario.rolePermissions[roleID], nil
}
