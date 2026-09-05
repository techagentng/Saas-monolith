package app

import (
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
	identityservice "github.com/techagentng/saas-monolith/internal/identity/service"
	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These exercise the exact production middleware chain app.New wires for
// Scheduling S7:
//
//	GET /api/v1/tenants/{tenantID}/availability  staff.read
//	    ?service_id=...&staff_id=...&date=YYYY-MM-DD
//
// Authentication -> Tenant Context -> Authorization -> Handler, using the REAL
// TenantContextService, the REAL AvailabilityService (so the timezone
// resolution, capability check and slot engine are genuinely exercised) and
// the REAL Authorizer, dispatched through a real http.ServeMux. It reuses S3's
// staffScenario / staff-profile fixtures.

// availMondayDate is a Monday, matching the MONDAY working-hours row the
// fixtures configure.
const availMondayDate = "2026-09-07"

// frozenClock is a deterministic schedulingservice.Clock for the route tests.
type frozenClock struct{ now time.Time }

func (c frozenClock) Now() time.Time { return c.now }

func availabilityPath(tenantID, serviceID, staffID, date string) string {
	return "/api/v1/tenants/" + tenantID + "/availability?service_id=" + serviceID + "&staff_id=" + staffID + "&date=" + date
}

func availabilityScenario(t *testing.T) *staffScenarioState {
	t.Helper()
	scenario := staffScenario()
	scenario.tenant.Timezone = strPtrApp("Africa/Lagos")
	scenario.profiles[staffRouteStaffA] = activeStaff(staffRouteTenantA)
	scenario.services[staffRouteServiceA] = &schedulingmodel.Service{
		ID: staffRouteServiceA, TenantID: staffRouteTenantA, Name: "Gel Manicure",
		DurationMinutes: 30, PriceMinor: 1999, Status: schedulingmodel.StatusActive,
	}
	return scenario
}

func strPtrApp(v string) *string { return &v }

func TestAvailabilityRouteRequiresAuthentication(t *testing.T) {
	handler, _, _ := buildAvailabilityRoutes(t, availabilityScenario(t), ownerStaffPermissions, nil, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, staffRouteStaffA, availMondayDate), nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s, want 401", recorder.Code, recorder.Body.String())
	}
}

func TestAvailabilityRouteReturnsSlotsForABusinessOwner(t *testing.T) {
	scenario := availabilityScenario(t)
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions,
		[]string{staffRouteServiceA},
		mondayHours("09:00", "12:00"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, staffRouteStaffA, availMondayDate), ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Date     string `json:"date"`
		Timezone string `json:"timezone"`
		Slots    []struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Date != availMondayDate || body.Timezone != "Africa/Lagos" {
		t.Fatalf("context = %+v", body)
	}
	got := make([]string, len(body.Slots))
	for i, s := range body.Slots {
		got[i] = s.Start
	}
	want := []string{"09:00", "09:30", "10:00", "10:30", "11:00", "11:30"}
	if len(got) != len(want) {
		t.Fatalf("slot starts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slot starts = %v, want %v", got, want)
		}
	}
}

// The STAFF role holds staff.read, so it may query availability.
func TestAvailabilityRouteAllowsTheStaffRole(t *testing.T) {
	scenario := availabilityScenario(t)
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, staffStaffPermissions,
		[]string{staffRouteServiceA}, mondayHours("09:00", "12:00"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, staffRouteStaffA, availMondayDate), ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 for STAFF", recorder.Code, recorder.Body.String())
	}
}

func TestAvailabilityRouteRejectsAMalformedDate(t *testing.T) {
	scenario := availabilityScenario(t)
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions,
		[]string{staffRouteServiceA}, mondayHours("09:00", "12:00"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, staffRouteStaffA, "07-09-2026"), ""))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
}

func TestAvailabilityRouteRejectsMissingQueryValues(t *testing.T) {
	scenario := availabilityScenario(t)
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions,
		[]string{staffRouteServiceA}, mondayHours("09:00", "12:00"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		"/api/v1/tenants/"+staffRouteTenantA+"/availability?service_id="+staffRouteServiceA, ""))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
}

func TestAvailabilityRouteReportsAnUnknownServiceAsNotFound(t *testing.T) {
	scenario := availabilityScenario(t)
	delete(scenario.services, staffRouteServiceA)
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions, nil, mondayHours("09:00", "12:00"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, staffRouteStaffA, availMondayDate), ""))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "SERVICE_NOT_FOUND")
}

