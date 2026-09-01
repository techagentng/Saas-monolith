package app

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
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
	tenanthandler "github.com/techagentng/saas-monolith/internal/tenant/handler"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires for
// Scheduling S1:
//
//	POST   /api/v1/tenants/{tenantID}/services                     service.create
//	GET    /api/v1/tenants/{tenantID}/services                     service.read
//	GET    /api/v1/tenants/{tenantID}/services/{serviceID}         service.read
//	PATCH  /api/v1/tenants/{tenantID}/services/{serviceID}         service.update
//	POST   /api/v1/tenants/{tenantID}/services/{serviceID}/archive service.archive
//	PUT    /api/v1/tenants/{tenantID}/currency                     tenant.update
//
// all: Authentication -> Tenant Context -> Authorization -> Handler
//
// using the REAL TenantContextService, the REAL CatalogService and
// CurrencyService (so the currency prerequisite and the write-once rule are
// genuinely exercised rather than mocked away), and the REAL Authorizer,
// dispatched through a real http.ServeMux so the {tenantID}/{serviceID}
// patterns are genuinely captured. Backing repositories are stateful fakes so
// denial tests can additionally assert nothing was mutated.

const (
	catalogRouteUserID    = "550e8400-e29b-41d4-a716-446655445001"
	catalogRouteSessionID = "550e8400-e29b-41d4-a716-446655445002"
	catalogRouteTenantA   = "550e8400-e29b-41d4-a716-446655445003"
	catalogRouteTenantB   = "550e8400-e29b-41d4-a716-446655445004"
	catalogRouteServiceA  = "550e8400-e29b-41d4-a716-446655445005"
)

// Permission sets matching the role grants migration 000010 installs.
var (
	businessOwnerPermissions = []string{"tenant.read", "tenant.update", "service.read", "service.create", "service.update", "service.archive"}
	staffPermissions         = []string{"tenant.read", "service.read"}
)

// --- authentication ----------------------------------------------------------

func TestServiceRoutesRequireAuthentication(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, "/api/v1/tenants/" + catalogRouteTenantA + "/services", `{"name":"Gel Manicure","duration_minutes":45,"price_minor":1999}`},
		{"list", http.MethodGet, "/api/v1/tenants/" + catalogRouteTenantA + "/services", ""},
		{"get", http.MethodGet, "/api/v1/tenants/" + catalogRouteTenantA + "/services/" + catalogRouteServiceA, ""},
		{"update", http.MethodPatch, "/api/v1/tenants/" + catalogRouteTenantA + "/services/" + catalogRouteServiceA, `{"name":"x"}`},
		{"archive", http.MethodPost, "/api/v1/tenants/" + catalogRouteTenantA + "/services/" + catalogRouteServiceA + "/archive", ""},
		{"currency", http.MethodPut, "/api/v1/tenants/" + catalogRouteTenantA + "/currency", `{"currency":"NGN"}`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			handler, _, services, tenants := buildCatalogRoutes(t, catalogScenarioWithCurrency(), businessOwnerPermissions)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s, want 401", recorder.Code, recorder.Body.String())
			}
			if services.writeCalls != 0 {
				t.Fatal("an unauthenticated request reached a catalog write")
			}
			if tenants.currencyWrites != 0 {
				t.Fatal("an unauthenticated request reached a currency write")
			}
		})
	}
}

// --- authorization: BUSINESS_OWNER -------------------------------------------

func TestBusinessOwnerCanCreateAService(t *testing.T) {
	handler, tokens, services, _ := buildCatalogRoutes(t, catalogScenarioWithCurrency(), businessOwnerPermissions)
	recorder := httptest.NewRecorder()

	body := `{"name":"Gel Manicure","description":"Long-lasting gel finish.","duration_minutes":45,"price_minor":1999}`
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPost, "/api/v1/tenants/"+catalogRouteTenantA+"/services", body))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if services.writeCalls != 1 {
		t.Fatalf("catalog writes = %d, want 1", services.writeCalls)
	}

	var created map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if created["status"] != string(schedulingmodel.StatusActive) {
		t.Fatalf("status = %v, want ACTIVE", created["status"])
	}
}

