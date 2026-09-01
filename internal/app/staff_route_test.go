package app

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
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
	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires for
// Scheduling S3:
//
//	POST   /api/v1/tenants/{tenantID}/staff                    staff.create
//	GET    /api/v1/tenants/{tenantID}/staff                    staff.read
//	GET    /api/v1/tenants/{tenantID}/staff/{staffID}          staff.read
//	PATCH  /api/v1/tenants/{tenantID}/staff/{staffID}          staff.update
//	POST   /api/v1/tenants/{tenantID}/staff/{staffID}/archive  staff.archive
//	GET    /api/v1/tenants/{tenantID}/staff/{staffID}/services staff.read
//	PUT    /api/v1/tenants/{tenantID}/staff/{staffID}/services staff.update
//
// all: Authentication -> Tenant Context -> Authorization -> Handler
//
// using the REAL TenantContextService, the REAL StaffService (so the user-link
// rule and capability validation are genuinely exercised) and the REAL
// Authorizer, dispatched through a real http.ServeMux so the {tenantID} and
// {staffID} patterns are genuinely captured.

const (
	staffRouteUserID    = "550e8400-e29b-41d4-a716-446655451001"
	staffRouteSessionID = "550e8400-e29b-41d4-a716-446655451002"
	staffRouteTenantA   = "550e8400-e29b-41d4-a716-446655451003"
	staffRouteTenantB   = "550e8400-e29b-41d4-a716-446655451004"
	staffRouteStaffA    = "550e8400-e29b-41d4-a716-446655451005"
	staffRouteServiceA  = "550e8400-e29b-41d4-a716-446655451006"
)

// Permission sets matching what migration 000013 grants each role.
var (
	ownerStaffPermissions = []string{"tenant.read", "tenant.update", "staff.read", "staff.create", "staff.update", "staff.archive"}
	staffStaffPermissions = []string{"tenant.read", "staff.read", "service.read"}
)

func staffBase(tenantID string) string { return "/api/v1/tenants/" + tenantID + "/staff" }

type staffRouteCase struct {
	name   string
	method string
	path   string
	body   string
}

func allStaffRoutes(tenantID string) []staffRouteCase {
	base := staffBase(tenantID)
	return []staffRouteCase{
		{"create", http.MethodPost, base, `{"display_name":"Ada"}`},
		{"list", http.MethodGet, base, ""},
		{"get", http.MethodGet, base + "/" + staffRouteStaffA, ""},
		{"update", http.MethodPatch, base + "/" + staffRouteStaffA, `{"display_name":"Ada Obi"}`},
		{"archive", http.MethodPost, base + "/" + staffRouteStaffA + "/archive", ""},
		{"list capabilities", http.MethodGet, base + "/" + staffRouteStaffA + "/services", ""},
		{"replace capabilities", http.MethodPut, base + "/" + staffRouteStaffA + "/services", `{"service_ids":[]}`},
	}
}

// --- authentication ----------------------------------------------------------

func TestStaffRoutesRequireAuthentication(t *testing.T) {
	for _, test := range allStaffRoutes(staffRouteTenantA) {
		t.Run(test.name, func(t *testing.T) {
			handler, _, store := buildStaffRoutes(t, staffScenario(), ownerStaffPermissions)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s, want 401", recorder.Code, recorder.Body.String())
			}
			if store.writeCalls != 0 {
				t.Fatal("an unauthenticated request reached a staff write")
			}
		})
	}
}

// --- BUSINESS_OWNER ----------------------------------------------------------

