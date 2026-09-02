package repository

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// s3Permissions is the exact permission set migration 000013 introduces.
var s3Permissions = []string{"staff.archive", "staff.create", "staff.read", "staff.update"}

// s3Grants is the exact role matrix 000013 installs.
//
// STAFF gets staff.read only: a technician needs to see the roster, but who is
// employed and who can perform what are owner decisions. There is deliberately
// no staff.assign — capability assignment rides on staff.update.
var s3Grants = map[string][]string{
	"SUPER_ADMIN":    {"staff.archive", "staff.create", "staff.read", "staff.update"},
	"BUSINESS_OWNER": {"staff.archive", "staff.create", "staff.read", "staff.update"},
	"STAFF":          {"staff.read"},
}

// scopedGrantsForRole returns one role's grants narrowed to a permission-code
// pattern, so each feature's migration test asserts only its own slice of the
// catalog rather than an absolute total every later migration must bump.
func scopedGrantsForRole(t *testing.T, db *sql.DB, roleName string, pattern string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
SELECT p.code
FROM role_permissions rp
JOIN roles r       ON r.id = rp.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE r.name = $1 AND p.code LIKE $2
ORDER BY p.code`, roleName, pattern)
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

func TestS3MigrationCreatesBothTables(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()

	for _, table := range []string{"staff_profiles", "staff_services"} {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("migration 000012 did not create %s", table)
		}
	}

	// user_id must be nullable, or a non-login worker is unrepresentable.
	var isNullable string
	err := db.QueryRowContext(ctx, `
SELECT is_nullable FROM information_schema.columns
WHERE table_name = 'staff_profiles' AND column_name = 'user_id'`).Scan(&isNullable)
	if err != nil {
		t.Fatal(err)
	}
	if isNullable != "YES" {
		t.Fatal("staff_profiles.user_id is NOT NULL — a technician who never signs in could not be represented without fabricating an account")
	}

	// The roster index must lead with tenant_id; the composite unique key leads
	// with id and cannot serve that query.
	var indexExists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'staff_profiles' AND indexname = 'staff_profiles_tenant_id_idx')").Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if !indexExists {
		t.Fatal("staff_profiles_tenant_id_idx is missing — every roster read filters on tenant_id")
	}
}

func TestS3MigrationInstallsThePartialUniqueIndex(t *testing.T) {
	db := openSchedulingTestDB(t)

	var definition string
	err := db.QueryRowContext(context.Background(),
		"SELECT indexdef FROM pg_indexes WHERE indexname = 'staff_profiles_tenant_user_unique'").Scan(&definition)
	if err != nil {
		t.Fatalf("staff_profiles_tenant_user_unique is missing: %v", err)
	}
	// The WHERE clause is the whole point: without it, a second unlinked profile
	// would collide on (tenant_id, NULL) in some engines and the constraint
	// would mean something quite different from what it is for.
	if !strings.Contains(definition, "WHERE") || !strings.Contains(definition, "user_id IS NOT NULL") {
		t.Fatalf("index is not partial on user_id IS NOT NULL: %s", definition)
	}
	if !strings.Contains(definition, "UNIQUE") {
		t.Fatalf("index is not unique: %s", definition)
	}
}

// Both composite foreign keys must exist and reference the (id, tenant_id) pair.
// Two independent single-column keys would permit the cross-tenant row this
// design exists to forbid.
func TestS3MigrationInstallsCompositeForeignKeys(t *testing.T) {
	db := openSchedulingTestDB(t)

	for _, constraint := range []string{"staff_services_staff_tenant_fkey", "staff_services_service_tenant_fkey"} {
		var definition string
		err := db.QueryRowContext(context.Background(), `
SELECT pg_get_constraintdef(oid) FROM pg_constraint
WHERE conname = $1 AND conrelid = 'staff_services'::regclass`, constraint).Scan(&definition)
		if err != nil {
			t.Fatalf("%s is missing: %v", constraint, err)
		}
		if !strings.Contains(definition, "tenant_id") {
			t.Fatalf("%s does not include tenant_id, so it cannot enforce tenant agreement: %s", constraint, definition)
		}
	}

	// The service-side index the availability engine will need in S7.
	var indexExists bool
	if err := db.QueryRowContext(context.Background(), "SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'staff_services' AND indexname = 'staff_services_service_id_idx')").Scan(&indexExists); err != nil {
		t.Fatal(err)
	}
	if !indexExists {
		t.Fatal("staff_services_service_id_idx is missing — PostgreSQL does not index foreign keys automatically")
	}
}

func TestS3MigrationEnforcesTheStatusVocabulary(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	// INVITED, SUSPENDED and ON_LEAVE are deliberately not part of S3's
	// vocabulary: nothing produces or consumes them yet.
	for _, status := range []string{"INVITED", "SUSPENDED", "ON_LEAVE", "DISABLED", "active"} {
		_, err := db.ExecContext(ctx,
			"INSERT INTO staff_profiles (id, tenant_id, display_name, status) VALUES ($1, $2, $3, $4)",
			"550e8400-e29b-41d4-a716-446655450501", integrationTenantA, "Direct Insert", status)
		if err == nil {
			t.Fatalf("the database accepted status %q — the CHECK constraint is missing or wrong", status)
		}
	}
}

func TestS3MigrationSeedsExactlyTheFourStaffPermissions(t *testing.T) {
	db := openSchedulingTestDB(t)

	rows, err := db.QueryContext(context.Background(), "SELECT code FROM permissions WHERE code LIKE 'staff.%' ORDER BY code")
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
	if !reflect.DeepEqual(codes, s3Permissions) {
		t.Fatalf("staff permissions = %v, want exactly %v", codes, s3Permissions)
	}
}

func TestS3MigrationInstallsExactlyTheApprovedRoleGrants(t *testing.T) {
	db := openSchedulingTestDB(t)

	for role, want := range s3Grants {
		got := scopedGrantsForRole(t, db, role, "staff.%")
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s staff grants = %v, want %v", role, got, want)
		}
	}

	rows, err := db.QueryContext(context.Background(), `
SELECT DISTINCT r.name
FROM role_permissions rp
JOIN roles r       ON r.id = rp.role_id
JOIN permissions p ON p.id = rp.permission_id
WHERE p.code LIKE 'staff.%'
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
		t.Fatalf("roles holding a staff permission = %v, want %v — S3 introduces no new role", granted, want)
	}
}

