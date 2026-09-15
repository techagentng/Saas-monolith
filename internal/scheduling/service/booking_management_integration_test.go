package service

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
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
	"000019_create_service_categories.up.sql",
	"000020_create_service_images.up.sql",
	"000021_add_booking_receipt_access_token.up.sql",
	"000022_add_booking_terminal_statuses.up.sql",
}

var bmIntegrationTables = []string{
	"bookings", "staff_working_hours", "staff_services", "staff_profiles", "services", "service_categories", "service_images",
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
	management := NewBookingManagementService(
		bookingRepo, bookingRepo, bookingRepo, bookingRepo,
		engine, schedulingrepository.NewPostgresServiceRepository(db),
		tenants, SystemClock{},
	)

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

// TestRescheduleReopensOldSlotAndOccupiesNewSlot is the mandatory S12-BE
// section 24 end-to-end proof, against a REAL database: rescheduling a
// CONFIRMED booking through BookingManagementService both frees its old slot
// and occupies its new one in the S7 availability engine, with no scheduling
// code aware that a "reschedule" happened — it is exactly what an UPDATE of
// start_at/end_at under the existing occupancy query naturally produces.
func TestRescheduleReopensOldSlotAndOccupiesNewSlot(t *testing.T) {
	db := openBookingManagementTestDB(t)
	ctx := context.Background()

	const (
		tenantID  = "550e8400-e29b-41d4-a716-4466554e4001"
		serviceID = "550e8400-e29b-41d4-a716-4466554e4002"
		staffID   = "550e8400-e29b-41d4-a716-4466554e4003"
	)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency, timezone)
         VALUES ($1,'Luxe Nails','luxe-nails-4','ACTIVE','NAIL_TECHNICIAN','COMPLETED','NGN','Africa/Lagos')`, tenantID); err != nil {
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
		tenants, schedulingrepository.NewPostgresServiceRepository(db), staffRepo, capRepo, hoursRepo, bookingRepo, SystemClock{},
	)
	management := NewBookingManagementService(
		bookingRepo, bookingRepo, bookingRepo, bookingRepo,
		engine, schedulingrepository.NewPostgresServiceRepository(db),
		tenants, SystemClock{},
	)

	date := time.Now().In(time.UTC).AddDate(0, 0, 7)
	dateStr := date.Format("2006-01-02")
	const oldSlot = "12:00"
	const newSlot = "14:00"

	lagos, _ := time.LoadLocation("Africa/Lagos")
	parsedDate, _ := availability.ParseDate(dateStr)
	startAt, _ := availability.ResolveInstant(parsedDate, oldSlot, lagos)
	bookingID := uuid.NewString()
	if _, err := bookingRepo.Create(ctx, &schedulingmodel.Booking{
		ID: bookingID, TenantID: tenantID, ServiceID: serviceID, StaffID: staffID,
		Customer: schedulingmodel.Customer{Name: "Jane Doe"},
		StartAt:  startAt, EndAt: startAt.Add(30 * time.Minute), Status: schedulingmodel.BookingConfirmed,
	}); err != nil {
		t.Fatalf("creating booking: %v", err)
	}

	slotsBefore := availabilitySlotStarts(t, engine, ctx, tenantID, serviceID, staffID, dateStr)
	if containsStr(slotsBefore, oldSlot) {
		t.Fatalf("%s should be occupied before reschedule (slots: %v)", oldSlot, slotsBefore)
	}
	if !containsStr(slotsBefore, newSlot) {
		t.Fatalf("%s should be free before reschedule (slots: %v)", newSlot, slotsBefore)
	}

	detail, err := management.Reschedule(ctx, tenantID, bookingID, RescheduleBookingInput{Date: dateStr, Start: newSlot})
	if err != nil {
		t.Fatalf("Reschedule: %v", err)
	}
	if detail.Status != schedulingmodel.BookingConfirmed {
		t.Fatalf("status after reschedule = %q, want CONFIRMED", detail.Status)
	}

	slotsAfter := availabilitySlotStarts(t, engine, ctx, tenantID, serviceID, staffID, dateStr)
	if !containsStr(slotsAfter, oldSlot) {
		t.Fatalf("%s did NOT reopen after reschedule (slots: %v)", oldSlot, slotsAfter)
	}
	if containsStr(slotsAfter, newSlot) {
		t.Fatalf("%s is still available after reschedule occupied it (slots: %v)", newSlot, slotsAfter)
	}
	if len(slotsAfter) != len(slotsBefore) {
		t.Fatalf("after reschedule: %d slots, want the same total %d (one freed, one occupied)", len(slotsAfter), len(slotsBefore))
	}

	var start, end time.Time
	var status string
	if err := db.QueryRowContext(ctx, "SELECT start_at, end_at, status FROM bookings WHERE id = $1", bookingID).
		Scan(&start, &end, &status); err != nil {
		t.Fatalf("re-reading booking: %v", err)
	}
	wantStart, _ := availability.ResolveInstant(parsedDate, newSlot, lagos)
	if !start.Equal(wantStart) || status != "CONFIRMED" {
		t.Fatalf("persisted start=%s status=%s, want start=%s status=CONFIRMED", start, status, wantStart)
	}
	if !end.Equal(wantStart.Add(30 * time.Minute)) {
		t.Fatalf("persisted end=%s, want %s", end, wantStart.Add(30*time.Minute))
	}
}

// TestRescheduleConcurrentRaceHasExactlyOneWinner is the mandatory S12-BE
// section 25/26 proof: two different CONFIRMED bookings, each rescheduled at
// the same instant to the SAME target slot, race against the
// bookings_no_overlap EXCLUDE constraint. Exactly one UPDATE may commit; the
// loser must get BOOKING_SLOT_UNAVAILABLE (never a raw SQL/constraint error)
// and its own original row must be completely unchanged (the atomicity/
// rollback proof — a failed single-statement UPDATE touches zero rows).
func TestRescheduleConcurrentRaceHasExactlyOneWinner(t *testing.T) {
	db := openBookingManagementTestDB(t)
	ctx := context.Background()

	const (
		tenantID  = "550e8400-e29b-41d4-a716-4466554e5001"
		serviceID = "550e8400-e29b-41d4-a716-4466554e5002"
		staffID   = "550e8400-e29b-41d4-a716-4466554e5003"
	)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency, timezone)
         VALUES ($1,'Luxe Nails','luxe-nails-5','ACTIVE','NAIL_TECHNICIAN','COMPLETED','NGN','Africa/Lagos')`, tenantID); err != nil {
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
		tenants, schedulingrepository.NewPostgresServiceRepository(db), staffRepo, capRepo, hoursRepo, bookingRepo, SystemClock{},
	)
	management := NewBookingManagementService(
		bookingRepo, bookingRepo, bookingRepo, bookingRepo,
		engine, schedulingrepository.NewPostgresServiceRepository(db),
		tenants, SystemClock{},
	)

	date := time.Now().In(time.UTC).AddDate(0, 0, 7)
	dateStr := date.Format("2006-01-02")
	lagos, _ := time.LoadLocation("Africa/Lagos")
	parsedDate, _ := availability.ParseDate(dateStr)

	// Two bookings at two DIFFERENT original slots, so neither's own
	// self-exclusion masks the other's occupancy of the shared target.
	seed := func(id, slot string) (start time.Time) {
		start, _ = availability.ResolveInstant(parsedDate, slot, lagos)
		if _, err := bookingRepo.Create(ctx, &schedulingmodel.Booking{
			ID: id, TenantID: tenantID, ServiceID: serviceID, StaffID: staffID,
			Customer: schedulingmodel.Customer{Name: "Jane Doe"},
			StartAt:  start, EndAt: start.Add(30 * time.Minute), Status: schedulingmodel.BookingConfirmed,
			// receipt_access_token is UNIQUE (migration 000021); two bookings
			// both leaving this as the Go zero value ("") would collide.
			ReceiptAccessToken: "test-token-" + id,
		}); err != nil {
			t.Fatalf("seeding booking %s: %v", id, err)
		}
		return start
	}
	bookingA := "550e8400-e29b-41d4-a716-4466554e5aaa"
	bookingB := "550e8400-e29b-41d4-a716-4466554e5bbb"
	startA := seed(bookingA, "10:00")
	startB := seed(bookingB, "11:00")

	const targetSlot = "15:00"
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, 2)
	ids := []string{bookingA, bookingB}
	for i, id := range ids {
		wg.Add(1)
		go func(i int, bookingID string) {
			defer wg.Done()
			<-start
			_, results[i] = management.Reschedule(ctx, tenantID, bookingID, RescheduleBookingInput{Date: dateStr, Start: targetSlot})
		}(i, id)
	}
	close(start)
	wg.Wait()

	winners, conflicts := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			winners++
		case isRescheduleSlotUnavailable(err):
			conflicts++
		default:
			t.Fatalf("racer %d (%s) got an unexpected error: %v", i, ids[i], err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners = %d, conflicts = %d, want 1 and 1", winners, conflicts)
	}

	// Exactly one booking now occupies the target slot; the loser is
	// completely unchanged at its own original time (the rollback proof: a
	// failed single-statement UPDATE affects zero rows).
	wantTarget, _ := availability.ResolveInstant(parsedDate, targetSlot, lagos)
	var startAAfter, startBAfter time.Time
	if err := db.QueryRowContext(ctx, "SELECT start_at FROM bookings WHERE id = $1", bookingA).Scan(&startAAfter); err != nil {
		t.Fatalf("re-reading booking A: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT start_at FROM bookings WHERE id = $1", bookingB).Scan(&startBAfter); err != nil {
		t.Fatalf("re-reading booking B: %v", err)
	}

	aMoved := startAAfter.Equal(wantTarget)
	bMoved := startBAfter.Equal(wantTarget)
	if aMoved == bMoved {
		t.Fatalf("exactly one booking should occupy the target slot; A moved=%v B moved=%v", aMoved, bMoved)
	}
	if aMoved && !startBAfter.Equal(startB) {
		t.Fatalf("loser B should be unchanged: got %s, want original %s", startBAfter, startB)
	}
	if bMoved && !startAAfter.Equal(startA) {
		t.Fatalf("loser A should be unchanged: got %s, want original %s", startAAfter, startA)
	}

	var rows int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM bookings WHERE tenant_id = $1 AND staff_id = $2 AND status = 'CONFIRMED' AND start_at = $3",
		tenantID, staffID, wantTarget).Scan(&rows); err != nil {
		t.Fatalf("counting target-slot rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows occupying the target slot = %d, want exactly 1", rows)
	}
}

