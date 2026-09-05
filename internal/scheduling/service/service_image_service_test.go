package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sort"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/media"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
)

// --- fakes -------------------------------------------------------------------

// fakeServiceImageRepository mirrors fakeServiceRepository/fakeCategoryRepository's
// shape: an in-memory ServiceImageRepository, so tests can prove tenant/service
// scoping and record calls without a real database.
type fakeServiceImageRepository struct {
	images map[string]*model.ServiceImage

	createCalls        int
	updateCalls        int
	deleteCalls        int
	clearPrimaryCalls  int
	setSortOrdersCalls int

	createErr error
}

func newFakeServiceImageRepository() *fakeServiceImageRepository {
	return &fakeServiceImageRepository{images: map[string]*model.ServiceImage{}}
}

func (r *fakeServiceImageRepository) Create(_ context.Context, image *model.ServiceImage) (*model.ServiceImage, error) {
	r.createCalls++
	if r.createErr != nil {
		return nil, r.createErr
	}
	stored := *image
	stored.CreatedAt = time.Now().UTC()
	stored.UpdatedAt = stored.CreatedAt
	r.images[stored.ID] = &stored
	return &stored, nil
}

func (r *fakeServiceImageRepository) FindByID(_ context.Context, tenantID string, imageID string) (*model.ServiceImage, error) {
	stored, ok := r.images[imageID]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
	}
	return stored, nil
}

