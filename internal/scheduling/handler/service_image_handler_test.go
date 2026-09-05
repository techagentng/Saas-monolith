package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

const (
	handlerImageID    = "550e8400-e29b-41d4-a716-446655448001"
	handlerServiceID2 = "550e8400-e29b-41d4-a716-446655448002"
)

// fakeServiceImageService records exactly what the handler passed through,
// the same DTO-protection-proof pattern fakeCatalogService/fakeCategoryService
// already use.
type fakeServiceImageService struct {
	tenantID  string
	serviceID string
	imageID   string
	uploaded  []service.UploadImageInput
	update    service.UpdateImageInput
	orderIDs  []string

	created []*model.ServiceImage
	list    []*model.ServiceImage
	single  *model.ServiceImage
	err     error
}

func (f *fakeServiceImageService) Upload(_ context.Context, tenantID string, serviceID string, files []service.UploadImageInput) ([]*model.ServiceImage, error) {
	f.tenantID, f.serviceID, f.uploaded = tenantID, serviceID, files
	return f.created, f.err
}

func (f *fakeServiceImageService) List(_ context.Context, tenantID string, serviceID string) ([]*model.ServiceImage, error) {
	f.tenantID, f.serviceID = tenantID, serviceID
	return f.list, f.err
}

func (f *fakeServiceImageService) UpdateMeta(_ context.Context, tenantID string, serviceID string, imageID string, input service.UpdateImageInput) (*model.ServiceImage, error) {
	f.tenantID, f.serviceID, f.imageID, f.update = tenantID, serviceID, imageID, input
	return f.single, f.err
}

func (f *fakeServiceImageService) Delete(_ context.Context, tenantID string, serviceID string, imageID string) error {
	f.tenantID, f.serviceID, f.imageID = tenantID, serviceID, imageID
	return f.err
}

func (f *fakeServiceImageService) Reorder(_ context.Context, tenantID string, serviceID string, imageIDs []string) ([]*model.ServiceImage, error) {
	f.tenantID, f.serviceID, f.orderIDs = tenantID, serviceID, imageIDs
	return f.list, f.err
}

