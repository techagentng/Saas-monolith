package repository

import (
	"context"
	"errors"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

func newImage(id, tenantID, serviceID string, sortOrder int, isPrimary bool) *model.ServiceImage {
	return &model.ServiceImage{
		ID: id, TenantID: tenantID, ServiceID: serviceID,
		StorageKey: "tenants/" + tenantID + "/services/" + serviceID + "/" + id + ".jpg",
		PublicURL:  "https://cdn.test.local/media/" + id + ".jpg",
		SortOrder:  sortOrder, IsPrimary: isPrimary,
		MimeType: "image/jpeg", FileSize: 1024,
	}
}

func assertImageNotFound(t *testing.T, err error, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperrors.CodeImageNotFound {
		t.Fatalf("%s: error = %v, want IMAGE_NOT_FOUND", context, err)
	}
}

func TestImageCreateRoundTrips(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a", &currency)
	service, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448101", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	created, err := repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448102", integrationTenantA, service.ID, 0, true))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("Create() did not return database timestamps")
	}

	found, err := repo.FindByID(ctx, integrationTenantA, created.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.MimeType != "image/jpeg" || found.FileSize != 1024 || !found.IsPrimary {
		t.Fatalf("round trip lost data: %+v", found)
	}
}

func TestImageFindByIDDoesNotCrossTenants(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a2", &currency)
	seedTenant(t, db, integrationTenantB, "image-tenant-b2", &currency)
	service, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448103", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	created, err := repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448104", integrationTenantA, service.ID, 0, true))
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.FindByID(ctx, integrationTenantB, created.ID)
	assertImageNotFound(t, err, "FindByID(cross-tenant)")
}

// The composite FK (service_images_service_tenant_fkey) is what makes
// cross-tenant image attachment impossible at the schema level, independent
// of the service layer's own checks.
func TestImageCannotBeAttachedToAnotherTenantsService(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a3", &currency)
	seedTenant(t, db, integrationTenantB, "image-tenant-b3", &currency)
	serviceA, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448105", integrationTenantA, "Tenant A Service"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	// tenant_id says B, but service_id names a service that belongs to A —
	// the composite FK requires (service_id, tenant_id) to match a real
	// (id, tenant_id) row in services, so this must be rejected.
	_, err = repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448106", integrationTenantB, serviceA.ID, 0, false))
	if err == nil {
		t.Fatal("Create() accepted an image whose tenant_id does not match its service's real tenant")
	}
}

// At most one primary image per service — enforced by the partial unique
// index service_images_service_primary_unique, independent of any
// application-level check.
func TestOnlyOnePrimaryImagePerServiceAtTheDatabaseLevel(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a4", &currency)
	service, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448107", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	if _, err := repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448108", integrationTenantA, service.ID, 0, true)); err != nil {
		t.Fatal(err)
	}
	_, err = repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448109", integrationTenantA, service.ID, 1, true))
	if err == nil {
		t.Fatal("the database accepted a second primary image for the same service")
	}

	// A second NON-primary image for the same service is unaffected by the
	// partial index — only PRIMARY rows compete for the uniqueness.
	if _, err := repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448110", integrationTenantA, service.ID, 1, false)); err != nil {
		t.Fatalf("the database rejected a second NON-primary image: %v", err)
	}
}

func TestImageListByServiceReturnsSortOrder(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a5", &currency)
	service, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448111", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	second := newImage("550e8400-e29b-41d4-a716-446655448112", integrationTenantA, service.ID, 1, false)
	first := newImage("550e8400-e29b-41d4-a716-446655448113", integrationTenantA, service.ID, 0, true)
	if _, err := repo.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, first); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListByService(ctx, integrationTenantA, service.ID)
	if err != nil {
		t.Fatalf("ListByService() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("ListByService() = %+v, want [first, second] by sort_order", list)
	}
}