// S1's grants must survive S3 untouched: adding a feature's permissions must
// never disturb another's.
func TestS3MigrationLeavesS1ServiceGrantsIntact(t *testing.T) {
	db := openSchedulingTestDB(t)

	wantOwner := []string{"service.archive", "service.create", "service.read", "service.update"}
	if got := scopedGrantsForRole(t, db, "BUSINESS_OWNER", "service.%"); !reflect.DeepEqual(got, wantOwner) {
		t.Fatalf("BUSINESS_OWNER service grants = %v, want %v", got, wantOwner)
	}
	if got := scopedGrantsForRole(t, db, "STAFF", "service.%"); !reflect.DeepEqual(got, []string{"service.read"}) {
		t.Fatalf("STAFF service grants = %v, want [service.read]", got)
	}

	// And Epic 01's own catalog is untouched.
	var preExisting int
	err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM permissions WHERE code NOT LIKE 'service.%' AND code NOT LIKE 'staff.%'").Scan(&preExisting)
	if err != nil {
		t.Fatal(err)
	}
	if preExisting != 13 {
		t.Fatalf("non-scheduling permissions = %d, want the original 13 untouched", preExisting)
	}
}

func TestS3DownMigrationRemovesOnlyTheS3Additions(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()

	// Applied newest-first. S5's migration is included because
	// staff_working_hours holds a composite foreign key into staff_profiles:
	// rolling S3 back while S5 is still applied is not a legal database
	// state, the same accommodation this file's own TestDownMigration...
	// sibling in migration_s1_integration_test.go already makes for S3 on
	// top of S1.
	for _, migration := range []string{
		"000015_create_staff_working_hours.down.sql",
		"000013_seed_staff_permissions.down.sql",
		"000012_create_staff_profiles_and_capabilities.down.sql",
	} {
		applySchedulingMigration(t, db, migration)
	}

	for _, table := range []string{"staff_working_hours", "staff_services", "staff_profiles"} {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("the down migration left %s behind", table)
		}
	}

	var staffPermissions int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM permissions WHERE code LIKE 'staff.%'").Scan(&staffPermissions); err != nil {
		t.Fatal(err)
	}
	if staffPermissions != 0 {
		t.Fatalf("the down migration left %d staff permissions behind", staffPermissions)
	}

	var orphanedGrants int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM role_permissions rp
LEFT JOIN permissions p ON p.id = rp.permission_id
WHERE p.id IS NULL`).Scan(&orphanedGrants)
	if err != nil {
		t.Fatal(err)
	}
	if orphanedGrants != 0 {
		t.Fatalf("the down migration left %d orphaned role_permissions rows", orphanedGrants)
	}

	// S1 survives S3's rollback entirely: services, tenants.currency and the
	// service permissions are all still there.
	var servicesExists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'services')").Scan(&servicesExists); err != nil {
		t.Fatal(err)
	}
	if !servicesExists {
		t.Fatal("rolling back S3 removed S1's services table")
	}
	if got := scopedGrantsForRole(t, db, "BUSINESS_OWNER", "service.%"); len(got) != 4 {
		t.Fatalf("BUSINESS_OWNER service grants after S3 rollback = %v, want all four intact", got)
	}
}