func TestAvailabilityRouteReportsAnUnknownStaffMemberAsNotFound(t *testing.T) {
	scenario := availabilityScenario(t)
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions,
		[]string{staffRouteServiceA}, mondayHours("09:00", "12:00"))
	unknownStaff := "550e8400-e29b-41d4-a716-446655459999"
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, unknownStaff, availMondayDate), ""))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "STAFF_NOT_FOUND")
}

func TestAvailabilityRouteRejectsATechnicianNotAssignedTheService(t *testing.T) {
	scenario := availabilityScenario(t)
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions,
		nil, // no capability assignment
		mondayHours("09:00", "12:00"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, staffRouteStaffA, availMondayDate), ""))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
}

func TestAvailabilityRouteReturnsAnEmptySlotArrayWhenNoHoursConfigured(t *testing.T) {
	scenario := availabilityScenario(t)
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions,
		[]string{staffRouteServiceA}, nil) // no working hours
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, staffRouteStaffA, availMondayDate), ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"slots":[]`) {
		t.Fatalf("body = %s, want an empty slots array", body)
	}
}

// --- tenant isolation ---------------------------------------------------

func TestAvailabilityRouteDeniesCrossTenantAccess(t *testing.T) {
	scenario := availabilityScenario(t)
	scenario.otherTenant = &tenantmodel.Tenant{ID: staffRouteTenantB, Name: "Rival Nails", Slug: "rival-nails", Status: tenantmodel.StatusActive, Timezone: strPtrApp("Africa/Lagos")}
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions,
		[]string{staffRouteServiceA}, mondayHours("09:00", "12:00"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantB, staffRouteServiceA, staffRouteStaffA, availMondayDate), ""))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
}

// A service that genuinely exists under another tenant must be
// indistinguishable from one that does not exist.
func TestAvailabilityRouteDoesNotDiscloseAnotherTenantsService(t *testing.T) {
	scenario := availabilityScenario(t)
	scenario.services[staffRouteServiceA].TenantID = staffRouteTenantB
	handler, tokens, _ := buildAvailabilityRoutes(t, scenario, ownerStaffPermissions,
		[]string{staffRouteServiceA}, mondayHours("09:00", "12:00"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, staffRequest(t, tokens, http.MethodGet,
		availabilityPath(staffRouteTenantA, staffRouteServiceA, staffRouteStaffA, availMondayDate), ""))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "SERVICE_NOT_FOUND")
}

// --- test wiring ---------------------------------------------------------

func mondayHours(start, end string) []*schedulingmodel.WorkingHourInterval {
	return []*schedulingmodel.WorkingHourInterval{
		{TenantID: staffRouteTenantA, StaffID: staffRouteStaffA, DayOfWeek: schedulingmodel.Monday, StartTime: start, EndTime: end},
	}
}

// buildAvailabilityRoutes mirrors buildWorkingHoursRoutes: a real
// AvailabilityService over the S3/S5 in-memory fakes, behind the real
// production middleware chain. capabilityServiceIDs are assigned to
// staffRouteStaffA; workingHours are seeded for that staff member. The clock
// is frozen in the year 2000 so no slot is ever "in the past".
func buildAvailabilityRoutes(
	t *testing.T,
	scenario *staffScenarioState,
	tenantPermissions []string,
	capabilityServiceIDs []string,
	workingHours []*schedulingmodel.WorkingHourInterval,
) (http.Handler, *identityservice.TokenManager, *statefulWorkingHoursRepository) {
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
	serviceStore := &statefulServiceRepository{services: scenario.services}
	capabilityStore := &statefulCapabilityRepository{assignments: map[string][]string{}}
	if capabilityServiceIDs != nil {
		capabilityStore.assignments[staffRouteStaffA] = capabilityServiceIDs
	}
	hoursStore := &statefulWorkingHoursRepository{byStaff: map[string][]*schedulingmodel.WorkingHourInterval{}}
	if workingHours != nil {
		hoursStore.byStaff[staffRouteStaffA] = workingHours
	}

	availabilitySvc := schedulingservice.NewAvailabilityService(
		tenants, serviceStore, staffStore, capabilityStore, hoursStore,
		schedulingservice.NoOccupancy{},
		frozenClock{now: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
	)
	availabilityHandler := schedulinghandler.NewAvailabilityHandler(availabilitySvc)

	wrap := func(permission string, next http.HandlerFunc) http.Handler {
		return authMiddleware.Wrap(tenantMiddleware.Wrap(
			authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: permission}.Wrap(next),
		))
	}

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/tenants/{tenantID}/availability", wrap("staff.read", func(w http.ResponseWriter, r *http.Request) {
		availabilityHandler.Get(w, r, r.PathValue("tenantID"))
	}))
	return mux, tokens, hoursStore
}
