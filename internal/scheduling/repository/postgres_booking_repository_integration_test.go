package repository

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// These exercise PostgresBookingRepository against a REAL database — the only
// place the bookings_no_overlap exclusion constraint, the composite foreign
// keys, and (critically) concurrent conflicting inserts can actually be
// proven. They skip unless TEST_DATABASE_URL / DATABASE_URL points at the
// disposable Docker database (docker-compose.test.yml), exactly like every
// other *_integration_test.go here.

const (
	bkTenantA  = "550e8400-e29b-41d4-a716-4466554d1001"
	bkTenantB  = "550e8400-e29b-41d4-a716-4466554d1002"
	bkServiceA = "550e8400-e29b-41d4-a716-4466554d1003"
	bkStaffA   = "550e8400-e29b-41d4-a716-4466554d1004"
	bkStaffB   = "550e8400-e29b-41d4-a716-4466554d1005"
)

// bookingAt builds a 30-minute booking starting at the given UTC wall time on
// 2026-09-07, for the given tenant/staff.
func bookingAt(id, tenantID, staffID string, hour, minute int) *model.Booking {
	start := time.Date(2026, 9, 7, hour, minute, 0, 0, time.UTC)
	return &model.Booking{
		ID:        id,
		TenantID:  tenantID,
		ServiceID: bkServiceA,
		StaffID:   staffID,
		Customer:  model.Customer{Name: "Jane Doe"},
		StartAt:   start,
		EndAt:     start.Add(30 * time.Minute),
		Status:    model.BookingConfirmed,
	}
}

func seedBookingPrerequisites(t *testing.T, db *sql.DB) {
	t.Helper()
	currency := "NGN"
	seedTenant(t, db, bkTenantA, "tenant-a", &currency)
	seedTenant(t, db, bkTenantB, "tenant-b", &currency)
	staffRepo := NewPostgresStaffRepository(db)
	if _, err := staffRepo.Create(context.Background(), &model.StaffProfile{ID: bkStaffA, TenantID: bkTenantA, DisplayName: "Ada", IsBookable: true}); err != nil {
		t.Fatalf("seeding staff A: %v", err)
	}
	if _, err := staffRepo.Create(context.Background(), &model.StaffProfile{ID: bkStaffB, TenantID: bkTenantB, DisplayName: "Bola", IsBookable: true}); err != nil {
		t.Fatalf("seeding staff B: %v", err)
	}
	if _, err := NewPostgresServiceRepository(db).Create(context.Background(), &model.Service{
		ID: bkServiceA, TenantID: bkTenantA, Name: "Gel Manicure", DurationMinutes: 30, PriceMinor: 1999,
	}); err != nil {
		t.Fatalf("seeding service A: %v", err)
	}
}

func assertBookingSlotUnavailable(t *testing.T, err error, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeBookingSlotUnavailable {
		t.Fatalf("%s: error = %v, want BOOKING_SLOT_UNAVAILABLE", context, err)
	}
}

// --- round trip + occupancy --------------------------------------------

func TestBookingRepositoryCreateRoundTrips(t *testing.T) {
	db := openSchedulingTestDB(t)
	seedBookingPrerequisites(t, db)
	repo := NewPostgresBookingRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, bookingAt("550e8400-e29b-41d4-a716-4466554d2001", bkTenantA, bkStaffA, 10, 0))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != model.BookingConfirmed || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("Create() = %+v", created)
	}

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	occ, err := repo.OccupiedIntervals(ctx, bkTenantA, bkStaffA, from, to)
	if err != nil {
		t.Fatalf("OccupiedIntervals() error = %v", err)
	}
	if len(occ) != 1 || !occ[0].Start.Equal(created.StartAt) || !occ[0].End.Equal(created.EndAt) {
		t.Fatalf("OccupiedIntervals() = %+v, want the one booking", occ)
	}
}

func TestBookingRepositoryOccupancyIsTenantAndStaffScopedAndHalfOpen(t *testing.T) {
	db := openSchedulingTestDB(t)
	seedBookingPrerequisites(t, db)
	repo := NewPostgresBookingRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, bookingAt("550e8400-e29b-41d4-a716-4466554d2101", bkTenantA, bkStaffA, 10, 0)); err != nil {
		t.Fatalf("seeding booking: %v", err)
	}

	// A window that ends exactly when the booking starts must not see it
	// (half-open overlap).
	before, err := repo.OccupiedIntervals(ctx, bkTenantA, bkStaffA,
		time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC), time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("OccupiedIntervals() error = %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("a window ending exactly at the booking start saw it: %+v", before)
	}

	// A different staff member's window sees nothing.
	other, err := repo.OccupiedIntervals(ctx, bkTenantA, bkStaffB,
		time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("OccupiedIntervals() error = %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("occupancy leaked across staff members: %+v", other)
	}
}

