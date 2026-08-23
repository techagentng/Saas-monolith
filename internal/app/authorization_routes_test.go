package app

import (
	"context"
	"crypto/ed25519"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization"
	authzhandler "github.com/techagentng/saas-monolith/internal/authorization/handler"
	authzmodel "github.com/techagentng/saas-monolith/internal/authorization/model"
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

// These tests exercise the exact production middleware chain that app.New
// wires for a Feature 6 tenant-scoped route:
//
//	Authentication -> Tenant Context -> Authorization -> Handler
//
// using Feature 3/4/5's real middleware and service types with fakes only
// at the repository boundary, so no PostgreSQL connection is required.

const (
	routeUserID   = "550e8400-e29b-41d4-a716-446655440501"
	routeTenantID = "550e8400-e29b-41d4-a716-446655440502"
)

func TestMemberCreateRouteRequiresAuthentication(t *testing.T) {
	handler, _ := buildMemberCreateRoute(t, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+routeTenantID+"/members", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for unauthenticated request", recorder.Code)
	}
}

func TestMemberCreateRouteDeniesUserWithoutTenantAccess(t *testing.T) {
	handler, deps := buildMemberCreateRoute(t, nil)
	deps.tenantContext.deny = true
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, authenticatedRequest(t, deps, http.MethodPost, "/api/v1/tenants/"+routeTenantID+"/members"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 TENANT_ACCESS_DENIED", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

func TestMemberCreateRouteDeniesValidTenantAccessWithoutPermission(t *testing.T) {
	handler, deps := buildMemberCreateRoute(t, []string{"user.read"}) // missing user.create
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, authenticatedRequest(t, deps, http.MethodPost, "/api/v1/tenants/"+routeTenantID+"/members"))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 PERMISSION_DENIED", recorder.Code)
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
}

func TestMemberCreateRouteExecutesHandlerWithRequiredPermission(t *testing.T) {
	handler, deps := buildMemberCreateRoute(t, []string{"user.create"})
	deps.membership.created = &tenantmodel.TenantMembership{ID: "membership", TenantID: routeTenantID, UserID: "target", Status: tenantmodel.MembershipStatusActive}
	recorder := httptest.NewRecorder()

	request := authenticatedRequest(t, deps, http.MethodPost, "/api/v1/tenants/"+routeTenantID+"/members")
	request.Body = requestBody(`{"user_id":"target"}`)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
}

func TestRoleAssignmentRouteDeniesStaffWithoutRoleAssignPermission(t *testing.T) {
	handler, deps := buildRoleAssignmentRoute(t, []string{"tenant.read", "user.read", "role.read", "permission.read"}) // STAFF matrix
	recorder := httptest.NewRecorder()

	request := authenticatedRequest(t, deps, http.MethodPost, "/api/v1/tenants/"+routeTenantID+"/role-assignments")
	request.Body = requestBody(`{"user_id":"target","role_id":"staff-role"}`)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 PERMISSION_DENIED", recorder.Code)
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
}

const (
	routeTargetUserID = "550e8400-e29b-41d4-a716-446655440504"
	routeStaffRoleID  = "650e8400-e29b-41d4-a716-446655440003"
	routeOwnerRoleID  = "650e8400-e29b-41d4-a716-446655440002"
)

func TestRoleAssignmentRouteAllowsBusinessOwnerAssigningStaff(t *testing.T) {
	handler, deps := buildRoleAssignmentRoute(t, []string{"role.assign"})
	deps.roles.role = &authzmodel.Role{ID: routeStaffRoleID, Name: "STAFF", Scope: authzmodel.ScopeTenant}
	deps.roleUserRoles.assignment = &authzmodel.UserRole{ID: "assignment", UserID: routeTargetUserID, RoleID: routeStaffRoleID, TenantID: routeTenantID}
	recorder := httptest.NewRecorder()

	request := authenticatedRequest(t, deps, http.MethodPost, "/api/v1/tenants/"+routeTenantID+"/role-assignments")
	request.Body = requestBody(`{"user_id":"` + routeTargetUserID + `","role_id":"` + routeStaffRoleID + `"}`)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
}

func TestRoleAssignmentRouteDeniesBusinessOwnerAssigningBusinessOwner(t *testing.T) {
	handler, deps := buildRoleAssignmentRoute(t, []string{"role.assign"})
	deps.roles.role = &authzmodel.Role{ID: routeOwnerRoleID, Name: "BUSINESS_OWNER", Scope: authzmodel.ScopeTenant}
	recorder := httptest.NewRecorder()

	request := authenticatedRequest(t, deps, http.MethodPost, "/api/v1/tenants/"+routeTenantID+"/role-assignments")
	request.Body = requestBody(`{"user_id":"` + routeTargetUserID + `","role_id":"` + routeOwnerRoleID + `"}`)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when assigning BUSINESS_OWNER", recorder.Code)
	}
}

// --- test wiring -----------------------------------------------------------------

type routeDeps struct {
	tokens        *identityservice.TokenManager
	sessions      *fakeSessionRepository
	tenantContext *fakeTenantContextService
	membership    *fakeMembershipService
	roles         *fakeRoleRepository
	roleUserRoles *fakeUserRoleRepository
	users         *fakeAuthzUserRepository
	tenants       *fakeAuthzTenantRepository
	memberships   *fakeAuthzMembershipRepository
}

func buildMemberCreateRoute(t *testing.T, tenantPermissions []string) (http.Handler, *routeDeps) {
	t.Helper()
	deps := newRouteDeps(t)
	membershipHandler := tenanthandler.NewMembershipHandler(deps.membership)
	authMiddleware := auth.Middleware{Tokens: deps.tokens, Sessions: deps.sessions}
	tenantMiddleware := tenant.Middleware{Resolver: deps.tenantContext}
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})

	handler := authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "user.create"}.Wrap(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				membershipHandler.Create(w, r, r.PathValue("tenantID"))
			}),
		),
	))
	return withTenantIDPath(handler), deps
}

