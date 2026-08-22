package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

type PostgresTenantRepository struct{ db *sql.DB }

func NewPostgresTenantRepository(db *sql.DB) *PostgresTenantRepository {
	return &PostgresTenantRepository{db: db}
}

func (r *PostgresTenantRepository) FindByID(ctx context.Context, id string) (*model.Tenant, error) {
	var tenant model.Tenant
	err := r.db.QueryRowContext(ctx, `SELECT id, name, status, created_at, updated_at FROM tenants WHERE id = $1`, id).
		Scan(&tenant.ID, &tenant.Name, &tenant.Status, &tenant.CreatedAt, &tenant.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding tenant: %w", err)
	}
	return &tenant, nil
}
