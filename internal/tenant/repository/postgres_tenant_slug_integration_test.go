package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

func TestFindBySlugReturnsTenantForExactSlug(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441201", Name: "Acme Salon", Slug: "acme-salon-lookup",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	found, err := repo.FindBySlug(ctx, "acme-salon-lookup")
	if err != nil {
		t.Fatalf("FindBySlug() error = %v", err)
	}
	if found.ID != created.ID || found.Slug != "acme-salon-lookup" || found.Name != "Acme Salon" {
		t.Fatalf("FindBySlug() = %#v", found)
	}
	if found.Status != model.StatusActive {
		t.Fatalf("Status = %q", found.Status)
	}
}

func TestFindBySlugReturnsNotFoundForMissingSlug(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)

	_, err := repo.FindBySlug(context.Background(), "no-such-slug")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantNotFound {
		t.Fatalf("FindBySlug() error = %v, want TENANT_NOT_FOUND", err)
	}
}

// Lookup is exact: the repository performs no case-folding or trimming, so a
// non-canonical spelling of a stored slug simply does not match.
func TestFindBySlugIsExactAndCaseSensitive(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441202", Name: "Acme", Slug: "acme-exact",
	}); err != nil {
		t.Fatal(err)
	}

	for _, variant := range []string{"ACME-EXACT", "Acme-Exact", " acme-exact", "acme-exact "} {
		if _, err := repo.FindBySlug(ctx, variant); err == nil {
			t.Fatalf("FindBySlug(%q) matched; lookup must be exact", variant)
		}
	}
}

// The slug reaches the database byte-for-byte; creation must not rewrite it.
func TestCreatePersistsSlugVerbatim(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441203", Name: "Acme Beauty Studio", Slug: "acme-beauty",
	}); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := db.QueryRowContext(ctx, `SELECT slug FROM tenants WHERE id = $1`, "550e8400-e29b-41d4-a716-446655441203").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "acme-beauty" {
		t.Fatalf("stored slug = %q, want verbatim %q", stored, "acme-beauty")
	}
}

// Feature 5 keeps the slug immutable: the Feature 4 profile update input has
// no slug field, so no caller can express a slug change through it.
func TestTenantProfileUpdateCannotExpressSlugChange(t *testing.T) {
	structType := reflect.TypeOf(TenantProfileUpdate{})
	for i := 0; i < structType.NumField(); i++ {
		if structType.Field(i).Name == "Slug" {
			t.Fatal("TenantProfileUpdate exposes a Slug field; the slug must stay immutable in Feature 5")
		}
	}
}

// A profile update must leave the slug untouched even as the name changes.
func TestUpdateProfileNeverAltersSlug(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441204", Name: "Acme Salon", Slug: "acme-immutable",
	})
	if err != nil {
		t.Fatal(err)
	}

	newName := "Acme Beauty Studio"
	updated, err := repo.UpdateProfile(ctx, created.ID, TenantProfileUpdate{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("Name = %q", updated.Name)
	}
	if updated.Slug != "acme-immutable" {
		t.Fatalf("Slug = %q, want unchanged", updated.Slug)
	}

	// The slug must still resolve to the same tenant after the rename.
	found, err := repo.FindBySlug(ctx, "acme-immutable")
	if err != nil {
		t.Fatalf("FindBySlug() after rename error = %v", err)
	}
	if found.ID != created.ID || found.Name != newName {
		t.Fatalf("post-rename lookup = %#v", found)
	}
}

func TestFindBySlugRespectsCancelledContext(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.FindBySlug(cancelled, "acme-salon"); err == nil {
		t.Fatal("FindBySlug() with cancelled context returned no error")
	}
}

// Slug uniqueness (Feature 1/2) must survive the Feature 5 changes.
func TestSlugUniquenessStillEnforced(t *testing.T) {
	db := openTenantTestDB(t)
	repo := NewPostgresTenantRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441205", Name: "First", Slug: "duplicate-slug",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := repo.Create(ctx, &model.Tenant{
		ID: "550e8400-e29b-41d4-a716-446655441206", Name: "Second", Slug: "duplicate-slug",
	})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantSlugTaken {
		t.Fatalf("duplicate slug error = %v, want TENANT_SLUG_TAKEN", err)
	}
}