func (r *fakeServiceImageRepository) ListByService(_ context.Context, tenantID string, serviceID string) ([]*model.ServiceImage, error) {
	var result []*model.ServiceImage
	for _, stored := range r.images {
		if stored.TenantID == tenantID && stored.ServiceID == serviceID {
			result = append(result, stored)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SortOrder != result[j].SortOrder {
			return result[i].SortOrder < result[j].SortOrder
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (r *fakeServiceImageRepository) ListByServiceIDs(_ context.Context, tenantID string, serviceIDs []string) ([]*model.ServiceImage, error) {
	wanted := make(map[string]bool, len(serviceIDs))
	for _, id := range serviceIDs {
		wanted[id] = true
	}
	var result []*model.ServiceImage
	for _, stored := range r.images {
		if stored.TenantID == tenantID && wanted[stored.ServiceID] {
			result = append(result, stored)
		}
	}
	return result, nil
}

func (r *fakeServiceImageRepository) Update(_ context.Context, tenantID string, imageID string, update repository.ServiceImageUpdate) (*model.ServiceImage, error) {
	r.updateCalls++
	stored, ok := r.images[imageID]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
	}
	if update.AltText != nil {
		stored.AltText = update.AltText
	}
	if update.IsPrimary != nil {
		stored.IsPrimary = *update.IsPrimary
	}
	return stored, nil
}

func (r *fakeServiceImageRepository) ClearPrimary(_ context.Context, tenantID string, serviceID string) error {
	r.clearPrimaryCalls++
	for _, stored := range r.images {
		if stored.TenantID == tenantID && stored.ServiceID == serviceID {
			stored.IsPrimary = false
		}
	}
	return nil
}

func (r *fakeServiceImageRepository) Delete(_ context.Context, tenantID string, imageID string) error {
	r.deleteCalls++
	stored, ok := r.images[imageID]
	if !ok || stored.TenantID != tenantID {
		return apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
	}
	delete(r.images, imageID)
	return nil
}

func (r *fakeServiceImageRepository) SetSortOrders(_ context.Context, tenantID string, serviceID string, orderedImageIDs []string) error {
	r.setSortOrdersCalls++
	for index, id := range orderedImageIDs {
		stored, ok := r.images[id]
		if !ok || stored.TenantID != tenantID || stored.ServiceID != serviceID {
			return apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
		}
		stored.SortOrder = index
	}
	return nil
}

var _ repository.ServiceImageRepository = (*fakeServiceImageRepository)(nil)

const (
	imgTenantA   = "550e8400-e29b-41d4-a716-446655447001"
	imgTenantB   = "550e8400-e29b-41d4-a716-446655447002"
	imgServiceA  = "550e8400-e29b-41d4-a716-446655447003"
	imgServiceB  = "550e8400-e29b-41d4-a716-446655447004"
	imgImageID   = "550e8400-e29b-41d4-a716-446655447005"
)

func newImageFixture(services *fakeServiceRepository, images *fakeServiceImageRepository, storage media.MediaStorage) ServiceImageService {
	return NewServiceImageService(nilTxBeginner{}, images, services, storage)
}

func serviceFixtureWith(tenantID, serviceID string) *fakeServiceRepository {
	services := newFakeServiceRepository()
	services.services[serviceID] = &model.Service{ID: serviceID, TenantID: tenantID, Name: "Gel Manicure", Status: model.StatusActive}
	return services
}

// --- test image encoding ------------------------------------------------

func validJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func validPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// --- Upload ------------------------------------------------------------------

func TestUploadOneValidImage(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	storage := media.NewFakeStorage()
	svc := newImageFixture(services, images, storage)

	created, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{
		{Body: bytes.NewReader(validJPEGBytes(t))},
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created %d images, want 1", len(created))
	}
	if created[0].MimeType != "image/jpeg" {
		t.Fatalf("MimeType = %q, want image/jpeg", created[0].MimeType)
	}
	if created[0].TenantID != imgTenantA || created[0].ServiceID != imgServiceA {
		t.Fatalf("tenant/service scoping wrong: %+v", created[0])
	}
	if storage.UploadCalls != 1 {
		t.Fatalf("storage upload calls = %d, want 1", storage.UploadCalls)
	}
}

func TestUploadMultipleValidImages(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	storage := media.NewFakeStorage()
	svc := newImageFixture(services, images, storage)

	created, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{
		{Body: bytes.NewReader(validJPEGBytes(t))},
		{Body: bytes.NewReader(validPNGBytes(t))},
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created %d images, want 2", len(created))
	}
	if created[0].SortOrder != 0 || created[1].SortOrder != 1 {
		t.Fatalf("sort orders = %d, %d, want 0, 1", created[0].SortOrder, created[1].SortOrder)
	}
}

func TestFirstUploadedImageBecomesPrimary(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	svc := newImageFixture(services, images, media.NewFakeStorage())

	created, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{
		{Body: bytes.NewReader(validJPEGBytes(t))},
		{Body: bytes.NewReader(validPNGBytes(t))},
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if !created[0].IsPrimary {
		t.Fatal("first uploaded image is not primary")
	}
	if created[1].IsPrimary {
		t.Fatal("second uploaded image was also made primary — only one may be")
	}
}

func TestUploadingMoreImagesLaterDoesNotDisturbTheExistingPrimary(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	svc := newImageFixture(services, images, media.NewFakeStorage())
	ctx := context.Background()

	if _, err := svc.Upload(ctx, imgTenantA, imgServiceA, []UploadImageInput{{Body: bytes.NewReader(validJPEGBytes(t))}}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Upload(ctx, imgTenantA, imgServiceA, []UploadImageInput{{Body: bytes.NewReader(validPNGBytes(t))}})
	if err != nil {
		t.Fatal(err)
	}
	if second[0].IsPrimary {
		t.Fatal("a later upload became primary even though one already existed")
	}
}

func TestUploadEnforcesMaximumImageCount(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	svc := newImageFixture(services, images, media.NewFakeStorage())
	ctx := context.Background()

	inputs := func(n int) []UploadImageInput {
		list := make([]UploadImageInput, n)
		for i := range list {
			list[i] = UploadImageInput{Body: bytes.NewReader(validJPEGBytes(t))}
		}
		return list
	}

	if _, err := svc.Upload(ctx, imgTenantA, imgServiceA, inputs(model.MaxImagesPerService)); err != nil {
		t.Fatalf("uploading exactly the max error = %v", err)
	}
	_, err := svc.Upload(ctx, imgTenantA, imgServiceA, inputs(1))
	assertImgCode(t, err, apperrors.CodeValidationFailed, "Upload(over the max)")
	if images.createCalls != model.MaxImagesPerService {
		t.Fatalf("createCalls = %d, want exactly %d — the rejected batch must not have persisted anything", images.createCalls, model.MaxImagesPerService)
	}
}

func TestUploadRejectsUnsupportedMIMEType(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	storage := media.NewFakeStorage()
	svc := newImageFixture(services, images, storage)

	gif := []byte("GIF89a" + strings.Repeat("\x00", 20))
	_, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{{Body: bytes.NewReader(gif)}})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "Upload(unsupported mime)")
	if storage.UploadCalls != 0 {
		t.Fatal("an unsupported file reached storage")
	}
}

// SVG is explicitly out of scope for this feature and must never be sniffed
// as an accepted type, regardless of a client-declared Content-Type.
func TestUploadRejectsSVG(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	svc := newImageFixture(services, images, media.NewFakeStorage())

	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	_, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{{Body: bytes.NewReader(svg)}})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "Upload(svg)")
}

func TestUploadRejectsOversizedImage(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	storage := media.NewFakeStorage()
	svc := newImageFixture(services, images, storage)

	oversized := bytes.Repeat([]byte{0xFF}, int(model.MaxImageSizeBytes)+1)
	_, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{{Body: bytes.NewReader(oversized)}})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "Upload(oversized)")
	if storage.UploadCalls != 0 {
		t.Fatal("an oversized file reached storage")
	}
}

