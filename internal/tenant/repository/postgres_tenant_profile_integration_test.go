package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

func stringPtr(value string) *string { return &value }

// seedProfileTenant inserts a baseline tenant with no profile fields set,
// mirroring a tenant freshly created by Feature 2.
func seedProfileTenant(t *testing.T, repo *PostgresTenantRepository, id, name, slug string) *model.Tenant {
	t.Helper()
	created, err := repo.Create(context.Background(), &model.Tenant{ID: id, Name: name, Slug: slug})
	if err != nil {
		t.Fatalf("seeding tenant: %v", err)
	}
	return created
}

// --- Migration 000008 schema contract ---------------------------------------

func TestMigration000008AddsNullableProfileColumns(t *testing.T) {
	db := openTenantTestDB(t)
	ctx := context.Background()

	want := map[string]string{
		"description":   "YES",
		"contact_email": "YES",
		"contact_phone": "YES",
		"timezone":      "YES",
	}
	for column, wantNullable := range want {
		var isNullable string
		err := db.QueryRowContext(ctx, `
SELECT is_nullable FROM information_schema.columns
WHERE table_name = 'tenants' AND column_name = $1 AND table_schema = current_schema()`, column).Scan(&isNullable)
		if err != nil {
			t.Fatalf("column %q missing after migration 000008: %v", column, err)
		}
		if isNullable != wantNullable {
			t.Fatalf("column %q is_nullable = %q, want %q", column, isNullable, wantNullable)
		}
	}
}

func TestMigration000008PreservesTenantCreationWithoutProfileFields(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)

	created, err := repo.Create(context.Background(), &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655440801", Name: "No Profile", Slug: "no-profile",
	})
	if err != nil {
		t.Fatalf("Create() error = %v; tenant creation must not require Feature 4 fields", err)
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Create() status = %q, want ACTIVE", created.Status)
	}

	found, err := repo.FindByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.Description != nil || found.ContactEmail != nil || found.ContactPhone != nil || found.Timezone != nil {
		t.Fatalf("new tenant profile fields = %#v, want all nil", found)
	}
}

