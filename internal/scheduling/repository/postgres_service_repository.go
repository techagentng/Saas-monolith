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

// dbtx is satisfied by both *sql.DB and *sql.Tx, letting this repository run
// standalone or participate in a caller-owned transaction — the same shape
// PostgresTenantRepository already uses.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type PostgresServiceRepository struct{ db dbtx }

func NewPostgresServiceRepository(db dbtx) *PostgresServiceRepository {
	return &PostgresServiceRepository{db: db}
}

// serviceColumns is the full column set every SELECT below reads, kept as one
// constant so the query strings and scanService's argument order cannot
// silently drift apart.
const serviceColumns = "id, tenant_id, name, description, duration_minutes, price_minor, category_id, status, created_at, updated_at"

// scanner is satisfied by both *sql.Row and *sql.Rows, letting the single-row
// and multi-row paths share one scan routine.
type scanner interface {
	Scan(dest ...any) error
}

func scanService(row scanner) (*model.Service, error) {
	var service model.Service
	err := row.Scan(
		&service.ID, &service.TenantID, &service.Name, &service.Description,
		&service.DurationMinutes, &service.PriceMinor, &service.CategoryID, &service.Status,
		&service.CreatedAt, &service.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &service, nil
}

// notFound is the single response to "no such row in this tenant". It is used
// identically for a service that does not exist and one that exists under a
// different tenant, which is what keeps the API from disclosing the existence
// of another tenant's catalog rows.
func notFound(cause error) error {
	return apperrors.New(apperrors.CodeServiceNotFound, "service not found", cause)
}

// isForeignCategoryViolation recognizes a category_id that does not name a
// category belonging to this service's own tenant — the composite foreign key
// services_category_tenant_fkey (migration 000019) is what actually rejects
// it. CatalogService validates the category against the tenant before ever
// reaching here, so hitting this is a race (the category was reassigned or
// archived — no, deleted is impossible while referenced — between that check
// and this write) rather than routine bad input; it is still reported as a
// presentable validation failure rather than a 500, the same defense-in-depth
// treatment isMissingUserViolation gives staff_profiles.user_id.
func isForeignCategoryViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "services_category_tenant_fkey"
}

// Create persists a new service. Status is defaulted here the same way
// PostgresTenantRepository.Create already defaults tenant status: a caller that
// leaves it unset gets ACTIVE. The service layer always leaves it unset — a
// newly created service is ACTIVE, and no code path lets a client choose.
//
// Constraint violations are deliberately NOT translated into business errors.
// Every CHECK on this table (duration, price, status) mirrors a service-layer
// validation that runs first, so reaching one means a genuine defect rather
// than bad user input, and it must surface as INTERNAL_ERROR rather than being
// reported to the caller as though they could fix it.
func (r *PostgresServiceRepository) Create(ctx context.Context, service *model.Service) (*model.Service, error) {
	status := service.Status
	if status == "" {
		status = model.StatusActive
	}
	created := *service
	created.Status = status
	const query = `INSERT INTO services (id, tenant_id, name, description, duration_minutes, price_minor, category_id, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING status, created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query,
		service.ID, service.TenantID, service.Name, service.Description,
		service.DurationMinutes, service.PriceMinor, service.CategoryID, status,
	).Scan(&created.Status, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		if isForeignCategoryViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "service category does not belong to this tenant", err)
		}
		return nil, fmt.Errorf("inserting service: %w", err)
	}
	return &created, nil
}

// FindByID resolves one service inside one tenant. tenant_id is part of the
// WHERE clause rather than something the caller is trusted to have checked.
func (r *PostgresServiceRepository) FindByID(ctx context.Context, tenantID string, serviceID string) (*model.Service, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+serviceColumns+" FROM services WHERE id = $1 AND tenant_id = $2", serviceID, tenantID)
	service, err := scanService(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding service: %w", err)
	}
	return service, nil
}

// ListByTenant returns the tenant's catalog ordered by name then id — the same
// deterministic-ordering discipline ListAccessibleByUserID already applies, so
// pagination added later cannot produce a shifting window.
func (r *PostgresServiceRepository) ListByTenant(ctx context.Context, tenantID string, filter ServiceListFilter) ([]*model.Service, error) {
	query := "SELECT " + serviceColumns + " FROM services WHERE tenant_id = $1"
	args := []any{tenantID}
	if filter.Status != nil {
		query += " AND status = $2"
		args = append(args, string(*filter.Status))
	}
	query += " ORDER BY name ASC, id ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}
	defer rows.Close()
	var services []*model.Service
	for rows.Next() {
		service, err := scanService(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning service: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating services: %w", err)
	}
	return services, nil
}

// Update writes only the non-nil fields of update. status, tenant_id, id and
// created_at are never part of the SET clause — ServiceUpdate has no fields for
// them — which is the same "absence is the enforcement" pattern
// UpdateProfile relies on for business_type immutability.
func (r *PostgresServiceRepository) Update(ctx context.Context, tenantID string, serviceID string, update ServiceUpdate) (*model.Service, error) {
	sets := []string{}
	args := []any{}
	argIndex := 1

	if update.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *update.Name)
		argIndex++
	}
	if update.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, *update.Description)
		argIndex++
	}
	if update.DurationMinutes != nil {
		sets = append(sets, fmt.Sprintf("duration_minutes = $%d", argIndex))
		args = append(args, *update.DurationMinutes)
		argIndex++
	}
	if update.CategoryID != nil {
		sets = append(sets, fmt.Sprintf("category_id = $%d", argIndex))
		args = append(args, *update.CategoryID)
		argIndex++
	}
	if update.PriceMinor != nil {
		sets = append(sets, fmt.Sprintf("price_minor = $%d", argIndex))
		args = append(args, *update.PriceMinor)
		argIndex++
	}
	if len(sets) == 0 {
		// Guarded at the service layer too; refused here as well so no caller
		// can produce a syntactically broken UPDATE.
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, serviceID, tenantID)

	query := fmt.Sprintf(
		"UPDATE services SET %s WHERE id = $%d AND tenant_id = $%d RETURNING %s",
		strings.Join(sets, ", "), argIndex, argIndex+1, serviceColumns,
	)

	row := r.db.QueryRowContext(ctx, query, args...)
	service, err := scanService(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound(err)
	}
	if err != nil {
		if isForeignCategoryViolation(err) {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "service category does not belong to this tenant", err)
		}
		return nil, fmt.Errorf("updating service: %w", err)
	}
	return service, nil
}

// Archive sets status to ARCHIVED. The row is never deleted: appointments will
// hold a foreign key to it from S10, and a customer's booking history must keep
// resolving after an owner retires a service.
func (r *PostgresServiceRepository) Archive(ctx context.Context, tenantID string, serviceID string) (*model.Service, error) {
	row := r.db.QueryRowContext(ctx,
		"UPDATE services SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND tenant_id = $3 RETURNING "+serviceColumns,
		string(model.StatusArchived), serviceID, tenantID,
	)
	service, err := scanService(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound(err)
	}
	if err != nil {
		return nil, fmt.Errorf("archiving service: %w", err)
	}
	return service, nil
}

// compile-time guard: the Postgres implementation must keep satisfying the
// repository interface.
var _ ServiceRepository = (*PostgresServiceRepository)(nil)
