package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// IdentityTransactor runs a unit of work against user and identity
// repositories bound to a single database transaction.
//
// It exists so a service can require atomicity without importing
// database/sql: creating a federated user is one indivisible act — a user row
// with no identity would be an orphan nobody can ever sign into again, and an
// identity row with no user cannot exist at all. Expressing that as a seam
// rather than an *sql.DB also lets the rollback path be unit-tested with a
// fake, instead of only against a live Postgres.
type IdentityTransactor interface {
	WithinTransaction(ctx context.Context, work func(users UserRepository, identities IdentityRepository) error) error
}

type postgresIdentityTransactor struct{ db *sql.DB }

func NewPostgresIdentityTransactor(db *sql.DB) IdentityTransactor {
	return &postgresIdentityTransactor{db: db}
}

// WithinTransaction commits only when work returns nil. Every other exit —
// an error from work, or a panic — rolls back, following the same
// committed-flag pattern the tenant module already uses.
func (t *postgresIdentityTransactor) WithinTransaction(ctx context.Context, work func(users UserRepository, identities IdentityRepository) error) error {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting identity transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := work(NewPostgresUserRepository(tx), NewPostgresIdentityRepository(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing identity transaction: %w", err)
	}
	committed = true
	return nil
}