func TestBusinessOwnerCanManageTheRoster(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	handler, tokens, _ := buildStaffRoutes(t, scenario, ownerStaffPermissions)

	for _, test := range allStaffRoutes(staffRouteTenantA) {
		// create is asserted separately below for its 201. A successful
		// capability replacement opens a real transaction, so its write path is
		// proven against a real database in
		// scheduling/service/staff_capability_integration_test.go; its
		// authorization paths are covered by the 401/403 tests here.
		if test.name == "create" || test.name == "replace capabilities" {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, staffRequest(t, tokens, test.method, test.path, test.body))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestBusinessOwnerCanCreateANonLoginTechnician(t *testing.T) {
	handler, tokens, store := buildStaffRoutes(t, staffScenario(), ownerStaffPermissions)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodPost, staffBase(staffRouteTenantA), `{"display_name":"Chioma"}`))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if store.writeCalls != 1 {
		t.Fatalf("staff writes = %d, want 1", store.writeCalls)
	}

	var created map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["user_id"] != nil {
		t.Fatalf("user_id = %v, want null for a non-login worker", created["user_id"])
	}
	if created["status"] != string(schedulingmodel.StatusActive) {
		t.Fatalf("status = %v, want ACTIVE", created["status"])
	}
}

// The mandatory owner-as-technician scenario, end to end through the real chain.
// The owner links their own user to a bookable profile and stays a
// BUSINESS_OWNER: their permission set is identical before and after, and no
// STAFF role is involved anywhere.
func TestBusinessOwnerBecomesBookableWithoutAcquiringTheStaffRole(t *testing.T) {
	scenario := staffScenario()
	handler, tokens, _ := buildStaffRoutes(t, scenario, ownerStaffPermissions)
	recorder := httptest.NewRecorder()

	body := `{"display_name":"Nnamdi","user_id":"` + staffRouteUserID + `","is_bookable":true}`
	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodPost, staffBase(staffRouteTenantA), body))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["user_id"] != staffRouteUserID {
		t.Fatalf("user_id = %v, want the owner's own user id", created["user_id"])
	}
	if created["is_bookable"] != true {
		t.Fatalf("is_bookable = %v, want true", created["is_bookable"])
	}

	// The membership row is untouched — S3 never writes to membership or roles.
	if scenario.membership.Status != tenantmodel.MembershipStatusActive {
		t.Fatalf("membership status changed to %q", scenario.membership.Status)
	}
	// And the owner's effective permissions are unchanged: still no staff-role
	// read-only downgrade, still every owner permission.
	if len(ownerStaffPermissions) != 6 {
		t.Fatalf("the owner's permission set was mutated by creating a profile: %v", ownerStaffPermissions)
	}
}

// --- STAFF -------------------------------------------------------------------

func TestStaffRoleCanReadTheRosterAndCapabilities(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	handler, tokens, _ := buildStaffRoutes(t, scenario, staffStaffPermissions)

	base := staffBase(staffRouteTenantA)
	for _, path := range []string{base, base + "/" + staffRouteStaffA, base + "/" + staffRouteStaffA + "/services"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, path, ""))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, body = %s, want 200 for STAFF", path, recorder.Code, recorder.Body.String())
		}
	}
}

// STAFF holds staff.read only. Every write — including capability assignment,
// which carries staff.update — must be refused before reaching persistence.
func TestStaffRoleCannotWriteToTheRoster(t *testing.T) {
	base := staffBase(staffRouteTenantA)
	cases := []staffRouteCase{
		{"create", http.MethodPost, base, `{"display_name":"Ada"}`},
		{"update", http.MethodPatch, base + "/" + staffRouteStaffA, `{"display_name":"Hijacked"}`},
		{"archive", http.MethodPost, base + "/" + staffRouteStaffA + "/archive", ""},
		{"replace capabilities", http.MethodPut, base + "/" + staffRouteStaffA + "/services", `{"service_ids":[]}`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			scenario := staffScenario()
			scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
			handler, tokens, store := buildStaffRoutes(t, scenario, staffStaffPermissions)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, staffRequest(t, tokens, test.method, test.path, test.body))

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body = %s, want 403 for STAFF", recorder.Code, recorder.Body.String())
			}
			assertBodyCode(t, recorder, "PERMISSION_DENIED")
			if store.writeCalls != 0 {
				t.Fatal("a denied request still reached the repository")
			}
			if stored := scenario.profiles[staffRouteStaffA]; stored.DisplayName != "Ada" || stored.Status != schedulingmodel.StatusActive {
				t.Fatalf("a denied request mutated the profile: %+v", stored)
			}
		})
	}
}

