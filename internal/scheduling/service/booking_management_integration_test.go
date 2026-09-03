package service

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// This is the mandatory S11 end-to-end availability check, against a REAL
// database: a CONFIRMED booking removes its slot from the S7 availability
// engine, and cancelling that booking through BookingManagementService brings
// the exact slot back — with no change to any scheduling code, purely because
// the occupancy query filters on status = 'CONFIRMED'.
//
// It skips unless TEST_DATABASE_URL / DATABASE_URL points at a disposable
// database (it DROPs tables), the same guard every other *_integration_test.go
// here uses.

var bmIntegrationMigrations = []string{
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
	"000016_create_bookings.up.sql",
	"000017_seed_booking_permissions.up.sql",
	"000018_add_booking_status_index.up.sql",
}

var bmIntegrationTables = []string{
	"bookings", "staff_working_hours", "staff_services", "staff_profiles", "services",
	"user_roles", "role_permissions", "permissions", "roles",
	"tenant_memberships", "sessions", "tenants", "users",
}

func openBookingManagementTestDB(t *testing.T) *sql.DB {
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
			t.Fatalf("refusing to run destructive tests against the development database %q", name)
		}
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	drop := func() {
		for _, table := range bmIntegrationTables {
			db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table+" CASCADE")
		}
	}
	drop()
	for _, m := range bmIntegrationMigrations {
		script, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", m))
		if err != nil {
			t.Fatalf("reading %s: %v", m, err)
		}
		if _, err := db.ExecContext(context.Background(), string(script)); err != nil {
			t.Fatalf("applying %s: %v", m, err)
		}
	}
	t.Cleanup(drop)
	return db
}

