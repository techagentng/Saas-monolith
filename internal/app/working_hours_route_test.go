package app

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization"
	authzservice "github.com/techagentng/saas-monolith/internal/authorization/service"
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These exercise the exact production middleware chain app.New wires for
// Scheduling S5:
//
//	GET  /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours  staff.read
//	PUT  /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours  staff.update
//
// all: Authentication -> Tenant Context -> Authorization -> Handler
//
// using the REAL TenantContextService and the REAL Authorizer, dispatched
// through a real http.ServeMux, so the {tenantID} and {staffID} patterns are
// genuinely captured. It reuses staffScenario/staffMembershipRepository from
// staff_route_test.go: this feature's tenant, membership and staff-profile
// fixtures are identical to S3's.

func workingHoursPath(tenantID, staffID string) string {
	return staffBase(tenantID) + "/" + staffID + "/working-hours"
}

func TestWorkingHoursRoutesRequireAuthentication(t *testing.T) {
	cases := []staffRouteCase{
		{"get", http.MethodGet, workingHoursPath(staffRouteTenantA, staffRouteStaffA), ""},
		{"put", http.MethodPut, workingHoursPath(staffRouteTenantA, staffRouteStaffA), `{"intervals":[]}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			handler, _, store := buildWorkingHoursRoutes(t, staffScenario(), ownerStaffPermissions)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s, want 401", recorder.Code, recorder.Body.String())
			}
			if store.writeCalls != 0 {
				t.Fatal("an unauthenticated request reached a working hours write")
			}
		})
	}
}

// A successful PUT opens a real transaction (schedulingservice.WorkingHoursService.
// ReplaceWeeklySchedule always writes through BeginTx on success), so this
// builder's unreachableTxBeginner would fail it — deliberately, the same
// choice staff_route_test.go makes for "replace capabilities". Its write path
// is proven against a real database in
// scheduling/service/working_hours_integration_test.go
// (TestReplaceWeeklyScheduleCommitsTheWholeSchedule); its request-decoding
// and authorization paths are proven here and in the handler's own unit
// tests (TestWorkingHoursReplacePassesIntervalsThroughToTheService).
func TestBusinessOwnerCanReadWorkingHours(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	handler, tokens, _ := buildWorkingHoursRoutes(t, scenario, ownerStaffPermissions)

	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, staffRequest(t, tokens, http.MethodGet, workingHoursPath(staffRouteTenantA, staffRouteStaffA), ""))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s, want 200", getRecorder.Code, getRecorder.Body.String())
	}
}

// A staff member with no configured hours yet is a successful empty
// schedule, never a 404.
func TestWorkingHoursGetReturnsAnEmptyScheduleForANewStaffMember(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	handler, tokens, _ := buildWorkingHoursRoutes(t, scenario, ownerStaffPermissions)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, workingHoursPath(staffRouteTenantA, staffRouteStaffA), ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"intervals":[]`) {
		t.Fatalf("body = %s, want an empty intervals array", recorder.Body.String())
	}
}

func TestWorkingHoursReplaceRejectsMalformedJSONThroughTheRealChain(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	handler, tokens, store := buildWorkingHoursRoutes(t, scenario, ownerStaffPermissions)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodPut, workingHoursPath(staffRouteTenantA, staffRouteStaffA), "{not json"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "INVALID_REQUEST")
	if store.writeCalls != 0 {
		t.Fatal("malformed JSON reached the repository")
	}
}

// A nonexistent staff ID under the caller's own tenant is STAFF_NOT_FOUND —
// not a 500, not an empty schedule implying the profile exists.
func TestWorkingHoursHandlesNonexistentStaff(t *testing.T) {
	handler, tokens, store := buildWorkingHoursRoutes(t, staffScenario(), ownerStaffPermissions)
	unknownStaff := "550e8400-e29b-41d4-a716-446655459999"
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, workingHoursPath(staffRouteTenantA, unknownStaff), ""))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "STAFF_NOT_FOUND")
	if store.writeCalls != 0 {
		t.Fatal("a request against a nonexistent staff id reached a write")
	}
}

// --- STAFF role ----------------------------------------------------------

func TestStaffRoleCanReadWorkingHoursButNotReplaceThem(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	handler, tokens, store := buildWorkingHoursRoutes(t, scenario, staffStaffPermissions)

	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, staffRequest(t, tokens, http.MethodGet, workingHoursPath(staffRouteTenantA, staffRouteStaffA), ""))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s, want 200 for STAFF", getRecorder.Code, getRecorder.Body.String())
	}

	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, staffRequest(t, tokens, http.MethodPut, workingHoursPath(staffRouteTenantA, staffRouteStaffA), `{"intervals":[]}`))
	if putRecorder.Code != http.StatusForbidden {
		t.Fatalf("PUT status = %d, body = %s, want 403 for STAFF", putRecorder.Code, putRecorder.Body.String())
	}
	assertBodyCode(t, putRecorder, "PERMISSION_DENIED")
	if store.writeCalls != 0 {
		t.Fatal("a denied replacement reached the repository")
	}
}

// --- tenant isolation ------------------------------------------------------

