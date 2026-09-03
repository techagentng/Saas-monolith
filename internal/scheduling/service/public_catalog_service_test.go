package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// fakePublicTenantResolver stands in for tenant/service's PublicTenantService.
// The catalog service must never look past it to a tenant repository, so this
// is the only tenant-side dependency the tests wire.
type fakePublicTenantResolver struct {
	context  *tenantservice.PublicTenantContext
	err      error
	calls    int
	lastSlug string
}

func (r *fakePublicTenantResolver) ResolvePublicTenant(_ context.Context, slug string) (*tenantservice.PublicTenantContext, error) {
	r.calls++
	r.lastSlug = slug
	if r.err != nil {
		return nil, r.err
	}
	return r.context, nil
}

func nailResolver(currency string) *fakePublicTenantResolver {
	nail := tenantmodel.BusinessTypeNailTechnician
	c := currency
	return &fakePublicTenantResolver{context: &tenantservice.PublicTenantContext{
		TenantID:     tenantA,
		BusinessType: &nail,
		Currency:     &c,
		Identity:     tenantservice.PublicTenantIdentity{Slug: "glamour-nails", Name: "Glamour Nails"},
	}}
}

func activeCatalogService(id, tenantID, name string, duration int, price int64) *model.Service {
	return &model.Service{ID: id, TenantID: tenantID, Name: name, DurationMinutes: duration, PriceMinor: price, Status: model.StatusActive}
}

// --- happy path -----------------------------------------------------------

func TestGetCatalogReturnsActiveServicesForANailTenant(t *testing.T) {
	resolver := nailResolver("NGN")
	services, catalog := newCatalogFixture(resolver)
	desc := "Long-lasting gel finish."
	services.services["s1"] = &model.Service{ID: "s1", TenantID: tenantA, Name: "Gel Manicure", Description: &desc, DurationMinutes: 45, PriceMinor: 1999, Status: model.StatusActive}
	services.services["s2"] = activeCatalogService("s2", tenantA, "Pedicure", 60, 2999)

	result, err := catalog.GetCatalog(context.Background(), "glamour-nails")
	if err != nil {
		t.Fatalf("GetCatalog() error = %v", err)
	}
	if result.Currency == nil || *result.Currency != "NGN" {
		t.Fatalf("Currency = %v, want NGN", result.Currency)
	}
	if len(result.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(result.Services))
	}
	if resolver.lastSlug != "glamour-nails" {
		t.Fatalf("resolver queried for %q, want the slug unchanged", resolver.lastSlug)
	}
	var gel *PublicServiceView
	for i := range result.Services {
		if result.Services[i].Name == "Gel Manicure" {
			gel = &result.Services[i]
		}
	}
	if gel == nil || gel.DurationMinutes != 45 || gel.PriceMinor != 1999 || gel.Description == nil {
		t.Fatalf("gel manicure view = %#v", gel)
	}
}

func TestGetCatalogExcludesArchivedServices(t *testing.T) {
	_, catalog := newCatalogFixtureWith(t, nailResolver("NGN"), func(s *fakeServiceRepository) {
		s.services["active"] = activeCatalogService("active", tenantA, "Manicure", 30, 1500)
		s.services["archived"] = &model.Service{ID: "archived", TenantID: tenantA, Name: "Old Deal", DurationMinutes: 30, PriceMinor: 500, Status: model.StatusArchived}
	})

	result, err := catalog.GetCatalog(context.Background(), "glamour-nails")
	if err != nil {
		t.Fatalf("GetCatalog() error = %v", err)
	}
	if len(result.Services) != 1 || result.Services[0].Name != "Manicure" {
		t.Fatalf("services = %#v, want only the active one", result.Services)
	}
}

func TestGetCatalogReturnsAnEmptySliceForANailTenantWithNoActiveServices(t *testing.T) {
	_, catalog := newCatalogFixture(nailResolver("NGN"))

	result, err := catalog.GetCatalog(context.Background(), "glamour-nails")
	if err != nil {
		t.Fatalf("GetCatalog() error = %v, want a successful empty catalog", err)
	}
	if result.Services == nil {
		t.Fatal("Services is nil; it must be an empty slice so the handler emits []")
	}
	if len(result.Services) != 0 {
		t.Fatalf("services = %#v, want empty", result.Services)
	}
}

func TestGetCatalogEchoesANilCurrencyWhenTheTenantHasNotDeclaredOne(t *testing.T) {
	resolver := nailResolver("NGN")
	resolver.context.Currency = nil
	_, catalog := newCatalogFixture(resolver)

	result, err := catalog.GetCatalog(context.Background(), "glamour-nails")
	if err != nil {
		t.Fatalf("GetCatalog() error = %v", err)
	}
	if result.Currency != nil {
		t.Fatalf("Currency = %v, want nil", result.Currency)
	}
}

// --- vertical guard -----------------------------------------------------

