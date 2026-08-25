package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRBACMigrationsCreateConstraintsAndSeedDefinitions(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database is unavailable: %v", err)
	}
	for _, query := range []string{"DROP TABLE IF EXISTS user_roles", "DROP TABLE IF EXISTS role_permissions", "DROP TABLE IF EXISTS permissions", "DROP TABLE IF EXISTS roles"} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	// Cleanup for each object is deferred immediately after it is created,
	// not batched at the end of the function. A t.Fatal on any later step
	// (a failed migration file, a failed assertion) calls runtime.Goexit,
	// which only runs defers already registered — batching them at the end
	// left a t.Fatal on the migration files a chance to strand these stub
	// users/tenants tables permanently in a database this test doesn't own
	// (whatever TEST_DATABASE_URL/DATABASE_URL happened to point at), since
	// none of the drops would have been registered yet.
	for _, query := range []string{"CREATE TABLE users (id UUID PRIMARY KEY)", "CREATE TABLE tenants (id UUID PRIMARY KEY)"} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS tenants")
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS users")

	rolesMigration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000005_create_roles_permissions.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(rolesMigration)); err != nil {
		t.Fatalf("migration 000005_create_roles_permissions.up.sql: %v", err)
	}
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS user_roles")
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS role_permissions")
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS permissions")
	defer db.ExecContext(ctx, "DROP TABLE IF EXISTS roles")

	seedMigration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "000006_seed_roles_permissions.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(seedMigration)); err != nil {
		t.Fatalf("migration 000006_seed_roles_permissions.up.sql: %v", err)
	}

	var roles, permissions int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles").Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM permissions").Scan(&permissions); err != nil {
		t.Fatal(err)
	}
	if roles != 3 || permissions != 13 {
		t.Fatalf("seed counts = roles:%d permissions:%d", roles, permissions)
	}
	assertSeedPermissions(t, db, ctx, "650e8400-e29b-41d4-a716-446655440001", []string{
		"permission.assign", "permission.read", "role.assign", "role.create", "role.delete", "role.read", "role.update",
		"tenant.read", "tenant.update", "user.create", "user.disable", "user.read", "user.update",
	})
	assertSeedPermissions(t, db, ctx, "650e8400-e29b-41d4-a716-446655440002", []string{
		"permission.read", "role.assign", "role.read", "tenant.read", "tenant.update", "user.create", "user.disable", "user.read", "user.update",
	})
	assertSeedPermissions(t, db, ctx, "650e8400-e29b-41d4-a716-446655440003", []string{"permission.read", "role.read", "tenant.read", "user.read"})
}

func assertSeedPermissions(t *testing.T, db *sql.DB, ctx context.Context, roleID string, expected []string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT p.code FROM permissions p JOIN role_permissions rp ON rp.permission_id = p.id WHERE rp.role_id = $1 ORDER BY p.code`, roleID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, code)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("role %s permissions = %#v, want %#v", roleID, actual, expected)
	}
}
