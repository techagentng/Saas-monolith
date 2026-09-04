package service

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// These exercise StaffService.ReplaceCapabilities against a REAL transaction.
// The service layer's own unit tests deliberately cannot: they run over a
// txBeginner that fails on use, which is what proves validation happens before
// a transaction opens. Atomicity itself can only be demonstrated where a real
// commit and rollback exist.

const (
	capTenantA  = "550e8400-e29b-41d4-a716-446655452001"
	capTenantB  = "550e8400-e29b-41d4-a716-446655452002"
	capStaffA   = "550e8400-e29b-41d4-a716-446655452003"
	capServiceA = "550e8400-e29b-41d4-a716-446655452004"
	capServiceB = "550e8400-e29b-41d4-a716-446655452005"
	capServiceC = "550e8400-e29b-41d4-a716-446655452006"
)

// openCapabilityTestDB rebuilds the full schema on the disposable Docker
// database. It refuses to run against the development database: this drops
// tables, so pointing TEST_DATABASE_URL at the dev DSN would destroy real data.
func openCapabilityTestDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	if parsed, err := url.Parse(databaseURL); err == nil {
		if name := strings.TrimPrefix(parsed.Path, "/"); name == "booking" {
			t.Fatalf("refusing to run destructive tests against the development database %q — point TEST_DATABASE_URL at the disposable Docker database (docker-compose.test.yml)", name)
		}
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("database is unavailable: %v", err)
	}

	tables := []string{"staff_services", "staff_profiles", "services", "service_categories", "user_roles", "role_permissions", "permissions", "roles", "tenant_memberships", "sessions", "tenants", "users"}
	drop := func() {
		for _, table := range tables {
			db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table+" CASCADE")
		}
	}
	drop()
	for _, migration := range []string{
		"000001_create_users.up.sql",
		"000002_create_sessions.up.sql",
		"000003_create_tenants.up.sql",
		"000004_create_tenant_memberships.up.sql",
		"000005_create_roles_permissions.up.sql",
		"000006_seed_roles_permissions.up.sql",
		"000007_add_slug_to_tenants.up.sql",
		"000008_add_tenant_profile_fields.up.sql",
		"000009_add_business_type_and_onboarding_to_tenants.up.sql",
		"000010_create_services_and_tenant_currency.up.sql",
		"000011_seed_service_permissions.up.sql",
		"000012_create_staff_profiles_and_capabilities.up.sql",
		"000013_seed_staff_permissions.up.sql",
		"000019_create_service_categories.up.sql",
	} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("reading migration %s: %v", migration, err)
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("applying migration %s: %v", migration, err)
		}
	}
	t.Cleanup(drop)
	return db
}

// capabilityFixture wires a real StaffService over real repositories and a real
// *sql.DB, seeding one tenant with one technician and three services.
type capabilityFixture struct {
	db      *sql.DB
	service StaffService
}

func newCapabilityFixture(t *testing.T) *capabilityFixture {
	t.Helper()
	db := openCapabilityTestDB(t)
	ctx := context.Background()

	for _, seed := range []struct{ id, slug string }{{capTenantA, "tenant-a"}, {capTenantB, "tenant-b"}} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency) VALUES ($1, $2, $3, 'ACTIVE', 'NAIL_TECHNICIAN', 'COMPLETED', 'NGN')",
			seed.id, "Tenant "+seed.slug, seed.slug); err != nil {
			t.Fatalf("seeding tenant %s: %v", seed.slug, err)
		}
	}

	staffRepo := schedulingrepository.NewPostgresStaffRepository(db)
	serviceRepo := schedulingrepository.NewPostgresServiceRepository(db)
	capabilityRepo := schedulingrepository.NewPostgresCapabilityRepository(db)

	if _, err := staffRepo.Create(ctx, &schedulingmodel.StaffProfile{
		ID: capStaffA, TenantID: capTenantA, DisplayName: "Ada", IsBookable: true,
	}); err != nil {
		t.Fatalf("seeding staff: %v", err)
	}
	for _, seed := range []struct{ id, tenantID, name string }{
		{capServiceA, capTenantA, "Manicure"},
		{capServiceB, capTenantA, "Pedicure"},
		{capServiceC, capTenantB, "Rival Service"},
	} {
		if _, err := serviceRepo.Create(ctx, &schedulingmodel.Service{
			ID: seed.id, TenantID: seed.tenantID, Name: seed.name, DurationMinutes: 45, PriceMinor: 1999,
		}); err != nil {
			t.Fatalf("seeding service %s: %v", seed.name, err)
		}
	}

	memberships := &alwaysActiveMembership{}
	return &capabilityFixture{
		db:      db,
		service: NewStaffService(db, staffRepo, capabilityRepo, serviceRepo, memberships),
	}
}

type alwaysActiveMembership struct{}

func (alwaysActiveMembership) FindByTenantAndUser(_ context.Context, tenantID, userID string) (*tenantmodel.TenantMembership, error) {
	return &tenantmodel.TenantMembership{TenantID: tenantID, UserID: userID, Status: tenantmodel.MembershipStatusActive}, nil
}

