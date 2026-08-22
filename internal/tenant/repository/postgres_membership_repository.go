package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

type PostgresMembershipRepository struct{ db dbtx }

func NewPostgresMembershipRepository(db dbtx) *PostgresMembershipRepository {
	return &PostgresMembershipRepository{db: db}
}

func (r *PostgresMembershipRepository) Create(ctx context.Context, membership model.TenantMembership) (*model.TenantMembership, error) {
	err := r.db.QueryRowContext(ctx, `INSERT INTO tenant_memberships (id, tenant_id, user_id, status) VALUES ($1, $2, $3, $4) RETURNING created_at, updated_at`, membership.ID, membership.TenantID, membership.UserID, membership.Status).Scan(&membership.CreatedAt, &membership.UpdatedAt)
	if err != nil {
		if isMembershipUniqueViolation(err) {
			return nil, apperrors.New(apperrors.CodeTenantMembershipAlreadyExists, "tenant membership already exists", err)
		}
		return nil, fmt.Errorf("creating tenant membership: %w", err)
	}
	return &membership, nil
}

func (r *PostgresMembershipRepository) FindByTenantAndUser(ctx context.Context, tenantID, userID string) (*model.TenantMembership, error) {
	var membership model.TenantMembership
	err := r.db.QueryRowContext(ctx, `SELECT id, tenant_id, user_id, status, created_at, updated_at FROM tenant_memberships WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID).Scan(&membership.ID, &membership.TenantID, &membership.UserID, &membership.Status, &membership.CreatedAt, &membership.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding tenant membership: %w", err)
	}
	return &membership, nil
}

func (r *PostgresMembershipRepository) ListByUser(ctx context.Context, userID string) ([]model.TenantMembership, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, tenant_id, user_id, status, created_at, updated_at FROM tenant_memberships WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing tenant memberships: %w", err)
	}
	defer rows.Close()
	var memberships []model.TenantMembership
	for rows.Next() {
		var membership model.TenantMembership
		if err := rows.Scan(&membership.ID, &membership.TenantID, &membership.UserID, &membership.Status, &membership.CreatedAt, &membership.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning tenant membership: %w", err)
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tenant memberships: %w", err)
	}
	return memberships, nil
}

func (r *PostgresMembershipRepository) Disable(ctx context.Context, tenantID, userID string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tenant_memberships SET status = 'DISABLED', updated_at = $3 WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID, now)
	if err != nil {
		return fmt.Errorf("disabling tenant membership: %w", err)
	}
	return nil
}

func isMembershipUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
