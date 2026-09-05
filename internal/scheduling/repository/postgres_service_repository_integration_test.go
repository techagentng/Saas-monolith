package repository

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

const (
	integrationTenantA = "550e8400-e29b-41d4-a716-446655446001"
	integrationTenantB = "550e8400-e29b-41d4-a716-446655446002"
)

// schedulingMigrations is the full chain S1 depends on, in order. 000010 is not
// applicable on its own: it inserts permission and role_permission rows, so the
// RBAC tables from 000005/000006 must exist first.
var schedulingMigrations = []string{
	"000001_create_users.up.sql",
	"000002_create_sessions.up.sql",
	"000003_create_tenants.up.sql",
	"000004_create_tenant_memberships.up.sql",
	"000005_create_roles_permissions.up.sql",
	"000006_seed_roles_permissions.up.sql",
	"000007_add_slug_to_tenants.up.sql",
	"000008_add_tenant_profile_fields.up.sql",
	"000009_add_business_type_and_onboarding_to_tenants.up.sql",
	"000010_create_services_and_tenant_currency.up.sql",
	"000011_seed_service_permissions.up.sql",
	"000012_create_staff_profiles_and_capabilities.up.sql",
	"000013_seed_staff_permissions.up.sql",
	// 000014_create_user_identities is Google OAuth (unrelated to scheduling)
	// and deliberately not applied here — this chain only needs the
	// migrations scheduling actually depends on.
	"000015_create_staff_working_hours.up.sql",
	"000016_create_bookings.up.sql",
	"000017_seed_booking_permissions.up.sql",
	"000018_add_booking_status_index.up.sql",
	"000019_create_service_categories.up.sql",
	"000020_create_service_images.up.sql",
}

// schedulingTables is the drop order — children before parents, so foreign keys
// never block the reset. services references service_categories (SC1), so it
// is listed first.
var schedulingTables = []string{
	"bookings", "staff_working_hours", "staff_services", "staff_profiles", "services", "service_categories", "service_images", "user_roles", "role_permissions", "permissions", "roles",
	"tenant_memberships", "sessions", "tenants", "users",
}

// openSchedulingTestDB connects to the disposable Docker Postgres and rebuilds
// the schema from the real migration files.
//
// It refuses to run against the development database. These tests DROP every
// table before rebuilding, so pointing TEST_DATABASE_URL at the dev DSN would
// destroy real data — a mistake worth making structurally impossible rather
// than relying on the operator's care. Use docker-compose.test.yml, which
// exposes booking_test on port 5434, distinct from the dev database on 5433.
func openSchedulingTestDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	assertDisposableDatabase(t, databaseURL)

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database is unavailable: %v", err)
	}

	dropSchedulingTables(t, db)
	for _, migration := range schedulingMigrations {
		applySchedulingMigration(t, db, migration)
	}
	t.Cleanup(func() { dropSchedulingTables(t, db) })
	return db
}

// assertDisposableDatabase fails loudly rather than skipping: silently skipping
// would let a misconfigured run report "no failures" while verifying nothing.
func assertDisposableDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parsing database URL: %v", err)
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if name == "booking" {
		t.Fatalf("refusing to run destructive tests against the development database %q — point TEST_DATABASE_URL at the disposable Docker database (docker-compose.test.yml)", name)
	}
}

func dropSchedulingTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range schedulingTables {
		if _, err := db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table+" CASCADE"); err != nil {
			t.Fatalf("dropping %s: %v", table, err)
		}
	}
}

func applySchedulingMigration(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", name))
	if err != nil {
		t.Fatalf("reading migration %s: %v", name, err)
	}
	if _, err := db.ExecContext(context.Background(), string(contents)); err != nil {
		t.Fatalf("applying migration %s: %v", name, err)
	}
}

// seedTenant inserts a tenant directly. currency is passed through as supplied
// so tests can exercise both a legacy tenant (NULL) and a configured one.
func seedTenant(t *testing.T, db *sql.DB, id string, slug string, currency *string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency) VALUES ($1, $2, $3, 'ACTIVE', 'NAIL_TECHNICIAN', 'COMPLETED', $4)",
		id, "Tenant "+slug, slug, currency)
	if err != nil {
		t.Fatalf("seeding tenant %s: %v", slug, err)
	}
}

