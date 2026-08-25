package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

func TestCreateTenantProvisionsOwnerAtomically(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db, "owner@example.com")
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	tenant, err := svc.Create(ctx, CreateTenantInput{Name: "Acme Salon", Slug: "acme-salon", BusinessType: "NAIL_TECHNICIAN", CreatorUserID: userID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if tenant.Name != "Acme Salon" || tenant.Slug != "acme-salon" {
		t.Fatalf("tenant = %#v", tenant)
	}
	// F1: a successful creation persists the chosen business type and always
	// starts IN_PROGRESS — there is no COMPLETED path through Create.
	if tenant.BusinessType == nil || *tenant.BusinessType != model.BusinessTypeNailTechnician {
		t.Fatalf("BusinessType = %v, want NAIL_TECHNICIAN", tenant.BusinessType)
	}
	if tenant.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want IN_PROGRESS", tenant.OnboardingStatus)
	}

	var membershipStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM tenant_memberships WHERE tenant_id = $1 AND user_id = $2`, tenant.ID, userID).Scan(&membershipStatus); err != nil {
		t.Fatalf("membership row missing: %v", err)
	}
	if membershipStatus != "ACTIVE" {
		t.Fatalf("membership status = %q, want ACTIVE", membershipStatus)
	}

	var roleName string
	if err := db.QueryRowContext(ctx, `SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = $1 AND ur.tenant_id = $2`, userID, tenant.ID).Scan(&roleName); err != nil {
		t.Fatalf("role assignment row missing: %v", err)
	}
	if roleName != "BUSINESS_OWNER" {
		t.Fatalf("assigned role = %q, want BUSINESS_OWNER", roleName)
	}
}

func TestCreateTenantRollsBackOnDuplicateSlug(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db, "first@example.com")
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	if _, err := svc.Create(ctx, CreateTenantInput{Name: "First", Slug: "shared-slug", BusinessType: "NAIL_TECHNICIAN", CreatorUserID: userID}); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	secondUserID := insertTestUser(t, db, "second@example.com")
	_, err := svc.Create(ctx, CreateTenantInput{Name: "Second", Slug: "shared-slug", BusinessType: "NAIL_TECHNICIAN", CreatorUserID: secondUserID})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantSlugTaken {
		t.Fatalf("error = %v, want TENANT_SLUG_TAKEN", err)
	}

	assertNoProvisioningFor(t, db, secondUserID)
}

func TestCreateTenantRollsBackWhenMembershipValidationFails(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	// A well-formed but nonexistent creator UUID lets tenant insertion
	// succeed and then fails inside MembershipService.Create's creator
	// existence check — proving the just-inserted tenant is rolled back.
	nonexistentUserID := uuid.NewString()
	_, err := svc.Create(ctx, CreateTenantInput{Name: "Orphan", Slug: "orphan-slug", BusinessType: "NAIL_TECHNICIAN", CreatorUserID: nonexistentUserID})
	if err == nil {
		t.Fatal("Create() succeeded with nonexistent creator, want error")
	}

	var tenantCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tenants WHERE slug = $1`, "orphan-slug").Scan(&tenantCount); err != nil {
		t.Fatal(err)
	}
	if tenantCount != 0 {
		t.Fatalf("tenant row survived rollback: count = %d", tenantCount)
	}
	assertNoProvisioningFor(t, db, nonexistentUserID)
}