func TestWorkingHoursRoutesDenyCrossTenantAccess(t *testing.T) {
	cases := []staffRouteCase{
		{"get", http.MethodGet, workingHoursPath(staffRouteTenantB, staffRouteStaffA), ""},
		{"put", http.MethodPut, workingHoursPath(staffRouteTenantB, staffRouteStaffA), `{"intervals":[]}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			scenario := staffScenario()
			scenario.otherTenant = &tenantmodel.Tenant{ID: staffRouteTenantB, Name: "Rival Nails", Slug: "rival-nails", Status: tenantmodel.StatusActive}
			scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantB)
			handler, tokens, store := buildWorkingHoursRoutes(t, scenario, ownerStaffPermissions)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, staffRequest(t, tokens, test.method, test.path, test.body))

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body = %s, want 403", recorder.Code, recorder.Body.String())
			}
			assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
			if store.writeCalls != 0 || store.readCalls != 0 {
				t.Fatal("a cross-tenant request reached the working hours repository")
			}
		})
	}
}

// A staff ID that genuinely exists under another tenant must be
// indistinguishable from one that does not exist — the same non-disclosure
// TestGetDoesNotDiscloseAnotherTenantsStaffID proves for the roster.
func TestWorkingHoursGetDoesNotDiscloseAnotherTenantsStaffID(t *testing.T) {
	scenario := staffScenario()
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantB)
	handler, tokens, _ := buildWorkingHoursRoutes(t, scenario, ownerStaffPermissions)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, workingHoursPath(staffRouteTenantA, staffRouteStaffA), ""))
	elsewhereCode, elsewhereBody := recorder.Code, recorder.Body.String()

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet, workingHoursPath(staffRouteTenantA, "550e8400-e29b-41d4-a716-446655459999"), ""))

	if elsewhereCode != http.StatusNotFound || recorder.Code != http.StatusNotFound {
		t.Fatalf("statuses = %d (exists elsewhere) / %d (nonexistent), want both 404", elsewhereCode, recorder.Code)
	}
	if elsewhereBody != recorder.Body.String() {
		t.Fatalf("responses differ, disclosing that the id exists elsewhere:\n  %s\n  %s", elsewhereBody, recorder.Body.String())
	}
}

// --- test wiring -----------------------------------------------------------

// buildWorkingHoursRoutes mirrors buildStaffRoutes, adding only the two
// working-hours routes over a real WorkingHoursService backed by
// statefulStaffRepository (S3's own fake, which already satisfies
// schedulingservice.StaffReader) and an in-memory statefulWorkingHoursRepository.
func buildWorkingHoursRoutes(t *testing.T, scenario *staffScenarioState, tenantPermissions []string) (http.Handler, *identityservice.TokenManager, *statefulWorkingHoursRepository) {
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
	hoursStore := &statefulWorkingHoursRepository{byStaff: map[string][]*schedulingmodel.WorkingHourInterval{}}
	// unreachableTxBeginner asserts no route exercised here reaches a
	// transaction. A successful replacement would; its write path is proven
	// against a real database in
	// scheduling/service/working_hours_integration_test.go, and its
	// authorization paths are covered by the tests in this file.
	hoursSvc := schedulingservice.NewWorkingHoursService(unreachableTxBeginner{}, hoursStore, staffStore)
	hoursHandler := schedulinghandler.NewWorkingHoursHandler(hoursSvc)

	wrap := func(permission string, next http.HandlerFunc) http.Handler {
		return authMiddleware.Wrap(tenantMiddleware.Wrap(
			authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: permission}.Wrap(next),
		))
	}

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours", wrap("staff.read", func(w http.ResponseWriter, r *http.Request) {
		hoursHandler.Get(w, r, r.PathValue("tenantID"), r.PathValue("staffID"))
	}))
	mux.Handle("PUT /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours", wrap("staff.update", func(w http.ResponseWriter, r *http.Request) {
		hoursHandler.Replace(w, r, r.PathValue("tenantID"), r.PathValue("staffID"))
	}))
	return mux, tokens, hoursStore
}

// statefulWorkingHoursRepository is an in-memory WorkingHoursRepository fake,
// the same role statefulCapabilityRepository plays for staff_route_test.go.
type statefulWorkingHoursRepository struct {
	byStaff    map[string][]*schedulingmodel.WorkingHourInterval
	readCalls  int
	writeCalls int
}

func (r *statefulWorkingHoursRepository) ListByStaff(_ context.Context, _ string, staffID string) ([]*schedulingmodel.WorkingHourInterval, error) {
	r.readCalls++
	return append([]*schedulingmodel.WorkingHourInterval{}, r.byStaff[staffID]...), nil
}

func (r *statefulWorkingHoursRepository) DeleteAllForStaff(_ context.Context, _ string, staffID string) error {
	r.writeCalls++
	delete(r.byStaff, staffID)
	return nil
}

func (r *statefulWorkingHoursRepository) Create(_ context.Context, interval *schedulingmodel.WorkingHourInterval) (*schedulingmodel.WorkingHourInterval, error) {
	r.writeCalls++
	r.byStaff[interval.StaffID] = append(r.byStaff[interval.StaffID], interval)
	return interval, nil
}

// compile-time guards: the fakes must keep satisfying the real interfaces.
var (
	_ schedulingrepository.WorkingHoursRepository = (*statefulWorkingHoursRepository)(nil)
	_ schedulingservice.StaffReader               = (*statefulStaffRepository)(nil)
)