func newService(id string, tenantID string, name string) *model.Service {
	return &model.Service{ID: id, TenantID: tenantID, Name: name, DurationMinutes: 45, PriceMinor: 1999}
}

func assertServiceNotFound(t *testing.T, err error, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeServiceNotFound {
		t.Fatalf("%s: error = %v, want SERVICE_NOT_FOUND", context, err)
	}
}

// --- round trip --------------------------------------------------------------

func TestCreateRoundTripsAndDefaultsToActive(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	description := "Long-lasting gel finish."
	input := newService("550e8400-e29b-41d4-a716-446655446101", integrationTenantA, "Gel Manicure")
	input.Description = &description

	created, err := repo.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE by repository default", created.Status)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("Create() did not return database timestamps")
	}

	found, err := repo.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.Name != "Gel Manicure" || found.DurationMinutes != 45 || found.PriceMinor != 1999 {
		t.Fatalf("round trip lost data: %+v", found)
	}
	if found.Description == nil || *found.Description != description {
		t.Fatalf("Description = %v, want %q", found.Description, description)
	}
	if found.TenantID != integrationTenantA {
		t.Fatalf("TenantID = %q, want %q", found.TenantID, integrationTenantA)
	}
}

func TestCreateStoresNullDescriptionWhenAbsent(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	created, err := repo.Create(context.Background(), newService("550e8400-e29b-41d4-a716-446655446102", integrationTenantA, "Classic Manicure"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Description != nil {
		t.Fatalf("Description = %v, want NULL", *created.Description)
	}
}

// A tenant created before Scheduling S1 has currency NULL and no value was
// inferred for it. Its rows must still read and write normally.
func TestLegacyTenantWithNullCurrencyStillSupportsTheCatalogSchema(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	seedTenant(t, db, integrationTenantA, "legacy-tenant", nil)

	var currency *string
	if err := db.QueryRowContext(context.Background(), "SELECT currency FROM tenants WHERE id = $1", integrationTenantA).Scan(&currency); err != nil {
		t.Fatalf("reading legacy tenant currency: %v", err)
	}
	if currency != nil {
		t.Fatalf("currency = %q, want NULL for a legacy tenant — the migration must not infer one", *currency)
	}

	// The schema itself imposes no currency prerequisite; that rule lives in
	// CatalogService, which is what makes it presentable rather than a
	// constraint violation.
	if _, err := repo.Create(context.Background(), newService("550e8400-e29b-41d4-a716-446655446103", integrationTenantA, "Legacy Service")); err != nil {
		t.Fatalf("Create() against a legacy tenant error = %v", err)
	}
}

// --- tenant isolation --------------------------------------------------------

func TestFindByIDDoesNotCrossTenants(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)

	created, err := repo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655446104", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.FindByID(ctx, integrationTenantB, created.ID)
	assertServiceNotFound(t, err, "FindByID(tenant B, tenant A's service)")
}

func TestListByTenantReturnsOnlyThatTenantsCatalogInNameOrder(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)

	// Inserted out of alphabetical order to prove the ORDER BY is real.
	for id, name := range map[string]string{
		"550e8400-e29b-41d4-a716-446655446201": "Pedicure",
		"550e8400-e29b-41d4-a716-446655446202": "Acrylic Full Set",
		"550e8400-e29b-41d4-a716-446655446203": "Gel Polish",
	} {
		if _, err := repo.Create(ctx, newService(id, integrationTenantA, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655446204", integrationTenantB, "Rival Service")); err != nil {
		t.Fatal(err)
	}

	services, err := repo.ListByTenant(ctx, integrationTenantA, ServiceListFilter{})
	if err != nil {
		t.Fatalf("ListByTenant() error = %v", err)
	}
	if len(services) != 3 {
		t.Fatalf("returned %d services, want 3 — tenant B's row must not appear", len(services))
	}
	want := []string{"Acrylic Full Set", "Gel Polish", "Pedicure"}
	for i, service := range services {
		if service.Name != want[i] {
			t.Fatalf("position %d = %q, want %q (deterministic name ordering)", i, service.Name, want[i])
		}
		if service.TenantID != integrationTenantA {
			t.Fatalf("another tenant's service leaked into the listing: %+v", service)
		}
	}
}

func TestUpdateAndArchiveDoNotCrossTenants(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)

	created, err := repo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655446105", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	hijacked := "Hijacked"
	_, err = repo.Update(ctx, integrationTenantB, created.ID, ServiceUpdate{Name: &hijacked})
	assertServiceNotFound(t, err, "Update(cross-tenant)")

	_, err = repo.Archive(ctx, integrationTenantB, created.ID)
	assertServiceNotFound(t, err, "Archive(cross-tenant)")

	unchanged, err := repo.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Name != "Gel Manicure" || unchanged.Status != model.StatusActive {
		t.Fatalf("a cross-tenant write mutated the row: %+v", unchanged)
	}
}

// --- update ------------------------------------------------------------------

func TestUpdateWritesOnlySuppliedFields(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	created, err := repo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655446106", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	name := "Gel Manicure Deluxe"
	price := int64(2999)
	updated, err := repo.Update(ctx, integrationTenantA, created.ID, ServiceUpdate{Name: &name, PriceMinor: &price})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != name || updated.PriceMinor != price {
		t.Fatalf("Update() did not apply the supplied fields: %+v", updated)
	}
	if updated.DurationMinutes != 45 {
		t.Fatalf("DurationMinutes = %d, want the original 45 — an omitted field must not change", updated.DurationMinutes)
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE — Update never touches lifecycle", updated.Status)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) && !updated.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatal("updated_at went backwards")
	}
}

func TestUpdateWithNoFieldsIsRefused(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	created, err := repo.Create(context.Background(), newService("550e8400-e29b-41d4-a716-446655446107", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Update(context.Background(), integrationTenantA, created.ID, ServiceUpdate{}); err == nil {
		t.Fatal("Update() with an empty patch produced a query instead of refusing")
	}
}

func TestUpdateOnMissingServiceReportsNotFound(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	name := "Nothing"
	_, err := repo.Update(context.Background(), integrationTenantA, "550e8400-e29b-41d4-a716-446655449999", ServiceUpdate{Name: &name})
	assertServiceNotFound(t, err, "Update(missing service)")
}

// --- archive -----------------------------------------------------------------

func TestArchivePersistsArchivedAndKeepsTheRow(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	created, err := repo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655446108", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	archived, err := repo.Archive(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if archived.Status != model.StatusArchived {
		t.Fatalf("Status = %q, want ARCHIVED", archived.Status)
	}

	// The row survives: appointments will hold a foreign key to it from S10.
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM services WHERE id = $1", created.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("Archive() removed the row — archiving must never delete")
	}

	// It survives a re-read too, rather than only being correct in the
	// RETURNING clause.
	reread, err := repo.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Status != model.StatusArchived {
		t.Fatalf("re-read Status = %q, want ARCHIVED", reread.Status)
	}
}

func TestListFilterSeparatesActiveFromArchived(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	active, err := repo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655446109", integrationTenantA, "Active Service"))
	if err != nil {
		t.Fatal(err)
	}
	retired, err := repo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655446110", integrationTenantA, "Retired Service"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Archive(ctx, integrationTenantA, retired.ID); err != nil {
		t.Fatal(err)
	}

	activeStatus := model.StatusActive
	activeOnly, err := repo.ListByTenant(ctx, integrationTenantA, ServiceListFilter{Status: &activeStatus})
	if err != nil {
		t.Fatal(err)
	}
	if len(activeOnly) != 1 || activeOnly[0].ID != active.ID {
		t.Fatalf("ACTIVE filter returned %d rows, want just the active one", len(activeOnly))
	}

	archivedStatus := model.StatusArchived
	archivedOnly, err := repo.ListByTenant(ctx, integrationTenantA, ServiceListFilter{Status: &archivedStatus})
	if err != nil {
		t.Fatal(err)
	}
	if len(archivedOnly) != 1 || archivedOnly[0].ID != retired.ID {
		t.Fatalf("ARCHIVED filter returned %d rows, want just the archived one", len(archivedOnly))
	}

	all, err := repo.ListByTenant(ctx, integrationTenantA, ServiceListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered listing returned %d rows, want 2", len(all))
	}
}

