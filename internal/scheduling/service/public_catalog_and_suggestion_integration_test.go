package service

import (
	"context"
	"testing"

	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// These two tests round out the SC1 real-database verification pass:
//
//   - the anonymous public catalog resolves a service's category NAME through
//     a genuine SQL join-by-lookup against Postgres (not a fake), proving the
//     wiring in PublicCatalogService.GetCatalog actually works against the
//     real service_categories table, not just the fakes public_catalog_service_test.go
//     already exercises.
//   - the suggestion endpoint is proven to write nothing at all: it is
//     backed by a Go constant (internal/scheduling/suggestions) and must
//     never touch services or service_categories.
//
// Both reuse openCapabilityTestDB's schema (already includes migration
// 000019 for SC1) rather than standing up a fourth migration harness.

const (
	publicCatalogTenantID = "550e8400-e29b-41d4-a716-446655453001"
	publicCatalogCatID    = "550e8400-e29b-41d4-a716-446655453002"
	publicCatalogSvcID    = "550e8400-e29b-41d4-a716-446655453003"
)

// stubPublicTenantResolver stands in only for the tenant-slug visibility gate
// (PublicTenantService), which is unrelated to what these tests verify and is
// already covered by its own tests elsewhere. Everything else — services,
// categories, tenants — is the real Postgres-backed repository.
type stubPublicTenantResolver struct{ context *tenantservice.PublicTenantContext }

func (s *stubPublicTenantResolver) ResolvePublicTenant(context.Context, string) (*tenantservice.PublicTenantContext, error) {
	return s.context, nil
}

func TestPublicCatalogResolvesCategoryNameAgainstRealPostgres(t *testing.T) {
	db := openCapabilityTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx,
		"INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency) VALUES ($1, $2, $3, 'ACTIVE', 'NAIL_TECHNICIAN', 'COMPLETED', 'NGN')",
		publicCatalogTenantID, "Public Catalog Tenant", "public-catalog-tenant"); err != nil {
		t.Fatalf("seeding tenant: %v", err)
	}

	categoryRepo := repository.NewPostgresServiceCategoryRepository(db)
	category, err := categoryRepo.Create(ctx, &model.ServiceCategory{ID: publicCatalogCatID, TenantID: publicCatalogTenantID, Name: "Pedicures"})
	if err != nil {
		t.Fatalf("seeding category: %v", err)
	}

	serviceRepo := repository.NewPostgresServiceRepository(db)
	if _, err := serviceRepo.Create(ctx, &model.Service{
		ID: publicCatalogSvcID, TenantID: publicCatalogTenantID, Name: "Spa Pedicure",
		DurationMinutes: 60, PriceMinor: 650000, CategoryID: &category.ID,
	}); err != nil {
		t.Fatalf("seeding service: %v", err)
	}

	currency := "NGN"
	nailType := tenantmodel.BusinessTypeNailTechnician
	resolver := &stubPublicTenantResolver{context: &tenantservice.PublicTenantContext{
		TenantID: publicCatalogTenantID, Currency: &currency, BusinessType: &nailType,
	}}

	catalog := NewPublicCatalogService(resolver, serviceRepo, categoryRepo, repository.NewPostgresServiceImageRepository(db))
	result, err := catalog.GetCatalog(ctx, "public-catalog-tenant")
	if err != nil {
		t.Fatalf("GetCatalog() error = %v", err)
	}
	if len(result.Services) != 1 {
		t.Fatalf("Services = %#v, want exactly 1", result.Services)
	}
	got := result.Services[0]
	if got.Category == nil || *got.Category != "Pedicures" {
		t.Fatalf("Category = %v, want %q — resolved from a real Postgres join, not a fake", got.Category, "Pedicures")
	}
}

// The suggestion endpoint must perform no persistence: it is backed entirely
// by internal/scheduling/suggestions' Go constant, and SuggestionService.List
// only ever reads the tenant row. Proven here by comparing real row counts in
// every table a write could land in, before and after the call.
func TestSuggestionListPerformsNoPersistenceAgainstRealPostgres(t *testing.T) {
	db := openCapabilityTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx,
		"INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency) VALUES ($1, $2, $3, 'ACTIVE', 'NAIL_TECHNICIAN', 'COMPLETED', 'NGN')",
		publicCatalogTenantID, "Suggestion Tenant", "suggestion-tenant"); err != nil {
		t.Fatalf("seeding tenant: %v", err)
	}

	countRows := func(table string) int {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		return count
	}
	tables := []string{"tenants", "services", "service_categories", "staff_profiles", "staff_services"}
	before := map[string]int{}
	for _, table := range tables {
		before[table] = countRows(table)
	}

	tenants := tenantrepository.NewPostgresTenantRepository(db)
	suggestionService := NewSuggestionService(tenants)

	list, err := suggestionService.List(ctx, publicCatalogTenantID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) == 0 {
		t.Fatal("List() returned nothing for a NAIL_TECHNICIAN tenant — the assertion below would be vacuous")
	}

	for _, table := range tables {
		after := countRows(table)
		if after != before[table] {
			t.Fatalf("table %s: row count changed from %d to %d — the suggestion endpoint must never write", table, before[table], after)
		}
	}
}