// --- the exclusion constraint -----------------------------------------

func TestBookingRepositorySequentialOverlapIsRejected(t *testing.T) {
	db := openSchedulingTestDB(t)
	seedBookingPrerequisites(t, db)
	repo := NewPostgresBookingRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, bookingAt("550e8400-e29b-41d4-a716-4466554d2201", bkTenantA, bkStaffA, 10, 0)); err != nil {
		t.Fatalf("first booking: %v", err)
	}

	// Exact same interval.
	_, err := repo.Create(ctx, bookingAt("550e8400-e29b-41d4-a716-4466554d2202", bkTenantA, bkStaffA, 10, 0))
	assertBookingSlotUnavailable(t, err, "exact overlap")

	// Partial overlap (10:15-10:45).
	_, err = repo.Create(ctx, bookingAt("550e8400-e29b-41d4-a716-4466554d2203", bkTenantA, bkStaffA, 10, 15))
	assertBookingSlotUnavailable(t, err, "partial overlap")
}

func TestBookingRepositoryTouchingBoundaryIsAllowed(t *testing.T) {
	db := openSchedulingTestDB(t)
	seedBookingPrerequisites(t, db)
	repo := NewPostgresBookingRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, bookingAt("550e8400-e29b-41d4-a716-4466554d2301", bkTenantA, bkStaffA, 10, 0)); err != nil {
		t.Fatalf("first booking: %v", err)
	}
	// 10:30-11:00 starts exactly when the first ends — allowed.
	if _, err := repo.Create(ctx, bookingAt("550e8400-e29b-41d4-a716-4466554d2302", bkTenantA, bkStaffA, 10, 30)); err != nil {
		t.Fatalf("a touching booking was rejected: %v", err)
	}
}

func TestBookingRepositoryRejectsCrossTenantServiceOrStaff(t *testing.T) {
	db := openSchedulingTestDB(t)
	seedBookingPrerequisites(t, db)
	repo := NewPostgresBookingRepository(db)
	ctx := context.Background()

	// Tenant B claiming tenant A's staff (bkStaffA belongs to A).
	booking := bookingAt("550e8400-e29b-41d4-a716-4466554d2401", bkTenantB, bkStaffA, 10, 0)
	_, err := repo.Create(ctx, booking)
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("cross-tenant staff: error = %v, want VALIDATION_FAILED", err)
	}
}

// --- CONCURRENCY (mandatory) -----------------------------------------

// Eight goroutines race to book the identical tenant/staff/interval. Exactly
// one must win; the other seven must each get BOOKING_SLOT_UNAVAILABLE; and
// exactly one row must exist afterwards. This is the property a
// check-then-insert cannot provide and the exclusion constraint does.
func TestBookingRepositoryConcurrentConflictHasExactlyOneWinner(t *testing.T) {
	db := openSchedulingTestDB(t)
	seedBookingPrerequisites(t, db)
	repo := NewPostgresBookingRepository(db)
	ctx := context.Background()

	const racers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, racers)
	ids := []string{
		"550e8400-e29b-41d4-a716-4466554d3001", "550e8400-e29b-41d4-a716-4466554d3002",
		"550e8400-e29b-41d4-a716-4466554d3003", "550e8400-e29b-41d4-a716-4466554d3004",
		"550e8400-e29b-41d4-a716-4466554d3005", "550e8400-e29b-41d4-a716-4466554d3006",
		"550e8400-e29b-41d4-a716-4466554d3007", "550e8400-e29b-41d4-a716-4466554d3008",
	}

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // all fire together
			_, results[i] = repo.Create(ctx, bookingAt(ids[i], bkTenantA, bkStaffA, 14, 0))
		}(i)
	}
	close(start)
	wg.Wait()

	winners, conflicts := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			winners++
		case isCodeBookingSlotUnavailable(err):
			conflicts++
		default:
			t.Fatalf("racer %d got an unexpected error: %v", i, err)
		}
	}
	if winners != 1 || conflicts != racers-1 {
		t.Fatalf("winners = %d, conflicts = %d, want 1 and %d", winners, conflicts, racers-1)
	}

	var rows int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM bookings WHERE tenant_id = $1 AND staff_id = $2 AND status = 'CONFIRMED'",
		bkTenantA, bkStaffA).Scan(&rows); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("persisted rows = %d, want exactly 1 after a concurrent race", rows)
	}
}

func isCodeBookingSlotUnavailable(err error) bool {
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && appErr.Code == apperrors.CodeBookingSlotUnavailable
}

func assertBookingNotFound(t *testing.T, err error, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeBookingNotFound {
		t.Fatalf("%s: error = %v, want BOOKING_NOT_FOUND", context, err)
	}
}

// --- S11 list / find / cancel ---------------------------------------

