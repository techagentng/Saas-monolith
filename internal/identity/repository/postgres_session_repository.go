package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
)

type PostgresSessionRepository struct{ db *sql.DB }

func NewPostgresSessionRepository(db *sql.DB) *PostgresSessionRepository {
	return &PostgresSessionRepository{db: db}
}

func (r *PostgresSessionRepository) Create(ctx context.Context, session model.Session) (*model.Session, error) {
	const query = `INSERT INTO sessions (id, user_id, refresh_token_hash, created_at, updated_at, expires_at, revoked_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.ExecContext(ctx, query, session.ID, session.UserID, session.RefreshTokenHash, session.CreatedAt, session.UpdatedAt, session.ExpiresAt, session.RevokedAt)
	if err != nil {
		return nil, fmt.Errorf("inserting session: %w", err)
	}
	return &session, nil
}

func (r *PostgresSessionRepository) Rotate(ctx context.Context, oldHash, newHash string, now time.Time) (*model.Session, error) {
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("starting session rotation: %w", err)
	}
	defer transaction.Rollback()
	var session model.Session
	const selectQuery = `SELECT id, user_id, refresh_token_hash, created_at, updated_at, expires_at, revoked_at FROM sessions WHERE refresh_token_hash = $1 FOR UPDATE`
	err = transaction.QueryRowContext(ctx, selectQuery, oldHash).Scan(&session.ID, &session.UserID, &session.RefreshTokenHash, &session.CreatedAt, &session.UpdatedAt, &session.ExpiresAt, &session.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding session for rotation: %w", err)
	}
	if session.Revoked() {
		return nil, apperrors.New(apperrors.CodeSessionRevoked, "session revoked", nil)
	}
	if session.Expired(now) {
		return nil, apperrors.New(apperrors.CodeSessionExpired, "session expired", nil)
	}
	result, err := transaction.ExecContext(ctx, `UPDATE sessions SET refresh_token_hash = $1, updated_at = $2 WHERE id = $3 AND refresh_token_hash = $4 AND revoked_at IS NULL AND expires_at > $2`, newHash, now, session.ID, oldHash)
	if err != nil {
		return nil, fmt.Errorf("rotating session credential: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", nil)
	}
	session.RefreshTokenHash, session.UpdatedAt = newHash, now
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("committing session rotation: %w", err)
	}
	return &session, nil
}

func (r *PostgresSessionRepository) Revoke(ctx context.Context, sessionID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = COALESCE(revoked_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP WHERE id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) FindActive(ctx context.Context, userID, sessionID string, now time.Time) (*model.Session, error) {
	const query = `SELECT id, user_id, refresh_token_hash, created_at, updated_at, expires_at, revoked_at FROM sessions WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > $3`
	var session model.Session
	err := r.db.QueryRowContext(ctx, query, sessionID, userID, now).Scan(&session.ID, &session.UserID, &session.RefreshTokenHash, &session.CreatedAt, &session.UpdatedAt, &session.ExpiresAt, &session.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeSessionExpired, "session is not active", err)
	}
	if err != nil {
		return nil, fmt.Errorf("finding active session: %w", err)
	}
	return &session, nil
}