func (f *capabilityFixture) storedCapabilities(t *testing.T) []string {
	t.Helper()
	rows, err := f.db.QueryContext(context.Background(),
		"SELECT service_id FROM staff_services WHERE staff_id = $1 ORDER BY service_id", capStaffA)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestReplaceCapabilitiesCommitsTheWholeSet(t *testing.T) {
	fixture := newCapabilityFixture(t)

	result, err := fixture.service.ReplaceCapabilities(context.Background(), capTenantA, capStaffA, []string{capServiceA, capServiceB})
	if err != nil {
		t.Fatalf("ReplaceCapabilities() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("returned %v, want both services", result)
	}
	if stored := fixture.storedCapabilities(t); len(stored) != 2 {
		t.Fatalf("persisted %v, want both services", stored)
	}
}

// Replacement is a full-set operation: whatever is sent becomes the set.
func TestReplaceCapabilitiesReplacesRatherThanAppends(t *testing.T) {
	fixture := newCapabilityFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceA, capServiceB}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceB}); err != nil {
		t.Fatalf("second replacement error = %v", err)
	}

	stored := fixture.storedCapabilities(t)
	if len(stored) != 1 || stored[0] != capServiceB {
		t.Fatalf("persisted %v, want exactly the second set", stored)
	}
}

// Sending the same set twice changes nothing — the operation is idempotent.
func TestReplaceCapabilitiesIsIdempotent(t *testing.T) {
	fixture := newCapabilityFixture(t)
	ctx := context.Background()

	first, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceA})
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceA})
	if err != nil {
		t.Fatalf("repeating the same set error = %v", err)
	}
	if len(first) != len(second) || first[0] != second[0] {
		t.Fatalf("idempotent replacement diverged: %v vs %v", first, second)
	}
}

// An empty set is a legitimate state: this technician performs nothing.
func TestReplaceCapabilitiesAcceptsAnEmptySet(t *testing.T) {
	fixture := newCapabilityFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceA}); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, nil)
	if err != nil {
		t.Fatalf("ReplaceCapabilities(empty) error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("returned %v, want an empty set", result)
	}
	if stored := fixture.storedCapabilities(t); len(stored) != 0 {
		t.Fatalf("persisted %v, want none", stored)
	}
}

func TestReplaceCapabilitiesDeduplicatesARepeatedService(t *testing.T) {
	fixture := newCapabilityFixture(t)

	result, err := fixture.service.ReplaceCapabilities(context.Background(), capTenantA, capStaffA, []string{capServiceA, capServiceA, capServiceA})
	if err != nil {
		t.Fatalf("ReplaceCapabilities() error = %v, want a duplicated entry tolerated", err)
	}
	if len(result) != 1 {
		t.Fatalf("returned %v, want one row despite three mentions", result)
	}
}

// THE ATOMICITY PROOF: a set naming another tenant's service must leave the
// previous set completely intact — not partially applied, not cleared.
func TestReplaceCapabilitiesRollsBackEntirelyOnAnInvalidMember(t *testing.T) {
	fixture := newCapabilityFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceA, capServiceB}); err != nil {
		t.Fatal(err)
	}
	before := fixture.storedCapabilities(t)
	if len(before) != 2 {
		t.Fatalf("precondition: persisted %v, want two services", before)
	}

	// capServiceC belongs to tenant B. It is deliberately placed LAST, after two
	// valid entries, so a naive implementation that deleted and then inserted
	// one at a time would already have destroyed the previous set by the time it
	// failed.
	_, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceA, capServiceB, capServiceC})
	if err == nil {
		t.Fatal("ReplaceCapabilities() accepted another tenant's service")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("error = %v, want VALIDATION_FAILED", err)
	}

	after := fixture.storedCapabilities(t)
	if len(after) != 2 || after[0] != before[0] || after[1] != before[1] {
		t.Fatalf("the previous capability set was disturbed by a rejected replacement:\n  before %v\n  after  %v", before, after)
	}
}

func TestReplaceCapabilitiesRollsBackOnAnUnknownService(t *testing.T) {
	fixture := newCapabilityFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceA}); err != nil {
		t.Fatal(err)
	}

	unknown := "550e8400-e29b-41d4-a716-446655459999"
	if _, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceB, unknown}); err == nil {
		t.Fatal("ReplaceCapabilities() accepted an unknown service")
	}

	stored := fixture.storedCapabilities(t)
	if len(stored) != 1 || stored[0] != capServiceA {
		t.Fatalf("the previous set was disturbed: %v", stored)
	}
}

// Archiving a technician keeps their capability rows: they record what this
// person could do while they worked here, which stays true, and an accidental
// archive must not silently destroy configuration.
func TestArchivingStaffKeepsCapabilityRows(t *testing.T) {
	fixture := newCapabilityFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceCapabilities(ctx, capTenantA, capStaffA, []string{capServiceA, capServiceB}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Archive(ctx, capTenantA, capStaffA); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	if stored := fixture.storedCapabilities(t); len(stored) != 2 {
		t.Fatalf("archiving removed capability rows: %v", stored)
	}
	profile, err := fixture.service.Get(ctx, capTenantA, capStaffA)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Status != schedulingmodel.StatusArchived {
		t.Fatalf("Status = %q, want ARCHIVED", profile.Status)
	}
}