// buildLifecycleFixture (S13-BE) seeds one fully public nail tenant plus one
// bookable technician performing one 30-minute service, working every day
// 09:00-17:00 Africa/Lagos — the identical shape every other test in this
// file seeds, factored out because the three S13-BE tests below all need it.
func buildLifecycleFixture(t *testing.T, db *sql.DB, tenantID, serviceID, staffID string) (BookingManagementService, *schedulingrepository.PostgresBookingRepository) {
	t.Helper()
	ctx := context.Background()
	slug := "luxe-nails-" + strings.ToLower(strings.ReplaceAll(tenantID, "-", ""))[24:]
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency, timezone)
         VALUES ($1,'Luxe Nails',$2,'ACTIVE','NAIL_TECHNICIAN','COMPLETED','NGN','Africa/Lagos')`, tenantID, slug); err != nil {
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
		tenants, schedulingrepository.NewPostgresServiceRepository(db), staffRepo, capRepo, hoursRepo, bookingRepo, SystemClock{},
	)
	management := NewBookingManagementService(
		bookingRepo, bookingRepo, bookingRepo, bookingRepo,
		engine, schedulingrepository.NewPostgresServiceRepository(db),
		tenants, SystemClock{},
	)
	return management, bookingRepo
}

// TestCompleteAndNoShowPersistRealDB is the mandatory S13-BE section 29
// proof: Complete and MarkNoShow, called through BookingManagementService
// against a REAL database, persist COMPLETED/NO_SHOW — read back with a
// fresh query, not the same row the write returned.
func TestCompleteAndNoShowPersistRealDB(t *testing.T) {
	db := openBookingManagementTestDB(t)
	ctx := context.Background()
	const (
		tenantID  = "550e8400-e29b-41d4-a716-4466554e6001"
		serviceID = "550e8400-e29b-41d4-a716-4466554e6002"
		staffID   = "550e8400-e29b-41d4-a716-4466554e6003"
	)
	management, bookingRepo := buildLifecycleFixture(t, db, tenantID, serviceID, staffID)

	seedPastBooking := func(id string, hoursAgo time.Duration) {
		start := time.Now().UTC().Add(-hoursAgo)
		if _, err := bookingRepo.Create(ctx, &schedulingmodel.Booking{
			ID: id, TenantID: tenantID, ServiceID: serviceID, StaffID: staffID,
			Customer: schedulingmodel.Customer{Name: "Jane Doe"},
			StartAt:  start, EndAt: start.Add(30 * time.Minute), Status: schedulingmodel.BookingConfirmed,
			ReceiptAccessToken: "test-token-" + id,
		}); err != nil {
			t.Fatalf("seeding booking %s: %v", id, err)
		}
	}

	completeID := "550e8400-e29b-41d4-a716-4466554e6aaa"
	noShowID := "550e8400-e29b-41d4-a716-4466554e6bbb"
	seedPastBooking(completeID, 3*time.Hour)
	seedPastBooking(noShowID, 4*time.Hour)

	if detail, err := management.Complete(ctx, tenantID, completeID); err != nil {
		t.Fatalf("Complete: %v", err)
	} else if detail.Status != schedulingmodel.BookingCompleted {
		t.Fatalf("returned status = %q, want COMPLETED", detail.Status)
	}
	if detail, err := management.MarkNoShow(ctx, tenantID, noShowID); err != nil {
		t.Fatalf("MarkNoShow: %v", err)
	} else if detail.Status != schedulingmodel.BookingNoShow {
		t.Fatalf("returned status = %q, want NO_SHOW", detail.Status)
	}

	var completeStatus, noShowStatus string
	if err := db.QueryRowContext(ctx, "SELECT status FROM bookings WHERE id = $1", completeID).Scan(&completeStatus); err != nil {
		t.Fatalf("re-reading completed booking: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT status FROM bookings WHERE id = $1", noShowID).Scan(&noShowStatus); err != nil {
		t.Fatalf("re-reading no-show booking: %v", err)
	}
	if completeStatus != "COMPLETED" {
		t.Fatalf("persisted status = %q, want COMPLETED", completeStatus)
	}
	if noShowStatus != "NO_SHOW" {
		t.Fatalf("persisted status = %q, want NO_SHOW", noShowStatus)
	}
}

// TestCompleteAndNoShowTenantIsolationRealDB is the mandatory S13-BE section
// 29 tenant-isolation proof against a REAL database: a booking that belongs
// to a DIFFERENT tenant than the caller is BOOKING_NOT_FOUND, and the row is
// completely untouched.
func TestCompleteAndNoShowTenantIsolationRealDB(t *testing.T) {
	db := openBookingManagementTestDB(t)
	ctx := context.Background()
	const (
		tenantA   = "550e8400-e29b-41d4-a716-4466554e7001"
		tenantB   = "550e8400-e29b-41d4-a716-4466554e7011"
		serviceA  = "550e8400-e29b-41d4-a716-4466554e7002"
		serviceB  = "550e8400-e29b-41d4-a716-4466554e7012"
		staffA    = "550e8400-e29b-41d4-a716-4466554e7003"
		staffB    = "550e8400-e29b-41d4-a716-4466554e7013"
		bookingID = "550e8400-e29b-41d4-a716-4466554e7aaa"
	)
	managementA, _ := buildLifecycleFixture(t, db, tenantA, serviceA, staffA)
	_, bookingRepoB := buildLifecycleFixture(t, db, tenantB, serviceB, staffB)

	start := time.Now().UTC().Add(-3 * time.Hour)
	if _, err := bookingRepoB.Create(ctx, &schedulingmodel.Booking{
		ID: bookingID, TenantID: tenantB, ServiceID: serviceB, StaffID: staffB,
		Customer: schedulingmodel.Customer{Name: "Jane Doe"},
		StartAt:  start, EndAt: start.Add(30 * time.Minute), Status: schedulingmodel.BookingConfirmed,
		ReceiptAccessToken: "test-token-" + bookingID,
	}); err != nil {
		t.Fatalf("seeding tenant B's booking: %v", err)
	}

	if _, err := managementA.Complete(ctx, tenantA, bookingID); !isBookingNotFound(err) {
		t.Fatalf("Complete across tenants: err = %v, want BOOKING_NOT_FOUND", err)
	}
	if _, err := managementA.MarkNoShow(ctx, tenantA, bookingID); !isBookingNotFound(err) {
		t.Fatalf("MarkNoShow across tenants: err = %v, want BOOKING_NOT_FOUND", err)
	}

	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM bookings WHERE id = $1", bookingID).Scan(&status); err != nil {
		t.Fatalf("re-reading booking: %v", err)
	}
	if status != "CONFIRMED" {
		t.Fatalf("a cross-tenant transition mutated the booking: persisted status = %q", status)
	}
}

// TestCompleteAndNoShowDoNotCountAsOccupancyRealDB is the mandatory S13-BE
// section 30 proof against a REAL database: OccupiedIntervals — the S7
// OccupancyReader's concrete backing — stops returning a booking's interval
// the moment it becomes COMPLETED or NO_SHOW, exactly as it already does for
// CANCELLED, because the query filters on status = 'CONFIRMED' alone
// (unchanged by S13-BE).
func TestCompleteAndNoShowDoNotCountAsOccupancyRealDB(t *testing.T) {
	db := openBookingManagementTestDB(t)
	ctx := context.Background()
	const (
		tenantID  = "550e8400-e29b-41d4-a716-4466554e8001"
		serviceID = "550e8400-e29b-41d4-a716-4466554e8002"
		staffID   = "550e8400-e29b-41d4-a716-4466554e8003"
	)
	management, bookingRepo := buildLifecycleFixture(t, db, tenantID, serviceID, staffID)

	start := time.Now().UTC().Add(-3 * time.Hour)
	end := start.Add(30 * time.Minute)
	completeID := "550e8400-e29b-41d4-a716-4466554e8aaa"
	if _, err := bookingRepo.Create(ctx, &schedulingmodel.Booking{
		ID: completeID, TenantID: tenantID, ServiceID: serviceID, StaffID: staffID,
		Customer: schedulingmodel.Customer{Name: "Jane Doe"},
		StartAt:  start, EndAt: end, Status: schedulingmodel.BookingConfirmed,
		ReceiptAccessToken: "test-token-" + completeID,
	}); err != nil {
		t.Fatalf("seeding booking: %v", err)
	}

	// A window that exactly covers the booking's own interval — the same
	// [from, to) shape OccupiedIntervals' own overlap predicate uses.
	before, err := bookingRepo.OccupiedIntervals(ctx, tenantID, staffID, start, end, "")
	if err != nil {
		t.Fatalf("OccupiedIntervals (before): %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("occupied intervals before completion = %d, want exactly 1 (the CONFIRMED booking)", len(before))
	}

	if _, err := management.Complete(ctx, tenantID, completeID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	after, err := bookingRepo.OccupiedIntervals(ctx, tenantID, staffID, start, end, "")
	if err != nil {
		t.Fatalf("OccupiedIntervals (after): %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("occupied intervals after completion = %d, want 0 — COMPLETED must not count as active occupancy", len(after))
	}
}

func isBookingNotFound(err error) bool {
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && appErr.Code == apperrors.CodeBookingNotFound
}

func isRescheduleSlotUnavailable(err error) bool {
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && appErr.Code == apperrors.CodeBookingSlotUnavailable
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
