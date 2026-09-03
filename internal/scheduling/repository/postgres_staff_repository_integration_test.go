package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

const integrationUserA = "550e8400-e29b-41d4-a716-446655450001"

// userPtr exists because a Go constant has no address, and StaffProfile.UserID
// is a pointer so that "no linked user" is representable.
func userPtr(id string) *string { return &id }

// seedUser inserts a user directly. Staff profiles may link to one, so the
// fixtures need real rows for the foreign key to resolve.
func seedUser(t *testing.T, db *sql.DB, id string, email string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO users (id, email, password_hash, status) VALUES ($1, $2, 'x', 'ACTIVE')", id, email)
	if err != nil {
		t.Fatalf("seeding user %s: %v", email, err)
	}
}

func newStaff(id string, tenantID string, displayName string, userID *string) *model.StaffProfile {
	return &model.StaffProfile{ID: id, TenantID: tenantID, DisplayName: displayName, UserID: userID, IsBookable: true}
}

func assertStaffNotFound(t *testing.T, err error, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeStaffNotFound {
		t.Fatalf("%s: error = %v, want STAFF_NOT_FOUND", context, err)
	}
}

func assertValidationFailure(t *testing.T, err error, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeValidationFailed {
		t.Fatalf("%s: error = %v, want VALIDATION_FAILED", context, err)
	}
}

// --- profiles ----------------------------------------------------------------

func TestCreateStaffRoundTripsAndDefaultsToActive(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedUser(t, db, integrationUserA, "ada@example.com")

	bio := "Ten years of gel work."
	profile := newStaff("550e8400-e29b-41d4-a716-446655450101", integrationTenantA, "Ada", userPtr(integrationUserA))
	profile.Bio = &bio

	created, err := repo.Create(ctx, profile)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE by repository default", created.Status)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("Create() did not return database timestamps")
	}

	found, err := repo.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.DisplayName != "Ada" || found.Bio == nil || *found.Bio != bio {
		t.Fatalf("round trip lost data: %+v", found)
	}
	if found.UserID == nil || *found.UserID != integrationUserA {
		t.Fatalf("UserID = %v, want the linked user", found.UserID)
	}
	if !found.IsBookable {
		t.Fatal("IsBookable = false, want true")
	}
}

// A technician who never signs in gets a profile with no user at all — no
// synthetic account, no fabricated email or password hash.
func TestCreateNonLoginStaffPersistsWithNullUser(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	created, err := repo.Create(context.Background(), newStaff("550e8400-e29b-41d4-a716-446655450102", integrationTenantA, "Chioma", nil))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.UserID != nil {
		t.Fatalf("UserID = %q, want NULL", *created.UserID)
	}

	var users int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM users").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("users table has %d rows — a non-login worker must not cause an account to be created", users)
	}
}

// The partial unique index constrains linked users only. Many unlinked profiles
// must be able to coexist, which a plain UNIQUE (tenant_id, user_id) would break.
func TestManyNonLoginStaffCanCoexist(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	for _, id := range []string{
		"550e8400-e29b-41d4-a716-446655450103",
		"550e8400-e29b-41d4-a716-446655450104",
		"550e8400-e29b-41d4-a716-446655450105",
	} {
		if _, err := repo.Create(ctx, newStaff(id, integrationTenantA, "Walk-in Helper", nil)); err != nil {
			t.Fatalf("creating an unlinked profile: %v", err)
		}
	}
}

func TestOneProfilePerLinkedUserPerTenant(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedUser(t, db, integrationUserA, "ada@example.com")

	if _, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450106", integrationTenantA, "Ada", userPtr(integrationUserA))); err != nil {
		t.Fatal(err)
	}
	_, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450107", integrationTenantA, "Ada Again", userPtr(integrationUserA)))
	assertValidationFailure(t, err, "Create(duplicate linked user)")
}

// Staff profiles are tenant-scoped, so the same person may be staff in two
// different businesses. The partial unique index is per tenant, not global.
func TestTheSameUserMayBeStaffInTwoTenants(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)
	seedUser(t, db, integrationUserA, "ada@example.com")

	if _, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450108", integrationTenantA, "Ada", userPtr(integrationUserA))); err != nil {
		t.Fatalf("first tenant: %v", err)
	}
	if _, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450109", integrationTenantB, "Ada", userPtr(integrationUserA))); err != nil {
		t.Fatalf("second tenant rejected a legitimate profile: %v", err)
	}
}

