package repository

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// s1Permissions is the exact permission set migration 000011 introduces.
var s1Permissions = []string{"service.archive", "service.create", "service.read", "service.update"}

// s1Grants is the exact role matrix 000011 installs, by role name.
//
// STAFF gets service.read only: a technician needs to see the menu to do their
// job, but pricing and catalog structure are owner decisions.
//
// SUPER_ADMIN's grants confer no tenant access — it is PLATFORM-scoped and
// PermissionResolutionService.ResolveTenant reads only TENANT-scoped roles —
// but they are listed explicitly so the catalog stays consistent.
var s1Grants = map[string][]string{
	"SUPER_ADMIN":    {"service.archive", "service.create", "service.read", "service.update"},
	"BUSINESS_OWNER": {"service.archive", "service.create", "service.read", "service.update"},
	"STAFF":          {"service.read"},
}

func grantsForRole(t *testing.T, db *sql.DB, roleName string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
SELECT p.code
FROM role_permissions rp
JOIN roles r       ON r.id = rp.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE r.name = $1 AND p.code LIKE 'service.%'
ORDER BY p.code`, roleName)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatal(err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return codes
}

func TestMigrationCreatesTheServicesTableAndCurrencyColumn(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()

	var servicesExists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'services')").Scan(&servicesExists); err != nil {
		t.Fatal(err)
	}
	if !servicesExists {
		t.Fatal("migration 000010 did not create the services table")
	}

	var dataType, isNullable string
	err := db.QueryRowContext(ctx, `
SELECT data_type, is_nullable
FROM information_schema.columns
WHERE table_name = 'tenants' AND column_name = 'currency'`).Scan(&dataType, &isNullable)
	if err != nil {
		t.Fatalf("tenants.currency was not added: %v", err)
	}
	if isNullable != "YES" {
		t.Fatal("tenants.currency is NOT NULL — existing tenants have no currency and none can be inferred for them")
	}

	// The tenant-scoped listing index must lead with tenant_id; the composite
	// unique key leads with id and therefore cannot serve that query.
	var indexExists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'services' AND indexname = 'services_tenant_id_idx')").Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if !indexExists {
		t.Fatal("services_tenant_id_idx is missing — every catalog read filters on tenant_id")
	}
}

func TestMigrationRejectsAMalformedCurrencyShape(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	seedTenant(t, db, integrationTenantA, "shape-check", nil)

	// The CHECK guards the shape only; membership of the supported set lives in
	// internal/money, because that list is expected to grow without a schema
	// migration.
	for _, candidate := range []string{"ngn", "n1n", "12"} {
		if _, err := db.ExecContext(ctx, "UPDATE tenants SET currency = $1 WHERE id = $2", candidate, integrationTenantA); err == nil {
			t.Fatalf("the database accepted a malformed currency %q", candidate)
		}
	}
	if _, err := db.ExecContext(ctx, "UPDATE tenants SET currency = 'NGN' WHERE id = $1", integrationTenantA); err != nil {
		t.Fatalf("the database rejected a well-formed currency: %v", err)
	}
}

func TestMigrationSeedsExactlyTheFourServicePermissions(t *testing.T) {
	db := openSchedulingTestDB(t)

	rows, err := db.QueryContext(context.Background(), "SELECT code FROM permissions WHERE code LIKE 'service.%' ORDER BY code")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatal(err)
		}
		codes = append(codes, code)
	}
	if !reflect.DeepEqual(codes, s1Permissions) {
		t.Fatalf("service permissions = %v, want exactly %v", codes, s1Permissions)
	}

	// The pre-existing catalog must be untouched: 13 seeded by 000006, plus
	// these 4.
	var total int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM permissions").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 17 {
		t.Fatalf("permission catalog has %d rows, want 17 (13 pre-existing + 4 from S1)", total)
	}
}

func TestMigrationInstallsExactlyTheApprovedRoleGrants(t *testing.T) {
	db := openSchedulingTestDB(t)

	for role, want := range s1Grants {
		got := grantsForRole(t, db, role)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s service grants = %v, want %v", role, got, want)
		}
	}

	// No role beyond the three seeded ones received a service permission.
	rows, err := db.QueryContext(context.Background(), `
SELECT DISTINCT r.name
FROM role_permissions rp
JOIN roles r       ON r.id = rp.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE p.code LIKE 'service.%'
ORDER BY r.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	granted := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		granted = append(granted, name)
	}
	want := []string{"BUSINESS_OWNER", "STAFF", "SUPER_ADMIN"}
	if !reflect.DeepEqual(granted, want) {
		t.Fatalf("roles holding a service permission = %v, want %v", granted, want)
	}
}

