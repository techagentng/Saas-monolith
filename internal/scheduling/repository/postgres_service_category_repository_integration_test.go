package repository

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

func newCategory(id string, tenantID string, name string) *model.ServiceCategory {
	return &model.ServiceCategory{ID: id, TenantID: tenantID, Name: name}
}

func assertCategoryNotFound(t *testing.T, err error, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeCategoryNotFound {
		t.Fatalf("%s: error = %v, want CATEGORY_NOT_FOUND", context, err)
	}
}

// --- round trip --------------------------------------------------------------

func TestCategoryCreateRoundTripsAndDefaultsToActive(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a", &currency)

	created, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447101", integrationTenantA, "Pedicures"))
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
	if found.Name != "Pedicures" || found.SortOrder != 0 {
		t.Fatalf("round trip lost data: %+v", found)
	}
}

// --- tenant isolation --------------------------------------------------------

func TestCategoryFindByIDDoesNotCrossTenants(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a2", &currency)
	seedTenant(t, db, integrationTenantB, "category-tenant-b2", &currency)

	created, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447102", integrationTenantA, "Pedicures"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.FindByID(ctx, integrationTenantB, created.ID)
	assertCategoryNotFound(t, err, "FindByID(tenant B, tenant A's category)")
}

func TestCategoryUpdateAndArchiveDoNotCrossTenants(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a3", &currency)
	seedTenant(t, db, integrationTenantB, "category-tenant-b3", &currency)

	created, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447103", integrationTenantA, "Pedicures"))
	if err != nil {
		t.Fatal(err)
	}

	hijacked := "Hijacked"
	_, err = repo.Update(ctx, integrationTenantB, created.ID, ServiceCategoryUpdate{Name: &hijacked})
	assertCategoryNotFound(t, err, "Update(cross-tenant)")

	_, err = repo.Archive(ctx, integrationTenantB, created.ID)
	assertCategoryNotFound(t, err, "Archive(cross-tenant)")

	unchanged, err := repo.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Name != "Pedicures" || unchanged.Status != model.StatusActive {
		t.Fatalf("a cross-tenant write mutated the row: %+v", unchanged)
	}
}

// --- listing and status filtering --------------------------------------------

// Archived categories excluded from normal (default/ACTIVE) lists — the
// explicit SC1 requirement, proven against the real partial index and query.
func TestCategoryListFilterSeparatesActiveFromArchived(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a4", &currency)

	active, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447104", integrationTenantA, "Pedicures"))
	if err != nil {
		t.Fatal(err)
	}
	retired, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447105", integrationTenantA, "Old Promo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Archive(ctx, integrationTenantA, retired.ID); err != nil {
		t.Fatal(err)
	}

	activeStatus := model.StatusActive
	activeOnly, err := repo.ListByTenant(ctx, integrationTenantA, ServiceCategoryListFilter{Status: &activeStatus})
	if err != nil {
		t.Fatal(err)
	}
	if len(activeOnly) != 1 || activeOnly[0].ID != active.ID {
		t.Fatalf("ACTIVE filter returned %d rows, want just the active one", len(activeOnly))
	}

	archivedStatus := model.StatusArchived
	archivedOnly, err := repo.ListByTenant(ctx, integrationTenantA, ServiceCategoryListFilter{Status: &archivedStatus})
	if err != nil {
		t.Fatal(err)
	}
	if len(archivedOnly) != 1 || archivedOnly[0].ID != retired.ID {
		t.Fatalf("ARCHIVED filter returned %d rows, want just the archived one", len(archivedOnly))
	}

	all, err := repo.ListByTenant(ctx, integrationTenantA, ServiceCategoryListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered listing returned %d rows, want 2", len(all))
	}
}

func TestCategoryListOrdersBySortOrderThenName(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a5", &currency)

	zebra := newCategory("550e8400-e29b-41d4-a716-446655447106", integrationTenantA, "Zebra Nails")
	zebra.SortOrder = 0
	apple := newCategory("550e8400-e29b-41d4-a716-446655447107", integrationTenantA, "Apple Care")
	apple.SortOrder = 0
	first := newCategory("550e8400-e29b-41d4-a716-446655447108", integrationTenantA, "Always First")
	first.SortOrder = -1
	for _, category := range []*model.ServiceCategory{zebra, apple, first} {
		if _, err := repo.Create(ctx, category); err != nil {
			t.Fatal(err)
		}
	}

	result, err := repo.ListByTenant(ctx, integrationTenantA, ServiceCategoryListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Always First", "Apple Care", "Zebra Nails"}
	if len(result) != len(want) {
		t.Fatalf("returned %d categories, want %d", len(result), len(want))
	}
	for i, category := range result {
		if category.Name != want[i] {
			t.Fatalf("position %d = %q, want %q (sort_order then name)", i, category.Name, want[i])
		}
	}
}

// --- name uniqueness (partial index) -----------------------------------------

func TestCategoryCreateRejectsDuplicateActiveName(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a6", &currency)

	if _, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447109", integrationTenantA, "Pedicures")); err != nil {
		t.Fatal(err)
	}

	_, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447110", integrationTenantA, "Pedicures"))
	if err == nil {
		t.Fatal("Create() accepted a duplicate ACTIVE category name")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("error = %v, want VALIDATION_FAILED", err)
	}
}