func storedImage() *model.ServiceImage {
	return &model.ServiceImage{
		ID: handlerImageID, TenantID: handlerTenantID, ServiceID: handlerServiceID2,
		StorageKey: "tenants/x/services/y/z.jpg", PublicURL: "https://cdn.test.local/media/z.jpg",
		SortOrder: 0, IsPrimary: true, MimeType: "image/jpeg", FileSize: 1024,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

// multipartImageRequest builds a real multipart/form-data request with one
// or more files under the "images" field, so Create is exercised through
// actual multipart parsing rather than a hand-built body.
func multipartImageRequest(t *testing.T, fieldFiles map[string][]byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for filename, content := range fieldFiles {
		part, err := writer.CreateFormFile("images", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", &buf)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

// --- Create ------------------------------------------------------------------

func TestImageCreateReturns201WithTheUploadedImages(t *testing.T) {
	fake := &fakeServiceImageService{created: []*model.ServiceImage{storedImage()}}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	request := multipartImageRequest(t, map[string][]byte{"photo.jpg": []byte("fake-bytes")})
	handler.Create(recorder, request, handlerTenantID, handlerServiceID2)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if fake.tenantID != handlerTenantID || fake.serviceID != handlerServiceID2 {
		t.Fatalf("identifiers = %q/%q", fake.tenantID, fake.serviceID)
	}
	if len(fake.uploaded) != 1 {
		t.Fatalf("uploaded %d files to the service layer, want 1", len(fake.uploaded))
	}

	var body struct {
		Images []map[string]any `json:"images"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body.Images) != 1 {
		t.Fatalf("response images = %v, want 1", body.Images)
	}
	for _, key := range []string{"id", "url", "alt_text", "sort_order", "is_primary"} {
		if _, ok := body.Images[0][key]; !ok {
			t.Fatalf("image missing %q: %s", key, recorder.Body.String())
		}
	}
	if _, present := body.Images[0]["storage_key"]; present {
		t.Fatal("response exposed storage_key, which must never reach even an authenticated caller")
	}
}

func TestImageCreateAcceptsMultipleFilesInOneRequest(t *testing.T) {
	fake := &fakeServiceImageService{created: []*model.ServiceImage{storedImage(), storedImage()}}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	request := multipartImageRequest(t, map[string][]byte{"a.jpg": []byte("a"), "b.png": []byte("b")})
	handler.Create(recorder, request, handlerTenantID, handlerServiceID2)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if len(fake.uploaded) != 2 {
		t.Fatalf("uploaded %d files, want 2", len(fake.uploaded))
	}
}

func TestImageCreateRejectsAnEmptyImagesField(t *testing.T) {
	handler := NewServiceImageHandler(&fakeServiceImageService{})
	recorder := httptest.NewRecorder()

	request := multipartImageRequest(t, map[string][]byte{})
	handler.Create(recorder, request, handlerTenantID, handlerServiceID2)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestImageCreateRejectsAMalformedMultipartBody(t *testing.T) {
	handler := NewServiceImageHandler(&fakeServiceImageService{})
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not multipart"))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	handler.Create(recorder, request, handlerTenantID, handlerServiceID2)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestImageCreateSurfacesServiceLayerValidationFailure(t *testing.T) {
	fake := &fakeServiceImageService{err: apperrors.New(apperrors.CodeValidationFailed, "a service may have at most 5 images", nil)}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	request := multipartImageRequest(t, map[string][]byte{"a.jpg": []byte("a")})
	handler.Create(recorder, request, handlerTenantID, handlerServiceID2)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "VALIDATION_FAILED")
}

// --- List --------------------------------------------------------------------

func TestImageListReturnsAnEmptyArrayRatherThanNull(t *testing.T) {
	handler := NewServiceImageHandler(&fakeServiceImageService{list: nil})
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerServiceID2)

	if got := strings.TrimSpace(recorder.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

func TestImageListReturnsTheStoredImages(t *testing.T) {
	fake := &fakeServiceImageService{list: []*model.ServiceImage{storedImage()}}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerServiceID2)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var images []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &images); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("returned %d images, want 1", len(images))
	}
}

// --- Update ------------------------------------------------------------------

func TestImageUpdateAppliesOnlySuppliedFields(t *testing.T) {
	fake := &fakeServiceImageService{single: storedImage()}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"alt_text":"Finished set"}`)), handlerTenantID, handlerServiceID2, handlerImageID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.update.AltText == nil || *fake.update.AltText != "Finished set" {
		t.Fatalf("AltText = %v, want %q", fake.update.AltText, "Finished set")
	}
	if fake.update.IsPrimary != nil {
		t.Fatal("IsPrimary was populated despite not being in the request body")
	}
}

func TestImageUpdateRejectsMalformedJSON(t *testing.T) {
	handler := NewServiceImageHandler(&fakeServiceImageService{})
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader("{oops")), handlerTenantID, handlerServiceID2, handlerImageID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestImageUpdateSurfacesNotFound(t *testing.T) {
	fake := &fakeServiceImageService{err: apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"is_primary":true}`)), handlerTenantID, handlerServiceID2, handlerImageID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertErrorCode(t, recorder, "IMAGE_NOT_FOUND")
}

// --- Delete -----------------------------------------------------------------

func TestImageDeleteReturns204WithNoBody(t *testing.T) {
	fake := &fakeServiceImageService{}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Delete(recorder, httptest.NewRequest(http.MethodDelete, "/", nil), handlerTenantID, handlerServiceID2, handlerImageID)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", recorder.Body.String())
	}
	if fake.tenantID != handlerTenantID || fake.serviceID != handlerServiceID2 || fake.imageID != handlerImageID {
		t.Fatalf("identifiers = %q/%q/%q", fake.tenantID, fake.serviceID, fake.imageID)
	}
}

func TestImageDeleteSurfacesNotFound(t *testing.T) {
	fake := &fakeServiceImageService{err: apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Delete(recorder, httptest.NewRequest(http.MethodDelete, "/", nil), handlerTenantID, handlerServiceID2, handlerImageID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

// --- ReplaceOrder --------------------------------------------------------

func TestImageReplaceOrderPassesTheIDListThrough(t *testing.T) {
	fake := &fakeServiceImageService{list: []*model.ServiceImage{storedImage()}}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	body := `{"image_ids":["a","b","c"]}`
	handler.ReplaceOrder(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), handlerTenantID, handlerServiceID2)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(fake.orderIDs) != 3 || fake.orderIDs[0] != "a" || fake.orderIDs[2] != "c" {
		t.Fatalf("orderIDs = %v", fake.orderIDs)
	}
}

func TestImageReplaceOrderSurfacesValidationFailure(t *testing.T) {
	fake := &fakeServiceImageService{err: apperrors.New(apperrors.CodeValidationFailed, "reorder request names an image that does not belong to this service", nil)}
	handler := NewServiceImageHandler(fake)
	recorder := httptest.NewRecorder()

	handler.ReplaceOrder(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"image_ids":["a"]}`)), handlerTenantID, handlerServiceID2)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "VALIDATION_FAILED")
}

// compile-time guard: the fake must keep satisfying the real interface.
var _ service.ServiceImageService = (*fakeServiceImageService)(nil)
