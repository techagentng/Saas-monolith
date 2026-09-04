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

// These exercise the Scheduling S8 public catalog route exactly as app.New
// registers it — a bare mux entry, the REAL PublicCatalogService over the REAL
// PublicTenantService, and NO authentication / tenant-context / permission
// middleware. That absence is part of the contract and is asserted below.
// It reuses publicSlugRepository (public_tenant_route_test.go) for the tenant
// side and statefulServiceRepository (service_catalog_route_test.go) for the
// catalog.

const (
	catalogRouteTenantAID = "550e8400-e29b-41d4-a716-4466554b1001"
	catalogRouteTenantBID = "550e8400-e29b-41d4-a716-4466554b1002"
)

func nailTenant(id, slug string) *tenantmodel.Tenant {
	description := "Bright, clean, friendly nail studio."
	timezone := "Africa/Lagos"
	currency := "NGN"
	businessType := tenantmodel.BusinessTypeNailTechnician
	return &tenantmodel.Tenant{
		ID: id, Name: "Glamour Nails", Slug: slug,
		Status: tenantmodel.StatusActive, OnboardingStatus: tenantmodel.OnboardingStatusCompleted,
		Description: &description, Timezone: &timezone, Currency: &currency,
		BusinessType: &businessType,
	}
}

func buildPublicCatalogRoute(tenant *tenantmodel.Tenant, services map[string]*schedulingmodel.Service) (http.Handler, *statefulServiceRepository) {
	tenantRepo := &publicSlugRepository{tenant: tenant}
	serviceRepo := &statefulServiceRepository{services: services}
	catalog := schedulingservice.NewPublicCatalogService(tenantservice.NewPublicTenantService(tenantRepo), serviceRepo, nil)
	handler := schedulinghandler.NewPublicServiceHandler(catalog)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/public/tenants/{slug}/services", func(w http.ResponseWriter, r *http.Request) {
		handler.List(w, r, r.PathValue("slug"))
	})
	return mux, serviceRepo
}

type catalogRouteBody struct {
	Currency *string `json:"currency"`
	Services []struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Description     *string `json:"description"`
		DurationMinutes int     `json:"duration_minutes"`
		PriceMinor      int64   `json:"price_minor"`
	} `json:"services"`
}

func decodeCatalog(t *testing.T, recorder *httptest.ResponseRecorder) catalogRouteBody {
	t.Helper()
	var body catalogRouteBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v (body = %s)", err, recorder.Body.String())
	}
	return body
}

func TestPublicCatalogRouteReturnsActiveServicesWithoutAuthentication(t *testing.T) {
	desc := "Long-lasting gel finish."
	services := map[string]*schedulingmodel.Service{
		"s1":       {ID: "s1", TenantID: catalogRouteTenantAID, Name: "Gel Manicure", Description: &desc, DurationMinutes: 45, PriceMinor: 1999, Status: schedulingmodel.StatusActive},
		"s2":       {ID: "s2", TenantID: catalogRouteTenantAID, Name: "Classic Pedicure", DurationMinutes: 60, PriceMinor: 2999, Status: schedulingmodel.StatusActive},
		"archived": {ID: "archived", TenantID: catalogRouteTenantAID, Name: "Retired Special", DurationMinutes: 30, PriceMinor: 500, Status: schedulingmodel.StatusArchived},
	}
	handler, _ := buildPublicCatalogRoute(nailTenant(catalogRouteTenantAID, "glamour-nails"), services)
	recorder := httptest.NewRecorder()

	// Deliberately no Authorization header, and a garbage one below proves the
	// route is not behind auth middleware.
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/glamour-nails/services", nil)
	request.Header.Set("Authorization", "Bearer not-a-real-token")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 without authentication", recorder.Code, recorder.Body.String())
	}
	body := decodeCatalog(t, recorder)
	if body.Currency == nil || *body.Currency != "NGN" {
		t.Fatalf("currency = %v, want NGN", body.Currency)
	}
	if len(body.Services) != 2 {
		t.Fatalf("services = %d, want 2 (archived excluded)", len(body.Services))
	}
	for _, s := range body.Services {
		if s.Name == "Retired Special" {
			t.Fatal("an archived service reached the public catalog")
		}
	}
	// No owner/admin internals in the raw body.
	for _, forbidden := range []string{"tenant_id", "\"status\"", "created_at", "updated_at", catalogRouteTenantAID} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("public catalog leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestPublicCatalogRouteEmptyCatalogIsAnEmptyArray(t *testing.T) {
	handler, _ := buildPublicCatalogRoute(nailTenant(catalogRouteTenantAID, "glamour-nails"), map[string]*schedulingmodel.Service{})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/glamour-nails/services", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"services":[]`) {
		t.Fatalf("body = %s, want an empty services array", recorder.Body.String())
	}
}

