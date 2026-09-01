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

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

// These exercise the currency column added by migration 000010 through the
// repository, against the disposable Docker database.

// openTenantCurrencyTestDB applies the full migration chain rather than reusing
// openTenantTestDB, which stops at 000009 and builds only the tenants table.
// 000010 cannot be applied on its own on top of that: it also inserts permission
// and role_permission rows, so the RBAC tables from 000005/000006 must exist.
// A separate helper leaves every existing suite's fixture untouched.
//
// It refuses to run against the development database. This drops tables before
// rebuilding, so pointing TEST_DATABASE_URL at the dev DSN would destroy real
// data. Use docker-compose.test.yml (booking_test on port 5434), which is
// distinct from the dev database on 5433.
func openTenantCurrencyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	if parsed, err := url.Parse(databaseURL); err == nil {
		if name := strings.TrimPrefix(parsed.Path, "/"); name == "booking" {
			t.Fatalf("refusing to run destructive tests against the development database %q — point TEST_DATABASE_URL at the disposable Docker database (docker-compose.test.yml)", name)
		}
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

	tables := []string{"services", "user_roles", "role_permissions", "permissions", "roles", "tenant_memberships", "sessions", "tenants", "users"}
	drop := func() {
		for _, table := range tables {
			db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table+" CASCADE")
		}
	}
	drop()
	for _, migration := range []string{
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
	} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("reading migration %s: %v", migration, err)
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("applying migration %s: %v", migration, err)
		}
	}
	t.Cleanup(drop)
	return db
}

func TestNewTenantsAreCreatedWithoutACurrency(t *testing.T) {
	db := openTenantCurrencyTestDB(t)
	repo := NewPostgresTenantRepository(db)

	businessType := model.BusinessTypeNailTechnician
	created, err := repo.Create(context.Background(), &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655447001", Name: "Acme Nails", Slug: "acme-nails-currency", BusinessType: &businessType,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// Currency is never set at creation: a tenant declares it explicitly, and
	// nothing infers one on its behalf.
	if created.Currency != nil {
		t.Fatalf("Currency = %q on a newly created tenant, want NULL", *created.Currency)
	}
}

func TestSetCurrencyRoundTripsAndTouchesNothingElse(t *testing.T) {
	db := openTenantCurrencyTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	businessType := model.BusinessTypeNailTechnician
	created, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655447002", Name: "Acme Nails", Slug: "acme-nails-set", BusinessType: &businessType,
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := repo.SetCurrency(ctx, created.ID, "NGN")
	if err != nil {
		t.Fatalf("SetCurrency() error = %v", err)
	}
	if updated.Currency == nil || *updated.Currency != "NGN" {
		t.Fatalf("Currency = %v, want NGN", updated.Currency)
	}
	// Only currency may move. business_type, status, onboarding_status, name
	// and slug must all survive unchanged — the SET clause has no other column
	// to write, and this proves it.
	if updated.BusinessType == nil || *updated.BusinessType != model.BusinessTypeNailTechnician {
		t.Fatalf("BusinessType = %v, want unchanged", updated.BusinessType)
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want unchanged ACTIVE", updated.Status)
	}
	if updated.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want unchanged IN_PROGRESS", updated.OnboardingStatus)
	}
	if updated.Name != "Acme Nails" || updated.Slug != "acme-nails-set" {
		t.Fatalf("identity changed: name=%q slug=%q", updated.Name, updated.Slug)
	}

	// It survives a re-read, not only the RETURNING clause.
	reread, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Currency == nil || *reread.Currency != "NGN" {
		t.Fatalf("re-read Currency = %v, want NGN", reread.Currency)
	}
}

func TestSetCurrencyReportsNotFoundForAnUnknownTenant(t *testing.T) {
	db := openTenantCurrencyTestDB(t)
	repo := NewPostgresTenantRepository(db)

	_, err := repo.SetCurrency(context.Background(), "550e8400-e29b-41d4-a716-446655449999", "NGN")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantNotFound {
		t.Fatalf("SetCurrency() error = %v, want TENANT_NOT_FOUND", err)
	}
}

// UpdateProfile must not be a second route to the currency column. The
// protection is structural — TenantProfileUpdate has no currency field — and
// this proves the column genuinely survives a profile write.
func TestUpdateProfileLeavesCurrencyUntouched(t *testing.T) {
	db := openTenantCurrencyTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	businessType := model.BusinessTypeNailTechnician
	created, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655447003", Name: "Acme Nails", Slug: "acme-nails-profile", BusinessType: &businessType,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetCurrency(ctx, created.ID, "NGN"); err != nil {
		t.Fatal(err)
	}

	name := "Acme Nails Renamed"
	timezone := "Africa/Lagos"
	updated, err := repo.UpdateProfile(ctx, created.ID, TenantProfileUpdate{Name: &name, Timezone: &timezone})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.Name != name {
		t.Fatalf("Name = %q, want the profile update applied", updated.Name)
	}
	if updated.Currency == nil || *updated.Currency != "NGN" {
		t.Fatalf("Currency = %v after a profile update, want unchanged NGN", updated.Currency)
	}
}

// Every tenant-reading path must return the currency, not just the one that
// wrote it — the column list and the scan order are shared, and this catches a
// drift between them.
func TestEveryTenantReadPathReturnsCurrency(t *testing.T) {
	db := openTenantCurrencyTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	businessType := model.BusinessTypeNailTechnician
	created, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655447004", Name: "Acme Nails", Slug: "acme-nails-reads", BusinessType: &businessType,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetCurrency(ctx, created.ID, "GBP"); err != nil {
		t.Fatal(err)
	}

	byID, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if byID.Currency == nil || *byID.Currency != "GBP" {
		t.Fatalf("FindByID Currency = %v, want GBP", byID.Currency)
	}

	bySlug, err := repo.FindBySlug(ctx, "acme-nails-reads")
	if err != nil {
		t.Fatal(err)
	}
	if bySlug.Currency == nil || *bySlug.Currency != "GBP" {
		t.Fatalf("FindBySlug Currency = %v, want GBP", bySlug.Currency)
	}
}