func TestUploadRejectsWhenServiceDoesNotBelongToTenant(t *testing.T) {
	services := serviceFixtureWith(imgTenantB, imgServiceA)
	images := newFakeServiceImageRepository()
	svc := newImageFixture(services, images, media.NewFakeStorage())

	_, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{{Body: bytes.NewReader(validJPEGBytes(t))}})
	assertImgCode(t, err, apperrors.CodeServiceNotFound, "Upload(cross-tenant service)")
}

// Storage failure must not persist DB metadata: nothing is created for a
// file whose upload to storage itself failed.
func TestUploadStorageFailureDoesNotPersistDBMetadata(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	storage := media.NewFakeStorage()
	storage.UploadErr = errors.New("storage unavailable")
	svc := newImageFixture(services, images, storage)

	_, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{{Body: bytes.NewReader(validJPEGBytes(t))}})
	if err == nil {
		t.Fatal("Upload() succeeded despite a storage failure")
	}
	if images.createCalls != 0 {
		t.Fatal("DB metadata was persisted despite the storage upload failing")
	}
}

// A DB failure after a successful storage upload must trigger a compensating
// delete of the object just uploaded.
func TestUploadDBFailureTriggersStorageCleanup(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.createErr = errors.New("db unavailable")
	storage := media.NewFakeStorage()
	svc := newImageFixture(services, images, storage)

	_, err := svc.Upload(context.Background(), imgTenantA, imgServiceA, []UploadImageInput{{Body: bytes.NewReader(validJPEGBytes(t))}})
	if err == nil {
		t.Fatal("Upload() succeeded despite a DB failure")
	}
	if storage.UploadCalls != 1 {
		t.Fatalf("storage upload calls = %d, want 1", storage.UploadCalls)
	}
	if storage.DeleteCalls != 1 {
		t.Fatal("a DB failure after a successful upload did not trigger a compensating storage delete")
	}
	if len(storage.Objects) != 0 {
		t.Fatal("the compensating delete did not actually remove the object")
	}
}

// --- List ----------------------------------------------------------------

func TestListReturnsImagesInSortOrder(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images["b"] = &model.ServiceImage{ID: "b", TenantID: imgTenantA, ServiceID: imgServiceA, SortOrder: 1}
	images.images["a"] = &model.ServiceImage{ID: "a", TenantID: imgTenantA, ServiceID: imgServiceA, SortOrder: 0}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	list, err := svc.List(context.Background(), imgTenantA, imgServiceA)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("List() = %+v, want [a, b]", list)
	}
}

func TestListOnMissingServiceIsNotFound(t *testing.T) {
	services := newFakeServiceRepository()
	svc := newImageFixture(services, newFakeServiceImageRepository(), media.NewFakeStorage())

	_, err := svc.List(context.Background(), imgTenantA, imgServiceA)
	assertImgCode(t, err, apperrors.CodeServiceNotFound, "List(missing service)")
}

// Existing services without images must continue working — an empty list is
// success, not an error.
func TestListOnServiceWithNoImagesReturnsEmpty(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	svc := newImageFixture(services, newFakeServiceImageRepository(), media.NewFakeStorage())

	list, err := svc.List(context.Background(), imgTenantA, imgServiceA)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List() = %+v, want empty", list)
	}
}

