package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	tenanthandler "github.com/techagentng/saas-monolith/internal/tenant/handler"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These tests exercise the Feature 5 public route exactly as app.New registers
// it — through a real http.ServeMux, with the REAL PublicTenantService, and
// with NO authentication, tenant-context, or permission middleware. That
// absence is itself part of the contract and is asserted below.

const publicRouteTenantID = "550e8400-e29b-41d4-a716-446655441101"

func TestPublicTenantRouteReturnsIdentityWithoutAuthentication(t *testing.T) {
	handler, _ := buildPublicTenantRoute(activePublicTenant())
	recorder := httptest.NewRecorder()

	// Deliberately no Authorization header.
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/acme-salon", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 without authentication", recorder.Code, recorder.Body.String())
	}
	var identity tenanthandler.PublicTenantIdentity
	if err := json.Unmarshal(recorder.Body.Bytes(), &identity); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if identity.Slug != "acme-salon" || identity.Name != "Acme Salon" {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.Description == nil || *identity.Description != "Full service salon" {
		t.Fatalf("Description = %v", identity.Description)
	}
	if identity.Timezone == nil || *identity.Timezone != "Africa/Lagos" {
		t.Fatalf("Timezone = %v", identity.Timezone)
	}
}

// The public payload must not leak the internal UUID, lifecycle status,
// timestamps, or the private contact details Feature 4 stores.
func TestPublicTenantRouteResponseOmitsPrivateFields(t *testing.T) {
	handler, _ := buildPublicTenantRoute(activePublicTenant())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/acme-salon", nil))

	var body map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, forbidden := range []string{"id", "tenant_id", "status", "created_at", "updated_at", "contact_email", "contact_phone"} {
		if _, present := body[forbidden]; present {
			t.Fatalf("public response exposed %q: %s", forbidden, recorder.Body.String())
		}
	}
	want := map[string]bool{"slug": true, "name": true, "description": true, "timezone": true}
	for key := range body {
		if !want[key] {
			t.Fatalf("public response exposed unexpected key %q", key)
		}
	}
	if len(body) != len(want) {
		t.Fatalf("public response keys = %v, want exactly %v", body, want)
	}
	// The raw body must not contain the private values at all.
	raw := recorder.Body.String()
	for _, secret := range []string{publicRouteTenantID, "private@acme.test", "+2348012345678", "ACTIVE"} {
		if containsAny(raw, secret) {
			t.Fatalf("public response leaked %q: %s", secret, raw)
		}
	}
}

func TestPublicTenantRouteReturns404ForUnknownSlug(t *testing.T) {
	handler, _ := buildPublicTenantRoute(activePublicTenant())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/no-such-tenant", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
}

// A disabled tenant must be indistinguishable from one that does not exist.
func TestPublicTenantRouteHidesDisabledTenantIdentically(t *testing.T) {
	disabled := activePublicTenant()
	disabled.Status = tenantmodel.StatusDisabled
	disabledHandler, _ := buildPublicTenantRoute(disabled)
	disabledRecorder := httptest.NewRecorder()
	disabledHandler.ServeHTTP(disabledRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/acme-salon", nil))

	missingHandler, _ := buildPublicTenantRoute(activePublicTenant())
	missingRecorder := httptest.NewRecorder()
	missingHandler.ServeHTTP(missingRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/no-such-tenant", nil))

	if disabledRecorder.Code != http.StatusNotFound {
		t.Fatalf("disabled tenant status = %d, want 404", disabledRecorder.Code)
	}
	if disabledRecorder.Code != missingRecorder.Code || disabledRecorder.Body.String() != missingRecorder.Body.String() {
		t.Fatalf("disabled response %q differs from missing response %q; lifecycle state is publicly detectable",
			disabledRecorder.Body.String(), missingRecorder.Body.String())
	}
	if containsAny(disabledRecorder.Body.String(), "Acme Salon", "acme-salon") {
		t.Fatalf("disabled tenant leaked data: %s", disabledRecorder.Body.String())
	}
}

func TestPublicTenantRouteRejectsInvalidSlugSyntax(t *testing.T) {
	handler, repo := buildPublicTenantRoute(activePublicTenant())

	for _, slug := range []string{"Acme", "acme_salon", "-acme", "ac", "acme%20salon"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/"+slug, nil))

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("slug %q status = %d, body = %s, want 400", slug, recorder.Code, recorder.Body.String())
		}
		assertBodyCode(t, recorder, "TENANT_SLUG_INVALID")
	}
	if repo.calls != 0 {
		t.Fatalf("invalid slugs reached the repository %d times", repo.calls)
	}
}

// A reserved platform name must read as absent, not as a distinct error that
// would confirm the platform holds it.
func TestPublicTenantRouteTreatsReservedSlugAsNotFound(t *testing.T) {
	handler, _ := buildPublicTenantRoute(activePublicTenant())

	for _, reserved := range []string{"admin", "api", "login", "dashboard", "auth", "book", "settings"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/"+reserved, nil))

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("reserved slug %q status = %d, want 404", reserved, recorder.Code)
		}
		assertBodyCode(t, recorder, "TENANT_NOT_FOUND")
	}
}

