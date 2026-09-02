package repository

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

const (
	whTenantA = "550e8400-e29b-41d4-a716-446655461001"
	whTenantB = "550e8400-e29b-41d4-a716-446655461002"
	whStaffA  = "550e8400-e29b-41d4-a716-446655461003"
	whStaffB  = "550e8400-e29b-41d4-a716-446655461004"
)

func seedWorkingHoursStaff(t *testing.T, staffRepo *PostgresStaffRepository, id, tenantID, name string) {
	t.Helper()
	if _, err := staffRepo.Create(context.Background(), &model.StaffProfile{ID: id, TenantID: tenantID, DisplayName: name, IsBookable: true}); err != nil {
		t.Fatalf("seeding staff %s: %v", name, err)
	}
}

func newWorkingHourInterval(id, tenantID, staffID string, day model.DayOfWeek, start, end string) *model.WorkingHourInterval {
	return &model.WorkingHourInterval{ID: id, TenantID: tenantID, StaffID: staffID, DayOfWeek: day, StartTime: start, EndTime: end}
}

// --- saves and lists ----------------------------------------------------------

func TestWorkingHoursRepositorySavesAndListsAnInterval(t *testing.T) {
	db := openSchedulingTestDB(t)
	currency := "NGN"
	seedTenant(t, db, whTenantA, "tenant-a", &currency)
	staffRepo := NewPostgresStaffRepository(db)
	seedWorkingHoursStaff(t, staffRepo, whStaffA, whTenantA, "Ada")
	hours := NewPostgresWorkingHoursRepository(db)
	ctx := context.Background()

	created, err := hours.Create(ctx, newWorkingHourInterval("550e8400-e29b-41d4-a716-446655462001", whTenantA, whStaffA, model.Monday, "09:00", "17:00"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("Create() did not populate timestamps")
	}

	listed, err := hours.ListByStaff(ctx, whTenantA, whStaffA)
	if err != nil {
		t.Fatalf("ListByStaff() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListByStaff() = %d intervals, want 1", len(listed))
	}
	if listed[0].DayOfWeek != model.Monday || listed[0].StartTime != "09:00" || listed[0].EndTime != "17:00" {
		t.Fatalf("round-tripped interval = %+v, want Monday 09:00-17:00", listed[0])
	}
}

// The precise round-trip a naive TIME<->string binding could silently corrupt:
// a non-trivial minute value must survive exactly, not get rounded to the hour.
func TestWorkingHoursRepositoryRoundTripsMinutePrecisionExactly(t *testing.T) {
	db := openSchedulingTestDB(t)
	currency := "NGN"
	seedTenant(t, db, whTenantA, "tenant-a", &currency)
	staffRepo := NewPostgresStaffRepository(db)
	seedWorkingHoursStaff(t, staffRepo, whStaffA, whTenantA, "Ada")
	hours := NewPostgresWorkingHoursRepository(db)
	ctx := context.Background()

	if _, err := hours.Create(ctx, newWorkingHourInterval("550e8400-e29b-41d4-a716-446655462002", whTenantA, whStaffA, model.Thursday, "10:17", "18:43")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	listed, err := hours.ListByStaff(ctx, whTenantA, whStaffA)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].StartTime != "10:17" || listed[0].EndTime != "18:43" {
		t.Fatalf("listed = %+v, want exactly 10:17-18:43", listed)
	}
}

// --- deterministic ordering ----------------------------------------------------

func TestWorkingHoursRepositoryListsInDayThenStartTimeOrder(t *testing.T) {
	db := openSchedulingTestDB(t)
	currency := "NGN"
	seedTenant(t, db, whTenantA, "tenant-a", &currency)
	staffRepo := NewPostgresStaffRepository(db)
	seedWorkingHoursStaff(t, staffRepo, whStaffA, whTenantA, "Ada")
	hours := NewPostgresWorkingHoursRepository(db)
	ctx := context.Background()

	// Inserted out of order on purpose: Friday before Monday, and Monday's
	// second interval before its first.
	for i, seed := range []struct {
		id         string
		day        model.DayOfWeek
		start, end string
	}{
		{"550e8400-e29b-41d4-a716-446655463001", model.Friday, "09:00", "16:00"},
		{"550e8400-e29b-41d4-a716-446655463002", model.Monday, "13:00", "17:00"},
		{"550e8400-e29b-41d4-a716-446655463003", model.Monday, "09:00", "12:00"},
		{"550e8400-e29b-41d4-a716-446655463004", model.Wednesday, "09:00", "17:00"},
	} {
		if _, err := hours.Create(ctx, newWorkingHourInterval(seed.id, whTenantA, whStaffA, seed.day, seed.start, seed.end)); err != nil {
			t.Fatalf("seeding interval %d: %v", i, err)
		}
	}

	listed, err := hours.ListByStaff(ctx, whTenantA, whStaffA)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		day   model.DayOfWeek
		start string
	}{
		{model.Monday, "09:00"}, {model.Monday, "13:00"}, {model.Wednesday, "09:00"}, {model.Friday, "09:00"},
	}
	if len(listed) != len(want) {
		t.Fatalf("ListByStaff() = %d intervals, want %d", len(listed), len(want))
	}
	for i, w := range want {
		if listed[i].DayOfWeek != w.day || listed[i].StartTime != w.start {
			t.Fatalf("position %d = %s %s, want %s %s", i, listed[i].DayOfWeek, listed[i].StartTime, w.day, w.start)
		}
	}
}

