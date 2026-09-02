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
)

// These exercise WorkingHoursService.ReplaceWeeklySchedule against a REAL
// transaction. The service layer's own unit tests deliberately cannot: they
// run over a txBeginner that fails on use, which is what proves validation
// happens before a transaction opens. Atomicity itself can only be
// demonstrated where a real commit and rollback exist — mirroring
// staff_capability_integration_test.go's own reasoning for the identical
// situation on ReplaceCapabilities.

const (
	whSvcTenantA = "550e8400-e29b-41d4-a716-446655453001"
	whSvcTenantB = "550e8400-e29b-41d4-a716-446655453002"
	whSvcStaffA  = "550e8400-e29b-41d4-a716-446655453003"
)

func openWorkingHoursTestDB(t *testing.T) *sql.DB {
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

	tables := []string{"staff_working_hours", "staff_services", "staff_profiles", "services", "user_roles", "role_permissions", "permissions", "roles", "tenant_memberships", "sessions", "tenants", "users"}
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
		"000015_create_staff_working_hours.up.sql",
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

type workingHoursFixture struct {
	db      *sql.DB
	service WorkingHoursService
}

func newWorkingHoursFixture(t *testing.T) *workingHoursFixture {
	t.Helper()
	db := openWorkingHoursTestDB(t)
	ctx := context.Background()

	for _, seed := range []struct{ id, slug string }{{whSvcTenantA, "tenant-a"}, {whSvcTenantB, "tenant-b"}} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency) VALUES ($1, $2, $3, 'ACTIVE', 'NAIL_TECHNICIAN', 'COMPLETED', 'NGN')",
			seed.id, "Tenant "+seed.slug, seed.slug); err != nil {
			t.Fatalf("seeding tenant %s: %v", seed.slug, err)
		}
	}

	staffRepo := schedulingrepository.NewPostgresStaffRepository(db)
	hoursRepo := schedulingrepository.NewPostgresWorkingHoursRepository(db)

	if _, err := staffRepo.Create(ctx, &schedulingmodel.StaffProfile{
		ID: whSvcStaffA, TenantID: whSvcTenantA, DisplayName: "Ada", IsBookable: true,
	}); err != nil {
		t.Fatalf("seeding staff: %v", err)
	}

	return &workingHoursFixture{
		db:      db,
		service: NewWorkingHoursService(db, hoursRepo, staffRepo),
	}
}

func (f *workingHoursFixture) storedIntervals(t *testing.T) []schedulingmodel.WorkingHourInterval {
	t.Helper()
	rows, err := f.db.QueryContext(context.Background(),
		"SELECT day_of_week, start_time::text, end_time::text FROM staff_working_hours WHERE staff_id = $1 ORDER BY day_of_week, start_time", whSvcStaffA)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	intervals := []schedulingmodel.WorkingHourInterval{}
	for rows.Next() {
		var day, start, end string
		if err := rows.Scan(&day, &start, &end); err != nil {
			t.Fatal(err)
		}
		// Postgres renders TIME as text with seconds ("09:00:00"); trim to
		// the HH:MM this service always produces, so the raw-SQL assertion
		// below compares like with like.
		intervals = append(intervals, schedulingmodel.WorkingHourInterval{
			DayOfWeek: schedulingmodel.DayOfWeek(day), StartTime: start[:5], EndTime: end[:5],
		})
	}
	return intervals
}

func input(day schedulingmodel.DayOfWeek, start, end string) IntervalInput {
	return IntervalInput{DayOfWeek: string(day), StartTime: start, EndTime: end}
}

func TestReplaceWeeklyScheduleCommitsTheWholeSchedule(t *testing.T) {
	fixture := newWorkingHoursFixture(t)

	result, err := fixture.service.ReplaceWeeklySchedule(context.Background(), whSvcTenantA, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Monday, "09:00", "17:00"),
		input(schedulingmodel.Tuesday, "09:00", "17:00"),
	})
	if err != nil {
		t.Fatalf("ReplaceWeeklySchedule() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("returned %d intervals, want 2", len(result))
	}
	if stored := fixture.storedIntervals(t); len(stored) != 2 {
		t.Fatalf("persisted %v, want 2", stored)
	}
}

// Replacement is a full-set operation: whatever is sent becomes the schedule.
func TestReplaceWeeklyScheduleReplacesRatherThanAppends(t *testing.T) {
	fixture := newWorkingHoursFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Monday, "09:00", "17:00"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Tuesday, "10:00", "18:00"),
	}); err != nil {
		t.Fatalf("second replacement error = %v", err)
	}

	stored := fixture.storedIntervals(t)
	if len(stored) != 1 || stored[0].DayOfWeek != schedulingmodel.Tuesday {
		t.Fatalf("persisted %v, want exactly the second schedule", stored)
	}
}

