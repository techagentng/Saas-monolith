package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

// --- Migration 000009 schema contract ---------------------------------------

func TestMigration000009AddsExpectedColumns(t *testing.T) {
	db := openTenantTestDB(t)
	ctx := context.Background()

	wantNullable := map[string]string{
		"business_type":     "YES",
		"onboarding_status": "NO",
		"onboarding_step":   "YES",
	}
	for column, nullable := range wantNullable {
		var isNullable string
		err := db.QueryRowContext(ctx, `
SELECT is_nullable FROM information_schema.columns
WHERE table_name = 'tenants' AND column_name = $1 AND table_schema = current_schema()`, column).Scan(&isNullable)
		if err != nil {
			t.Fatalf("column %q missing after migration 000009: %v", column, err)
		}
		if isNullable != nullable {
			t.Fatalf("column %q is_nullable = %q, want %q", column, isNullable, nullable)
		}
	}

	var defaultValue string
	if err := db.QueryRowContext(ctx, `
SELECT column_default FROM information_schema.columns
WHERE table_name = 'tenants' AND column_name = 'onboarding_status' AND table_schema = current_schema()`).Scan(&defaultValue); err != nil {
		t.Fatalf("reading onboarding_status default: %v", err)
	}
	if defaultValue == "" {
		t.Fatal("onboarding_status has no default; legacy rows would violate NOT NULL")
	}
}