func buildRoleAssignmentRoute(t *testing.T, tenantPermissions []string) (http.Handler, *routeDeps) {
	t.Helper()
	deps := newRouteDeps(t)
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})
	assignmentService := authzservice.NewAssignmentService(deps.users, deps.tenants, deps.roles, deps.roleUserRoles, deps.memberships)
	roleAssignmentService := authzservice.NewTenantRoleAssignmentService(authorizer, deps.roles, assignmentService)
	roleAssignmentHandler := authzhandler.NewRoleAssignmentHandler(roleAssignmentService)
	authMiddleware := auth.Middleware{Tokens: deps.tokens, Sessions: deps.sessions}
	tenantMiddleware := tenant.Middleware{Resolver: deps.tenantContext}

	handler := authMiddleware.Wrap(tenantMiddleware.Wrap(
		authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: "role.assign"}.Wrap(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				roleAssignmentHandler.Assign(w, r, r.PathValue("tenantID"))
			}),
		),
	))
	return withTenantIDPath(handler), deps
}

func withTenantIDPath(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tenants/{tenantID}/members", next)
	mux.Handle("POST /api/v1/tenants/{tenantID}/role-assignments", next)
	return mux
}

func newRouteDeps(t *testing.T) *routeDeps {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	return &routeDeps{
		tokens:        tokens,
		sessions:      &fakeSessionRepository{},
		tenantContext: &fakeTenantContextService{tenantID: routeTenantID},
		membership:    &fakeMembershipService{},
		roles:         &fakeRoleRepository{},
		roleUserRoles: &fakeUserRoleRepository{},
		users:         &fakeAuthzUserRepository{},
		tenants:       &fakeAuthzTenantRepository{},
		memberships:   &fakeAuthzMembershipRepository{},
	}
}

const routeSessionID = "550e8400-e29b-41d4-a716-446655440503"