func TestCancelBookingReopensPublicAvailability(t *testing.T) {
	db := openBookingManagementTestDB(t)
	ctx := context.Background()

	const (
		tenantID  = "550e8400-e29b-41d4-a716-4466554e1001"
		serviceID = "550e8400-e29b-41d4-a716-4466554e1002"
		staffID   = "550e8400-e29b-41d4-a716-4466554e1003"
	)

	// Seed a fully public nail tenant with Africa/Lagos, one 30-min service,
	// one bookable technician who performs it, working every day 09:00-17:00.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency, timezone)
         VALUES ($1,'Luxe Nails','luxe-nails','ACTIVE','NAIL_TECHNICIAN','COMPLETED','NGN','Africa/Lagos')`, tenantID); err != nil {
		t.Fatalf("seeding tenant: %v", err)
	}
	staffRepo := schedulingrepository.NewPostgresStaffRepository(db)
	if _, err := staffRepo.Create(ctx, &schedulingmodel.StaffProfile{ID: staffID, TenantID: tenantID, DisplayName: "Ada", IsBookable: true}); err != nil {
		t.Fatalf("seeding staff: %v", err)
	}
	if _, err := schedulingrepository.NewPostgresServiceRepository(db).Create(ctx, &schedulingmodel.Service{
		ID: serviceID, TenantID: tenantID, Name: "Gel Manicure", DurationMinutes: 30, PriceMinor: 150000,
	}); err != nil {
		t.Fatalf("seeding service: %v", err)
	}
	capRepo := schedulingrepository.NewPostgresCapabilityRepository(db)
	if err := capRepo.Assign(ctx, tenantID, staffID, serviceID); err != nil {
		t.Fatalf("assigning capability: %v", err)
	}
	hoursRepo := schedulingrepository.NewPostgresWorkingHoursRepository(db)
	for _, day := range []schedulingmodel.DayOfWeek{
		schedulingmodel.Monday, schedulingmodel.Tuesday, schedulingmodel.Wednesday, schedulingmodel.Thursday,
		schedulingmodel.Friday, schedulingmodel.Saturday, schedulingmodel.Sunday,
	} {
		if _, err := hoursRepo.Create(ctx, &schedulingmodel.WorkingHourInterval{
			ID: uuid.NewString(), TenantID: tenantID, StaffID: staffID, DayOfWeek: day, StartTime: "09:00", EndTime: "17:00",
		}); err != nil {
			t.Fatalf("seeding working hours: %v", err)
		}
	}

	bookingRepo := schedulingrepository.NewPostgresBookingRepository(db)
	tenants := tenantrepository.NewPostgresTenantRepository(db)

	engine := NewAvailabilityService(
		tenants,
		schedulingrepository.NewPostgresServiceRepository(db),
		staffRepo,
		capRepo,
		hoursRepo,
		bookingRepo, // real occupancy
		SystemClock{},
	)
	management := NewBookingManagementService(bookingRepo, bookingRepo, tenants, SystemClock{})

	// A date a week out (a real future weekday) and a slot in the middle of
	// the working day, so past-slot filtering is irrelevant.
	date := time.Now().In(time.UTC).AddDate(0, 0, 7)
	dateStr := date.Format("2006-01-02")
	const slot = "12:00"

	slotsBefore := availabilitySlotStarts(t, engine, ctx, tenantID, serviceID, staffID, dateStr)
	if !containsStr(slotsBefore, slot) {
		t.Fatalf("%s not initially available (slots: %v)", slot, slotsBefore)
	}

	// Book it.
	lagos, _ := time.LoadLocation("Africa/Lagos")
	parsedDate, _ := availability.ParseDate(dateStr)
	startAt, _ := availability.ResolveInstant(parsedDate, slot, lagos)
	bookingID := uuid.NewString()
	if _, err := bookingRepo.Create(ctx, &schedulingmodel.Booking{
		ID: bookingID, TenantID: tenantID, ServiceID: serviceID, StaffID: staffID,
		Customer: schedulingmodel.Customer{Name: "Jane Doe"},
		StartAt:  startAt, EndAt: startAt.Add(30 * time.Minute), Status: schedulingmodel.BookingConfirmed,
	}); err != nil {
		t.Fatalf("creating booking: %v", err)
	}

	slotsBooked := availabilitySlotStarts(t, engine, ctx, tenantID, serviceID, staffID, dateStr)
	if containsStr(slotsBooked, slot) {
		t.Fatalf("%s still available after a CONFIRMED booking (slots: %v)", slot, slotsBooked)
	}
	if len(slotsBooked) != len(slotsBefore)-1 {
		t.Fatalf("booking removed %d slots, want exactly 1", len(slotsBefore)-len(slotsBooked))
	}

	// Cancel it through the management service.
	detail, err := management.Cancel(ctx, tenantID, bookingID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if detail.Status != schedulingmodel.BookingCancelled {
		t.Fatalf("cancel status = %q", detail.Status)
	}

	slotsAfter := availabilitySlotStarts(t, engine, ctx, tenantID, serviceID, staffID, dateStr)
	if !containsStr(slotsAfter, slot) {
		t.Fatalf("%s did NOT reopen after cancellation (slots: %v)", slot, slotsAfter)
	}
	if len(slotsAfter) != len(slotsBefore) {
		t.Fatalf("after cancel: %d slots, want the original %d", len(slotsAfter), len(slotsBefore))
	}

	// The booking row survives, as CANCELLED.
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM bookings WHERE id = $1", bookingID).Scan(&status); err != nil {
		t.Fatalf("re-reading booking: %v", err)
	}
	if status != "CANCELLED" {
		t.Fatalf("persisted status = %q, want CANCELLED (history preserved)", status)
	}
}

func availabilitySlotStarts(t *testing.T, engine AvailabilityService, ctx context.Context, tenantID, serviceID, staffID, date string) []string {
	t.Helper()
	result, err := engine.GetAvailability(ctx, tenantID, serviceID, staffID, date)
	if err != nil {
		t.Fatalf("GetAvailability(%s): %v", date, err)
	}
	starts := make([]string, len(result.Slots))
	for i, s := range result.Slots {
		starts[i] = s.Start
	}
	return starts
}

func containsStr(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
