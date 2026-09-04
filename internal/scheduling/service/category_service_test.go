package service

import (
	"context"
	"errors"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
)

// categoryID is a var, not a const: several catalog_service_test.go cases
// need its address to exercise CategoryID's *string/**string tri-state.
var categoryID = "550e8400-e29b-41d4-a716-446655443001"

// --- fakes -------------------------------------------------------------------

// fakeCategoryRepository mirrors fakeServiceRepository's shape so category
// tests read the same way the catalog's own tests do.
type fakeCategoryRepository struct {
	categories map[string]*model.ServiceCategory

	createCalls  int
	updateCalls  int
	archiveCalls int
	lastFilter   repository.ServiceCategoryListFilter
	lastTenantID string

	createErr error
	findErr   error
}

func newFakeCategoryRepository() *fakeCategoryRepository {
	return &fakeCategoryRepository{categories: map[string]*model.ServiceCategory{}}
}

func (r *fakeCategoryRepository) Create(_ context.Context, category *model.ServiceCategory) (*model.ServiceCategory, error) {
	r.createCalls++
	if r.createErr != nil {
		return nil, r.createErr
	}
	stored := *category
	if stored.Status == "" {
		stored.Status = model.StatusActive
	}
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	r.categories[stored.ID] = &stored
	return &stored, nil
}

func (r *fakeCategoryRepository) FindByID(_ context.Context, tenantID string, id string) (*model.ServiceCategory, error) {
	r.lastTenantID = tenantID
	if r.findErr != nil {
		return nil, r.findErr
	}
	stored, ok := r.categories[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", nil)
	}
	return stored, nil
}

func (r *fakeCategoryRepository) ListByTenant(_ context.Context, tenantID string, filter repository.ServiceCategoryListFilter) ([]*model.ServiceCategory, error) {
	r.lastTenantID = tenantID
	r.lastFilter = filter
	var result []*model.ServiceCategory
	for _, stored := range r.categories {
		if stored.TenantID != tenantID {
			continue
		}
		if filter.Status != nil && stored.Status != *filter.Status {
			continue
		}
		result = append(result, stored)
	}
	return result, nil
}

func (r *fakeCategoryRepository) Update(_ context.Context, tenantID string, id string, update repository.ServiceCategoryUpdate) (*model.ServiceCategory, error) {
	r.updateCalls++
	stored, ok := r.categories[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", nil)
	}
	if update.Name != nil {
		stored.Name = *update.Name
	}
	if update.SortOrder != nil {
		stored.SortOrder = *update.SortOrder
	}
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

func (r *fakeCategoryRepository) Archive(_ context.Context, tenantID string, id string) (*model.ServiceCategory, error) {
	r.archiveCalls++
	stored, ok := r.categories[id]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", nil)
	}
	stored.Status = model.StatusArchived
	stored.UpdatedAt = time.Now().UTC()
	return stored, nil
}

// --- Create ------------------------------------------------------------------

