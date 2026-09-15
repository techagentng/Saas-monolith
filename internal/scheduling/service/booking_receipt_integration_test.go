package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/receipt"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// S12-BE's mandatory end-to-end proof, against a REAL database: create a
// booking through the real public booking flow, then retrieve its receipt
// through the real public receipt flow, and confirm a real, non-empty PDF
// comes back — plus that a booking genuinely persisted under one tenant can
// never be retrieved through another tenant's slug, even with its own
// correct token.
//
// Skips unless TEST_DATABASE_URL / DATABASE_URL points at a disposable
// database (it DROPs tables), the same guard every other
// *_integration_test.go here uses.

var brIntegrationMigrations = []string{
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

var brIntegrationTables = []string{
	"bookings", "staff_working_hours", "staff_services", "staff_profiles", "services", "service_categories", "service_images",
	"user_roles", "role_permissions", "permissions", "roles",
	"tenant_memberships", "sessions", "tenants", "users",
}

func openReceiptTestDB(t *testing.T) *sql.DB {
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
		for _, table := range brIntegrationTables {
			db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table+" CASCADE")
		}
	}
	drop()
	for _, m := range brIntegrationMigrations {
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

// seedNailTenant creates a fully public NAIL_TECHNICIAN tenant with one
// 30-minute service, one bookable technician assigned to it, and working
// hours every day 09:00-17:00 — enough for a real public booking to succeed
// through the real S7/S9/S10 stack.
func seedNailTenant(t *testing.T, db *sql.DB, tenantID, slug, serviceID, staffID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, business_type, onboarding_status, currency, timezone)
         VALUES ($1, $2, $3, 'ACTIVE', 'NAIL_TECHNICIAN', 'COMPLETED', 'NGN', 'Africa/Lagos')`,
		tenantID, "Tenant "+slug, slug); err != nil {
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
	if err := schedulingrepository.NewPostgresCapabilityRepository(db).Assign(ctx, tenantID, staffID, serviceID); err != nil {
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
}

// nextMonday finds a real future Monday in YYYY-MM-DD form, so the booking
// created against it is never in the past regardless of when this test runs.
func nextMonday() string {
	now := time.Now().UTC()
	for {
		now = now.AddDate(0, 0, 1)
		if now.Weekday() == time.Monday {
			return now.Format("2006-01-02")
		}
	}
}

func TestPublicBookingReceiptEndToEndAgainstARealDatabase(t *testing.T) {
	db := openReceiptTestDB(t)
	ctx := context.Background()

	const (
		tenantID  = "550e8400-e29b-41d4-a716-4466554f1001"
		serviceID = "550e8400-e29b-41d4-a716-4466554f1002"
		staffID   = "550e8400-e29b-41d4-a716-4466554f1003"
	)
	seedNailTenant(t, db, tenantID, "receipt-nails", serviceID, staffID)

	tenants := tenantrepository.NewPostgresTenantRepository(db)
	publicTenants := tenantservice.NewPublicTenantService(tenants)
	serviceRepo := schedulingrepository.NewPostgresServiceRepository(db)
	staffRepo := schedulingrepository.NewPostgresStaffRepository(db)
	capRepo := schedulingrepository.NewPostgresCapabilityRepository(db)
	hoursRepo := schedulingrepository.NewPostgresWorkingHoursRepository(db)
	bookingRepo := schedulingrepository.NewPostgresBookingRepository(db)

	availabilityEngine := NewAvailabilityService(tenants, serviceRepo, staffRepo, capRepo, hoursRepo, bookingRepo, SystemClock{})
	bookingSvc := NewBookingService(publicTenants, availabilityEngine, serviceRepo, staffRepo, bookingRepo)
	receiptSvc := NewBookingReceiptService(publicTenants, bookingRepo, serviceRepo, staffRepo, receipt.NewPDFGenerator(nil))

	// Step 1: create a real booking through the real public flow.
	created, err := bookingSvc.CreatePublicBooking(ctx, "receipt-nails", CreateBookingInput{
		ServiceID: serviceID,
		StaffID:   staffID,
		Date:      nextMonday(),
		Start:     "09:00",
		Customer:  CustomerInput{Name: "Jane Doe", Email: strPtr("jane@example.test")},
	})
	if err != nil {
		t.Fatalf("CreatePublicBooking() error = %v", err)
	}
	if created.ReceiptToken == "" {
		t.Fatal("created booking has no receipt token")
	}

	// Step 2: request the receipt through the real public receipt flow.
	pdfBytes, err := receiptSvc.GetReceipt(ctx, "receipt-nails", created.Reference, created.ReceiptToken)
	if err != nil {
		t.Fatalf("GetReceipt() error = %v", err)
	}
	if len(pdfBytes) == 0 {
		t.Fatal("GetReceipt() returned an empty body")
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatalf("body does not start with the PDF magic bytes: %q", pdfBytes[:20])
	}

	// Step 3: tenant isolation. A second, completely separate tenant's slug
	// must never retrieve tenant one's booking, even with its own correct
	// token and reference.
	const (
		otherTenantID = "550e8400-e29b-41d4-a716-4466554f2001"
		otherService  = "550e8400-e29b-41d4-a716-4466554f2002"
		otherStaff    = "550e8400-e29b-41d4-a716-4466554f2003"
	)
	seedNailTenant(t, db, otherTenantID, "other-nails", otherService, otherStaff)

	_, err = receiptSvc.GetReceipt(ctx, "other-nails", created.Reference, created.ReceiptToken)
	if err == nil {
		t.Fatal("a booking from tenant one was retrievable through tenant two's slug")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeBookingNotFound {
		t.Fatalf("error = %v, want BOOKING_NOT_FOUND", err)
	}
}
