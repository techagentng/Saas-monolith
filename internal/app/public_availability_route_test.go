package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These exercise the Scheduling S9 public availability route exactly as app.New
// registers it — a bare mux entry, the REAL PublicAvailabilityService over the
// REAL S7 AvailabilityService and the REAL PublicTenantService, with NO
// authentication / tenant-context / permission middleware. That absence is
// part of the contract and is asserted below. The S7 slot engine is genuinely
// run (not stubbed), so the capability check, timezone resolution and
// deterministic slot maths are all covered.

const (
	s9TenantAID = "550e8400-e29b-41d4-a716-4466554c9001"
	s9TenantBID = "550e8400-e29b-41d4-a716-4466554c9002"
	s9ServiceA  = "550e8400-e29b-41d4-a716-4466554c9003"
	s9ServiceB  = "550e8400-e29b-41d4-a716-4466554c9004"
	s9StaffA    = "550e8400-e29b-41d4-a716-4466554c9005"
	s9StaffB    = "550e8400-e29b-41d4-a716-4466554c9006"
	// A Monday, matching the MONDAY working-hours the fixtures configure.
	s9MondayDate = "2026-09-07"
)

type s9Fixture struct {
	tenant       *tenantmodel.Tenant
	services     map[string]*schedulingmodel.Service
	profiles     map[string]*schedulingmodel.StaffProfile
	capabilities map[string][]string
	hours        map[string][]*schedulingmodel.WorkingHourInterval
}

func s9NailFixture() *s9Fixture {
	return &s9Fixture{
		tenant: nailTenant(s9TenantAID, "glamour-nails"),
		services: map[string]*schedulingmodel.Service{
			s9ServiceA: {ID: s9ServiceA, TenantID: s9TenantAID, Name: "Gel Manicure", DurationMinutes: 30, PriceMinor: 1999, Status: schedulingmodel.StatusActive},
			// s9ServiceB belongs to tenant B — used for the cross-tenant case.
			s9ServiceB: {ID: s9ServiceB, TenantID: s9TenantBID, Name: "Tenant B Service", DurationMinutes: 30, PriceMinor: 1000, Status: schedulingmodel.StatusActive},
		},
		profiles: map[string]*schedulingmodel.StaffProfile{
			s9StaffA: {ID: s9StaffA, TenantID: s9TenantAID, DisplayName: "Ada", IsBookable: true, Status: schedulingmodel.StatusActive},
			// s9StaffB belongs to tenant B — used for the cross-tenant case.
			s9StaffB: {ID: s9StaffB, TenantID: s9TenantBID, DisplayName: "Bola", IsBookable: true, Status: schedulingmodel.StatusActive},
		},
		capabilities: map[string][]string{s9StaffA: {s9ServiceA}},
		hours: map[string][]*schedulingmodel.WorkingHourInterval{
			s9StaffA: {{TenantID: s9TenantAID, StaffID: s9StaffA, DayOfWeek: schedulingmodel.Monday, StartTime: "09:00", EndTime: "12:00"}},
		},
	}
}

func buildPublicAvailabilityRoute(f *s9Fixture) (http.Handler, *s9TenantRepo) {
	tenantRepo := &s9TenantRepo{tenant: f.tenant}
	publicTenant := tenantservice.NewPublicTenantService(tenantRepo)

	serviceStore := &statefulServiceRepository{services: f.services}
	staffStore := &statefulStaffRepository{profiles: f.profiles}
	capStore := &statefulCapabilityRepository{assignments: f.capabilities}
	hoursStore := &statefulWorkingHoursRepository{byStaff: f.hours}

	engine := schedulingservice.NewAvailabilityService(
		tenantRepo, serviceStore, staffStore, capStore, hoursStore,
		schedulingservice.NoOccupancy{},
		frozenClock{now: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
	)
	publicAvail := schedulingservice.NewPublicAvailabilityService(publicTenant, engine)
	handler := schedulinghandler.NewPublicAvailabilityHandler(publicAvail)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/public/tenants/{slug}/availability", func(w http.ResponseWriter, r *http.Request) {
		handler.Get(w, r, r.PathValue("slug"))
	})
	return mux, tenantRepo
}

func s9AvailPath(slug, serviceID, staffID, date string) string {
	return "/api/v1/public/tenants/" + slug + "/availability?service_id=" + serviceID + "&staff_id=" + staffID + "&date=" + date
}