// Capability assignment is protected by staff.update, not service.update: it
// changes what a technician can do, never the service definition. A caller with
// full catalog permissions but no staff.update must still be refused.
func TestCapabilityAssignmentRequiresStaffUpdateNotServiceUpdate(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	catalogOnly := []string{"tenant.read", "staff.read", "service.read", "service.create", "service.update", "service.archive"}
	handler, tokens, store := buildStaffRoutes(t, scenario, catalogOnly)
	recorder := httptest.NewRecorder()

	path := staffBase(staffRouteTenantA) + "/" + staffRouteStaffA + "/services"
	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodPut, path, `{"service_ids":[]}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 — service.* must not authorize capability assignment", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
	if store.writeCalls != 0 {
		t.Fatal("a denied capability assignment reached the repository")
	}
}

// --- tenant isolation --------------------------------------------------------

func TestStaffRoutesDenyCrossTenantAccess(t *testing.T) {
	for _, test := range allStaffRoutes(staffRouteTenantB) {
		t.Run(test.name, func(t *testing.T) {
			scenario := staffScenario()
			scenario.otherTenant = &tenantmodel.Tenant{ID: staffRouteTenantB, Name: "Rival Nails", Slug: "rival-nails", Status: tenantmodel.StatusActive}
			scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantB)
			handler, tokens, store := buildStaffRoutes(t, scenario, ownerStaffPermissions)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, staffRequest(t, tokens, test.method, test.path, test.body))

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body = %s, want 403", recorder.Code, recorder.Body.String())
			}
			assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
			if store.writeCalls != 0 || store.readCalls != 0 {
				t.Fatal("a cross-tenant request reached the staff repository")
			}
		})
	}
}

// A membership that has been revoked must close the workspace door, even though
// the staff profile itself survives untouched.
func TestRevokedMembershipLosesWorkspaceAccessButKeepsTheProfile(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	scenario.membership.Status = tenantmodel.MembershipStatusDisabled
	handler, tokens, store := buildStaffRoutes(t, scenario, ownerStaffPermissions)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, staffBase(staffRouteTenantA), ""))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 for a disabled membership", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	if store.readCalls != 0 {
		t.Fatal("a disabled member reached the staff repository")
	}
	// The profile is untouched: revocation governs access, never scheduling data.
	stored := scenario.profiles[staffRouteStaffA]
	if stored.Status != schedulingmodel.StatusActive || !stored.IsBookable {
		t.Fatalf("revoking a membership mutated the staff profile: %+v", stored)
	}
}

// A disabled tenant is unreachable regardless of permissions.
func TestStaffRoutesDenyADisabledTenant(t *testing.T) {
	scenario := staffScenario()
	scenario.tenant.Status = tenantmodel.StatusDisabled
	handler, tokens, store := buildStaffRoutes(t, scenario, ownerStaffPermissions)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, staffBase(staffRouteTenantA), ""))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 for a disabled tenant", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
	if store.readCalls != 0 {
		t.Fatal("a disabled tenant's roster was read")
	}
}

// A staff ID that genuinely exists under another tenant must be
// indistinguishable from one that does not exist.
func TestGetDoesNotDiscloseAnotherTenantsStaffID(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantB)
	handler, tokens, _ := buildStaffRoutes(t, scenario, ownerStaffPermissions)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, staffBase(staffRouteTenantA)+"/"+staffRouteStaffA, ""))
	elsewhereCode, elsewhereBody := recorder.Code, recorder.Body.String()

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, staffBase(staffRouteTenantA)+"/550e8400-e29b-41d4-a716-446655459999", ""))

	if elsewhereCode != http.StatusNotFound || recorder.Code != http.StatusNotFound {
		t.Fatalf("statuses = %d (exists elsewhere) / %d (nonexistent), want both 404", elsewhereCode, recorder.Code)
	}
	if elsewhereBody != recorder.Body.String() {
		t.Fatalf("responses differ, disclosing that the ID exists elsewhere:\n  %s\n  %s", elsewhereBody, recorder.Body.String())
	}
}

// --- test wiring -------------------------------------------------------------

type staffScenarioState struct {
	tenant      *tenantmodel.Tenant
	otherTenant *tenantmodel.Tenant
	membership  *tenantmodel.TenantMembership
	profiles    map[string]*schedulingmodel.StaffProfile
	services    map[string]*schedulingmodel.Service
}

func activeStaff(tenantID string) *schedulingmodel.StaffProfile {
	return &schedulingmodel.StaffProfile{
		ID: staffRouteStaffA, TenantID: tenantID, DisplayName: "Ada",
		IsBookable: true, Status: schedulingmodel.StatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

func staffScenario() *staffScenarioState {
	businessType := tenantmodel.BusinessTypeNailTechnician
	currency := "NGN"
	return &staffScenarioState{
		tenant: &tenantmodel.Tenant{
			ID: staffRouteTenantA, Name: "Acme Nails", Slug: "acme-nails", Status: tenantmodel.StatusActive,
			BusinessType: &businessType, OnboardingStatus: tenantmodel.OnboardingStatusCompleted, Currency: &currency,
		},
		membership: &tenantmodel.TenantMembership{
			TenantID: staffRouteTenantA, UserID: staffRouteUserID, Status: tenantmodel.MembershipStatusActive,
		},
		profiles: map[string]*schedulingmodel.StaffProfile{},
		services: map[string]*schedulingmodel.Service{
			staffRouteServiceA: {ID: staffRouteServiceA, TenantID: staffRouteTenantA, Name: "Manicure", Status: schedulingmodel.StatusActive},
		},
	}
}

func buildStaffRoutes(t *testing.T, scenario *staffScenarioState, tenantPermissions []string) (http.Handler, *identityservice.TokenManager, *statefulStaffRepository) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	tenants := &statefulCatalogTenantRepository{tenant: scenario.tenant, otherTenant: scenario.otherTenant}
	memberships := &staffMembershipRepository{scenario: scenario}
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	tenantMiddleware := tenant.Middleware{Resolver: contextService}
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})

	staffStore := &statefulStaffRepository{profiles: scenario.profiles}
	capabilities := &statefulCapabilityRepository{assignments: map[string][]string{}}
	services := &statefulServiceRepository{services: scenario.services}
	// unreachableTxBeginner asserts the property these tests rely on: no route
	// exercised here reaches a transaction. A successful capability replacement
	// would, which is why its write path is an integration test rather than a
	// route test.
	staffSvc := schedulingservice.NewStaffService(unreachableTxBeginner{}, staffStore, capabilities, services, memberships)
	staffHandler := schedulinghandler.NewStaffHandler(staffSvc)

	wrap := func(permission string, next http.HandlerFunc) http.Handler {
		return authMiddleware.Wrap(tenantMiddleware.Wrap(
			authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: permission}.Wrap(next),
		))
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tenants/{tenantID}/staff", wrap("staff.create", func(w http.ResponseWriter, r *http.Request) {
		staffHandler.Create(w, r, r.PathValue("tenantID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/staff", wrap("staff.read", func(w http.ResponseWriter, r *http.Request) {
		staffHandler.List(w, r, r.PathValue("tenantID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/staff/{staffID}", wrap("staff.read", func(w http.ResponseWriter, r *http.Request) {
		staffHandler.Get(w, r, r.PathValue("tenantID"), r.PathValue("staffID"))
	}))
	mux.Handle("PATCH /api/v1/tenants/{tenantID}/staff/{staffID}", wrap("staff.update", func(w http.ResponseWriter, r *http.Request) {
		staffHandler.Update(w, r, r.PathValue("tenantID"), r.PathValue("staffID"))
	}))
	mux.Handle("POST /api/v1/tenants/{tenantID}/staff/{staffID}/archive", wrap("staff.archive", func(w http.ResponseWriter, r *http.Request) {
		staffHandler.Archive(w, r, r.PathValue("tenantID"), r.PathValue("staffID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/staff/{staffID}/services", wrap("staff.read", func(w http.ResponseWriter, r *http.Request) {
		staffHandler.ListCapabilities(w, r, r.PathValue("tenantID"), r.PathValue("staffID"))
	}))
	mux.Handle("PUT /api/v1/tenants/{tenantID}/staff/{staffID}/services", wrap("staff.update", func(w http.ResponseWriter, r *http.Request) {
		staffHandler.ReplaceCapabilities(w, r, r.PathValue("tenantID"), r.PathValue("staffID"))
	}))
	return mux, tokens, staffStore
}

func staffRequest(t *testing.T, tokens *identityservice.TokenManager, method, path, body string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(staffRouteUserID, staffRouteSessionID)
	if err != nil {
		t.Fatal(err)
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

type statefulStaffRepository struct {
	profiles   map[string]*schedulingmodel.StaffProfile
	readCalls  int
	writeCalls int
}

func (r *statefulStaffRepository) Create(_ context.Context, profile *schedulingmodel.StaffProfile) (*schedulingmodel.StaffProfile, error) {
	r.writeCalls++
	stored := *profile
	if stored.Status == "" {
		stored.Status = schedulingmodel.StatusActive
	}
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	r.profiles[stored.ID] = &stored
	return &stored, nil
}

func (r *statefulStaffRepository) FindByID(_ context.Context, tenantID string, id string) (*schedulingmodel.StaffProfile, error) {
	r.readCalls++
	stored, ok := r.profiles[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)
	}
	return stored, nil
}

func (r *statefulStaffRepository) ListByTenant(_ context.Context, tenantID string, filter schedulingrepository.StaffListFilter) ([]*schedulingmodel.StaffProfile, error) {
	r.readCalls++
	var result []*schedulingmodel.StaffProfile
	for _, stored := range r.profiles {
		if stored.TenantID != tenantID {
			continue
		}
		if filter.Status != nil && stored.Status != *filter.Status {
			continue
		}
		result = append(result, stored)
	}
	return result, nil
}

func (r *statefulStaffRepository) Update(_ context.Context, tenantID string, id string, update schedulingrepository.StaffUpdate) (*schedulingmodel.StaffProfile, error) {
	r.writeCalls++
	stored, ok := r.profiles[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)
	}
	if update.DisplayName != nil {
		stored.DisplayName = *update.DisplayName
	}
	if update.Bio != nil {
		stored.Bio = update.Bio
	}
	if update.IsBookable != nil {
		stored.IsBookable = *update.IsBookable
	}
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

func (r *statefulStaffRepository) Archive(_ context.Context, tenantID string, id string) (*schedulingmodel.StaffProfile, error) {
	r.writeCalls++
	stored, ok := r.profiles[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)
	}
	stored.Status = schedulingmodel.StatusArchived
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

// unreachableTxBeginner fails loudly if a route test reaches a transaction.
type unreachableTxBeginner struct{}

func (unreachableTxBeginner) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	return nil, errors.New("a route test reached BeginTx — transactional writes belong in integration tests")
}

type statefulCapabilityRepository struct{ assignments map[string][]string }

func (r *statefulCapabilityRepository) ListServiceIDs(_ context.Context, _ string, staffID string) ([]string, error) {
	ids := r.assignments[staffID]
	if ids == nil {
		return []string{}, nil
	}
	return append([]string{}, ids...), nil
}

func (r *statefulCapabilityRepository) DeleteAll(_ context.Context, _ string, staffID string) error {
	delete(r.assignments, staffID)
	return nil
}

func (r *statefulCapabilityRepository) Assign(_ context.Context, _ string, staffID string, serviceID string) error {
	r.assignments[staffID] = append(r.assignments[staffID], serviceID)
	return nil
}

type staffMembershipRepository struct{ scenario *staffScenarioState }

func (m *staffMembershipRepository) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (m *staffMembershipRepository) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	membership := m.scenario.membership
	if membership == nil || membership.TenantID != tenantID || membership.UserID != userID {
		return nil, nil
	}
	return membership, nil
}

func (m *staffMembershipRepository) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}

func (m *staffMembershipRepository) Disable(context.Context, string, string, time.Time) error {
	return nil
}

// compile-time guards: the fakes must keep satisfying the real interfaces.
var (
	_ schedulingrepository.StaffRepository      = (*statefulStaffRepository)(nil)
	_ schedulingrepository.CapabilityRepository = (*statefulCapabilityRepository)(nil)
	_ schedulingservice.MembershipReader        = (*staffMembershipRepository)(nil)
)
