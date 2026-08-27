package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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
	for _, query := range []string{"DROP TABLE IF EXISTS services", "DROP TABLE IF EXISTS tenant_memberships", "DROP TABLE IF EXISTS tenants", "DROP TABLE IF EXISTS users"} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("schema query failed: %v", err)
		}
	}
	for _, migration := range []string{"000001_create_users.up.sql", "000003_create_tenants.up.sql", "000004_create_tenant_memberships.up.sql", "000010_create_services_and_tenant_currency.up.sql"} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("reading migration %s: %v", migration, err)
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("applying migration %s: %v", migration, err)
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