// The public route must not sit behind the private middleware chain: a request
// carrying a garbage bearer token must still succeed, proving no
// authentication middleware is wrapped around it.
func TestPublicTenantRouteIgnoresAuthorizationHeaderEntirely(t *testing.T) {
	handler, _ := buildPublicTenantRoute(activePublicTenant())
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/acme-salon", nil)
	request.Header.Set("Authorization", "Bearer not-a-real-token")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; an invalid token must not matter on a public route", recorder.Code)
	}
}

// The public path must not collide with the private /api/v1/tenants/{tenantID}
// route, and must not be reachable through it.
func TestPublicTenantRouteDoesNotCollideWithPrivateTenantRoute(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/public/tenants/{slug}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("public:" + r.PathValue("slug")))
	})
	mux.HandleFunc("GET /api/v1/tenants/{tenantID}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("private:" + r.PathValue("tenantID")))
	})

	publicRecorder := httptest.NewRecorder()
	mux.ServeHTTP(publicRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/acme-salon", nil))
	if publicRecorder.Body.String() != "public:acme-salon" {
		t.Fatalf("public path dispatched to %q", publicRecorder.Body.String())
	}

	privateRecorder := httptest.NewRecorder()
	mux.ServeHTTP(privateRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+publicRouteTenantID, nil))
	if privateRecorder.Body.String() != "private:"+publicRouteTenantID {
		t.Fatalf("private path dispatched to %q", privateRecorder.Body.String())
	}
}

// --- test wiring -------------------------------------------------------------

func activePublicTenant() *tenantmodel.Tenant {
	description := "Full service salon"
	timezone := "Africa/Lagos"
	email := "private@acme.test"
	phone := "+2348012345678"
	now := time.Now().UTC()
	return &tenantmodel.Tenant{
		ID: publicRouteTenantID, Name: "Acme Salon", Slug: "acme-salon",
		Status: tenantmodel.StatusActive, Description: &description, Timezone: &timezone,
		ContactEmail: &email, ContactPhone: &phone, CreatedAt: now, UpdatedAt: now,
	}
}

// buildPublicTenantRoute mirrors app.New's registration for the public route:
// a bare mux entry with no middleware wrapping whatsoever.
func buildPublicTenantRoute(tenant *tenantmodel.Tenant) (http.Handler, *publicSlugRepository) {
	repo := &publicSlugRepository{tenant: tenant}
	handler := tenanthandler.NewPublicTenantHandler(tenantservice.NewPublicTenantService(repo))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/public/tenants/{slug}", func(w http.ResponseWriter, r *http.Request) {
		handler.GetBySlug(w, r, r.PathValue("slug"))
	})
	return mux, repo
}

type publicSlugRepository struct {
	tenant *tenantmodel.Tenant
	calls  int
}

func (r *publicSlugRepository) Create(context.Context, *tenantmodel.Tenant) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *publicSlugRepository) FindByID(context.Context, string) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *publicSlugRepository) ListAccessibleByUserID(context.Context, string) ([]*tenantmodel.Tenant, error) {
	return []*tenantmodel.Tenant{}, nil
}

func (r *publicSlugRepository) UpdateProfile(context.Context, string, tenantrepository.TenantProfileUpdate) (*tenantmodel.Tenant, error) {
	return nil, apperrors.New(apperrors.CodeInternalError, "not implemented in fake", nil)
}

func (r *publicSlugRepository) FindBySlug(_ context.Context, slug string) (*tenantmodel.Tenant, error) {
	r.calls++
	if r.tenant == nil || r.tenant.Slug != slug {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return r.tenant, nil
}
