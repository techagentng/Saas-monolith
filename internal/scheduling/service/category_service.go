package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
)

// CreateCategoryInput carries a transport-validated category creation
// request.
//
// TenantID is deliberately absent: it comes from the trusted tenant context
// resolved by the middleware chain and is passed as a separate argument,
// never read from a request body — the same discipline CreateServiceInput
// follows. Status is absent for the same reason: a new category is always
// ACTIVE, and no code path lets a client choose.
type CreateCategoryInput struct {
	Name string
	// SortOrder nil defaults to 0 — the common case for a tenant's first few
	// categories, which sort stably by name until the owner reorders them.
	SortOrder *int
}

// UpdateCategoryInput carries a partial update. Only non-nil fields change.
//
// The set is limited to genuinely editable data: status (owned by Archive)
// and tenant_id/id/timestamps stay absent by design, the same reasoning
// UpdateServiceInput documents.
type UpdateCategoryInput struct {
	Name      *string
	SortOrder *int
}

// CategoryService owns the service-category rules: validation, the archive
// idempotency decision, and tenant-scoped lookups.
//
// Tenant access itself — membership and the service.* permission — is
// verified by the production middleware chain before any method here is
// reached (SC1 reuses the catalog's own permissions rather than inventing a
// parallel category.* family, the same reasoning the working-hours module
// gives for reusing staff.read/staff.update). This service does not re-derive
// or re-check authorization; it does, however, scope every repository call by
// tenantID, so a defect in the chain cannot turn into a cross-tenant read or
// write.
type CategoryService interface {
	Create(ctx context.Context, tenantID string, input CreateCategoryInput) (*model.ServiceCategory, error)
	Get(ctx context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error)
	// List returns the tenant's categories. statusFilter accepts "ACTIVE",
	// "ARCHIVED" or "ALL"; an empty string means the default, ACTIVE — the
	// same vocabulary CatalogService.List uses, so an archived category is
	// excluded from the listing a caller gets by default.
	List(ctx context.Context, tenantID string, statusFilter string) ([]*model.ServiceCategory, error)
	Update(ctx context.Context, tenantID string, categoryID string, input UpdateCategoryInput) (*model.ServiceCategory, error)
	// Archive moves a category ACTIVE -> ARCHIVED. Calling it on an already
	// archived category is idempotent: it returns the category unchanged
	// without writing, so a repeated call never disturbs updated_at. The
	// services filed under it are untouched — they keep their CategoryID and
	// stay individually bookable; only this category's own listing visibility
	// changes.
	Archive(ctx context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error)
}

type categoryService struct {
	categories repository.ServiceCategoryRepository
}

func NewCategoryService(categories repository.ServiceCategoryRepository) CategoryService {
	return &categoryService{categories: categories}
}

// ParseCategoryStatusFilter converts the list endpoint's status query
// parameter into a repository filter, mirroring ParseStatusFilter's
// vocabulary exactly so the category and service listings behave identically.
// An unrecognized value is rejected rather than silently falling back to the
// default.
func ParseCategoryStatusFilter(raw string) (repository.ServiceCategoryListFilter, error) {
	switch raw {
	case "", string(model.StatusActive):
		status := model.StatusActive
		return repository.ServiceCategoryListFilter{Status: &status}, nil
	case string(model.StatusArchived):
		status := model.StatusArchived
		return repository.ServiceCategoryListFilter{Status: &status}, nil
	case "ALL":
		return repository.ServiceCategoryListFilter{}, nil
	default:
		return repository.ServiceCategoryListFilter{}, apperrors.New(apperrors.CodeValidationFailed, "invalid status filter", nil)
	}
}

func (s *categoryService) Create(ctx context.Context, tenantID string, input CreateCategoryInput) (*model.ServiceCategory, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}

	name, err := model.ValidateCategoryName(input.Name)
	if err != nil {
		return nil, err
	}

	sortOrder := 0
	if input.SortOrder != nil {
		sortOrder = *input.SortOrder
	}

	return s.categories.Create(ctx, &model.ServiceCategory{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		Name:      name,
		SortOrder: sortOrder,
		// Status is left unset so the repository's own defaulting applies,
		// producing ACTIVE. There is no path through this method by which a
		// caller influences it.
	})
}

func (s *categoryService) Get(ctx context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error) {
	if err := validateCategoryIdentifiers(tenantID, categoryID); err != nil {
		return nil, err
	}
	return s.categories.FindByID(ctx, tenantID, categoryID)
}

func (s *categoryService) List(ctx context.Context, tenantID string, statusFilter string) ([]*model.ServiceCategory, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	filter, err := ParseCategoryStatusFilter(statusFilter)
	if err != nil {
		return nil, err
	}
	return s.categories.ListByTenant(ctx, tenantID, filter)
}

func (s *categoryService) Update(ctx context.Context, tenantID string, categoryID string, input UpdateCategoryInput) (*model.ServiceCategory, error) {
	if err := validateCategoryIdentifiers(tenantID, categoryID); err != nil {
		return nil, err
	}

	update := repository.ServiceCategoryUpdate{}

	if input.Name != nil {
		name, err := model.ValidateCategoryName(*input.Name)
		if err != nil {
			return nil, err
		}
		update.Name = &name
	}
	if input.SortOrder != nil {
		update.SortOrder = input.SortOrder
	}

	if update.IsEmpty() {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	return s.categories.Update(ctx, tenantID, categoryID, update)
}

func (s *categoryService) Archive(ctx context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error) {
	if err := validateCategoryIdentifiers(tenantID, categoryID); err != nil {
		return nil, err
	}

	current, err := s.categories.FindByID(ctx, tenantID, categoryID)
	if err != nil {
		return nil, err
	}
	// Idempotent: an already archived category is returned as-is, without a
	// write, mirroring CatalogService.Archive and StaffService.Archive.
	if current.Status == model.StatusArchived {
		return current, nil
	}
	return s.categories.Archive(ctx, tenantID, categoryID)
}

// validateCategoryIdentifiers refuses a malformed tenant or category id
// before any query runs. Both are reported as INVALID_REQUEST rather than
// CATEGORY_NOT_FOUND: a syntactically impossible id is a broken request, not
// a missing resource.
func validateCategoryIdentifiers(tenantID string, categoryID string) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	if _, err := uuid.Parse(categoryID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid category id", err)
	}
	return nil
}

// compile-time guard: the implementation must keep satisfying its interface.
var _ CategoryService = (*categoryService)(nil)
