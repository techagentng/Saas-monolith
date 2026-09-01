package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
)

// dbtx is satisfied by both *sql.DB and *sql.Tx, letting the identity
// repositories run standalone or inside a caller-owned transaction. Creating a
// user and their external identity together has to be atomic (see
// service.GoogleAuthenticationService), and this is the same seam
// internal/tenant/repository already uses for the same reason.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type PostgresUserRepository struct {
	db dbtx
}

func NewPostgresUserRepository(db dbtx) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) Create(ctx context.Context, user model.User) (*model.User, error) {
	const query = `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, $3, $4) RETURNING created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query, user.ID, user.Email, user.PasswordHash, user.Status).Scan(&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperrors.New(apperrors.CodeUserAlreadyExists, "user already exists", err)
		}
		return nil, fmt.Errorf("inserting user: %w", err)
	}
	return &user, nil
}

func (r *PostgresUserRepository) FindByID(ctx context.Context, id string) (*model.User, error) {
	const query = `SELECT id, email, password_hash, status, created_at, updated_at FROM users WHERE id = $1`
	return r.find(ctx, query, id)
}

func (r *PostgresUserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	const query = `SELECT id, email, password_hash, status, created_at, updated_at FROM users WHERE email = $1`
	return r.find(ctx, query, email)
}

func (r *PostgresUserRepository) find(ctx context.Context, query string, value string) (*model.User, error) {
	user := &model.User{}
	err := r.db.QueryRowContext(ctx, query, value).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperrors.New(apperrors.CodeUserNotFound, "user not found", err)
	}
	if err != nil {
		return nil, fmt.Errorf("querying user: %w", err)
	}
	return user, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