// --- schema constraints ------------------------------------------------------

// The CHECK constraints exist so no writer can bypass the service-layer rules —
// a future admin tool, a migration, or a manual psql session included. These
// insert directly, deliberately going around the repository.
func TestSchemaRejectsInvalidRowsWrittenDirectly(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	tests := []struct {
		name     string
		duration int
		price    int64
		status   string
	}{
		{"zero duration", 0, 1999, "ACTIVE"},
		{"negative duration", -30, 1999, "ACTIVE"},
		{"duration over the maximum", model.MaxDurationMinutes + 1, 1999, "ACTIVE"},
		{"negative price", 45, -1, "ACTIVE"},
		{"price over the maximum", 45, model.MaxPriceMinor + 1, "ACTIVE"},
		{"status outside the vocabulary", 45, 1999, "DISABLED"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx,
				"INSERT INTO services (id, tenant_id, name, duration_minutes, price_minor, status) VALUES ($1, $2, $3, $4, $5, $6)",
				"550e8400-e29b-41d4-a716-446655446301", integrationTenantA, "Direct Insert", test.duration, test.price, test.status)
			if err == nil {
				t.Fatalf("the database accepted %s — the CHECK constraint is missing or wrong", test.name)
			}
		})
	}
}

func TestSchemaRejectsAServiceForAnUnknownTenant(t *testing.T) {
	db := openSchedulingTestDB(t)

	_, err := db.ExecContext(context.Background(),
		"INSERT INTO services (id, tenant_id, name, duration_minutes, price_minor) VALUES ($1, $2, $3, $4, $5)",
		"550e8400-e29b-41d4-a716-446655446302", "550e8400-e29b-41d4-a716-446655449999", "Orphan", 45, 1999)
	if err == nil {
		t.Fatal("the database accepted a service whose tenant does not exist — the foreign key is missing")
	}
}

