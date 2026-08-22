package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

// dbtx is satisfied by both *sql.DB and *sql.Tx, letting this repository run
// standalone or participate in a caller-owned transaction (Feature 2).
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type PostgresTenantRepository struct{ db dbtx }

func NewPostgresTenantRepository(db dbtx) *PostgresTenantRepository {
	return &PostgresTenantRepository{db: db}
}

// Create persists a new tenant. A duplicate slug is a normal, presentable
// business outcome (Feature 2 is its first real caller) and maps to
// TENANT_SLUG_TAKEN. A duplicate ID (PK collision) remains an unexpected
// system failure per the Feature 1 decision — any other unique constraint
// added later falls back to the same system-failure wrapping rather than
// being guessed at.
func (r *PostgresTenantRepository) Create(ctx context.Context, tenant *model.Tenant) (*model.Tenant, error) {
	status := tenant.Status
	if status == "" {
		status = model.StatusActive
	}
	created := *tenant
	created.Status = status
	const query = `INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, $4) RETURNING status, created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query, tenant.ID, tenant.Name, tenant.Slug, status).Scan(&created.Status, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		if isSlugUniqueViolation(err) {
			return nil, apperrors.New(apperrors.CodeTenantSlugTaken, "tenant slug is already taken", err)
		}
		return nil, fmt.Errorf("inserting tenant: %w", err)
	}
	return &created, nil
}

func isSlugUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "tenants_slug_key"
}

func (r *PostgresTenantRepository) FindByID(ctx context.Context, id string) (*model.Tenant, error) {
	var tenant model.Tenant
	err := r.db.QueryRowContext(ctx, `SELECT id, name, slug, status, created_at, updated_at FROM tenants WHERE id = $1`, id).
		Scan(&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Status, &tenant.CreatedAt, &tenant.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding tenant: %w", err)
	}
	return &tenant, nil
}