type s9AvailBody struct {
	Date      string `json:"date"`
	Timezone  string `json:"timezone"`
	ServiceID string `json:"service_id"`
	StaffID   string `json:"staff_id"`
	Slots     []struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"slots"`
}

func TestPublicAvailabilityRouteReturnsSlotsWithoutAuthentication(t *testing.T) {
	handler, _ := buildPublicAvailabilityRoute(s9NailFixture())
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceA, s9StaffA, s9MondayDate), nil)
	// A garbage bearer must not change anything on a public route.
	request.Header.Set("Authorization", "Bearer not-a-real-token")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var body s9AvailBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, recorder.Body.String())
	}
	if body.Date != s9MondayDate || body.Timezone != "Africa/Lagos" {
		t.Fatalf("context = %+v, want date/timezone echoed from the tenant", body)
	}
	got := make([]string, len(body.Slots))
	for i, s := range body.Slots {
		got[i] = s.Start
	}
	want := []string{"09:00", "09:30", "10:00", "10:30", "11:00", "11:30"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("slot starts = %v, want %v", got, want)
	}
	// No internal scheduling data leaked.
	for _, forbidden := range []string{"occupied", "instant", "offset", s9TenantAID} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("public availability leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestPublicAvailabilityRouteEmptySlotsIsA200(t *testing.T) {
	f := s9NailFixture()
	// No working hours on the requested day.
	f.hours = map[string][]*schedulingmodel.WorkingHourInterval{}
	handler, _ := buildPublicAvailabilityRoute(f)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceA, s9StaffA, s9MondayDate), nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a day with no availability", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"slots":[]`) {
		t.Fatalf("body = %s, want an empty slots array", recorder.Body.String())
	}
}

func TestPublicAvailabilityRouteRejectsMalformedDate(t *testing.T) {
	handler, _ := buildPublicAvailabilityRoute(s9NailFixture())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceA, s9StaffA, "2026-9-7"), nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
}

func TestPublicAvailabilityRouteRequiresAllParameters(t *testing.T) {
	handler, _ := buildPublicAvailabilityRoute(s9NailFixture())
	for _, path := range []string{
		"/api/v1/public/tenants/glamour-nails/availability?staff_id=" + s9StaffA + "&date=" + s9MondayDate,
		"/api/v1/public/tenants/glamour-nails/availability?service_id=" + s9ServiceA + "&date=" + s9MondayDate,
		"/api/v1/public/tenants/glamour-nails/availability?service_id=" + s9ServiceA + "&staff_id=" + s9StaffA,
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("path %s: status = %d, want 400", path, recorder.Code)
		}
		assertBodyCode(t, recorder, "VALIDATION_FAILED")
	}
}

func TestPublicAvailabilityRouteRejectsCrossTenantService(t *testing.T) {
	handler, _ := buildPublicAvailabilityRoute(s9NailFixture())
	recorder := httptest.NewRecorder()

	// Tenant A's slug, tenant B's service id.
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceB, s9StaffA, s9MondayDate), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "SERVICE_NOT_FOUND")
}

func TestPublicAvailabilityRouteRejectsCrossTenantStaff(t *testing.T) {
	handler, _ := buildPublicAvailabilityRoute(s9NailFixture())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceA, s9StaffB, s9MondayDate), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "STAFF_NOT_FOUND")
}

func TestPublicAvailabilityRouteRejectsStaffNotCapable(t *testing.T) {
	f := s9NailFixture()
	// Ada exists and is bookable, but is assigned nothing.
	f.capabilities = map[string][]string{}
	handler, _ := buildPublicAvailabilityRoute(f)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceA, s9StaffA, s9MondayDate), nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
}

func TestPublicAvailabilityRouteRejectsArchivedService(t *testing.T) {
	f := s9NailFixture()
	f.services[s9ServiceA].Status = schedulingmodel.StatusArchived
	handler, _ := buildPublicAvailabilityRoute(f)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceA, s9StaffA, s9MondayDate), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "SERVICE_NOT_FOUND")
}

func TestPublicAvailabilityRouteHidesNonPublicTenant(t *testing.T) {
	f := s9NailFixture()
	f.tenant.OnboardingStatus = tenantmodel.OnboardingStatusInProgress
	handler, repo := buildPublicAvailabilityRoute(f)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceA, s9StaffA, s9MondayDate), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
	if repo.findByIDCalls != 0 {
		t.Fatal("a non-public tenant reached the availability engine")
	}
}

func TestPublicAvailabilityRouteRefusesNonNailVertical(t *testing.T) {
	for _, bt := range []tenantmodel.BusinessType{
		tenantmodel.BusinessTypeHotel,
		tenantmodel.BusinessTypeRestaurant,
		tenantmodel.BusinessTypeTransport,
	} {
		f := s9NailFixture()
		businessType := bt
		f.tenant.BusinessType = &businessType
		handler, repo := buildPublicAvailabilityRoute(f)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("glamour-nails", s9ServiceA, s9StaffA, s9MondayDate), nil))

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", bt, recorder.Code)
		}
		assertBodyCode(t, recorder, "RESOURCE_NOT_FOUND")
		if repo.findByIDCalls != 0 {
			t.Fatalf("%s: a non-nail tenant reached the availability engine", bt)
		}
	}
}

func TestPublicAvailabilityRouteUnknownSlugIsNotFound(t *testing.T) {
	handler, _ := buildPublicAvailabilityRoute(s9NailFixture())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("no-such-salon", s9ServiceA, s9StaffA, s9MondayDate), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
}

func TestPublicAvailabilityRouteTreatsReservedSlugAsNotFound(t *testing.T) {
	handler, _ := buildPublicAvailabilityRoute(s9NailFixture())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9AvailPath("admin", s9ServiceA, s9StaffA, s9MondayDate), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
}
