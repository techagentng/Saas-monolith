package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/identity/model"
)

func TestPostgresUserRepositoryMigrationAndEmailUniqueness(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("database is unavailable: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS users"); err != nil {
		t.Fatalf("cleaning users table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE users (
        id UUID PRIMARY KEY,
        email VARCHAR(320) NOT NULL,
        password_hash TEXT NOT NULL,
        status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
        created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT users_email_unique UNIQUE (email),
        CONSTRAINT users_status_valid CHECK (status IN ('ACTIVE', 'DISABLED'))
    )`); err != nil {
		t.Fatalf("applying users migration: %v", err)
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS users")

	repository := NewPostgresUserRepository(db)
	user := model.User{ID: "550e8400-e29b-41d4-a716-446655440000", Email: "user@example.com", PasswordHash: "bcrypt-hash", Status: model.StatusActive}
	created, err := repository.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Email != user.Email || created.Status != model.StatusActive || created.CreatedAt.IsZero() {
		t.Fatalf("created user = %#v", created)
	}

	_, err = repository.Create(ctx, model.User{ID: "550e8400-e29b-41d4-a716-446655440001", Email: user.Email, PasswordHash: "another-hash", Status: model.StatusActive})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeUserAlreadyExists {
		t.Fatalf("duplicate error = %v, want USER_ALREADY_EXISTS", err)
	}

	found, err := repository.FindByID(ctx, user.ID)
	if err != nil || found.Email != user.Email {
		t.Fatalf("FindByID() = %#v, %v", found, err)
	}
}