// An empty schedule is a legitimate state — no configured hours yet, or a
// deliberate clear — and is the mechanism ReplaceWeeklySchedule's own empty
// input is meant to express.
func TestReplaceWeeklyScheduleAcceptsAnEmptySchedule(t *testing.T) {
	fixture := newWorkingHoursFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Monday, "09:00", "17:00"),
	}); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, nil)
	if err != nil {
		t.Fatalf("ReplaceWeeklySchedule(empty) error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("returned %v, want empty", result)
	}
	if stored := fixture.storedIntervals(t); len(stored) != 0 {
		t.Fatalf("persisted %v, want none", stored)
	}
}

// THE ATOMICITY PROOF: a schedule containing one overlapping interval must
// leave the previous schedule completely intact — not partially applied.
func TestReplaceWeeklyScheduleRollsBackEntirelyOnAnOverlap(t *testing.T) {
	fixture := newWorkingHoursFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Monday, "09:00", "17:00"),
	}); err != nil {
		t.Fatal(err)
	}
	before := fixture.storedIntervals(t)
	if len(before) != 1 {
		t.Fatalf("precondition: persisted %v, want one interval", before)
	}

	// Tuesday is valid; Wednesday overlaps itself. Placed last, after a valid
	// entry, so a naive implementation that inserted one at a time would
	// already have destroyed the previous schedule by the time it failed.
	_, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Tuesday, "09:00", "17:00"),
		input(schedulingmodel.Wednesday, "09:00", "13:00"),
		input(schedulingmodel.Wednesday, "12:00", "17:00"),
	})
	if err == nil {
		t.Fatal("ReplaceWeeklySchedule() accepted an overlapping schedule")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("error = %v, want VALIDATION_FAILED", err)
	}

	after := fixture.storedIntervals(t)
	if len(after) != 1 || after[0].DayOfWeek != before[0].DayOfWeek || after[0].StartTime != before[0].StartTime {
		t.Fatalf("the previous schedule was disturbed by a rejected replacement:\n  before %+v\n  after  %+v", before, after)
	}
}

// A schedule is validated in full BEFORE any transaction opens, so an
// invalid day never opens a transaction at all — proven the same way
// unreachableTxBeginner proves it at the unit level, but here over a real
// *sql.DB that would genuinely begin a transaction if reached.
func TestReplaceWeeklyScheduleRollsBackOnAnInvalidDayWithoutDisturbingTheExistingSchedule(t *testing.T) {
	fixture := newWorkingHoursFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Friday, "09:00", "16:00"),
	}); err != nil {
		t.Fatal(err)
	}

	_, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, []IntervalInput{
		{DayOfWeek: "FUNDAY", StartTime: "09:00", EndTime: "17:00"},
	})
	if err == nil {
		t.Fatal("ReplaceWeeklySchedule() accepted an invalid day")
	}

	stored := fixture.storedIntervals(t)
	if len(stored) != 1 || stored[0].DayOfWeek != schedulingmodel.Friday {
		t.Fatalf("the previous schedule was disturbed: %+v", stored)
	}
}

// A staff ID belonging to another tenant is reported as STAFF_NOT_FOUND, and
// no cross-tenant schedule is ever written.
func TestReplaceWeeklyScheduleRefusesACrossTenantStaffID(t *testing.T) {
	fixture := newWorkingHoursFixture(t)

	_, err := fixture.service.ReplaceWeeklySchedule(context.Background(), whSvcTenantB, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Monday, "09:00", "17:00"),
	})
	if err == nil {
		t.Fatal("ReplaceWeeklySchedule() accepted another tenant's staff id")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeStaffNotFound {
		t.Fatalf("error = %v, want STAFF_NOT_FOUND", err)
	}
	if stored := fixture.storedIntervals(t); len(stored) != 0 {
		t.Fatalf("a rejected cross-tenant request wrote a schedule: %+v", stored)
	}
}

// Archiving a technician keeps their working-hours rows: it changes
// bookability, not history, mirroring TestArchivingStaffKeepsCapabilityRows.
func TestArchivingStaffKeepsWorkingHoursRows(t *testing.T) {
	fixture := newWorkingHoursFixture(t)
	ctx := context.Background()
	staffRepo := schedulingrepository.NewPostgresStaffRepository(fixture.db)

	if _, err := fixture.service.ReplaceWeeklySchedule(ctx, whSvcTenantA, whSvcStaffA, []IntervalInput{
		input(schedulingmodel.Monday, "09:00", "17:00"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := staffRepo.Archive(ctx, whSvcTenantA, whSvcStaffA); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	if stored := fixture.storedIntervals(t); len(stored) != 1 {
		t.Fatalf("archiving removed working hours: %v", stored)
	}
}