func TestCreateStaffRejectsAnUnknownUser(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	unknown := "550e8400-e29b-41d4-a716-446655459999"
	_, err := repo.Create(context.Background(), newStaff("550e8400-e29b-41d4-a716-446655450110", integrationTenantA, "Ghost", &unknown))
	assertValidationFailure(t, err, "Create(unknown user)")
}

func TestStaffLookupsDoNotCrossTenants(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)

	created, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450111", integrationTenantA, "Ada", nil))
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.FindByID(ctx, integrationTenantB, created.ID)
	assertStaffNotFound(t, err, "FindByID(cross-tenant)")

	name := "Hijacked"
	_, err = repo.Update(ctx, integrationTenantB, created.ID, StaffUpdate{DisplayName: &name})
	assertStaffNotFound(t, err, "Update(cross-tenant)")

	_, err = repo.Archive(ctx, integrationTenantB, created.ID)
	assertStaffNotFound(t, err, "Archive(cross-tenant)")

	unchanged, err := repo.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.DisplayName != "Ada" || unchanged.Status != model.StatusActive {
		t.Fatalf("a cross-tenant write mutated the row: %+v", unchanged)
	}
}

func TestListStaffReturnsOnlyThatTenantsRosterInNameOrder(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)

	for id, name := range map[string]string{
		"550e8400-e29b-41d4-a716-446655450201": "Zara",
		"550e8400-e29b-41d4-a716-446655450202": "Ada",
		"550e8400-e29b-41d4-a716-446655450203": "Chioma",
	} {
		if _, err := repo.Create(ctx, newStaff(id, integrationTenantA, name, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450204", integrationTenantB, "Rival", nil)); err != nil {
		t.Fatal(err)
	}

	profiles, err := repo.ListByTenant(ctx, integrationTenantA, StaffListFilter{})
	if err != nil {
		t.Fatalf("ListByTenant() error = %v", err)
	}
	if len(profiles) != 3 {
		t.Fatalf("returned %d profiles, want 3 — tenant B's row must not appear", len(profiles))
	}
	want := []string{"Ada", "Chioma", "Zara"}
	for i, profile := range profiles {
		if profile.DisplayName != want[i] {
			t.Fatalf("position %d = %q, want %q (deterministic name ordering)", i, profile.DisplayName, want[i])
		}
	}
}

func TestUpdateStaffWritesOnlySuppliedFields(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedUser(t, db, integrationUserA, "ada@example.com")

	created, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450112", integrationTenantA, "Ada", userPtr(integrationUserA)))
	if err != nil {
		t.Fatal(err)
	}

	notBookable := false
	updated, err := repo.Update(ctx, integrationTenantA, created.ID, StaffUpdate{IsBookable: &notBookable})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.IsBookable {
		t.Fatal("IsBookable was not cleared")
	}
	if updated.DisplayName != "Ada" {
		t.Fatalf("DisplayName = %q, want unchanged", updated.DisplayName)
	}
	if updated.Status != model.StatusActive {
		t.Fatalf("Status = %q, want ACTIVE — Update never touches lifecycle", updated.Status)
	}
	if updated.UserID == nil || *updated.UserID != integrationUserA {
		t.Fatalf("UserID = %v, want the link preserved — StaffUpdate has no field for it", updated.UserID)
	}
}

func TestArchiveStaffPersistsAndKeepsTheRow(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	created, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450113", integrationTenantA, "Ada", nil))
	if err != nil {
		t.Fatal(err)
	}

	archived, err := repo.Archive(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if archived.Status != model.StatusArchived {
		t.Fatalf("Status = %q, want ARCHIVED", archived.Status)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM staff_profiles WHERE id = $1", created.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("Archive() removed the row — archiving must never delete")
	}

	reread, err := repo.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Status != model.StatusArchived {
		t.Fatalf("re-read Status = %q, want ARCHIVED", reread.Status)
	}
}

func TestStaffListFilterSeparatesActiveFromArchived(t *testing.T) {
	db := openSchedulingTestDB(t)
	repo := NewPostgresStaffRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	active, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450114", integrationTenantA, "Active Ada", nil))
	if err != nil {
		t.Fatal(err)
	}
	departed, err := repo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450115", integrationTenantA, "Departed Zara", nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Archive(ctx, integrationTenantA, departed.ID); err != nil {
		t.Fatal(err)
	}

	activeStatus := model.StatusActive
	activeOnly, err := repo.ListByTenant(ctx, integrationTenantA, StaffListFilter{Status: &activeStatus})
	if err != nil {
		t.Fatal(err)
	}
	if len(activeOnly) != 1 || activeOnly[0].ID != active.ID {
		t.Fatalf("ACTIVE filter returned %d rows, want just the active one", len(activeOnly))
	}

	all, err := repo.ListByTenant(ctx, integrationTenantA, StaffListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered listing returned %d rows, want 2", len(all))
	}
}

// --- capabilities ------------------------------------------------------------

func TestCapabilityAssignmentRoundTrips(t *testing.T) {
	db := openSchedulingTestDB(t)
	staffRepo := NewPostgresStaffRepository(db)
	serviceRepo := NewPostgresServiceRepository(db)
	capabilities := NewPostgresCapabilityRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	staff, err := staffRepo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450301", integrationTenantA, "Ada", nil))
	if err != nil {
		t.Fatal(err)
	}
	manicure, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655450302", integrationTenantA, "Manicure"))
	if err != nil {
		t.Fatal(err)
	}
	pedicure, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655450303", integrationTenantA, "Pedicure"))
	if err != nil {
		t.Fatal(err)
	}

	for _, serviceID := range []string{manicure.ID, pedicure.ID} {
		if err := capabilities.Assign(ctx, integrationTenantA, staff.ID, serviceID); err != nil {
			t.Fatalf("Assign() error = %v", err)
		}
	}

	assigned, err := capabilities.ListServiceIDs(ctx, integrationTenantA, staff.ID)
	if err != nil {
		t.Fatalf("ListServiceIDs() error = %v", err)
	}
	if len(assigned) != 2 {
		t.Fatalf("assigned = %v, want both services", assigned)
	}

	if err := capabilities.DeleteAll(ctx, integrationTenantA, staff.ID); err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	remaining, err := capabilities.ListServiceIDs(ctx, integrationTenantA, staff.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining = %v, want none after DeleteAll", remaining)
	}
}

// TestListStaffIDsForServiceIsTenantScoped exercises the S9 reverse lookup: the
// staff assigned to one service, isolated by tenant.
func TestListStaffIDsForServiceIsTenantScoped(t *testing.T) {
	db := openSchedulingTestDB(t)
	staffRepo := NewPostgresStaffRepository(db)
	serviceRepo := NewPostgresServiceRepository(db)
	capabilities := NewPostgresCapabilityRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)

	svcA, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-4466554504a1", integrationTenantA, "Manicure"))
	if err != nil {
		t.Fatal(err)
	}
	ada, err := staffRepo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-4466554504a2", integrationTenantA, "Ada", nil))
	if err != nil {
		t.Fatal(err)
	}
	cara, err := staffRepo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-4466554504a3", integrationTenantA, "Cara", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, staffID := range []string{ada.ID, cara.ID} {
		if err := capabilities.Assign(ctx, integrationTenantA, staffID, svcA.ID); err != nil {
			t.Fatalf("Assign() error = %v", err)
		}
	}

	got, err := capabilities.ListStaffIDsForService(ctx, integrationTenantA, svcA.ID)
	if err != nil {
		t.Fatalf("ListStaffIDsForService() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("staff for service = %v, want both Ada and Cara", got)
	}

	crossTenant, err := capabilities.ListStaffIDsForService(ctx, integrationTenantB, svcA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(crossTenant) != 0 {
		t.Fatalf("cross-tenant lookup returned %v, want none", crossTenant)
	}
}

func TestDuplicateCapabilityIsRejected(t *testing.T) {
	db := openSchedulingTestDB(t)
	staffRepo := NewPostgresStaffRepository(db)
	serviceRepo := NewPostgresServiceRepository(db)
	capabilities := NewPostgresCapabilityRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)

	staff, err := staffRepo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450304", integrationTenantA, "Ada", nil))
	if err != nil {
		t.Fatal(err)
	}
	manicure, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655450305", integrationTenantA, "Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	if err := capabilities.Assign(ctx, integrationTenantA, staff.ID, manicure.ID); err != nil {
		t.Fatal(err)
	}
	if err := capabilities.Assign(ctx, integrationTenantA, staff.ID, manicure.ID); err == nil {
		t.Fatal("the same capability was recorded twice — the primary key must prevent it")
	}
}