// TestMigration000009PreservesExistingTenantRows proves a tenant created
// before this migration (i.e. under the schema Features 1-5 already
// established) survives it unchanged, and its onboarding_status defaults to
// COMPLETED rather than IN_PROGRESS — the compatibility strategy this
// feature's plan requires: a pre-existing tenant must not vanish from
// public visibility once a later feature gates on onboarding completion.
func TestMigration000009PreservesExistingTenantRows(t *testing.T) {
	db := openTenantTestDBAtMigration000008(t)
	ctx := context.Background()

	const tenantID = "550e8400-e29b-41d4-a716-446655440300"
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, slug, status, description) VALUES ($1, $2, $3, 'ACTIVE', $4)`,
		tenantID, "Pre-Existing Tenant", "pre-existing-tenant", "already launched"); err != nil {
		t.Fatalf("seeding pre-migration tenant: %v", err)
	}

	applyMigrationFile(t, db, "000009_add_business_type_and_onboarding_to_tenants.up.sql")
	// Scheduling S1 added tenants.currency, which every tenant SELECT now reads.
	applyMigrationFile(t, db, "000010_create_services_and_tenant_currency.up.sql")

	repo := NewPostgresTenantRepository(db)
	tenant, err := repo.FindByID(ctx, tenantID)
	if err != nil {
		t.Fatalf("FindByID() after migration = %v", err)
	}
	if tenant.Name != "Pre-Existing Tenant" || tenant.Slug != "pre-existing-tenant" || tenant.Status != model.StatusActive {
		t.Fatalf("pre-existing columns not preserved: %#v", tenant)
	}
	if tenant.Description == nil || *tenant.Description != "already launched" {
		t.Fatalf("Feature 4 column not preserved: %#v", tenant.Description)
	}
	if tenant.BusinessType != nil {
		t.Fatalf("BusinessType = %v, want nil for a pre-existing tenant", tenant.BusinessType)
	}
	if tenant.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want COMPLETED for a pre-existing tenant (not IN_PROGRESS)", tenant.OnboardingStatus)
	}
	if tenant.OnboardingStep != nil {
		t.Fatalf("OnboardingStep = %v, want nil for a pre-existing tenant", tenant.OnboardingStep)
	}
}

// TestMigration000009DownRemovesOnlyItsOwnColumns proves DOWN removes
// exactly what UP introduced and nothing from earlier migrations.
func TestMigration000009DownRemovesOnlyItsOwnColumns(t *testing.T) {
	db := openTenantTestDB(t)
	ctx := context.Background()

	down, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000009_add_business_type_and_onboarding_to_tenants.down.sql"))
	if err != nil {
		t.Fatalf("reading down migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(down)); err != nil {
		t.Fatalf("applying down migration: %v", err)
	}

	for _, column := range []string{"business_type", "onboarding_status", "onboarding_step"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'tenants' AND column_name = $1 AND table_schema = current_schema())`, column).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("down migration left F1 column %q in place", column)
		}
	}

	// Feature 1-4 columns must survive the down migration.
	for _, column := range []string{"id", "name", "slug", "status", "description", "contact_email", "contact_phone", "timezone"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'tenants' AND column_name = $1 AND table_schema = current_schema())`, column).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("down migration removed pre-F1 column %q", column)
		}
	}

	applyMigrationFile(t, db, "000009_add_business_type_and_onboarding_to_tenants.up.sql")
}

// --- Round-trip persistence -------------------------------------------------

func TestPostgresTenantRepositoryPersistsBusinessTypeAndOnboardingState(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	businessType := model.BusinessTypeHotel
	step := "room_types"
	tenant := &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440301", Name: "Hotel Co", Slug: "hotel-co", BusinessType: &businessType, OnboardingStep: &step}
	created, err := repo.Create(ctx, tenant)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.BusinessType == nil || *created.BusinessType != model.BusinessTypeHotel {
		t.Fatalf("Create() BusinessType = %v, want HOTEL", created.BusinessType)
	}
	if created.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("Create() OnboardingStatus = %q, want IN_PROGRESS (repository default for an unset value)", created.OnboardingStatus)
	}
	if created.OnboardingStep == nil || *created.OnboardingStep != "room_types" {
		t.Fatalf("Create() OnboardingStep = %v, want room_types", created.OnboardingStep)
	}

	byID, err := repo.FindByID(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	assertBusinessTypeAndOnboardingRoundTrip(t, byID)

	bySlug, err := repo.FindBySlug(ctx, "hotel-co")
	if err != nil {
		t.Fatalf("FindBySlug() error = %v", err)
	}
	assertBusinessTypeAndOnboardingRoundTrip(t, bySlug)
}

func assertBusinessTypeAndOnboardingRoundTrip(t *testing.T, tenant *model.Tenant) {
	t.Helper()
	if tenant.BusinessType == nil || *tenant.BusinessType != model.BusinessTypeHotel {
		t.Fatalf("BusinessType = %v, want HOTEL", tenant.BusinessType)
	}
	if tenant.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want IN_PROGRESS", tenant.OnboardingStatus)
	}
	if tenant.OnboardingStep == nil || *tenant.OnboardingStep != "room_types" {
		t.Fatalf("OnboardingStep = %v, want room_types", tenant.OnboardingStep)
	}
}

// TestPostgresTenantRepositoryScansLegacyNullBusinessTypeSafely proves a row
// with no business_type (a pre-F1 tenant, or any row inserted without one)
// scans without error and without panicking — this is the majority case for
// every tenant that exists before this feature ships, not an edge case.
func TestPostgresTenantRepositoryScansLegacyNullBusinessTypeSafely(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	const tenantID = "550e8400-e29b-41d4-a716-446655440302"
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		tenantID, "Legacy Tenant", "legacy-tenant"); err != nil {
		t.Fatalf("seeding legacy tenant: %v", err)
	}

	tenant, err := repo.FindByID(ctx, tenantID)
	if err != nil {
		t.Fatalf("FindByID() error = %v, want a safe nil-business_type scan, not an error", err)
	}
	if tenant.BusinessType != nil {
		t.Fatalf("BusinessType = %v, want nil", tenant.BusinessType)
	}
	if tenant.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want COMPLETED (column default) for a row inserted without one", tenant.OnboardingStatus)
	}
	if tenant.OnboardingStep != nil {
		t.Fatalf("OnboardingStep = %v, want nil", tenant.OnboardingStep)
	}
}

func TestPostgresTenantRepositoryListAccessibleByUserIDScansBusinessTypeAndOnboardingState(t *testing.T) {
	db := openTenantTestDBWithMemberships(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	userID := "550e8400-e29b-41d4-a716-446655440108"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		userID, "h@example.com", "hash"); err != nil {
		t.Fatal(err)
	}

	businessType := model.BusinessTypeRestaurant
	tenant, err := repo.Create(ctx, &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440125", Name: "Bistro", Slug: "bistro", BusinessType: &businessType})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_memberships (id, tenant_id, user_id, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		"550e8400-e29b-41d4-a716-446655440208", tenant.ID, userID); err != nil {
		t.Fatal(err)
	}

	tenants, err := repo.ListAccessibleByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("ListAccessibleByUserID() error = %v", err)
	}
	if len(tenants) != 1 {
		t.Fatalf("ListAccessibleByUserID() returned %d tenants, want 1", len(tenants))
	}
	if tenants[0].BusinessType == nil || *tenants[0].BusinessType != model.BusinessTypeRestaurant {
		t.Fatalf("BusinessType = %v, want RESTAURANT", tenants[0].BusinessType)
	}
	if tenants[0].OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want IN_PROGRESS", tenants[0].OnboardingStatus)
	}
}

// TestPostgresTenantRepositoryUpdateProfileNeverWritesBusinessType proves
// F1's immutability decision at the repository layer: UpdateProfile has no
// way to change business_type (TenantProfileUpdate has no field for it),
// and the tenant's business_type/onboarding state survive a profile update
// unchanged and are still read back accurately.
func TestPostgresTenantRepositoryUpdateProfileNeverWritesBusinessType(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	businessType := model.BusinessTypeTransport
	tenant, err := repo.Create(ctx, &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440303", Name: "Transit Co", Slug: "transit-co", BusinessType: &businessType})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	newName := "Transit Co Renamed"
	updated, err := repo.UpdateProfile(ctx, tenant.ID, TenantProfileUpdate{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.Name != "Transit Co Renamed" {
		t.Fatalf("Name = %q, want the update applied", updated.Name)
	}
	if updated.BusinessType == nil || *updated.BusinessType != model.BusinessTypeTransport {
		t.Fatalf("BusinessType = %v, want unchanged TRANSPORT after a profile update", updated.BusinessType)
	}
	if updated.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want unchanged IN_PROGRESS after a profile update", updated.OnboardingStatus)
	}
}

// openTenantTestDBAtMigration000008 mirrors openTenantTestDB but stops one
// migration short of this feature's own (000009), so a test can seed a row
// under the exact pre-F1 schema and apply 000009 itself to observe the
// transition, rather than starting from a database that already has it.
func openTenantTestDBAtMigration000008(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database is unavailable: %v", err)
	}
	// services references tenants (Scheduling S1), so it must go first.
	for _, table := range []string{"services", "tenants"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatalf("cleaning %s table: %v", table, err)
		}
	}
	for _, migration := range []string{"000003_create_tenants.up.sql", "000007_add_slug_to_tenants.up.sql", "000008_add_tenant_profile_fields.up.sql"} {
		applyMigrationFile(t, db, migration)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DROP TABLE IF EXISTS services")
		db.ExecContext(context.Background(), "DROP TABLE IF EXISTS tenants")
	})
	return db
}

func applyMigrationFile(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", name))
	if err != nil {
		t.Fatalf("reading migration %s: %v", name, err)
	}
	if _, err := db.ExecContext(context.Background(), string(contents)); err != nil {
		t.Fatalf("applying migration %s: %v", name, err)
	}
}