func TestCreateTenantRollsBackWhenBusinessOwnerRoleMissing(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db, "no-owner-role@example.com")
	if _, err := db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE name = 'BUSINESS_OWNER' AND scope = 'TENANT')`); err != nil {
		t.Fatalf("removing BUSINESS_OWNER role_permissions from isolated test schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM roles WHERE name = 'BUSINESS_OWNER' AND scope = 'TENANT'`); err != nil {
		t.Fatalf("removing BUSINESS_OWNER from isolated test schema: %v", err)
	}
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	_, err := svc.Create(ctx, CreateTenantInput{Name: "No Owner Role", Slug: "no-owner-role", BusinessType: "NAIL_TECHNICIAN", CreatorUserID: userID})
	if err == nil {
		t.Fatal("Create() succeeded despite missing BUSINESS_OWNER role, want error")
	}

	// Public/API-facing result: safely classified as an internal/system
	// failure, never as a caller-facing role lookup problem.
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeInternalError {
		t.Fatalf("error = %v, want top-level INTERNAL_ERROR", err)
	}
	mapped := apperrors.Map(err)
	if mapped.Status != http.StatusInternalServerError || mapped.Code != apperrors.CodeInternalError {
		t.Fatalf("Map() = %#v, want 500 INTERNAL_ERROR", mapped)
	}

	// Causality preserved: the original ROLE_NOT_FOUND is still reachable
	// by unwrapping, proving the chain was wrapped, not destroyed.
	var innerAppErr *apperrors.AppError
	if !errors.As(errors.Unwrap(appErr), &innerAppErr) || innerAppErr.Code != apperrors.CodeRoleNotFound {
		t.Fatalf("cause = %v, want preserved ROLE_NOT_FOUND cause", errors.Unwrap(appErr))
	}

	var tenantCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tenants WHERE slug = $1`, "no-owner-role").Scan(&tenantCount); err != nil {
		t.Fatal(err)
	}
	if tenantCount != 0 {
		t.Fatalf("tenant row survived rollback: count = %d", tenantCount)
	}
	assertNoProvisioningFor(t, db, userID)
}

func TestCreateTenantRollsBackOnContextCancellation(t *testing.T) {
	db := openTenantServiceTestDB(t)
	userID := insertTestUser(t, db, "cancelled@example.com")
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.Create(ctx, CreateTenantInput{Name: "Cancelled", Slug: "cancelled-slug", BusinessType: "NAIL_TECHNICIAN", CreatorUserID: userID})
	if err == nil {
		t.Fatal("Create() succeeded with a cancelled context, want error")
	}

	var tenantCount int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM tenants WHERE slug = $1`, "cancelled-slug").Scan(&tenantCount); err != nil {
		t.Fatal(err)
	}
	if tenantCount != 0 {
		t.Fatalf("tenant row survived cancelled-context rollback: count = %d", tenantCount)
	}
}

func assertNoProvisioningFor(t *testing.T, db *sql.DB, userID string) {
	t.Helper()
	var membershipCount int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM tenant_memberships WHERE user_id = $1`, userID).Scan(&membershipCount); err != nil {
		t.Fatal(err)
	}
	if membershipCount != 0 {
		t.Fatalf("membership row exists for %s despite rollback", userID)
	}
	var roleCount int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM user_roles WHERE user_id = $1`, userID).Scan(&roleCount); err != nil {
		t.Fatal(err)
	}
	if roleCount != 0 {
		t.Fatalf("role assignment row exists for %s despite rollback", userID)
	}
}

func insertTestUser(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, 'hash', 'ACTIVE')`, id, email); err != nil {
		t.Fatalf("inserting test user: %v", err)
	}
	return id
}

func openTenantServiceTestDB(t *testing.T) *sql.DB {
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
	for _, table := range []string{"user_roles", "role_permissions", "permissions", "roles", "tenant_memberships", "tenants", "users"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatalf("cleaning %s table: %v", table, err)
		}
	}
	migrationFiles := []string{
		"000001_create_users.up.sql",
		"000003_create_tenants.up.sql",
		"000007_add_slug_to_tenants.up.sql",
		"000008_add_tenant_profile_fields.up.sql",
		"000009_add_business_type_and_onboarding_to_tenants.up.sql",
		"000004_create_tenant_memberships.up.sql",
		"000005_create_roles_permissions.up.sql",
		"000006_seed_roles_permissions.up.sql",
	}
	for _, migration := range migrationFiles {
		contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("reading migration %s: %v", migration, err)
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("applying migration %s: %v", migration, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		for _, table := range []string{"user_roles", "role_permissions", "permissions", "roles", "tenant_memberships", "tenants", "users"} {
			_, _ = db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+table)
		}
	})
	return db
}