func TestPublicCatalogRouteUnknownSlugIsNotFound(t *testing.T) {
	handler, repo := buildPublicCatalogRoute(nailTenant(catalogRouteTenantAID, "glamour-nails"), map[string]*schedulingmodel.Service{})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/no-such-salon/services", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
	if repo.readCalls != 0 {
		t.Fatal("an unknown slug reached the service repository")
	}
}

// An IN_PROGRESS tenant is not publicly bookable and must not leak its
// services — identical 404 to a nonexistent slug.
func TestPublicCatalogRouteHidesANonPublicTenant(t *testing.T) {
	tenant := nailTenant(catalogRouteTenantAID, "glamour-nails")
	tenant.OnboardingStatus = tenantmodel.OnboardingStatusInProgress
	services := map[string]*schedulingmodel.Service{
		"s1": {ID: "s1", TenantID: catalogRouteTenantAID, Name: "Secret Manicure", DurationMinutes: 30, PriceMinor: 1000, Status: schedulingmodel.StatusActive},
	}
	handler, repo := buildPublicCatalogRoute(tenant, services)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/glamour-nails/services", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
	if strings.Contains(recorder.Body.String(), "Secret Manicure") {
		t.Fatal("a non-public tenant leaked its catalog")
	}
	if repo.readCalls != 0 {
		t.Fatal("a non-public tenant reached the service repository")
	}
}

func TestPublicCatalogRouteRefusesANonNailVertical(t *testing.T) {
	for _, bt := range []tenantmodel.BusinessType{
		tenantmodel.BusinessTypeHotel,
		tenantmodel.BusinessTypeRestaurant,
		tenantmodel.BusinessTypeTransport,
	} {
		tenant := nailTenant(catalogRouteTenantAID, "grand-hotel")
		businessType := bt
		tenant.BusinessType = &businessType
		services := map[string]*schedulingmodel.Service{
			"s1": {ID: "s1", TenantID: catalogRouteTenantAID, Name: "Not A Nail Service", DurationMinutes: 30, PriceMinor: 1000, Status: schedulingmodel.StatusActive},
		}
		handler, repo := buildPublicCatalogRoute(tenant, services)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/grand-hotel/services", nil))

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, body = %s, want 404", bt, recorder.Code, recorder.Body.String())
		}
		assertBodyCode(t, recorder, "RESOURCE_NOT_FOUND")
		if repo.readCalls != 0 {
			t.Fatalf("%s: a non-nail tenant reached the service repository", bt)
		}
	}
}

// slug A returns only tenant A's services; slug B only tenant B's; a tenant A
// service can never surface through tenant B's URL.
func TestPublicCatalogRouteIsolatesTenants(t *testing.T) {
	shared := map[string]*schedulingmodel.Service{
		"a1": {ID: "a1", TenantID: catalogRouteTenantAID, Name: "Tenant A Manicure", DurationMinutes: 30, PriceMinor: 1000, Status: schedulingmodel.StatusActive},
		"b1": {ID: "b1", TenantID: catalogRouteTenantBID, Name: "Tenant B Manicure", DurationMinutes: 30, PriceMinor: 2000, Status: schedulingmodel.StatusActive},
	}

	handlerA, _ := buildPublicCatalogRoute(nailTenant(catalogRouteTenantAID, "salon-a"), shared)
	recA := httptest.NewRecorder()
	handlerA.ServeHTTP(recA, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/salon-a/services", nil))
	bodyA := decodeCatalog(t, recA)
	if len(bodyA.Services) != 1 || bodyA.Services[0].Name != "Tenant A Manicure" {
		t.Fatalf("slug A returned %#v, want only tenant A's service", bodyA.Services)
	}

	handlerB, _ := buildPublicCatalogRoute(nailTenant(catalogRouteTenantBID, "salon-b"), shared)
	recB := httptest.NewRecorder()
	handlerB.ServeHTTP(recB, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/salon-b/services", nil))
	bodyB := decodeCatalog(t, recB)
	if len(bodyB.Services) != 1 || bodyB.Services[0].Name != "Tenant B Manicure" {
		t.Fatalf("slug B returned %#v, want only tenant B's service", bodyB.Services)
	}
	if strings.Contains(recB.Body.String(), "Tenant A Manicure") {
		t.Fatal("tenant A's service surfaced through tenant B's URL")
	}
}

func TestPublicCatalogRouteRejectsNonCanonicalSlug(t *testing.T) {
	handler, repo := buildPublicCatalogRoute(nailTenant(catalogRouteTenantAID, "glamour-nails"), map[string]*schedulingmodel.Service{})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/Glamour_Nails/services", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_SLUG_INVALID")
	if repo.readCalls != 0 {
		t.Fatal("a non-canonical slug reached the service repository")
	}
}

// A reserved platform slug reads as absent, never as a distinct error.
func TestPublicCatalogRouteTreatsReservedSlugAsNotFound(t *testing.T) {
	handler, _ := buildPublicCatalogRoute(nailTenant(catalogRouteTenantAID, "glamour-nails"), map[string]*schedulingmodel.Service{})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/admin/services", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
}
