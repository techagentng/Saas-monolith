package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
)

type PostgresIdentityRepository struct{ db dbtx }

func NewPostgresIdentityRepository(db dbtx) *PostgresIdentityRepository {
	return &PostgresIdentityRepository{db: db}
}

func (r *PostgresIdentityRepository) FindByProviderSubject(ctx context.Context, provider model.Provider, subject string) (*model.UserIdentity, error) {
	const query = `SELECT id, user_id, provider, provider_subject, COALESCE(provider_email, ''), created_at, updated_at
        FROM user_identities WHERE provider = $1 AND provider_subject = $2`
	identity := &model.UserIdentity{}
	err := r.db.QueryRowContext(ctx, query, provider, subject).Scan(
		&identity.ID, &identity.UserID, &identity.Provider, &identity.ProviderSubject,
		&identity.ProviderEmail, &identity.CreatedAt, &identity.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeResourceNotFound, "external identity not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("querying external identity: %w", err)
	}
	return identity, nil
}

// Create inserts the link. Either unique constraint firing means another
// request already linked this provider account (or this user's provider slot)
// — a real, presentable conflict rather than a system failure, so it maps to
// EXTERNAL_IDENTITY_CONFLICT instead of being wrapped as an internal error.
func (r *PostgresIdentityRepository) Create(ctx context.Context, identity model.UserIdentity) (*model.UserIdentity, error) {
	const query = `INSERT INTO user_identities (id, user_id, provider, provider_subject, provider_email)
        VALUES ($1, $2, $3, $4, NULLIF($5, '')) RETURNING created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query, identity.ID, identity.UserID, identity.Provider, identity.ProviderSubject, identity.ProviderEmail).
		Scan(&identity.CreatedAt, &identity.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.New(apperrors.CodeExternalIdentityConflict, "external identity already linked", err)
		}
		return nil, fmt.Errorf("inserting external identity: %w", err)
	}
	return &identity, nil
}

func (r *PostgresIdentityRepository) UpdateProviderEmail(ctx context.Context, id, providerEmail string) error {
	const query = `UPDATE user_identities SET provider_email = NULLIF($2, ''), updated_at = CURRENT_TIMESTAMP WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, query, id, providerEmail); err != nil {
		return fmt.Errorf("updating external identity email: %w", err)
	}
	return nil
}