func TestGetCatalogRefusesNonNailVerticals(t *testing.T) {
	for _, bt := range []tenantmodel.BusinessType{
		tenantmodel.BusinessTypeHotel,
		tenantmodel.BusinessTypeRestaurant,
		tenantmodel.BusinessTypeTransport,
	} {
		resolver := nailResolver("NGN")
		businessType := bt
		resolver.context.BusinessType = &businessType
		services, catalog := newCatalogFixture(resolver)
		services.services["s1"] = activeCatalogService("s1", tenantA, "Should never surface", 30, 1000)

		_, err := catalog.GetCatalog(context.Background(), "glamour-nails")
		assertCode(t, err, apperrors.CodeResourceNotFound, string(bt))
		if services.lastTenantID != "" {
			t.Fatalf("%s: catalog was queried for a non-nail tenant", bt)
		}
	}
}

func TestGetCatalogRefusesALegacyTenantWithNoBusinessType(t *testing.T) {
	resolver := nailResolver("NGN")
	resolver.context.BusinessType = nil
	_, catalog := newCatalogFixture(resolver)

	_, err := catalog.GetCatalog(context.Background(), "legacy-salon")
	assertCode(t, err, apperrors.CodeResourceNotFound, "legacy tenant")
}

// --- visibility gate is delegated -----------------------------------

func TestGetCatalogPropagatesTheResolversNotFound(t *testing.T) {
	resolver := &fakePublicTenantResolver{err: apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)}
	_, catalog := newCatalogFixture(resolver)

	_, err := catalog.GetCatalog(context.Background(), "does-not-exist")
	assertCode(t, err, apperrors.CodeTenantNotFound, "unknown slug")
}

func TestGetCatalogPropagatesTheResolversSlugInvalid(t *testing.T) {
	resolver := &fakePublicTenantResolver{err: apperrors.New(apperrors.CodeTenantSlugInvalid, "invalid slug", nil)}
	_, catalog := newCatalogFixture(resolver)

	_, err := catalog.GetCatalog(context.Background(), "Not_Canonical")
	assertCode(t, err, apperrors.CodeTenantSlugInvalid, "non-canonical slug")
}

func TestGetCatalogDoesNotSwallowARawResolverFailure(t *testing.T) {
	resolver := &fakePublicTenantResolver{err: errors.New("connection reset")}
	_, catalog := newCatalogFixture(resolver)

	_, err := catalog.GetCatalog(context.Background(), "glamour-nails")
	if err == nil {
		t.Fatal("GetCatalog() returned no error on a resolver failure")
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.Code == apperrors.CodeTenantNotFound {
		t.Fatalf("a raw failure surfaced as TENANT_NOT_FOUND: %v", err)
	}
}

// --- tenant isolation -------------------------------------------------

func TestGetCatalogReturnsOnlyTheResolvedTenantsServices(t *testing.T) {
	resolver := nailResolver("NGN") // resolves to tenantA
	services, catalog := newCatalogFixture(resolver)
	services.services["a1"] = activeCatalogService("a1", tenantA, "Tenant A Manicure", 30, 1000)
	services.services["b1"] = activeCatalogService("b1", tenantB, "Tenant B Manicure", 30, 1000)

	result, err := catalog.GetCatalog(context.Background(), "glamour-nails")
	if err != nil {
		t.Fatalf("GetCatalog() error = %v", err)
	}
	if len(result.Services) != 1 || result.Services[0].Name != "Tenant A Manicure" {
		t.Fatalf("services = %#v, want only tenant A's", result.Services)
	}
	if services.lastTenantID != tenantA {
		t.Fatalf("catalog queried tenant %q, want %q", services.lastTenantID, tenantA)
	}
}

// --- DTO surface ----------------------------------------------------

func TestPublicServiceViewExposesOnlyCustomerSafeFields(t *testing.T) {
	want := map[string]bool{"ID": true, "Name": true, "Description": true, "DurationMinutes": true, "PriceMinor": true}
	structType := reflect.TypeOf(PublicServiceView{})
	if structType.NumField() != len(want) {
		t.Fatalf("PublicServiceView has %d fields, want %d", structType.NumField(), len(want))
	}
	for i := 0; i < structType.NumField(); i++ {
		if name := structType.Field(i).Name; !want[name] {
			t.Fatalf("PublicServiceView exposes unexpected field %q — no status, tenant id, or timestamps belong here", name)
		}
	}
}

// --- test helpers -------------------------------------------------

func newCatalogFixture(resolver *fakePublicTenantResolver) (*fakeServiceRepository, PublicCatalogService) {
	services := newFakeServiceRepository()
	return services, NewPublicCatalogService(resolver, services)
}

func newCatalogFixtureWith(t *testing.T, resolver *fakePublicTenantResolver, seed func(*fakeServiceRepository)) (*fakeServiceRepository, PublicCatalogService) {
	t.Helper()
	services := newFakeServiceRepository()
	seed(services)
	return services, NewPublicCatalogService(resolver, services)
}