func TestBusinessOwnerCanReadListUpdateAndArchive(t *testing.T) {
	scenario := catalogScenarioWithCurrency()
	scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantA)
	handler, tokens, _, _ := buildCatalogRoutes(t, scenario, businessOwnerPermissions)

	base := "/api/v1/tenants/" + catalogRouteTenantA + "/services"
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"list", http.MethodGet, base, ""},
		{"get", http.MethodGet, base + "/" + catalogRouteServiceA, ""},
		{"update", http.MethodPatch, base + "/" + catalogRouteServiceA, `{"name":"Gel Manicure Deluxe"}`},
		{"archive", http.MethodPost, base + "/" + catalogRouteServiceA + "/archive", ""},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, catalogRequest(t, tokens, test.method, test.path, test.body))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// --- authorization: STAFF ----------------------------------------------------

func TestStaffCanReadTheCatalog(t *testing.T) {
	scenario := catalogScenarioWithCurrency()
	scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantA)
	handler, tokens, _, _ := buildCatalogRoutes(t, scenario, staffPermissions)

	base := "/api/v1/tenants/" + catalogRouteTenantA + "/services"
	for _, path := range []string{base, base + "/" + catalogRouteServiceA} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, path, ""))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, body = %s, want 200 for STAFF", path, recorder.Code, recorder.Body.String())
		}
	}
}

// STAFF holds service.read and nothing else. Pricing and catalog structure are
// owner decisions, so every write must be refused — and must not reach the
// repository at all.
func TestStaffCannotWriteToTheCatalog(t *testing.T) {
	base := "/api/v1/tenants/" + catalogRouteTenantA + "/services"
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, base, `{"name":"Gel Manicure","duration_minutes":45,"price_minor":1999}`},
		{"update", http.MethodPatch, base + "/" + catalogRouteServiceA, `{"price_minor":1}`},
		{"archive", http.MethodPost, base + "/" + catalogRouteServiceA + "/archive", ""},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			scenario := catalogScenarioWithCurrency()
			scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantA)
			handler, tokens, services, _ := buildCatalogRoutes(t, scenario, staffPermissions)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, catalogRequest(t, tokens, test.method, test.path, test.body))

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body = %s, want 403 for STAFF", recorder.Code, recorder.Body.String())
			}
			assertBodyCode(t, recorder, "PERMISSION_DENIED")
			if services.writeCalls != 0 {
				t.Fatal("a denied request still reached the repository")
			}
			if stored := scenario.services[catalogRouteServiceA]; stored.Status != schedulingmodel.StatusActive || stored.Name != "Gel Manicure" {
				t.Fatalf("a denied request mutated the service: %+v", stored)
			}
		})
	}
}

// A member with no service permissions at all cannot even read.
func TestMemberWithoutServicePermissionsCannotRead(t *testing.T) {
	handler, tokens, _, _ := buildCatalogRoutes(t, catalogScenarioWithCurrency(), []string{"tenant.read"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/services", ""))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
}

// --- tenant isolation --------------------------------------------------------

// The caller is authenticated and fully permissioned in their own tenant, but
// has no membership in the target one. Tenant context resolution denies before
// authorization is ever consulted.
func TestCatalogRoutesDenyCrossTenantAccess(t *testing.T) {
	base := "/api/v1/tenants/" + catalogRouteTenantB + "/services"
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, base, `{"name":"Gel Manicure","duration_minutes":45,"price_minor":1999}`},
		{"list", http.MethodGet, base, ""},
		{"get", http.MethodGet, base + "/" + catalogRouteServiceA, ""},
		{"update", http.MethodPatch, base + "/" + catalogRouteServiceA, `{"name":"Hijacked"}`},
		{"archive", http.MethodPost, base + "/" + catalogRouteServiceA + "/archive", ""},
		{"currency", http.MethodPut, "/api/v1/tenants/" + catalogRouteTenantB + "/currency", `{"currency":"USD"}`},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			scenario := catalogScenarioWithCurrency()
			// Tenant B exists and owns a service, but the caller is a member of
			// tenant A only.
			scenario.otherTenant = &tenantmodel.Tenant{ID: catalogRouteTenantB, Name: "Rival Nails", Slug: "rival-nails", Status: tenantmodel.StatusActive}
			scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantB)
			handler, tokens, services, tenants := buildCatalogRoutes(t, scenario, businessOwnerPermissions)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, catalogRequest(t, tokens, test.method, test.path, test.body))

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, body = %s, want 403", recorder.Code, recorder.Body.String())
			}
			assertBodyCode(t, recorder, "TENANT_ACCESS_DENIED")
			if services.writeCalls != 0 || services.readCalls != 0 {
				t.Fatal("a cross-tenant request reached the catalog repository")
			}
			if tenants.currencyWrites != 0 {
				t.Fatal("a cross-tenant request reached a currency write")
			}
			if stored := scenario.services[catalogRouteServiceA]; stored.Status != schedulingmodel.StatusActive || stored.Name != "Gel Manicure" {
				t.Fatalf("a cross-tenant request mutated another tenant's service: %+v", stored)
			}
		})
	}
}

