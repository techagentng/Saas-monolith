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

func TestPostgresTenantRepositoryListAccessibleByUserIDReturnsActiveMemberships(t *testing.T) {
	db := openTenantTestDBWithMemberships(t)
	tenantRepo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	// Create test data: User A with 2 tenants, User C with 1 tenant
	userAID := "550e8400-e29b-41d4-a716-446655440100"
	userCID := "550e8400-e29b-41d4-a716-446655440103"
	tenantAID := "550e8400-e29b-41d4-a716-446655440110"
	tenantBID := "550e8400-e29b-41d4-a716-446655440111"
	tenantCID := "550e8400-e29b-41d4-a716-446655440112"

	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, $3, 'ACTIVE'), ($4, $5, $6, 'ACTIVE')`,
		userAID, "a@example.com", "hash",
		userCID, "c@example.com", "hash"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'ACTIVE'), ($4, $5, $6, 'ACTIVE'), ($7, $8, $9, 'ACTIVE')`,
		tenantAID, "Tenant A", "tenant-a",
		tenantBID, "Tenant B", "tenant-b",
		tenantCID, "Tenant C", "tenant-c"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_memberships (id, tenant_id, user_id, status) VALUES ($1, $2, $3, 'ACTIVE'), ($4, $5, $6, 'ACTIVE'), ($7, $8, $9, 'ACTIVE')`,
		"550e8400-e29b-41d4-a716-446655440200", tenantAID, userAID,
		"550e8400-e29b-41d4-a716-446655440201", tenantBID, userAID,
		"550e8400-e29b-41d4-a716-446655440202", tenantCID, userCID); err != nil {
		t.Fatal(err)
	}

	tenants, err := tenantRepo.ListAccessibleByUserID(ctx, userAID)
	if err != nil {
		t.Fatalf("ListAccessibleByUserID() error = %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("ListAccessibleByUserID() returned %d tenants, want 2", len(tenants))
	}
	if tenants[0].ID != tenantAID || tenants[1].ID != tenantBID {
		t.Fatalf("ListAccessibleByUserID() returned incorrect tenants: %v", tenants)
	}
	if tenants[0].Name != "Tenant A" || tenants[1].Name != "Tenant B" {
		t.Fatalf("ListAccessibleByUserID() returned incorrect tenant names")
	}
}

func TestPostgresTenantRepositoryListAccessibleExcludesDisabledMemberships(t *testing.T) {
	db := openTenantTestDBWithMemberships(t)
	tenantRepo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	userID := "550e8400-e29b-41d4-a716-446655440104"
	tenantID := "550e8400-e29b-41d4-a716-446655440120"

	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		userID, "d@example.com", "hash"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		tenantID, "Disabled Membership", "disabled-membership"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_memberships (id, tenant_id, user_id, status) VALUES ($1, $2, $3, 'DISABLED')`,
		"550e8400-e29b-41d4-a716-446655440203", tenantID, userID); err != nil {
		t.Fatal(err)
	}

	tenants, err := tenantRepo.ListAccessibleByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("ListAccessibleByUserID() error = %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("ListAccessibleByUserID() returned %d tenants for user with DISABLED membership, want 0", len(tenants))
	}
}

func TestPostgresTenantRepositoryListAccessibleExcludesDisabledTenants(t *testing.T) {
	db := openTenantTestDBWithMemberships(t)
	tenantRepo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	userID := "550e8400-e29b-41d4-a716-446655440105"
	tenantID := "550e8400-e29b-41d4-a716-446655440121"

	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		userID, "e@example.com", "hash"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'DISABLED')`,
		tenantID, "Disabled Tenant", "disabled-tenant"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_memberships (id, tenant_id, user_id, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		"550e8400-e29b-41d4-a716-446655440204", tenantID, userID); err != nil {
		t.Fatal(err)
	}

	tenants, err := tenantRepo.ListAccessibleByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("ListAccessibleByUserID() error = %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("ListAccessibleByUserID() returned %d tenants for DISABLED tenant, want 0", len(tenants))
	}
}

func TestPostgresTenantRepositoryListAccessibleReturnsEmptyForNoMemberships(t *testing.T) {
	db := openTenantTestDBWithMemberships(t)
	tenantRepo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	userID := "550e8400-e29b-41d4-a716-446655440106"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		userID, "f@example.com", "hash"); err != nil {
		t.Fatal(err)
	}

	tenants, err := tenantRepo.ListAccessibleByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("ListAccessibleByUserID() error = %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("ListAccessibleByUserID() returned %d tenants, want 0 for user with no memberships", len(tenants))
	}
}

func TestPostgresTenantRepositoryListAccessibleDeterministicOrdering(t *testing.T) {
	db := openTenantTestDBWithMemberships(t)
	tenantRepo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	userID := "550e8400-e29b-41d4-a716-446655440107"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, $3, 'ACTIVE')`,
		userID, "g@example.com", "hash"); err != nil {
		t.Fatal(err)
	}

	// Create tenants in non-alphabetical order but with known created_at ordering
	tenantID2 := "550e8400-e29b-41d4-a716-446655440122"
	tenantID1 := "550e8400-e29b-41d4-a716-446655440123"
	tenantID3 := "550e8400-e29b-41d4-a716-446655440124"

	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, slug, status, created_at) VALUES
		($1, $2, $3, 'ACTIVE', NOW()),
		($4, $5, $6, 'ACTIVE', NOW() + INTERVAL '1 second'),
		($7, $8, $9, 'ACTIVE', NOW() + INTERVAL '2 seconds')`,
		tenantID2, "Z Tenant", "z-tenant",
		tenantID1, "A Tenant", "a-tenant",
		tenantID3, "M Tenant", "m-tenant"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO tenant_memberships (id, tenant_id, user_id, status) VALUES
		($1, $2, $3, 'ACTIVE'), ($4, $5, $6, 'ACTIVE'), ($7, $8, $9, 'ACTIVE')`,
		"550e8400-e29b-41d4-a716-446655440205", tenantID2, userID,
		"550e8400-e29b-41d4-a716-446655440206", tenantID1, userID,
		"550e8400-e29b-41d4-a716-446655440207", tenantID3, userID); err != nil {
		t.Fatal(err)
	}

	tenants, err := tenantRepo.ListAccessibleByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("ListAccessibleByUserID() error = %v", err)
	}
	if len(tenants) != 3 {
		t.Fatalf("expected 3 tenants, got %d", len(tenants))
	}
	// Should be ordered by created_at
	if tenants[0].ID != tenantID2 || tenants[1].ID != tenantID1 || tenants[2].ID != tenantID3 {
		t.Fatalf("ListAccessibleByUserID() not ordered correctly: got IDs %v", []string{tenants[0].ID, tenants[1].ID, tenants[2].ID})
	}
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

func openTenantTestDBWithMemberships(t *testing.T) *sql.DB {
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
	for _, table := range []string{"tenant_memberships", "tenants", "users"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatalf("cleaning %s table: %v", table, err)
		}
	}
	for _, migration := range []string{"000001_create_users.up.sql", "000003_create_tenants.up.sql", "000004_create_tenant_memberships.up.sql", "000007_add_slug_to_tenants.up.sql"} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("reading migration %s: %v", migration, err)
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("applying migration %s: %v", migration, err)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"tenant_memberships", "tenants", "users"} {
			db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
		}
	})
	return db
}