// --- UpdateMeta ------------------------------------------------------------

func TestUpdateMetaChangesAltText(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantA, ServiceID: imgServiceA}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	alt := "Finished gel manicure"
	updated, err := svc.UpdateMeta(context.Background(), imgTenantA, imgServiceA, imgImageID, UpdateImageInput{AltText: &alt})
	if err != nil {
		t.Fatalf("UpdateMeta() error = %v", err)
	}
	if updated.AltText == nil || *updated.AltText != alt {
		t.Fatalf("AltText = %v, want %q", updated.AltText, alt)
	}
}

func TestUpdateMetaRejectsAnImageFromAnotherService(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantA, ServiceID: imgServiceB}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	alt := "x"
	_, err := svc.UpdateMeta(context.Background(), imgTenantA, imgServiceA, imgImageID, UpdateImageInput{AltText: &alt})
	assertImgCode(t, err, apperrors.CodeImageNotFound, "UpdateMeta(wrong service)")
}

func TestUpdateMetaRejectsCrossTenantImage(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantB, ServiceID: imgServiceA}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	alt := "x"
	_, err := svc.UpdateMeta(context.Background(), imgTenantA, imgServiceA, imgImageID, UpdateImageInput{AltText: &alt})
	assertImgCode(t, err, apperrors.CodeImageNotFound, "UpdateMeta(cross-tenant)")
}

func TestUpdateMetaRejectsAnEmptyPatch(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantA, ServiceID: imgServiceA}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	_, err := svc.UpdateMeta(context.Background(), imgTenantA, imgServiceA, imgImageID, UpdateImageInput{})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "UpdateMeta(empty patch)")
}

// Unsetting primary with no replacement is not a supported operation —
// making a DIFFERENT image primary is how an owner changes it.
func TestUpdateMetaRejectsUnsettingPrimary(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantA, ServiceID: imgServiceA, IsPrimary: true}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	no := false
	_, err := svc.UpdateMeta(context.Background(), imgTenantA, imgServiceA, imgImageID, UpdateImageInput{IsPrimary: &no})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "UpdateMeta(unset primary)")
}

// Making an image primary opens a transaction (clear the old one, set the
// new one, atomically) — a pure unit test proves that validation (malformed
// ids, cross-service ownership) happens BEFORE that transaction ever opens,
// the same discipline StaffService.ReplaceCapabilities' own tests apply. The
// transaction's actual effect is proven for real in a Postgres integration
// test.
func TestUpdateMetaSetPrimaryValidatesBeforeOpeningATransaction(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantB, ServiceID: imgServiceA}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	yes := true
	_, err := svc.UpdateMeta(context.Background(), imgTenantA, imgServiceA, imgImageID, UpdateImageInput{IsPrimary: &yes})
	// Cross-tenant, so this must fail on the FindByID check, never reaching
	// nilTxBeginner.BeginTx (which would panic-equivalent via its own error
	// message identifying exactly that if it were reached).
	assertImgCode(t, err, apperrors.CodeImageNotFound, "UpdateMeta(set primary, cross-tenant)")
}

// --- Delete ------------------------------------------------------------------

func TestDeleteNonPrimaryImageNeedsNoPromotion(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images["primary"] = &model.ServiceImage{ID: "primary", TenantID: imgTenantA, ServiceID: imgServiceA, IsPrimary: true, SortOrder: 0}
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantA, ServiceID: imgServiceA, IsPrimary: false, SortOrder: 1, StorageKey: "k"}
	storage := media.NewFakeStorage()
	svc := newImageFixture(services, images, storage)

	if err := svc.Delete(context.Background(), imgTenantA, imgServiceA, imgImageID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, stillThere := images.images[imgImageID]; stillThere {
		t.Fatal("Delete() did not remove the row")
	}
	if !images.images["primary"].IsPrimary {
		t.Fatal("deleting a non-primary image disturbed the existing primary")
	}
	if storage.DeleteCalls != 1 {
		t.Fatalf("storage delete calls = %d, want 1", storage.DeleteCalls)
	}
}