func TestCategoryNameIsUniquePerTenantOnly(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a7", &currency)
	seedTenant(t, db, integrationTenantB, "category-tenant-b7", &currency)

	if _, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447111", integrationTenantA, "Pedicures")); err != nil {
		t.Fatal(err)
	}
	// The identical name under a different tenant is unrelated and must
	// succeed — the unique index is scoped by tenant_id.
	if _, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447112", integrationTenantB, "Pedicures")); err != nil {
		t.Fatalf("Create() rejected the same name under a different tenant: %v", err)
	}
}

// Allow reuse of an archived category name — the explicit SC1 requirement,
// proven against the real partial unique index (WHERE status = 'ACTIVE').
func TestCategoryArchivedNameCanBeReused(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a8", &currency)

	original, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447113", integrationTenantA, "Old Promo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Archive(ctx, integrationTenantA, original.ID); err != nil {
		t.Fatal(err)
	}

	// The name is now free again: a new ACTIVE category may claim it.
	recreated, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447114", integrationTenantA, "Old Promo"))
	if err != nil {
		t.Fatalf("Create() rejected reusing an archived category's name: %v", err)
	}
	if recreated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE", recreated.Status)
	}

	// Both rows now exist: the archived original and the new active one.
	all, err := repo.ListByTenant(ctx, integrationTenantA, ServiceCategoryListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected both the archived original and the new category to exist, got %d rows", len(all))
	}
}

// Renaming a category to a name already claimed by another ACTIVE category is
// the same conflict Create refuses, exercised through Update.
func TestCategoryUpdateRejectsDuplicateActiveName(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresServiceCategoryRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "category-tenant-a9", &currency)

	if _, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447115", integrationTenantA, "Pedicures")); err != nil {
		t.Fatal(err)
	}
	other, err := repo.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447116", integrationTenantA, "Extensions"))
	if err != nil {
		t.Fatal(err)
	}

	collidingName := "Pedicures"
	_, err = repo.Update(ctx, integrationTenantA, other.ID, ServiceCategoryUpdate{Name: &collidingName})
	if err == nil {
		t.Fatal("Update() renamed a category into a name already claimed by another ACTIVE category")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("error = %v, want VALIDATION_FAILED", err)
	}
}

// --- services.category_id tenant safety --------------------------------------

// The composite foreign key services_category_tenant_fkey (migration 000019)
// is what makes cross-tenant category assignment impossible at the schema
// level, independent of anything the service layer checks.
func TestServiceCannotReferenceAnotherTenantsCategory(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "fk-tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "fk-tenant-b", &currency)

	categories := NewPostgresServiceCategoryRepository(db)
	categoryB, err := categories.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447117", integrationTenantB, "Pedicures"))
	if err != nil {
		t.Fatal(err)
	}

	services := NewPostgresServiceRepository(db)
	crossTenant := newService("550e8400-e29b-41d4-a716-446655447118", integrationTenantA, "Foreign Category Service")
	crossTenant.CategoryID = &categoryB.ID
	_, err = services.Create(ctx, crossTenant)
	if err == nil {
		t.Fatal("Create() accepted a service naming another tenant's category")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("error = %v, want VALIDATION_FAILED (the composite FK is not being translated)", err)
	}
}

// Uncategorised services must continue to work: category_id stays nullable
// and untouched by SC1 for every pre-existing/legacy-shaped service.
func TestServiceWithNullCategoryStillRoundTrips(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "fk-tenant-null", &currency)

	services := NewPostgresServiceRepository(db)
	created, err := services.Create(ctx, newService("550e8400-e29b-41d4-a716-446655447119", integrationTenantA, "Uncategorised Service"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.CategoryID != nil {
		t.Fatalf("CategoryID = %v, want nil", *created.CategoryID)
	}

	found, err := services.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.CategoryID != nil {
		t.Fatalf("re-read CategoryID = %v, want nil", *found.CategoryID)
	}
}

// A service can assign, then clear, its category via Update's **string
// tri-state, exercised against the real database rather than the fake.
func TestServiceUpdateCanAssignThenClearCategory(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "fk-tenant-clear", &currency)

	categories := NewPostgresServiceCategoryRepository(db)
	category, err := categories.Create(ctx, newCategory("550e8400-e29b-41d4-a716-446655447120", integrationTenantA, "Pedicures"))
	if err != nil {
		t.Fatal(err)
	}

	services := NewPostgresServiceRepository(db)
	created, err := services.Create(ctx, newService("550e8400-e29b-41d4-a716-446655447121", integrationTenantA, "Pedicure Deluxe"))
	if err != nil {
		t.Fatal(err)
	}

	categoryIDPtr := &category.ID
	assigned, err := services.Update(ctx, integrationTenantA, created.ID, ServiceUpdate{CategoryID: &categoryIDPtr})
	if err != nil {
		t.Fatalf("Update(assign) error = %v", err)
	}
	if assigned.CategoryID == nil || *assigned.CategoryID != category.ID {
		t.Fatalf("CategoryID = %v, want %q", assigned.CategoryID, category.ID)
	}

	var cleared *string
	unassigned, err := services.Update(ctx, integrationTenantA, created.ID, ServiceUpdate{CategoryID: &cleared})
	if err != nil {
		t.Fatalf("Update(clear) error = %v", err)
	}
	if unassigned.CategoryID != nil {
		t.Fatalf("CategoryID = %v, want cleared to nil", *unassigned.CategoryID)
	}
}