// The composite unique key is the parent side of S3's staff_services guard:
// PostgreSQL only accepts a composite foreign key whose referenced columns
// carry a unique constraint of exactly that shape.
func TestServicesCarryTheCompositeUniqueKeyS3WillReference(t *testing.T) {
	db := openSchedulingTestDB(t)

	var exists bool
	err := db.QueryRowContext(context.Background(), `
SELECT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'services_id_tenant_unique'
      AND contype = 'u'
      AND conrelid = 'services'::regclass
)`).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("services_id_tenant_unique is missing — S3's composite foreign key cannot reference (id, tenant_id) without it")
	}

	// Prove it actually works as an FK target, which is the only reason it
	// exists, rather than merely asserting the catalog row is present.
	//
	// A permanent table, not a temporary one: PostgreSQL forbids a constraint
	// on a temporary table from referencing a permanent one, so a TEMPORARY
	// probe would fail for a reason that has nothing to do with what is being
	// verified here.
	ctx := context.Background()
	t.Cleanup(func() { db.ExecContext(context.Background(), "DROP TABLE IF EXISTS composite_fk_probe") })
	if _, err := db.ExecContext(ctx, `
CREATE TABLE composite_fk_probe (
    service_id UUID NOT NULL,
    tenant_id  UUID NOT NULL,
    FOREIGN KEY (service_id, tenant_id) REFERENCES services(id, tenant_id)
)`); err != nil {
		t.Fatalf("a composite foreign key to services(id, tenant_id) is not accepted: %v", err)
	}

	// And it genuinely enforces the pairing: a (service_id, tenant_id) row
	// naming the right service under the wrong tenant must be rejected. This
	// is the cross-tenant guarantee S3 depends on.
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "probe-a", &currency)
	seedTenant(t, db, integrationTenantB, "probe-b", &currency)
	repo := NewPostgresServiceRepository(db)
	created, err := repo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655446303", integrationTenantA, "Probe Service"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO composite_fk_probe (service_id, tenant_id) VALUES ($1, $2)", created.ID, integrationTenantA); err != nil {
		t.Fatalf("the correct (service, tenant) pairing was rejected: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO composite_fk_probe (service_id, tenant_id) VALUES ($1, $2)", created.ID, integrationTenantB); err == nil {
		t.Fatal("the database accepted tenant B paired with tenant A's service — the composite foreign key does not enforce tenant agreement")
	}
}