func TestImageListByServiceIDsBatchesAcrossServices(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a6", &currency)
	serviceRepo := NewPostgresServiceRepository(db)
	svcA, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655448114", integrationTenantA, "Service A"))
	if err != nil {
		t.Fatal(err)
	}
	svcB, err := serviceRepo.Create(ctx, newService("550e8400-e29b-41d4-a716-446655448115", integrationTenantA, "Service B"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	if _, err := repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448116", integrationTenantA, svcA.ID, 0, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448117", integrationTenantA, svcB.ID, 0, true)); err != nil {
		t.Fatal(err)
	}

	all, err := repo.ListByServiceIDs(ctx, integrationTenantA, []string{svcA.ID, svcB.ID})
	if err != nil {
		t.Fatalf("ListByServiceIDs() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListByServiceIDs() returned %d rows, want 2", len(all))
	}
}

// Deleting the primary and promoting the next one by sort_order — the real
// transactional behavior ServiceImageService.Delete relies on, proven here
// against a real database rather than a fake.
func TestDeletingPrimaryThenPromotingNextWorksInATransaction(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a7", &currency)
	service, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448118", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	primary := newImage("550e8400-e29b-41d4-a716-446655448119", integrationTenantA, service.ID, 0, true)
	next := newImage("550e8400-e29b-41d4-a716-446655448120", integrationTenantA, service.ID, 1, false)
	if _, err := repo.Create(ctx, primary); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, next); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	txRepo := NewPostgresServiceImageRepository(tx)
	if err := txRepo.Delete(ctx, integrationTenantA, primary.ID); err != nil {
		t.Fatal(err)
	}
	isPrimary := true
	if _, err := txRepo.Update(ctx, integrationTenantA, next.ID, ServiceImageUpdate{IsPrimary: &isPrimary}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	remaining, err := repo.ListByService(ctx, integrationTenantA, service.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != next.ID || !remaining[0].IsPrimary {
		t.Fatalf("remaining = %+v, want exactly [next] promoted to primary", remaining)
	}
}

// SetSortOrders applies a full reorder, exactly as ServiceImageService.Reorder
// uses it inside its own transaction.
func TestSetSortOrdersAppliesTheGivenPermutation(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a8", &currency)
	service, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448121", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	a := newImage("550e8400-e29b-41d4-a716-446655448122", integrationTenantA, service.ID, 0, true)
	b := newImage("550e8400-e29b-41d4-a716-446655448123", integrationTenantA, service.ID, 1, false)
	if _, err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}

	if err := repo.SetSortOrders(ctx, integrationTenantA, service.ID, []string{b.ID, a.ID}); err != nil {
		t.Fatalf("SetSortOrders() error = %v", err)
	}

	list, err := repo.ListByService(ctx, integrationTenantA, service.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("list after reorder = %+v, want [b, a]", list)
	}
}

func TestImageDeleteRemovesTheRow(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a9", &currency)
	service, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448124", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	repo := NewPostgresServiceImageRepository(db)
	image, err := repo.Create(ctx, newImage("550e8400-e29b-41d4-a716-446655448125", integrationTenantA, service.ID, 0, true))
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(ctx, integrationTenantA, image.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	_, err = repo.FindByID(ctx, integrationTenantA, image.ID)
	assertImageNotFound(t, err, "FindByID(after delete)")
}

// Existing services without images must continue working — a service with
// zero image rows is not an error state.
func TestServiceWithNoImagesListsAsEmpty(t *testing.T) {
	db := openSchedulingTestDB(t)
	ctx := context.Background()
	currency := "NGN"
	seedTenant(t, db, integrationTenantA, "image-tenant-a10", &currency)
	service, err := NewPostgresServiceRepository(db).Create(ctx, newService("550e8400-e29b-41d4-a716-446655448126", integrationTenantA, "Gel Manicure"))
	if err != nil {
		t.Fatal(err)
	}

	list, err := NewPostgresServiceImageRepository(db).ListByService(ctx, integrationTenantA, service.ID)
	if err != nil {
		t.Fatalf("ListByService() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListByService() = %+v, want empty", list)
	}
}