// --- DeleteAllForStaff scoping ---------------------------------------------

func TestDeleteAllForStaffAffectsOnlyTheTargetStaffMember(t *testing.T) {
	db := openSchedulingTestDB(t)
	currency := "NGN"
	seedTenant(t, db, whTenantA, "tenant-a", &currency)
	staffRepo := NewPostgresStaffRepository(db)
	seedWorkingHoursStaff(t, staffRepo, whStaffA, whTenantA, "Ada")
	seedWorkingHoursStaff(t, staffRepo, whStaffB, whTenantA, "Chioma")
	hours := NewPostgresWorkingHoursRepository(db)
	ctx := context.Background()

	if _, err := hours.Create(ctx, newWorkingHourInterval("550e8400-e29b-41d4-a716-446655464001", whTenantA, whStaffA, model.Monday, "09:00", "17:00")); err != nil {
		t.Fatal(err)
	}
	if _, err := hours.Create(ctx, newWorkingHourInterval("550e8400-e29b-41d4-a716-446655464002", whTenantA, whStaffB, model.Monday, "09:00", "17:00")); err != nil {
		t.Fatal(err)
	}

	if err := hours.DeleteAllForStaff(ctx, whTenantA, whStaffA); err != nil {
		t.Fatalf("DeleteAllForStaff() error = %v", err)
	}

	staffAHours, err := hours.ListByStaff(ctx, whTenantA, whStaffA)
	if err != nil {
		t.Fatal(err)
	}
	if len(staffAHours) != 0 {
		t.Fatalf("staff A still has %d intervals after DeleteAllForStaff", len(staffAHours))
	}
	staffBHours, err := hours.ListByStaff(ctx, whTenantA, whStaffB)
	if err != nil {
		t.Fatal(err)
	}
	if len(staffBHours) != 1 {
		t.Fatalf("staff B's schedule was disturbed: %d intervals, want 1", len(staffBHours))
	}
}

// --- tenant isolation --------------------------------------------------------