func TestMigration000008DownRemovesOnlyProfileColumns(t *testing.T) {
	db := openTenantTestDB(t)
	ctx := context.Background()

	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000008_add_tenant_profile_fields.down.sql"))
	if err != nil {
		t.Fatalf("reading down migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(contents)); err != nil {
		t.Fatalf("applying down migration: %v", err)
	}

	for _, column := range []string{"description", "contact_email", "contact_phone", "timezone"} {
		var count int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM information_schema.columns
WHERE table_name = 'tenants' AND column_name = $1 AND table_schema = current_schema()`, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("down migration left Feature 4 column %q in place", column)
		}
	}

	// Feature 1-3 columns must survive the down migration.
	for _, column := range []string{"id", "name", "slug", "status", "created_at", "updated_at"} {
		var count int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM information_schema.columns
WHERE table_name = 'tenants' AND column_name = $1 AND table_schema = current_schema()`, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("down migration removed pre-Feature-4 column %q", column)
		}
	}

	// Restore the schema so the shared cleanup path behaves normally.
	up, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000008_add_tenant_profile_fields.up.sql"))
	if err != nil {
		t.Fatalf("reading up migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(up)); err != nil {
		t.Fatalf("restoring up migration: %v", err)
	}
}

// --- UpdateProfile behaviour -------------------------------------------------

func TestUpdateProfileUpdatesSingleFieldAndLeavesOthersUnchanged(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	original := seedProfileTenant(t, repo, "550e8400-e29b-41d4-a716-446655440810", "Acme Salon", "acme-salon")

	updated, err := repo.UpdateProfile(ctx, original.ID, TenantProfileUpdate{
		Description: stringPtr("Best salon in town"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.Description == nil || *updated.Description != "Best salon in town" {
		t.Fatalf("Description = %v, want %q", updated.Description, "Best salon in town")
	}
	if updated.Name != "Acme Salon" {
		t.Fatalf("Name = %q, want unchanged %q", updated.Name, "Acme Salon")
	}
	if updated.ContactEmail != nil || updated.ContactPhone != nil || updated.Timezone != nil {
		t.Fatalf("omitted fields mutated: %#v", updated)
	}
}

func TestUpdateProfileUpdatesMultipleFields(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	original := seedProfileTenant(t, repo, "550e8400-e29b-41d4-a716-446655440811", "Acme Salon", "acme-multi")

	updated, err := repo.UpdateProfile(ctx, original.ID, TenantProfileUpdate{
		ContactEmail: stringPtr("hello@acme.test"),
		ContactPhone: stringPtr("+2348012345678"),
		Timezone:     stringPtr("Africa/Lagos"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.ContactEmail == nil || *updated.ContactEmail != "hello@acme.test" {
		t.Fatalf("ContactEmail = %v", updated.ContactEmail)
	}
	if updated.ContactPhone == nil || *updated.ContactPhone != "+2348012345678" {
		t.Fatalf("ContactPhone = %v", updated.ContactPhone)
	}
	if updated.Timezone == nil || *updated.Timezone != "Africa/Lagos" {
		t.Fatalf("Timezone = %v", updated.Timezone)
	}
	if updated.Description != nil {
		t.Fatalf("Description = %v, want nil (omitted)", updated.Description)
	}
}

// Mandatory Feature 4 domain/security invariant: renaming the business must
// never regenerate or mutate the slug (slug is Feature 5 territory).
func TestUpdateProfileNameChangeDoesNotChangeSlug(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	original := seedProfileTenant(t, repo, "550e8400-e29b-41d4-a716-446655440812", "Acme Salon", "acme-salon-slug")

	updated, err := repo.UpdateProfile(ctx, original.ID, TenantProfileUpdate{
		Name: stringPtr("Acme Beauty Studio"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.Name != "Acme Beauty Studio" {
		t.Fatalf("Name = %q, want %q", updated.Name, "Acme Beauty Studio")
	}
	if updated.Slug != "acme-salon-slug" {
		t.Fatalf("Slug = %q, want unchanged %q", updated.Slug, "acme-salon-slug")
	}

	// Confirm persisted state, not just the RETURNING projection.
	var slug string
	if err := db.QueryRowContext(ctx, `SELECT slug FROM tenants WHERE id = $1`, original.ID).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	if slug != "acme-salon-slug" {
		t.Fatalf("persisted slug = %q, want unchanged", slug)
	}
}

func TestUpdateProfileLeavesProtectedFieldsUnchanged(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	original := seedProfileTenant(t, repo, "550e8400-e29b-41d4-a716-446655440813", "Acme Salon", "acme-protected")

	updated, err := repo.UpdateProfile(ctx, original.ID, TenantProfileUpdate{
		Name:        stringPtr("Renamed"),
		Description: stringPtr("desc"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.ID != original.ID {
		t.Fatalf("ID = %q, want unchanged %q", updated.ID, original.ID)
	}
	if updated.Slug != original.Slug {
		t.Fatalf("Slug = %q, want unchanged %q", updated.Slug, original.Slug)
	}
	if updated.Status != original.Status {
		t.Fatalf("Status = %q, want unchanged %q", updated.Status, original.Status)
	}
	if !updated.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want unchanged %v", updated.CreatedAt, original.CreatedAt)
	}
}

func TestUpdateProfileAdvancesUpdatedAt(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	original := seedProfileTenant(t, repo, "550e8400-e29b-41d4-a716-446655440814", "Acme Salon", "acme-updated-at")

	// CURRENT_TIMESTAMP is transaction-start time; a short pause keeps the
	// assertion meaningful without depending on sub-millisecond resolution.
	time.Sleep(10 * time.Millisecond)

	updated, err := repo.UpdateProfile(ctx, original.ID, TenantProfileUpdate{Name: stringPtr("Renamed")})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want after %v", updated.UpdatedAt, original.UpdatedAt)
	}
	if !updated.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("CreatedAt changed during update")
	}
}

func TestUpdateProfileReturnsNotFoundForMissingTenant(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)

	_, err := repo.UpdateProfile(context.Background(), "550e8400-e29b-41d4-a716-446655440899", TenantProfileUpdate{
		Name: stringPtr("Ghost"),
	})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantNotFound {
		t.Fatalf("UpdateProfile() error = %v, want TENANT_NOT_FOUND", err)
	}
}

func TestUpdateProfileRespectsCancelledContext(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)

	original := seedProfileTenant(t, repo, "550e8400-e29b-41d4-a716-446655440815", "Acme Salon", "acme-cancelled")

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.UpdateProfile(cancelledCtx, original.ID, TenantProfileUpdate{Name: stringPtr("Should Not Persist")}); err == nil {
		t.Fatal("UpdateProfile() with cancelled context returned no error")
	}

	found, err := repo.FindByID(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Name != "Acme Salon" {
		t.Fatalf("Name = %q, want unchanged after cancelled update", found.Name)
	}
}

// Values are parameterized, so SQL metacharacters are stored as literal data.
func TestUpdateProfileStoresSQLMetacharactersAsData(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	original := seedProfileTenant(t, repo, "550e8400-e29b-41d4-a716-446655440816", "Acme Salon", "acme-injection")

	payload := "'; DROP TABLE tenants; --"
	updated, err := repo.UpdateProfile(ctx, original.ID, TenantProfileUpdate{Description: stringPtr(payload)})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.Description == nil || *updated.Description != payload {
		t.Fatalf("Description = %v, want literal payload", updated.Description)
	}

	// The table must still exist and still hold the row.
	found, err := repo.FindByID(ctx, original.ID)
	if err != nil {
		t.Fatalf("tenants table damaged by injection payload: %v", err)
	}
	if found.Description == nil || !strings.Contains(*found.Description, "DROP TABLE") {
		t.Fatalf("payload not stored verbatim: %v", found.Description)
	}
}
