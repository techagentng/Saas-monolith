package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

type PostgresServiceCategoryRepository struct{ db dbtx }

func NewPostgresServiceCategoryRepository(db dbtx) *PostgresServiceCategoryRepository {
	return &PostgresServiceCategoryRepository{db: db}
}

// categoryColumns is the full column set every SELECT below reads, kept as one
// constant so the query strings and scanCategory's argument order cannot
// silently drift apart — the same discipline serviceColumns applies.
const categoryColumns = "id, tenant_id, name, sort_order, status, created_at, updated_at"

func scanCategory(row scanner) (*model.ServiceCategory, error) {
	var category model.ServiceCategory
	err := row.Scan(
		&category.ID, &category.TenantID, &category.Name, &category.SortOrder,
		&category.Status, &category.CreatedAt, &category.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &category, nil
}

// categoryNotFound is the single response to "no such category in this
// tenant". It is used identically for a category that does not exist and one
// that exists under a different tenant, which is what keeps the API from
// disclosing the existence of another tenant's catalog rows — the same
// reasoning notFound (services) and staffNotFound (staff profiles) apply.
func categoryNotFound(cause error) error {
	return apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", cause)
}

// isCategoryNameUniqueViolation recognizes a second ACTIVE category claiming a
// name already in use by this tenant. It matches the specific partial index
// name rather than any 23505, the same discipline
// isLinkedUserUniqueViolation uses for staff_profiles: a different unique
// violation added later must not be silently reported as this one.
func isCategoryNameUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "service_categories_tenant_name_unique"
}

// Create persists a new category. Status is defaulted here the same way
// PostgresServiceRepository.Create defaults a service: a caller that leaves it
// unset gets ACTIVE, and no code path lets a client choose.
func (r *PostgresServiceCategoryRepository) Create(ctx context.Context, category *model.ServiceCategory) (*model.ServiceCategory, error) {
	status := category.Status
	if status == "" {
		status = model.StatusActive
	}
	created := *category
	created.Status = status
	const query = `INSERT INTO service_categories (id, tenant_id, name, sort_order, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING status, created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query,
		category.ID, category.TenantID, category.Name, category.SortOrder, status,
	).Scan(&created.Status, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		if isCategoryNameUniqueViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "a category with this name already exists", err)
		}
		return nil, fmt.Errorf("inserting service category: %w", err)
	}
	return &created, nil
}

// FindByID resolves one category inside one tenant. tenant_id is part of the
// WHERE clause rather than something the caller is trusted to have checked.
func (r *PostgresServiceCategoryRepository) FindByID(ctx context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+categoryColumns+" FROM service_categories WHERE id = $1 AND tenant_id = $2", categoryID, tenantID)
	category, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, categoryNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding service category: %w", err)
	}
	return category, nil
}

// ListByTenant returns the tenant's categories ordered by sort_order, then
// name, then id — ties break on name so a tenant that never sets a sort order
// still gets a stable alphabetical listing, and the trailing id keeps the
// order deterministic even between same-named categories.
func (r *PostgresServiceCategoryRepository) ListByTenant(ctx context.Context, tenantID string, filter ServiceCategoryListFilter) ([]*model.ServiceCategory, error) {
	query := "SELECT " + categoryColumns + " FROM service_categories WHERE tenant_id = $1"
	args := []any{tenantID}
	if filter.Status != nil {
		query += " AND status = $2"
		args = append(args, string(*filter.Status))
	}
	query += " ORDER BY sort_order ASC, name ASC, id ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing service categories: %w", err)
	}
	defer rows.Close()
	var categories []*model.ServiceCategory
	for rows.Next() {
		category, err := scanCategory(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning service category: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating service categories: %w", err)
	}
	return categories, nil
}

// Update writes only the non-nil fields of update. status, tenant_id, id and
// created_at are never part of the SET clause — ServiceCategoryUpdate has no
// fields for them — the same "absence is the enforcement" pattern
// PostgresServiceRepository.Update relies on.
func (r *PostgresServiceCategoryRepository) Update(ctx context.Context, tenantID string, categoryID string, update ServiceCategoryUpdate) (*model.ServiceCategory, error) {
	sets := []string{}
	args := []any{}
	argIndex := 1

	if update.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *update.Name)
		argIndex++
	}
	if update.SortOrder != nil {
		sets = append(sets, fmt.Sprintf("sort_order = $%d", argIndex))
		args = append(args, *update.SortOrder)
		argIndex++
	}
	if len(sets) == 0 {
		// Guarded at the service layer too; refused here as well so no caller
		// can produce a syntactically broken UPDATE.
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, categoryID, tenantID)

	query := fmt.Sprintf(
		"UPDATE service_categories SET %s WHERE id = $%d AND tenant_id = $%d RETURNING %s",
		strings.Join(sets, ", "), argIndex, argIndex+1, categoryColumns,
	)

	row := r.db.QueryRowContext(ctx, query, args...)
	category, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, categoryNotFound(err)
	}
	if err != nil {
		if isCategoryNameUniqueViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "a category with this name already exists", err)
		}
		return nil, fmt.Errorf("updating service category: %w", err)
	}
	return category, nil
}

// Archive sets status to ARCHIVED. The row is never deleted: the composite
// foreign key on services.category_id carries no ON DELETE action, so any
// service still filed under this category would block a hard delete anyway —
// archiving is the only retirement path SC1 exposes, and it keeps every
// referencing service resolving exactly as before.
func (r *PostgresServiceCategoryRepository) Archive(ctx context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error) {
	row := r.db.QueryRowContext(ctx,
		"UPDATE service_categories SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND tenant_id = $3 RETURNING "+categoryColumns,
		string(model.StatusArchived), categoryID, tenantID,
	)
	category, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, categoryNotFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("archiving service category: %w", err)
	}
	return category, nil
}

// compile-time guard: the Postgres implementation must keep satisfying the
// repository interface.
var _ ServiceCategoryRepository = (*PostgresServiceCategoryRepository)(nil)
