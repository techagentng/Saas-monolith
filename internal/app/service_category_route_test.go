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
	schedulinghandler "github.com/techagentng/saas-monolith/internal/scheduling/handler"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	schedulingservice "github.com/techagentng/saas-monolith/internal/scheduling/service"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the exact production middleware chain app.New wires for
// SC1:
//
//	POST   /api/v1/tenants/{tenantID}/service-categories                      service.create
//	GET    /api/v1/tenants/{tenantID}/service-categories                      service.read
//	GET    /api/v1/tenants/{tenantID}/service-categories/{categoryID}         service.read
//	PATCH  /api/v1/tenants/{tenantID}/service-categories/{categoryID}         service.update
//	POST   /api/v1/tenants/{tenantID}/service-categories/{categoryID}/archive service.archive
//	GET    /api/v1/tenants/{tenantID}/service-suggestions                     service.read
//
// all: Authentication -> Tenant Context -> Authorization -> Handler, reusing
// the catalog's own service.* permissions rather than a parallel family — see
// CategoryService's doc comment for why — so these tests reuse
// businessOwnerPermissions/staffPermissions and the tenant/membership fakes
// service_catalog_route_test.go already defines, rather than duplicating them.

const (
	categoryRouteCategoryA = "550e8400-e29b-41d4-a716-446655446005"
)