// Deleting the last image in a service leaves it with an empty collection —
// no error, nothing left to promote.
func TestDeletingTheLastImageLeavesAnEmptyCollection(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantA, ServiceID: imgServiceA, IsPrimary: false}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	if err := svc.Delete(context.Background(), imgTenantA, imgServiceA, imgImageID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	list, err := svc.List(context.Background(), imgTenantA, imgServiceA)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("List() after deleting the only image = %+v, want empty", list)
	}
}

func TestDeleteRejectsAnImageFromAnotherService(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantA, ServiceID: imgServiceB}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	err := svc.Delete(context.Background(), imgTenantA, imgServiceA, imgImageID)
	assertImgCode(t, err, apperrors.CodeImageNotFound, "Delete(wrong service)")
	if _, stillThere := images.images[imgImageID]; !stillThere {
		t.Fatal("Delete() removed an image belonging to a different service")
	}
}

func TestDeleteRejectsCrossTenantImage(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantB, ServiceID: imgServiceA}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	err := svc.Delete(context.Background(), imgTenantA, imgServiceA, imgImageID)
	assertImgCode(t, err, apperrors.CodeImageNotFound, "Delete(cross-tenant)")
}

// Deleting the primary image opens a transaction (delete + promote) — a pure
// unit test proves ownership validation happens before that transaction
// opens; the promotion itself is proven in a Postgres integration test.
func TestDeletePrimaryValidatesBeforeOpeningATransaction(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images[imgImageID] = &model.ServiceImage{ID: imgImageID, TenantID: imgTenantB, ServiceID: imgServiceA, IsPrimary: true}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	err := svc.Delete(context.Background(), imgTenantA, imgServiceA, imgImageID)
	assertImgCode(t, err, apperrors.CodeImageNotFound, "Delete(primary, cross-tenant)")
}

// --- Reorder -----------------------------------------------------------------

func TestReorderValidatesBeforeOpeningATransaction(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images["a"] = &model.ServiceImage{ID: "a", TenantID: imgTenantA, ServiceID: imgServiceA, SortOrder: 0}
	images.images["b"] = &model.ServiceImage{ID: "b", TenantID: imgTenantA, ServiceID: imgServiceA, SortOrder: 1}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	// nilTxBeginner would fail loudly with its own descriptive error if
	// Reorder ever reached BeginTx for an invalid request — reaching a
	// VALIDATION_FAILED here instead proves it did not.
	_, err := svc.Reorder(context.Background(), imgTenantA, imgServiceA, []string{"a"})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "Reorder(missing an id)")
}

func TestReorderRejectsDuplicateIDs(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images["a"] = &model.ServiceImage{ID: "a", TenantID: imgTenantA, ServiceID: imgServiceA}
	images.images["b"] = &model.ServiceImage{ID: "b", TenantID: imgTenantA, ServiceID: imgServiceA}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	_, err := svc.Reorder(context.Background(), imgTenantA, imgServiceA, []string{"a", "a"})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "Reorder(duplicate ids)")
}

func TestReorderRejectsAnIDFromAnotherService(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images["a"] = &model.ServiceImage{ID: "a", TenantID: imgTenantA, ServiceID: imgServiceA}
	images.images["foreign"] = &model.ServiceImage{ID: "foreign", TenantID: imgTenantA, ServiceID: imgServiceB}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	_, err := svc.Reorder(context.Background(), imgTenantA, imgServiceA, []string{"a", "foreign"})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "Reorder(foreign id injected)")
}

func TestReorderRejectsAnUnknownID(t *testing.T) {
	services := serviceFixtureWith(imgTenantA, imgServiceA)
	images := newFakeServiceImageRepository()
	images.images["a"] = &model.ServiceImage{ID: "a", TenantID: imgTenantA, ServiceID: imgServiceA}
	svc := newImageFixture(services, images, media.NewFakeStorage())

	_, err := svc.Reorder(context.Background(), imgTenantA, imgServiceA, []string{"550e8400-e29b-41d4-a716-446655449999"})
	assertImgCode(t, err, apperrors.CodeValidationFailed, "Reorder(unknown id)")
}

func assertImgCode(t *testing.T, err error, want apperrors.ErrorCode, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != want {
		t.Fatalf("%s: error = %v, want %s", context, err, want)
	}
}
