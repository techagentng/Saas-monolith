package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// Scheduling S9 public technician discovery, wired exactly as app.New does:
// a bare mux entry, the REAL PublicStaffService over the REAL
// PublicTenantService, no middleware. Reuses the s9Fixture / s9* ids from
// public_availability_route_test.go.

func buildPublicStaffRoute(f *s9Fixture) (http.Handler, *statefulServiceRepository) {
	tenantRepo := &s9TenantRepo{tenant: f.tenant}
	publicTenant := tenantservice.NewPublicTenantService(tenantRepo)

	serviceStore := &statefulServiceRepository{services: f.services}
	staffStore := &statefulStaffRepository{profiles: f.profiles}
	capStore := &statefulCapabilityRepository{assignments: f.capabilities}

	svc := schedulingservice.NewPublicStaffService(publicTenant, serviceStore, staffStore, capStore)
	handler := schedulinghandler.NewPublicStaffHandler(svc)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/public/tenants/{slug}/services/{serviceID}/staff", func(w http.ResponseWriter, r *http.Request) {
		handler.List(w, r, r.PathValue("slug"), r.PathValue("serviceID"))
	})
	return mux, serviceStore
}

func s9StaffPath(slug, serviceID string) string {
	return "/api/v1/public/tenants/" + slug + "/services/" + serviceID + "/staff"
}

type s9StaffBody struct {
	ServiceID string `json:"service_id"`
	Staff     []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"staff"`
}

// s9MultiStaffFixture: Ada and Cara both perform service A; Benny performs only
// service B (a different, tenant-A service); Dora performs A but is archived;
// Eve performs A but is not bookable.
func s9MultiStaffFixture() *s9Fixture {
	f := s9NailFixture()
	otherService := "550e8400-e29b-41d4-a716-4466554c9010"
	f.services[otherService] = &schedulingmodel.Service{ID: otherService, TenantID: s9TenantAID, Name: "Pedicure", DurationMinutes: 30, PriceMinor: 2000, Status: schedulingmodel.StatusActive}

	cara := "550e8400-e29b-41d4-a716-4466554c9011"
	benny := "550e8400-e29b-41d4-a716-4466554c9012"
	dora := "550e8400-e29b-41d4-a716-4466554c9013"
	eve := "550e8400-e29b-41d4-a716-4466554c9014"

	f.profiles[cara] = &schedulingmodel.StaffProfile{ID: cara, TenantID: s9TenantAID, DisplayName: "Cara", IsBookable: true, Status: schedulingmodel.StatusActive}
	f.profiles[benny] = &schedulingmodel.StaffProfile{ID: benny, TenantID: s9TenantAID, DisplayName: "Benny", IsBookable: true, Status: schedulingmodel.StatusActive}
	f.profiles[dora] = &schedulingmodel.StaffProfile{ID: dora, TenantID: s9TenantAID, DisplayName: "Dora", IsBookable: true, Status: schedulingmodel.StatusArchived}
	f.profiles[eve] = &schedulingmodel.StaffProfile{ID: eve, TenantID: s9TenantAID, DisplayName: "Eve", IsBookable: false, Status: schedulingmodel.StatusActive}

	f.capabilities = map[string][]string{
		s9StaffA:  {s9ServiceA},
		cara:      {s9ServiceA},
		benny:     {otherService},
		dora:      {s9ServiceA},
		eve:       {s9ServiceA},
	}
	// Ada's display name is "Ada"; leave it so sort order is Ada, Cara.
	return f
}

func TestPublicStaffRouteReturnsOnlyCapableActiveBookableStaffWithoutAuth(t *testing.T) {
	handler, _ := buildPublicStaffRoute(s9MultiStaffFixture())
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodGet, s9StaffPath("glamour-nails", s9ServiceA), nil)
	request.Header.Set("Authorization", "Bearer garbage")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 without auth", recorder.Code, recorder.Body.String())
	}
	var body s9StaffBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, recorder.Body.String())
	}
	names := make([]string, len(body.Staff))
	for i, s := range body.Staff {
		names[i] = s.Name
	}
	// Ada + Cara only, sorted by name. Benny (wrong service), Dora (archived),
	// Eve (not bookable) all excluded.
	if strings.Join(names, ",") != "Ada,Cara" {
		t.Fatalf("staff = %v, want [Ada Cara]", names)
	}
	if body.ServiceID != s9ServiceA {
		t.Fatalf("service_id = %q, want %q", body.ServiceID, s9ServiceA)
	}
}

func TestPublicStaffRouteLeaksNoInternalFields(t *testing.T) {
	f := s9NailFixture()
	linked := "550e8400-e29b-41d4-a716-4466554c9099"
	f.profiles[s9StaffA].UserID = &linked
	f.profiles[s9StaffA].Bio = strPtrApp("Owner and lead technician")
	handler, _ := buildPublicStaffRoute(f)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9StaffPath("glamour-nails", s9ServiceA), nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	raw := recorder.Body.String()
	for _, forbidden := range []string{"user_id", linked, "\"status\"", "is_bookable", "\"bio\"", "Owner and lead technician", "created_at", "tenant_id", s9TenantAID, "BUSINESS_OWNER"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("public staff list leaked %q: %s", forbidden, raw)
		}
	}
}

func TestPublicStaffRouteEmptyWhenNoCapableStaff(t *testing.T) {
	f := s9NailFixture()
	f.capabilities = map[string][]string{}
	handler, _ := buildPublicStaffRoute(f)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9StaffPath("glamour-nails", s9ServiceA), nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"staff":[]`) {
		t.Fatalf("body = %s, want an empty staff array", recorder.Body.String())
	}
}

func TestPublicStaffRouteRejectsCrossTenantService(t *testing.T) {
	handler, store := buildPublicStaffRoute(s9NailFixture())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9StaffPath("glamour-nails", s9ServiceB), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "SERVICE_NOT_FOUND")
	_ = store
}

func TestPublicStaffRouteRejectsArchivedService(t *testing.T) {
	f := s9NailFixture()
	f.services[s9ServiceA].Status = schedulingmodel.StatusArchived
	handler, _ := buildPublicStaffRoute(f)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9StaffPath("glamour-nails", s9ServiceA), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "SERVICE_NOT_FOUND")
}

func TestPublicStaffRouteHidesNonPublicTenant(t *testing.T) {
	f := s9NailFixture()
	f.tenant.Status = tenantmodel.StatusDisabled
	handler, store := buildPublicStaffRoute(f)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9StaffPath("glamour-nails", s9ServiceA), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
	if store.readCalls != 0 {
		t.Fatal("a non-public tenant reached the service repository")
	}
}

func TestPublicStaffRouteRefusesNonNailVertical(t *testing.T) {
	for _, bt := range []tenantmodel.BusinessType{
		tenantmodel.BusinessTypeHotel,
		tenantmodel.BusinessTypeRestaurant,
		tenantmodel.BusinessTypeTransport,
	} {
		f := s9NailFixture()
		businessType := bt
		f.tenant.BusinessType = &businessType
		handler, store := buildPublicStaffRoute(f)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9StaffPath("glamour-nails", s9ServiceA), nil))

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", bt, recorder.Code)
		}
		assertBodyCode(t, recorder, "RESOURCE_NOT_FOUND")
		if store.readCalls != 0 {
			t.Fatalf("%s: a non-nail tenant reached the service repository", bt)
		}
	}
}

func TestPublicStaffRouteUnknownSlugIsNotFound(t *testing.T) {
	handler, _ := buildPublicStaffRoute(s9NailFixture())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, s9StaffPath("no-such-salon", s9ServiceA), nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
}