func TestCategoryCreateStoresValidatedCategoryAsActive(t *testing.T) {
	categories := newFakeCategoryRepository()
	svc := NewCategoryService(categories)

	created, err := svc.Create(context.Background(), tenantA, CreateCategoryInput{Name: "  Pedicures  "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE", created.Status)
	}
	if created.Name != "Pedicures" {
		t.Fatalf("Name = %q, want trimmed", created.Name)
	}
	if created.TenantID != tenantA {
		t.Fatalf("TenantID = %q, want %q", created.TenantID, tenantA)
	}
	if created.SortOrder != 0 {
		t.Fatalf("SortOrder = %d, want 0 default", created.SortOrder)
	}
}

func TestCategoryCreateAppliesSuppliedSortOrder(t *testing.T) {
	categories := newFakeCategoryRepository()
	svc := NewCategoryService(categories)

	sortOrder := 5
	created, err := svc.Create(context.Background(), tenantA, CreateCategoryInput{Name: "Add-Ons", SortOrder: &sortOrder})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.SortOrder != 5 {
		t.Fatalf("SortOrder = %d, want 5", created.SortOrder)
	}
}

func TestCategoryCreateRejectsInvalidNameBeforeTouchingPersistence(t *testing.T) {
	categories := newFakeCategoryRepository()
	svc := NewCategoryService(categories)

	_, err := svc.Create(context.Background(), tenantA, CreateCategoryInput{Name: "   "})
	assertCode(t, err, apperrors.CodeValidationFailed, "Create(empty name)")
	if categories.createCalls != 0 {
		t.Fatal("Create() reached the repository with an invalid name")
	}
}

func TestCategoryCreateRejectsMalformedTenantID(t *testing.T) {
	categories := newFakeCategoryRepository()
	svc := NewCategoryService(categories)

	_, err := svc.Create(context.Background(), "not-a-uuid", CreateCategoryInput{Name: "Pedicures"})
	assertCode(t, err, apperrors.CodeInvalidRequest, "Create(malformed tenant id)")
	if categories.createCalls != 0 {
		t.Fatal("Create() reached the repository with a malformed tenant id")
	}
}

func TestCategoryCreatePropagatesNameConflict(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.createErr = apperrors.New(apperrors.CodeValidationFailed, "a category with this name already exists", nil)
	svc := NewCategoryService(categories)

	_, err := svc.Create(context.Background(), tenantA, CreateCategoryInput{Name: "Pedicures"})
	assertCode(t, err, apperrors.CodeValidationFailed, "Create(duplicate name)")
}

// --- Get / List ----------------------------------------------------------

func TestCategoryGetTreatsAnotherTenantsCategoryAsNotFound(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories[categoryID] = &model.ServiceCategory{ID: categoryID, TenantID: tenantA, Name: "Pedicures", Status: model.StatusActive}
	svc := NewCategoryService(categories)

	_, err := svc.Get(context.Background(), tenantB, categoryID)
	assertCode(t, err, apperrors.CodeCategoryNotFound, "Get(cross-tenant)")
}

func TestCategoryGetRejectsMalformedIdentifiers(t *testing.T) {
	svc := NewCategoryService(newFakeCategoryRepository())

	_, err := svc.Get(context.Background(), "not-a-uuid", categoryID)
	assertCode(t, err, apperrors.CodeInvalidRequest, "Get(malformed tenant id)")

	_, err = svc.Get(context.Background(), tenantA, "not-a-uuid")
	assertCode(t, err, apperrors.CodeInvalidRequest, "Get(malformed category id)")
}

func TestCategoryListDefaultsToActiveOnly(t *testing.T) {
	categories := newFakeCategoryRepository()
	svc := NewCategoryService(categories)

	if _, err := svc.List(context.Background(), tenantA, ""); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if categories.lastFilter.Status == nil || *categories.lastFilter.Status != model.StatusActive {
		t.Fatalf("filter = %v, want ACTIVE by default", categories.lastFilter.Status)
	}
}

// Archived categories excluded from normal active lists — the explicit SC1
// requirement, exercised end-to-end through the default filter.
func TestCategoryListExcludesArchivedByDefault(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories["active"] = &model.ServiceCategory{ID: "active", TenantID: tenantA, Name: "Pedicures", Status: model.StatusActive}
	categories.categories["archived"] = &model.ServiceCategory{ID: "archived", TenantID: tenantA, Name: "Old Promo", Status: model.StatusArchived}
	svc := NewCategoryService(categories)

	result, err := svc.List(context.Background(), tenantA, "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result) != 1 || result[0].ID != "active" {
		t.Fatalf("List() = %#v, want only the active category", result)
	}

	all, err := svc.List(context.Background(), tenantA, "ALL")
	if err != nil {
		t.Fatalf("List(ALL) error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List(ALL) returned %d rows, want 2", len(all))
	}
}

func TestCategoryListRejectsUnknownStatusFilter(t *testing.T) {
	svc := NewCategoryService(newFakeCategoryRepository())

	_, err := svc.List(context.Background(), tenantA, "disabled")
	assertCode(t, err, apperrors.CodeValidationFailed, "List(unknown filter)")
}

func TestCategoryListIsScopedToTheTenant(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories["a"] = &model.ServiceCategory{ID: "a", TenantID: tenantA, Name: "A", Status: model.StatusActive}
	categories.categories["b"] = &model.ServiceCategory{ID: "b", TenantID: tenantB, Name: "B", Status: model.StatusActive}
	svc := NewCategoryService(categories)

	result, err := svc.List(context.Background(), tenantA, "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result) != 1 || result[0].TenantID != tenantA {
		t.Fatalf("List() returned %d rows including another tenant's", len(result))
	}
}

// --- Update ------------------------------------------------------------------

func TestCategoryUpdateAppliesOnlySuppliedFields(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories[categoryID] = &model.ServiceCategory{ID: categoryID, TenantID: tenantA, Name: "Pedicures", SortOrder: 1, Status: model.StatusActive}
	svc := NewCategoryService(categories)

	name := "  Deluxe Pedicures  "
	updated, err := svc.Update(context.Background(), tenantA, categoryID, UpdateCategoryInput{Name: &name})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "Deluxe Pedicures" {
		t.Fatalf("Name = %q, want trimmed", updated.Name)
	}
	if updated.SortOrder != 1 {
		t.Fatalf("SortOrder = %d, want untouched", updated.SortOrder)
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want untouched — archiving owns lifecycle", updated.Status)
	}
}

func TestCategoryUpdateRejectsAnEmptyPatch(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories[categoryID] = &model.ServiceCategory{ID: categoryID, TenantID: tenantA, Name: "Pedicures", Status: model.StatusActive}
	svc := NewCategoryService(categories)

	_, err := svc.Update(context.Background(), tenantA, categoryID, UpdateCategoryInput{})
	assertCode(t, err, apperrors.CodeValidationFailed, "Update(empty patch)")
	if categories.updateCalls != 0 {
		t.Fatal("Update() reached the repository with nothing to write")
	}
}

func TestCategoryUpdateTreatsAnotherTenantsCategoryAsNotFound(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories[categoryID] = &model.ServiceCategory{ID: categoryID, TenantID: tenantA, Name: "Pedicures", Status: model.StatusActive}
	svc := NewCategoryService(categories)

	name := "Hijacked"
	_, err := svc.Update(context.Background(), tenantB, categoryID, UpdateCategoryInput{Name: &name})
	assertCode(t, err, apperrors.CodeCategoryNotFound, "Update(cross-tenant)")
	if categories.categories[categoryID].Name != "Pedicures" {
		t.Fatalf("a cross-tenant update mutated the category: %q", categories.categories[categoryID].Name)
	}
}

// --- Archive -----------------------------------------------------------------

func TestCategoryArchiveMovesActiveCategoryToArchived(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories[categoryID] = &model.ServiceCategory{ID: categoryID, TenantID: tenantA, Name: "Pedicures", Status: model.StatusActive}
	svc := NewCategoryService(categories)

	archived, err := svc.Archive(context.Background(), tenantA, categoryID)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if archived.Status != model.StatusArchived {
		t.Fatalf("Status = %q, want ARCHIVED", archived.Status)
	}
	if categories.archiveCalls != 1 {
		t.Fatalf("Archive repository calls = %d, want 1", categories.archiveCalls)
	}
	if _, stillPresent := categories.categories[categoryID]; !stillPresent {
		t.Fatal("Archive() removed the row — archiving must never delete")
	}
}

func TestCategoryArchiveIsIdempotent(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories[categoryID] = &model.ServiceCategory{ID: categoryID, TenantID: tenantA, Name: "Pedicures", Status: model.StatusArchived}
	svc := NewCategoryService(categories)

	archived, err := svc.Archive(context.Background(), tenantA, categoryID)
	if err != nil {
		t.Fatalf("Archive() error = %v, want idempotent success", err)
	}
	if archived.Status != model.StatusArchived {
		t.Fatalf("Status = %q, want ARCHIVED", archived.Status)
	}
	if categories.archiveCalls != 0 {
		t.Fatal("Archive() re-persisted an already archived category")
	}
}

func TestCategoryArchiveTreatsAnotherTenantsCategoryAsNotFound(t *testing.T) {
	categories := newFakeCategoryRepository()
	categories.categories[categoryID] = &model.ServiceCategory{ID: categoryID, TenantID: tenantA, Name: "Pedicures", Status: model.StatusActive}
	svc := NewCategoryService(categories)

	_, err := svc.Archive(context.Background(), tenantB, categoryID)
	assertCode(t, err, apperrors.CodeCategoryNotFound, "Archive(cross-tenant)")
	if categories.archiveCalls != 0 {
		t.Fatal("Archive() reached the repository write path on a cross-tenant request")
	}
}

func TestParseCategoryStatusFilterIsTheSingleFilterVocabulary(t *testing.T) {
	if _, err := ParseCategoryStatusFilter("ALL"); err != nil {
		t.Fatalf("ParseCategoryStatusFilter(ALL) error = %v", err)
	}
	if _, err := ParseCategoryStatusFilter("DISABLED"); err == nil {
		t.Fatal("ParseCategoryStatusFilter accepted DISABLED — the vocabulary is ACTIVE/ARCHIVED")
	}
}

// Sanity check that the fake's not-found error carries the dedicated code, so
// tests above genuinely exercise CATEGORY_NOT_FOUND rather than a stand-in.
func TestFakeCategoryRepositoryUsesCategoryNotFound(t *testing.T) {
	categories := newFakeCategoryRepository()
	_, err := categories.FindByID(context.Background(), tenantA, "missing")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeCategoryNotFound {
		t.Fatalf("error = %v, want CATEGORY_NOT_FOUND", err)
	}
}