func authenticatedRequest(t *testing.T, deps *routeDeps, method, path string) *http.Request {
	t.Helper()
	token, err := deps.tokens.Issue(routeUserID, routeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func requestBody(body string) io.ReadCloser { return io.NopCloser(strings.NewReader(body)) }

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

// --- fakes -------------------------------------------------------------------------

type fakeSessionRepository struct{}

func (fakeSessionRepository) Create(_ context.Context, session identitymodel.Session) (*identitymodel.Session, error) {
	return &session, nil
}
func (fakeSessionRepository) Rotate(context.Context, string, string, time.Time) (*identitymodel.Session, error) {
	return nil, nil
}
func (fakeSessionRepository) Revoke(context.Context, string) error { return nil }
func (fakeSessionRepository) FindActive(_ context.Context, userID, sessionID string, now time.Time) (*identitymodel.Session, error) {
	return &identitymodel.Session{UserID: userID, ID: sessionID, ExpiresAt: now.Add(time.Minute)}, nil
}

type fakeTenantContextService struct {
	tenantID string
	deny     bool
}

func (r *fakeTenantContextService) Resolve(context.Context, auth.Principal, string) (*tenantservice.TenantContext, error) {
	if r.deny {
		return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
	}
	return &tenantservice.TenantContext{TenantID: r.tenantID}, nil
}

type fakeMembershipService struct{ created *tenantmodel.TenantMembership }

func (s *fakeMembershipService) Create(context.Context, tenantservice.CreateMembershipInput) (*tenantmodel.TenantMembership, error) {
	return s.created, nil
}
func (*fakeMembershipService) Find(context.Context, string, string) (*tenantmodel.TenantMembership, error) {
	return nil, nil
}
func (*fakeMembershipService) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}
func (*fakeMembershipService) Revoke(context.Context, string, string) error { return nil }

type fakeResolutionService struct{ tenantPermissions []string }

func (r *fakeResolutionService) ResolveTenant(context.Context, string, string) ([]string, error) {
	return r.tenantPermissions, nil
}
func (r *fakeResolutionService) ResolvePlatform(context.Context, string) ([]string, error) {
	return nil, nil
}

type fakeRoleRepository struct{ role *authzmodel.Role }

func (r *fakeRoleRepository) FindByID(context.Context, string) (*authzmodel.Role, error) {
	return r.role, nil
}
func (*fakeRoleRepository) FindByNameScope(context.Context, string, authzmodel.Scope) (*authzmodel.Role, error) {
	return nil, nil
}

type fakeUserRoleRepository struct{ assignment *authzmodel.UserRole }

func (r *fakeUserRoleRepository) Assign(_ context.Context, assignment authzmodel.UserRole) (*authzmodel.UserRole, error) {
	if r.assignment != nil {
		return r.assignment, nil
	}
	return &assignment, nil
}
func (*fakeUserRoleRepository) Remove(context.Context, string) error { return nil }
func (*fakeUserRoleRepository) ListByUserScope(context.Context, string, authzmodel.Scope, string) ([]authzmodel.Role, error) {
	return nil, nil
}

type fakeAuthzUserRepository struct{}

func (*fakeAuthzUserRepository) Create(context.Context, identitymodel.User) (*identitymodel.User, error) {
	return nil, nil
}
func (*fakeAuthzUserRepository) FindByID(_ context.Context, id string) (*identitymodel.User, error) {
	return &identitymodel.User{ID: id}, nil
}
func (*fakeAuthzUserRepository) FindByEmail(context.Context, string) (*identitymodel.User, error) {
	return nil, nil
}

type fakeAuthzTenantRepository struct{}

func (*fakeAuthzTenantRepository) Create(_ context.Context, tenant *tenantmodel.Tenant) (*tenantmodel.Tenant, error) {
	return tenant, nil
}

func (*fakeAuthzTenantRepository) FindByID(_ context.Context, id string) (*tenantmodel.Tenant, error) {
	return &tenantmodel.Tenant{ID: id, Status: tenantmodel.StatusActive}, nil
}

func (*fakeAuthzTenantRepository) ListAccessibleByUserID(_ context.Context, userID string) ([]*tenantmodel.Tenant, error) {
	return []*tenantmodel.Tenant{}, nil
}

func (*fakeAuthzTenantRepository) UpdateProfile(context.Context, string, tenantrepository.TenantProfileUpdate) (*tenantmodel.Tenant, error) {
	return nil, nil
}

func (*fakeAuthzTenantRepository) FindBySlug(context.Context, string) (*tenantmodel.Tenant, error) {
	return nil, nil
}

type fakeAuthzMembershipRepository struct{}

func (*fakeAuthzMembershipRepository) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, nil
}
func (*fakeAuthzMembershipRepository) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	return &tenantmodel.TenantMembership{TenantID: tenantID, UserID: userID, Status: tenantmodel.MembershipStatusActive}, nil
}
func (*fakeAuthzMembershipRepository) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}
func (*fakeAuthzMembershipRepository) Disable(context.Context, string, string, time.Time) error {
	return nil
}