// A service ID that genuinely exists under another tenant must be
// indistinguishable from one that does not exist, so the API cannot be used to
// discover another tenant's catalog rows.
func TestGetDoesNotDiscloseAnotherTenantsServiceID(t *testing.T) {
	scenario := catalogScenarioWithCurrency()
	scenario.services[catalogRouteServiceA] = activeService(catalogRouteTenantB)
	handler, tokens, _, _ := buildCatalogRoutes(t, scenario, businessOwnerPermissions)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/services/"+catalogRouteServiceA, ""))
	existingElsewhere := recorder.Code
	existingBody := recorder.Body.String()

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/services/550e8400-e29b-41d4-a716-446655449999", ""))

	if existingElsewhere != http.StatusNotFound || recorder.Code != http.StatusNotFound {
		t.Fatalf("statuses = %d (exists elsewhere) / %d (nonexistent), want both 404", existingElsewhere, recorder.Code)
	}
	if existingBody != recorder.Body.String() {
		t.Fatalf("responses differ, disclosing that the ID exists elsewhere:\n  %s\n  %s", existingBody, recorder.Body.String())
	}
}

// --- currency route ----------------------------------------------------------

func TestCurrencyRouteRequiresTenantUpdate(t *testing.T) {
	// STAFF holds tenant.read but not tenant.update.
	handler, tokens, _, tenants := buildCatalogRoutes(t, catalogScenarioWithoutCurrency(), staffPermissions)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPut, "/api/v1/tenants/"+catalogRouteTenantA+"/currency", `{"currency":"NGN"}`))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "PERMISSION_DENIED")
	if tenants.currencyWrites != 0 {
		t.Fatal("a denied request still wrote the currency")
	}
	if tenants.tenant.Currency != nil {
		t.Fatalf("currency was set to %q by a denied request", *tenants.tenant.Currency)
	}
}

func TestCurrencyRouteSetsThenRefusesToChange(t *testing.T) {
	handler, tokens, _, tenants := buildCatalogRoutes(t, catalogScenarioWithoutCurrency(), businessOwnerPermissions)
	path := "/api/v1/tenants/" + catalogRouteTenantA + "/currency"

	// First write: NULL -> NGN.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPut, path, `{"currency":"NGN"}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if tenants.tenant.Currency == nil || *tenants.tenant.Currency != "NGN" {
		t.Fatalf("currency = %v, want NGN", tenants.tenant.Currency)
	}

	// Same value again: idempotent, and no second write.
	writesAfterFirst := tenants.currencyWrites
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPut, path, `{"currency":"NGN"}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 (idempotent)", recorder.Code, recorder.Body.String())
	}
	if tenants.currencyWrites != writesAfterFirst {
		t.Fatal("re-sending the same currency re-persisted it")
	}

	// A different value: refused, and the stored value survives.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPut, path, `{"currency":"USD"}`))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
	if *tenants.tenant.Currency != "NGN" {
		t.Fatalf("currency = %q, want it unchanged at NGN", *tenants.tenant.Currency)
	}
}

func TestCurrencyRouteRejectsUnsupportedCode(t *testing.T) {
	handler, tokens, _, tenants := buildCatalogRoutes(t, catalogScenarioWithoutCurrency(), businessOwnerPermissions)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPut, "/api/v1/tenants/"+catalogRouteTenantA+"/currency", `{"currency":"ngn"}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
	if tenants.tenant.Currency != nil {
		t.Fatalf("a lowercase code was normalized and stored as %q", *tenants.tenant.Currency)
	}
}