func TestBookingRepositoryListFindAndCancel(t *testing.T) {
	db := openSchedulingTestDB(t)
	seedBookingPrerequisites(t, db)
	repo := NewPostgresBookingRepository(db)
	ctx := context.Background()

	confirmedID := "550e8400-e29b-41d4-a716-4466554d5001"
	if _, err := repo.Create(ctx, bookingAt(confirmedID, bkTenantA, bkStaffA, 10, 0)); err != nil {
		t.Fatalf("seeding booking: %v", err)
	}

	// FindByTenantAndID joins the names.
	found, err := repo.FindByTenantAndID(ctx, bkTenantA, confirmedID)
	if err != nil {
		t.Fatalf("FindByTenantAndID: %v", err)
	}
	if found.ServiceName != "Gel Manicure" || found.StaffName != "Ada" || found.ServiceDurationMins != 30 {
		t.Fatalf("relations not joined: %+v", found)
	}

	// Cross-tenant and nonexistent both read as BOOKING_NOT_FOUND.
	_, err = repo.FindByTenantAndID(ctx, bkTenantB, confirmedID)
	assertBookingNotFound(t, err, "cross-tenant find")
	_, err = repo.FindByTenantAndID(ctx, bkTenantA, "550e8400-e29b-41d4-a716-4466554d9999")
	assertBookingNotFound(t, err, "nonexistent find")

	// List is tenant-scoped and status-filtered in SQL.
	confirmed := model.BookingConfirmed
	list, err := repo.ListByTenant(ctx, bkTenantA, BookingListFilter{Status: &confirmed})
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if len(list) != 1 || list[0].Booking.ID != confirmedID || list[0].ServiceName != "Gel Manicure" {
		t.Fatalf("list = %+v", list)
	}
	if crossTenant, _ := repo.ListByTenant(ctx, bkTenantB, BookingListFilter{}); len(crossTenant) != 0 {
		t.Fatalf("tenant B saw %d of tenant A's bookings", len(crossTenant))
	}

	// Cancel: CONFIRMED -> CANCELLED, row preserved.
	cancelled, updated, err := repo.Cancel(ctx, bkTenantA, confirmedID)
	if err != nil || !updated || cancelled.Status != model.BookingCancelled {
		t.Fatalf("Cancel: updated=%v status=%v err=%v", updated, cancelled.Status, err)
	}
	var rowCount int
	_ = db.QueryRowContext(ctx, "SELECT count(*) FROM bookings WHERE id = $1", confirmedID).Scan(&rowCount)
	if rowCount != 1 {
		t.Fatalf("row count = %d after cancel, want 1 (history preserved)", rowCount)
	}

	// A second Cancel updates nothing (already cancelled).
	_, updated, err = repo.Cancel(ctx, bkTenantA, confirmedID)
	if err != nil || updated {
		t.Fatalf("second Cancel: updated=%v err=%v, want updated=false", updated, err)
	}

	// It now shows only under the CANCELLED filter.
	cancelledList, _ := repo.ListByTenant(ctx, bkTenantA, BookingListFilter{Status: ptrStatus(model.BookingCancelled)})
	if len(cancelledList) != 1 {
		t.Fatalf("cancelled list = %d, want 1", len(cancelledList))
	}
	confirmedList, _ := repo.ListByTenant(ctx, bkTenantA, BookingListFilter{Status: &confirmed})
	if len(confirmedList) != 0 {
		t.Fatalf("confirmed list = %d after cancel, want 0", len(confirmedList))
	}
}

// The mandatory occupancy loop at the repository (source-of-truth) layer: a
// CONFIRMED booking occupies its window; cancelling it frees the window with
// no scheduling-code change, because OccupiedIntervals already filters on
// status = 'CONFIRMED'.
func TestBookingRepositoryCancelReopensOccupancy(t *testing.T) {
	db := openSchedulingTestDB(t)
	seedBookingPrerequisites(t, db)
	repo := NewPostgresBookingRepository(db)
	ctx := context.Background()

	id := "550e8400-e29b-41d4-a716-4466554d6001"
	if _, err := repo.Create(ctx, bookingAt(id, bkTenantA, bkStaffA, 10, 0)); err != nil {
		t.Fatalf("create: %v", err)
	}

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)

	occ, _ := repo.OccupiedIntervals(ctx, bkTenantA, bkStaffA, from, to)
	if len(occ) != 1 {
		t.Fatalf("before cancel: occupied = %d, want 1", len(occ))
	}

	if _, _, err := repo.Cancel(ctx, bkTenantA, id); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	occ, _ = repo.OccupiedIntervals(ctx, bkTenantA, bkStaffA, from, to)
	if len(occ) != 0 {
		t.Fatalf("after cancel: occupied = %d, want 0 — the slot must be free again", len(occ))
	}
}

func ptrStatus(s model.BookingStatus) *model.BookingStatus { return &s }