func buildCategoryRoutes(t *testing.T, scenario *catalogScenario, tenantPermissions []string, categories map[string]*schedulingmodel.ServiceCategory) (http.Handler, *identityservice.TokenManager, *statefulCategoryRepository) {
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

	repo := &statefulCategoryRepository{categories: categories}
	categoryService := schedulingservice.NewCategoryService(repo)
	categoryHandler := schedulinghandler.NewServiceCategoryHandler(categoryService)
	suggestionService := schedulingservice.NewSuggestionService(tenants)
	suggestionHandler := schedulinghandler.NewSuggestionHandler(suggestionService)

	wrap := func(permission string, next http.HandlerFunc) http.Handler {
		return authMiddleware.Wrap(tenantMiddleware.Wrap(
			authorization.TenantPermissionMiddleware{Authorizer: authorizer, Permission: permission}.Wrap(next),
		))
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/tenants/{tenantID}/service-categories", wrap("service.create", func(w http.ResponseWriter, r *http.Request) {
		categoryHandler.Create(w, r, r.PathValue("tenantID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/service-categories", wrap("service.read", func(w http.ResponseWriter, r *http.Request) {
		categoryHandler.List(w, r, r.PathValue("tenantID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/service-categories/{categoryID}", wrap("service.read", func(w http.ResponseWriter, r *http.Request) {
		categoryHandler.Get(w, r, r.PathValue("tenantID"), r.PathValue("categoryID"))
	}))
	mux.Handle("PATCH /api/v1/tenants/{tenantID}/service-categories/{categoryID}", wrap("service.update", func(w http.ResponseWriter, r *http.Request) {
		categoryHandler.Update(w, r, r.PathValue("tenantID"), r.PathValue("categoryID"))
	}))
	mux.Handle("POST /api/v1/tenants/{tenantID}/service-categories/{categoryID}/archive", wrap("service.archive", func(w http.ResponseWriter, r *http.Request) {
		categoryHandler.Archive(w, r, r.PathValue("tenantID"), r.PathValue("categoryID"))
	}))
	mux.Handle("GET /api/v1/tenants/{tenantID}/service-suggestions", wrap("service.read", func(w http.ResponseWriter, r *http.Request) {
		suggestionHandler.List(w, r, r.PathValue("tenantID"))
	}))

	return mux, tokens, repo
}

func activeCategory(tenantID string) *schedulingmodel.ServiceCategory {
	return &schedulingmodel.ServiceCategory{
		ID: categoryRouteCategoryA, TenantID: tenantID, Name: "Pedicures", Status: schedulingmodel.StatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

// --- authentication ------------------------------------------------------

func TestCategoryRoutesRequireAuthentication(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, "/api/v1/tenants/" + catalogRouteTenantA + "/service-categories", `{"name":"Pedicures"}`},
		{"list", http.MethodGet, "/api/v1/tenants/" + catalogRouteTenantA + "/service-categories", ""},
		{"get", http.MethodGet, "/api/v1/tenants/" + catalogRouteTenantA + "/service-categories/" + categoryRouteCategoryA, ""},
		{"update", http.MethodPatch, "/api/v1/tenants/" + catalogRouteTenantA + "/service-categories/" + categoryRouteCategoryA, `{"name":"x"}`},
		{"archive", http.MethodPost, "/api/v1/tenants/" + catalogRouteTenantA + "/service-categories/" + categoryRouteCategoryA + "/archive", ""},
		{"suggestions", http.MethodGet, "/api/v1/tenants/" + catalogRouteTenantA + "/service-suggestions", ""},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			categories := map[string]*schedulingmodel.ServiceCategory{categoryRouteCategoryA: activeCategory(catalogRouteTenantA)}
			handler, _, repo := buildCategoryRoutes(t, catalogScenarioWithCurrency(), businessOwnerPermissions, categories)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s, want 401", recorder.Code, recorder.Body.String())
			}
			if repo.writeCalls != 0 {
				t.Fatal("an unauthenticated request reached a category write")
			}
		})
	}
}

// --- authorization: BUSINESS_OWNER can do everything ----------------------

func TestBusinessOwnerCanManageCategories(t *testing.T) {
	categories := map[string]*schedulingmodel.ServiceCategory{categoryRouteCategoryA: activeCategory(catalogRouteTenantA)}
	handler, tokens, _ := buildCategoryRoutes(t, catalogScenarioWithCurrency(), businessOwnerPermissions, categories)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPost, "/api/v1/tenants/"+catalogRouteTenantA+"/service-categories", `{"name":"Extensions"}`))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/service-categories", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/service-categories/"+categoryRouteCategoryA, ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get: status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPatch, "/api/v1/tenants/"+catalogRouteTenantA+"/service-categories/"+categoryRouteCategoryA, `{"name":"Renamed"}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodPost, "/api/v1/tenants/"+catalogRouteTenantA+"/service-categories/"+categoryRouteCategoryA+"/archive", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("archive: status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v (body = %s)", err, recorder.Body.String())
	}
	if body["status"] != string(schedulingmodel.StatusArchived) {
		t.Fatalf("status = %v, want ARCHIVED", body["status"])
	}
}

// --- authorization: STAFF can read, never write ---------------------------

func TestStaffCanReadCategoriesButNotWrite(t *testing.T) {
	categories := map[string]*schedulingmodel.ServiceCategory{categoryRouteCategoryA: activeCategory(catalogRouteTenantA)}
	handler, tokens, repo := buildCategoryRoutes(t, catalogScenarioWithCurrency(), staffPermissions, categories)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/service-categories", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}

	writes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/tenants/" + catalogRouteTenantA + "/service-categories", `{"name":"Extensions"}`},
		{http.MethodPatch, "/api/v1/tenants/" + catalogRouteTenantA + "/service-categories/" + categoryRouteCategoryA, `{"name":"x"}`},
		{http.MethodPost, "/api/v1/tenants/" + catalogRouteTenantA + "/service-categories/" + categoryRouteCategoryA + "/archive", ""},
	}
	for _, write := range writes {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, catalogRequest(t, tokens, write.method, write.path, write.body))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status = %d, body = %s, want 403", write.method, write.path, recorder.Code, recorder.Body.String())
		}
	}
	if repo.writeCalls != 0 {
		t.Fatal("a STAFF request reached a category write")
	}
}

// --- tenant isolation ------------------------------------------------------

func TestCategoryRouteDoesNotLeakAnotherTenantsCategory(t *testing.T) {
	// The category row genuinely exists — just under tenant B, never named in
	// this request's URL. The route only ever resolves tenant A (the
	// authenticated caller's own tenant), so tenant B need not exist in the
	// tenant repository at all for this to prove the point.
	categories := map[string]*schedulingmodel.ServiceCategory{categoryRouteCategoryA: activeCategory(catalogRouteTenantB)}
	handler, tokens, _ := buildCategoryRoutes(t, catalogScenarioWithCurrency(), businessOwnerPermissions, categories)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/service-categories/"+categoryRouteCategoryA, ""))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404 — tenant A must not see tenant B's category", recorder.Code, recorder.Body.String())
	}
	assertBodyCode(t, recorder, "CATEGORY_NOT_FOUND")
}

// --- suggestions -------------------------------------------------------

func TestSuggestionsRouteReturnsTheTenantsStarterCatalogue(t *testing.T) {
	handler, tokens, _ := buildCategoryRoutes(t, catalogScenarioWithCurrency(), businessOwnerPermissions, map[string]*schedulingmodel.ServiceCategory{})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/service-suggestions", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if strings.TrimSpace(recorder.Body.String()) == "[]" {
		t.Fatal("a NAIL_TECHNICIAN tenant got an empty suggestion list, want the starter catalogue")
	}
}

func TestSuggestionsRouteRequiresServiceReadPermission(t *testing.T) {
	handler, tokens, _ := buildCategoryRoutes(t, catalogScenarioWithCurrency(), []string{"tenant.read"}, map[string]*schedulingmodel.ServiceCategory{})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, catalogRequest(t, tokens, http.MethodGet, "/api/v1/tenants/"+catalogRouteTenantA+"/service-suggestions", ""))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403 without service.read", recorder.Code, recorder.Body.String())
	}
}

// --- test wiring -------------------------------------------------------------

// statefulCategoryRepository is the SC1 analog of statefulServiceRepository:
// a small in-memory ServiceCategoryRepository backing the real CategoryService
// through the real production middleware chain.
type statefulCategoryRepository struct {
	categories map[string]*schedulingmodel.ServiceCategory
	writeCalls int
}

func (r *statefulCategoryRepository) Create(_ context.Context, category *schedulingmodel.ServiceCategory) (*schedulingmodel.ServiceCategory, error) {
	r.writeCalls++
	stored := *category
	if stored.Status == "" {
		stored.Status = schedulingmodel.StatusActive
	}
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	if r.categories == nil {
		r.categories = map[string]*schedulingmodel.ServiceCategory{}
	}
	r.categories[stored.ID] = &stored
	return &stored, nil
}

func (r *statefulCategoryRepository) FindByID(_ context.Context, tenantID string, id string) (*schedulingmodel.ServiceCategory, error) {
	stored, ok := r.categories[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", nil)
	}
	return stored, nil
}

func (r *statefulCategoryRepository) ListByTenant(_ context.Context, tenantID string, filter schedulingrepository.ServiceCategoryListFilter) ([]*schedulingmodel.ServiceCategory, error) {
	var result []*schedulingmodel.ServiceCategory
	for _, stored := range r.categories {
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

func (r *statefulCategoryRepository) Update(_ context.Context, tenantID string, id string, update schedulingrepository.ServiceCategoryUpdate) (*schedulingmodel.ServiceCategory, error) {
	r.writeCalls++
	stored, ok := r.categories[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", nil)
	}
	if update.Name != nil {
		stored.Name = *update.Name
	}
	if update.SortOrder != nil {
		stored.SortOrder = *update.SortOrder
	}
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

func (r *statefulCategoryRepository) Archive(_ context.Context, tenantID string, id string) (*schedulingmodel.ServiceCategory, error) {
	r.writeCalls++
	stored, ok := r.categories[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", nil)
	}
	stored.Status = schedulingmodel.StatusArchived
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

var _ schedulingrepository.ServiceCategoryRepository = (*statefulCategoryRepository)(nil)
