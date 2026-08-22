package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

func TestPostgresTenantRepositoryCreateAndFindByID(t *testing.T) {
	db := openTenantTestDB(t)
	repository := NewPostgresTenantRepository(db)
	ctx := context.Background()

	tenant := &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440010", Name: "Salon", Slug: "salon"}
	created, err := repository.Create(ctx, tenant)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID != tenant.ID || created.Name != "Salon" || created.Slug != "salon" {
		t.Fatalf("Create() = %#v", created)
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Create() status = %q, want ACTIVE default", created.Status)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("Create() timestamps = %#v", created)
	}

	found, err := repository.FindByID(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.ID != tenant.ID || found.Name != "Salon" || found.Slug != "salon" || found.Status != model.StatusActive {
		t.Fatalf("FindByID() = %#v", found)
	}
}

func TestPostgresTenantRepositoryFindByIDReturnsNotFound(t *testing.T) {
	db := openTenantTestDB(t)
	repository := NewPostgresTenantRepository(db)

	_, err := repository.FindByID(context.Background(), "550e8400-e29b-41d4-a716-446655440099")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantNotFound {
		t.Fatalf("FindByID() error = %v, want TENANT_NOT_FOUND", err)
	}
}

func TestPostgresTenantRepositoryCreateRejectsDuplicateID(t *testing.T) {
	db := openTenantTestDB(t)
	repository := NewPostgresTenantRepository(db)
	ctx := context.Background()

	tenant := &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440011", Name: "First", Slug: "first-slug"}
	if _, err := repository.Create(ctx, tenant); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := repository.Create(ctx, &model.Tenant{ID: tenant.ID, Name: "Second", Slug: "second-slug"})
	if err == nil {
		t.Fatal("Create() with duplicate ID succeeded, want error")
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		t.Fatalf("duplicate ID must not map to a typed AppError (unexpected system failure), got code %q", appErr.Code)
	}
}

func TestPostgresTenantRepositoryCreateRejectsDuplicateSlug(t *testing.T) {
	db := openTenantTestDB(t)
	repository := NewPostgresTenantRepository(db)
	ctx := context.Background()

	tenant := &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440012", Name: "First", Slug: "shared-slug"}
	if _, err := repository.Create(ctx, tenant); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := repository.Create(ctx, &model.Tenant{ID: "550e8400-e29b-41d4-a716-446655440013", Name: "Second", Slug: "shared-slug"})
	if err == nil {
		t.Fatal("Create() with duplicate slug succeeded, want error")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantSlugTaken {
		t.Fatalf("duplicate slug error = %v, want TENANT_SLUG_TAKEN", err)
	}
}

func TestPostgresTenantRepositoryCreateRejectsEmptySlug(t *testing.T) {
	db := openTenantTestDB(t)
	repository := NewPostgresTenantRepository(db)

	_, err := db.ExecContext(context.Background(), `INSERT INTO tenants (id, name) VALUES ($1, $2)`, "550e8400-e29b-41d4-a716-446655440014", "No Slug")
	if err == nil {
		t.Fatal("inserting a tenant without a slug succeeded, want NOT NULL violation")
	}
	_ = repository
}

func openTenantTestDB(t *testing.T) *sql.DB {
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
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS tenants"); err != nil {
		t.Fatalf("cleaning tenants table: %v", err)
	}
	for _, migration := range []string{"000003_create_tenants.up.sql", "000007_add_slug_to_tenants.up.sql"} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("reading migration %s: %v", migration, err)
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("applying migration %s: %v", migration, err)
		}
	}
	t.Cleanup(func() { db.ExecContext(context.Background(), "DROP TABLE IF EXISTS tenants") })
	return db
}
