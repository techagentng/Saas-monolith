package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

func TestPostgresMembershipRepositoryPersistsUniqueAndDisabledMemberships(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database is unavailable: %v", err)
	}
	for _, query := range []string{"DROP TABLE IF EXISTS tenant_memberships", "DROP TABLE IF EXISTS tenants", "DROP TABLE IF EXISTS users", `CREATE TABLE users (id UUID PRIMARY KEY, email TEXT NOT NULL, password_hash TEXT NOT NULL, status TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)`, `CREATE TABLE tenants (id UUID PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'ACTIVE', created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)`, `CREATE TABLE tenant_memberships (id UUID PRIMARY KEY, tenant_id UUID NOT NULL REFERENCES tenants(id), user_id UUID NOT NULL REFERENCES users(id), status TEXT NOT NULL DEFAULT 'ACTIVE', created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE (tenant_id, user_id))`, `CREATE INDEX tenant_memberships_user_id_idx ON tenant_memberships (user_id)`, `CREATE INDEX tenant_memberships_tenant_id_idx ON tenant_memberships (tenant_id)`} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("schema query failed: %v", err)
		}
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS tenant_memberships")
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS tenants")
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS users")
	tenantID, userID := "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, $3, 'ACTIVE'), ($4, $5, $6, 'ACTIVE')`, userID, "one@example.com", "hash", "550e8400-e29b-41d4-a716-446655440002", "two@example.com", "hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, "Tenant"); err != nil {
		t.Fatal(err)
	}
	r := NewPostgresMembershipRepository(db)
	membership, err := r.Create(ctx, model.TenantMembership{ID: "550e8400-e29b-41d4-a716-446655440003", TenantID: tenantID, UserID: userID, Status: model.MembershipStatusActive})
	if err != nil || membership.CreatedAt.IsZero() {
		t.Fatalf("Create() = %#v, %v", membership, err)
	}
	_, err = r.Create(ctx, model.TenantMembership{ID: "550e8400-e29b-41d4-a716-446655440004", TenantID: tenantID, UserID: userID, Status: model.MembershipStatusActive})
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeTenantMembershipAlreadyExists {
		t.Fatalf("duplicate error = %v", err)
	}
	if err := r.Disable(ctx, tenantID, userID, time.Now()); err != nil {
		t.Fatal(err)
	}
	found, err := r.FindByTenantAndUser(ctx, tenantID, userID)
	if err != nil || found.Status != model.MembershipStatusDisabled {
		t.Fatalf("disabled membership = %#v, %v", found, err)
	}
}