// --- the mandatory cross-tenant link proof -----------------------------------

// Tenant A's technician must not be linkable to Tenant B's service, and the
// refusal must come from the DATABASE rather than from Go validation. Every
// insert below deliberately bypasses the service layer entirely: the composite
// foreign keys are the guarantee under test, so the test must reach past
// anything that could be politely checking first.
func TestDatabaseRejectsCrossTenantStaffServiceLink(t *testing.T) {
	db := openSchedulingTestDB(t)
	staffRepo := NewPostgresStaffRepository(db)
	serviceRepo := NewPostgresServiceRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)

	staffA, err := staffRepo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450401", integrationTenantA, "Ada", nil))
	if err != nil {
		t.Fatal(err)
	}
	serviceB, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655450402", integrationTenantB, "Rival Service"))
	if err != nil {
		t.Fatal(err)
	}

	// Claim the pairing under tenant A: the staff FK resolves, the service FK
	// cannot, because (serviceB.ID, tenantA) is not a row in services.
	if _, err := db.ExecContext(ctx,
		"INSERT INTO staff_services (staff_id, service_id, tenant_id) VALUES ($1, $2, $3)",
		staffA.ID, serviceB.ID, integrationTenantA); err == nil {
		t.Fatal("the database accepted tenant A's staff paired with tenant B's service under tenant A")
	}

	// Claim it under tenant B instead: now the service FK resolves and the staff
	// FK cannot. Both directions must fail, or the constraint only half works.
	if _, err := db.ExecContext(ctx,
		"INSERT INTO staff_services (staff_id, service_id, tenant_id) VALUES ($1, $2, $3)",
		staffA.ID, serviceB.ID, integrationTenantB); err == nil {
		t.Fatal("the database accepted the cross-tenant pairing when claimed under tenant B")
	}

	// And prove the constraint is not simply rejecting everything: the honest
	// same-tenant pairing succeeds.
	serviceA, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655450403", integrationTenantA, "Manicure"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO staff_services (staff_id, service_id, tenant_id) VALUES ($1, $2, $3)",
		staffA.ID, serviceA.ID, integrationTenantA); err != nil {
		t.Fatalf("a legitimate same-tenant assignment was rejected: %v", err)
	}

	var rows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM staff_services").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("staff_services holds %d rows, want exactly the one legitimate assignment", rows)
	}
}

// The repository maps the composite-FK violation to a presentable validation
// failure rather than a 500: the database, not the Go layer, is the authority on
// that pairing.
func TestAssignSurfacesCrossTenantRejectionAsValidationFailure(t *testing.T) {
	db := openSchedulingTestDB(t)
	staffRepo := NewPostgresStaffRepository(db)
	serviceRepo := NewPostgresServiceRepository(db)
	capabilities := NewPostgresCapabilityRepository(db)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "tenant-a", &currency)
	seedTenant(t, db, integrationTenantB, "tenant-b", &currency)

	staffA, err := staffRepo.Create(ctx, newStaff("550e8400-e29b-41d4-a716-446655450404", integrationTenantA, "Ada", nil))
	if err != nil {
		t.Fatal(err)
	}
	serviceB, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655450405", integrationTenantB, "Rival Service"))
	if err != nil {
		t.Fatal(err)
	}

	err = capabilities.Assign(ctx, integrationTenantA, staffA.ID, serviceB.ID)
	assertValidationFailure(t, err, "Assign(cross-tenant)")
}