func TestWorkingHoursListByStaffDoesNotCrossTenantBoundaries(t *testing.T) {
	db := openSchedulingTestDB(t)
	currency := "NGN"
	seedTenant(t, db, whTenantA, "tenant-a", &currency)
	seedTenant(t, db, whTenantB, "tenant-b", &currency)
	staffRepo := NewPostgresStaffRepository(db)
	seedWorkingHoursStaff(t, staffRepo, whStaffA, whTenantA, "Ada")
	hours := NewPostgresWorkingHoursRepository(db)
	ctx := context.Background()

	if _, err := hours.Create(ctx, newWorkingHourInterval("550e8400-e29b-41d4-a716-446655465001", whTenantA, whStaffA, model.Monday, "09:00", "17:00")); err != nil {
		t.Fatal(err)
	}

	// Tenant B asking for tenant A's staff member sees nothing — not an
	// error, an empty result, the same non-disclosure staffNotFound relies on
	// at the service layer.
	crossTenant, err := hours.ListByStaff(ctx, whTenantB, whStaffA)
	if err != nil {
		t.Fatalf("ListByStaff() error = %v", err)
	}
	if len(crossTenant) != 0 {
		t.Fatalf("ListByStaff() leaked another tenant's schedule: %+v", crossTenant)
	}
}

// The database itself, not just the Go service layer, refuses to link a
// working-hours row to a staff profile in a different tenant than the one
// named on the row — the same guarantee staff_services already provides for
// capabilities.
func TestWorkingHoursCreateRefusesACrossTenantStaffAssociation(t *testing.T) {
	db := openSchedulingTestDB(t)
	currency := "NGN"
	seedTenant(t, db, whTenantA, "tenant-a", &currency)
	seedTenant(t, db, whTenantB, "tenant-b", &currency)
	staffRepo := NewPostgresStaffRepository(db)
	seedWorkingHoursStaff(t, staffRepo, whStaffA, whTenantA, "Ada")
	hours := NewPostgresWorkingHoursRepository(db)
	ctx := context.Background()

	// whStaffA belongs to tenant A; naming tenant B here must be refused by
	// the composite foreign key, not merely by application code.
	_, err := hours.Create(ctx, newWorkingHourInterval("550e8400-e29b-41d4-a716-446655466001", whTenantB, whStaffA, model.Monday, "09:00", "17:00"))
	if err == nil {
		t.Fatal("Create() accepted a staff/tenant pair that does not exist")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("error = %v, want VALIDATION_FAILED", err)
	}
}

// --- staff FK behavior --------------------------------------------------------

func TestWorkingHoursCreateRefusesAnUnknownStaffID(t *testing.T) {
	db := openSchedulingTestDB(t)
	currency := "NGN"
	seedTenant(t, db, whTenantA, "tenant-a", &currency)
	hours := NewPostgresWorkingHoursRepository(db)

	unknownStaff := "550e8400-e29b-41d4-a716-446655469999"
	_, err := hours.Create(context.Background(), newWorkingHourInterval("550e8400-e29b-41d4-a716-446655467001", whTenantA, unknownStaff, model.Monday, "09:00", "17:00"))
	if err == nil {
		t.Fatal("Create() accepted a nonexistent staff id")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("error = %v, want VALIDATION_FAILED", err)
	}
}

// The exact-duplicate backstop: the database refuses a byte-identical
// interval even if application validation were somehow bypassed.
func TestWorkingHoursCreateRefusesAnExactDuplicateAtTheDatabaseLevel(t *testing.T) {
	db := openSchedulingTestDB(t)
	currency := "NGN"
	seedTenant(t, db, whTenantA, "tenant-a", &currency)
	staffRepo := NewPostgresStaffRepository(db)
	seedWorkingHoursStaff(t, staffRepo, whStaffA, whTenantA, "Ada")
	hours := NewPostgresWorkingHoursRepository(db)
	ctx := context.Background()

	if _, err := hours.Create(ctx, newWorkingHourInterval("550e8400-e29b-41d4-a716-446655468001", whTenantA, whStaffA, model.Monday, "09:00", "17:00")); err != nil {
		t.Fatal(err)
	}
	_, err := hours.Create(ctx, newWorkingHourInterval("550e8400-e29b-41d4-a716-446655468002", whTenantA, whStaffA, model.Monday, "09:00", "17:00"))
	if err == nil {
		t.Fatal("Create() accepted a byte-identical duplicate interval")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("error = %v, want VALIDATION_FAILED", err)
	}
}