// The full prerequisite, end to end through the real chain: a tenant with no
// currency cannot create a priced service until it declares one.
func TestCreateIsBlockedUntilTheTenantDeclaresACurrency(t *testing.T) {
	handler, tokens, services, _ := buildCatalogRoutes(t, catalogScenarioWithoutCurrency(), businessOwnerPermissions)
	servicesPath := "/api/v1/tenants/" + catalogRouteTenantA + "/services"
	body := `{"name":"Gel Manicure","duration_minutes":45,"price_minor":1999}`

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPost, servicesPath, body))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 before a currency exists", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "VALIDATION_FAILED")
	if services.writeCalls != 0 {
		t.Fatal("a service was written with no currency to denominate its price")
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPut, "/api/v1/tenants/"+catalogRouteTenantA+"/currency", `{"currency":"NGN"}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("setting currency: status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPost, servicesPath, body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201 once a currency exists", recorder.Code, recorder.Body.String())
	}
}

// --- test wiring -------------------------------------------------------------

type catalogScenario struct {
	tenant      *tenantmodel.Tenant
	otherTenant *tenantmodel.Tenant
	membership  *tenantmodel.TenantMembership
	services    map[string]*schedulingmodel.Service
}

func activeService(tenantID string) *schedulingmodel.Service {
	return &schedulingmodel.Service{
		ID: catalogRouteServiceA, TenantID: tenantID, Name: "Gel Manicure",
		DurationMinutes: 45, PriceMinor: 1999, Status: schedulingmodel.StatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

func catalogScenarioWithoutCurrency() *catalogScenario {
	businessType := tenantmodel.BusinessTypeNailTechnician
	return &catalogScenario{
		tenant: &tenantmodel.Tenant{
			ID: catalogRouteTenantA, Name: "Acme Nails", Slug: "acme-nails", Status: tenantmodel.StatusActive,
			BusinessType: &businessType, OnboardingStatus: tenantmodel.OnboardingStatusCompleted,
		},
		membership: &tenantmodel.TenantMembership{
			TenantID: catalogRouteTenantA, UserID: catalogRouteUserID, Status: tenantmodel.MembershipStatusActive,
		},
		services: map[string]*schedulingmodel.Service{},
	}
}

func catalogScenarioWithCurrency() *catalogScenario {
	scenario := catalogScenarioWithoutCurrency()
	currency := "NGN"
	scenario.tenant.Currency = &currency
	return scenario
}

func buildCatalogRoutes(t *testing.T, scenario *catalogScenario, tenantPermissions []string) (http.Handler, *identityservice.TokenManager, *statefulServiceRepository, *statefulCatalogTenantRepository) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens := identityservice.NewTokenManager(identityservice.TokenConfig{PrivateKey: privateKey, PublicKey: publicKey, AccessLifetime: time.Minute})
	authMiddleware := auth.Middleware{Tokens: tokens, Sessions: &fakeSessionRepository{}}

	tenants := &statefulCatalogTenantRepository{tenant: scenario.tenant, otherTenant: scenario.otherTenant}
	memberships := &catalogMembershipRepository{scenario: scenario}
	contextService := tenantservice.NewTenantContextService(tenants, memberships)
	tenantMiddleware := tenant.Middleware{Resolver: contextService}
	authorizer := authzservice.NewAuthorizer(&fakeResolutionService{tenantPermissions: tenantPermissions})

	services := &statefulServiceRepository{services: scenario.services}
	catalog := schedulingservice.NewCatalogService(services, tenants)
	serviceHandler := schedulinghandler.NewServiceHandler(catalog)
	currencyHandler := tenanthandler.NewCurrencyHandler(tenantservice.NewCurrencyService(tenants))

	wrap := func(permission string, next http.HandlerFunc) http.Handler {
		return authMiddleware.Wrap(tenantMiddleware.Wrap(
			authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: permission}.Wrap(next),
		))
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tenants/{tenantID}/services", wrap("service.create", func(w http.ResponseWriter, r *http.Request) {
		serviceHandler.Create(w, r, r.PathValue("tenantID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/services", wrap("service.read", func(w http.ResponseWriter, r *http.Request) {
		serviceHandler.List(w, r, r.PathValue("tenantID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/services/{serviceID}", wrap("service.read", func(w http.ResponseWriter, r *http.Request) {
		serviceHandler.Get(w, r, r.PathValue("tenantID"), r.PathValue("serviceID"))
	}))
	mux.Handle("PATCH /api/v1/tenants/{tenantID}/services/{serviceID}", wrap("service.update", func(w http.ResponseWriter, r *http.Request) {
		serviceHandler.Update(w, r, r.PathValue("tenantID"), r.PathValue("serviceID"))
	}))
	mux.Handle("POST /api/v1/tenants/{tenantID}/services/{serviceID}/archive", wrap("service.archive", func(w http.ResponseWriter, r *http.Request) {
		serviceHandler.Archive(w, r, r.PathValue("tenantID"), r.PathValue("serviceID"))
	}))
	mux.Handle("PUT /api/v1/tenants/{tenantID}/currency", wrap("tenant.update", func(w http.ResponseWriter, r *http.Request) {
		currencyHandler.Set(w, r, r.PathValue("tenantID"))
	}))
	return mux, tokens, services, tenants
}

func catalogRequest(t *testing.T, tokens *identityservice.TokenManager, method, path, body string) *http.Request {
	t.Helper()
	token, err := tokens.Issue(catalogRouteUserID, catalogRouteSessionID)
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

// statefulServiceRepository counts reads and writes separately so denial tests
// can assert a rejected request never reached persistence at all.
type statefulServiceRepository struct {
	services   map[string]*schedulingmodel.Service
	readCalls  int
	writeCalls int
}

func (r *statefulServiceRepository) Create(_ context.Context, service *schedulingmodel.Service) (*schedulingmodel.Service, error) {
	r.writeCalls++
	stored := *service
	if stored.Status == "" {
		stored.Status = schedulingmodel.StatusActive
	}
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	r.services[stored.ID] = &stored
	return &stored, nil
}

func (r *statefulServiceRepository) FindByID(_ context.Context, tenantID string, id string) (*schedulingmodel.Service, error) {
	r.readCalls++
	stored, ok := r.services[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}
	return stored, nil
}

func (r *statefulServiceRepository) ListByTenant(_ context.Context, tenantID string, filter schedulingrepository.ServiceListFilter) ([]*schedulingmodel.Service, error) {
	r.readCalls++
	var result []*schedulingmodel.Service
	for _, stored := range r.services {
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

func (r *statefulServiceRepository) Update(_ context.Context, tenantID string, id string, update schedulingrepository.ServiceUpdate) (*schedulingmodel.Service, error) {
	r.writeCalls++
	stored, ok := r.services[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}
	if update.Name != nil {
		stored.Name = *update.Name
	}
	if update.DurationMinutes != nil {
		stored.DurationMinutes = *update.DurationMinutes
	}
	if update.PriceMinor != nil {
		stored.PriceMinor = *update.PriceMinor
	}
	if update.Description != nil {
		stored.Description = update.Description
	}
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

func (r *statefulServiceRepository) Archive(_ context.Context, tenantID string, id string) (*schedulingmodel.Service, error) {
	r.writeCalls++
	stored, ok := r.services[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}
	stored.Status = schedulingmodel.StatusArchived
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

// statefulCatalogTenantRepository satisfies tenantrepository.TenantRepository
// (backing TenantContextService, as in production), CurrencyRepository (backing
// CurrencyService), and scheduling's own TenantReader. Methods these routes
// never exercise fail loudly, mirroring statefulOnboardingRepository.
type statefulCatalogTenantRepository struct {
	tenant         *tenantmodel.Tenant
	otherTenant    *tenantmodel.Tenant
	currencyWrites int
}

func (r *statefulCatalogTenantRepository) Create(context.Context, *tenantmodel.Tenant) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulCatalogTenantRepository) FindByID(_ context.Context, id string) (*tenantmodel.Tenant, error) {
	if r.tenant != nil && r.tenant.ID == id {
		return r.tenant, nil
	}
	if r.otherTenant != nil && r.otherTenant.ID == id {
		return r.otherTenant, nil
	}
	return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
}

func (r *statefulCatalogTenantRepository) FindBySlug(context.Context, string) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulCatalogTenantRepository) ListAccessibleByUserID(context.Context, string) ([]*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulCatalogTenantRepository) UpdateProfile(context.Context, string, tenantrepository.TenantProfileUpdate) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *statefulCatalogTenantRepository) SetCurrency(_ context.Context, tenantID string, currency string) (*tenantmodel.Tenant, error) {
	r.currencyWrites++
	target, err := r.FindByID(context.Background(), tenantID)
	if err != nil {
		return nil, err
	}
	target.Currency = &currency
	target.UpdatedAt = time.Now().UTC()
	return target, nil
}

type catalogMembershipRepository struct{ scenario *catalogScenario }

func (m *catalogMembershipRepository) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (m *catalogMembershipRepository) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	membership := m.scenario.membership
	if membership == nil || membership.TenantID != tenantID || membership.UserID != userID {
		return nil, nil
	}
	return membership, nil
}

func (m *catalogMembershipRepository) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}

func (m *catalogMembershipRepository) Disable(context.Context, string, string, time.Time) error {
	return nil
}

// compile-time guards: the fakes must keep satisfying the real interfaces.
var (
	_ schedulingrepository.ServiceRepository = (*statefulServiceRepository)(nil)
	_ tenantrepository.TenantRepository      = (*statefulCatalogTenantRepository)(nil)
	_ tenantrepository.CurrencyRepository    = (*statefulCatalogTenantRepository)(nil)
	_ schedulingservice.TenantReader         = (*statefulCatalogTenantRepository)(nil)
	_ tenantrepository.MembershipRepository  = (*catalogMembershipRepository)(nil)
)
