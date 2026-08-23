package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// assertCreationLeftNothing proves a rejected creation wrote nothing to any of
// the three tables Feature 2 touches. Because slug validation runs before
// BeginTx, none of these rows should ever have existed.
func assertCreationLeftNothing(t *testing.T, db *sql.DB, ctx context.Context, userID string) {
	t.Helper()

	var tenants int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tenants`).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if tenants != 0 {
		t.Fatalf("tenant rows = %d, want 0 after rejected creations", tenants)
	}

	var memberships int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tenant_memberships WHERE user_id = $1`, userID).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if memberships != 0 {
		t.Fatalf("membership rows = %d, want 0 after rejected creations", memberships)
	}

	var assignments int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_roles WHERE user_id = $1`, userID).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if assignments != 0 {
		t.Fatalf("role assignment rows = %d, want 0 after rejected creations", assignments)
	}
}

// Feature 5 rejects a bad slug before BEGIN, so creation must leave no tenant,
// no membership, and no role assignment behind.
func TestCreateTenantWithInvalidSlugCreatesNothing(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db, "invalid-slug@example.com")
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	for _, slug := range []string{"Acme Salon", "acme_salon", "-acme", "acme-", "ac", ""} {
		_, err := svc.Create(ctx, CreateTenantInput{Name: "Acme Salon", Slug: slug, CreatorUserID: userID})

		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantSlugInvalid {
			t.Fatalf("Create(slug=%q) error = %v, want TENANT_SLUG_INVALID", slug, err)
		}
	}

	assertCreationLeftNothing(t, db, ctx, userID)
}

// A reserved platform name is refused for the same reason and just as early.
func TestCreateTenantWithReservedSlugCreatesNothing(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db, "reserved-slug@example.com")
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	for _, slug := range []string{"admin", "api", "login", "dashboard", "auth", "book", "settings"} {
		_, err := svc.Create(ctx, CreateTenantInput{Name: "Acme Salon", Slug: slug, CreatorUserID: userID})

		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantSlugInvalid {
			t.Fatalf("Create(slug=%q) error = %v, want TENANT_SLUG_INVALID", slug, err)
		}
	}

	assertCreationLeftNothing(t, db, ctx, userID)
}

// A valid slug still provisions the full Feature 2 unit atomically, and the
// resulting tenant is immediately resolvable through the Feature 5 lookup.
func TestCreateTenantWithValidSlugRemainsAtomicAndPubliclyResolvable(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, db, "valid-slug@example.com")
	tenants := repository.NewPostgresTenantRepository(db)
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), tenants)

	tenant, err := svc.Create(ctx, CreateTenantInput{Name: "Acme Beauty Studio", Slug: "acme-beauty-studio", CreatorUserID: userID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if tenant.Slug != "acme-beauty-studio" {
		t.Fatalf("slug = %q, want stored verbatim", tenant.Slug)
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

	identity, err := NewPublicTenantService(tenants).GetBySlug(ctx, "acme-beauty-studio")
	if err != nil {
		t.Fatalf("public lookup after creation error = %v", err)
	}
	if identity.Name != "Acme Beauty Studio" || identity.Slug != "acme-beauty-studio" {
		t.Fatalf("public identity = %#v", identity)
	}
}

// A duplicate slug remains a 409-class business outcome, unchanged by Feature 5.
func TestCreateTenantDuplicateSlugStillReportsSlugTaken(t *testing.T) {
	db := openTenantServiceTestDB(t)
	ctx := context.Background()
	firstUser := insertTestUser(t, db, "dup-first@example.com")
	secondUser := insertTestUser(t, db, "dup-second@example.com")
	svc := NewTenantService(db, identityrepository.NewPostgresUserRepository(db), repository.NewPostgresTenantRepository(db))

	if _, err := svc.Create(ctx, CreateTenantInput{Name: "First", Slug: "taken-slug", CreatorUserID: firstUser}); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	_, err := svc.Create(ctx, CreateTenantInput{Name: "Second", Slug: "taken-slug", CreatorUserID: secondUser})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantSlugTaken {
		t.Fatalf("duplicate slug error = %v, want TENANT_SLUG_TAKEN", err)
	}

	// The losing attempt must not have left a membership or role behind.
	var memberships int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tenant_memberships WHERE user_id = $1`, secondUser).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if memberships != 0 {
		t.Fatalf("membership rows for losing creator = %d, want 0", memberships)
	}
}