// The pre-existing role/permission catalog must survive S1 untouched — the
// wildcard-grant pattern from 000006 is deliberately not repeated, so nothing
// outside the service.* family may have moved.
func TestMigrationLeavesTheExistingRBACCatalogIntact(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()

	var roles int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles").Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if roles != 3 {
		t.Fatalf("roles = %d, want the 3 pre-existing roles and no new ones", roles)
	}

	// BUSINESS_OWNER's original 9 non-service grants must all still be present.
	var ownerNonService int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM role_permissions rp
JOIN roles r       ON r.id = rp.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE r.name = 'BUSINESS_OWNER' AND p.code NOT LIKE 'service.%'`).Scan(&ownerNonService)
	if err != nil {
		t.Fatal(err)
	}
	if ownerNonService != 9 {
		t.Fatalf("BUSINESS_OWNER non-service grants = %d, want the original 9", ownerNonService)
	}

	var staffNonService int
	err = db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM role_permissions rp
JOIN roles r       ON r.id = rp.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE r.name = 'STAFF' AND p.code NOT LIKE 'service.%'`).Scan(&staffNonService)
	if err != nil {
		t.Fatal(err)
	}
	if staffNonService != 4 {
		t.Fatalf("STAFF non-service grants = %d, want the original 4", staffNonService)
	}
}

// The down migration must reverse S1 and nothing else. Ordering matters:
// role_permissions holds a foreign key to permissions, so the grants have to go
// before the permission rows they reference.
func TestDownMigrationRemovesOnlyTheS1Additions(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()

	// Applied newest-first, the order a real rollback uses: 000011 removes the
	// permission catalog, then 000010 removes the schema.
	for _, migration := range []string{
		"000011_seed_service_permissions.down.sql",
		"000010_create_services_and_tenant_currency.down.sql",
	} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("applying down migration %s: %v", migration, err)
		}
	}

	var servicesExists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'services')").Scan(&servicesExists); err != nil {
		t.Fatal(err)
	}
	if servicesExists {
		t.Fatal("the down migration left the services table behind")
	}

	var currencyExists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'tenants' AND column_name = 'currency')").Scan(&currencyExists); err != nil {
		t.Fatal(err)
	}
	if currencyExists {
		t.Fatal("the down migration left tenants.currency behind")
	}

	var servicePermissions int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM permissions WHERE code LIKE 'service.%'").Scan(&servicePermissions); err != nil {
		t.Fatal(err)
	}
	if servicePermissions != 0 {
		t.Fatalf("the down migration left %d service permissions behind", servicePermissions)
	}

	// No orphaned grant may survive pointing at a deleted permission.
	var orphanedGrants int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM role_permissions rp
LEFT JOIN permissions p ON p.id = rp.permission_id
WHERE p.id IS NULL`).Scan(&orphanedGrants)
	if err != nil {
		t.Fatal(err)
	}
	if orphanedGrants != 0 {
		t.Fatalf("the down migration left %d orphaned role_permissions rows", orphanedGrants)
	}

	// Everything the migration did not add must survive: the original 13
	// permissions, 3 roles, and BUSINESS_OWNER's 9 grants.
	var permissions, roles, ownerGrants int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM permissions").Scan(&permissions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles").Scan(&roles); err != nil {
		t.Fatal(err)
	}
	err = db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM role_permissions rp
JOIN roles r ON r.id = rp.role_id
WHERE r.name = 'BUSINESS_OWNER'`).Scan(&ownerGrants)
	if err != nil {
		t.Fatal(err)
	}
	if permissions != 13 || roles != 3 || ownerGrants != 9 {
		t.Fatalf("after down: permissions=%d roles=%d ownerGrants=%d, want 13/3/9", permissions, roles, ownerGrants)
	}
}
