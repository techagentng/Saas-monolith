package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

const handlerCategoryID = "550e8400-e29b-41d4-a716-446655444001"

// fakeCategoryService mirrors fakeCatalogService's shape: it records exactly
// what the handler passed through, which is how the DTO-protection tests
// below prove a smuggled field never reached the service layer.
type fakeCategoryService struct {
	createInput  service.CreateCategoryInput
	updateInput  service.UpdateCategoryInput
	statusFilter string
	tenantID     string
	categoryID   string

	result *model.ServiceCategory
	list   []*model.ServiceCategory
	err    error
}

func (f *fakeCategoryService) Create(_ context.Context, tenantID string, input service.CreateCategoryInput) (*model.ServiceCategory, error) {
	f.tenantID, f.createInput = tenantID, input
	return f.result, f.err
}

func (f *fakeCategoryService) Get(_ context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error) {
	f.tenantID, f.categoryID = tenantID, categoryID
	return f.result, f.err
}

func (f *fakeCategoryService) List(_ context.Context, tenantID string, statusFilter string) ([]*model.ServiceCategory, error) {
	f.tenantID, f.statusFilter = tenantID, statusFilter
	return f.list, f.err
}

func (f *fakeCategoryService) Update(_ context.Context, tenantID string, categoryID string, input service.UpdateCategoryInput) (*model.ServiceCategory, error) {
	f.tenantID, f.categoryID, f.updateInput = tenantID, categoryID, input
	return f.result, f.err
}

func (f *fakeCategoryService) Archive(_ context.Context, tenantID string, categoryID string) (*model.ServiceCategory, error) {
	f.tenantID, f.categoryID = tenantID, categoryID
	return f.result, f.err
}

func storedCategory() *model.ServiceCategory {
	return &model.ServiceCategory{
		ID: handlerCategoryID, TenantID: handlerTenantID, Name: "Pedicures", SortOrder: 1,
		Status: model.StatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

// --- Create ------------------------------------------------------------------

func TestCategoryCreateRejectsMalformedJSON(t *testing.T) {
	handler := NewServiceCategoryHandler(&fakeCategoryService{})
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json")), handlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestCategoryCreateReturns201AndTheCategoryShape(t *testing.T) {
	categories := &fakeCategoryService{result: storedCategory()}
	handler := NewServiceCategoryHandler(categories)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Pedicures","sort_order":1}`)), handlerTenantID)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if categories.tenantID != handlerTenantID {
		t.Fatalf("tenant passed to the service = %q, want the route's %q", categories.tenantID, handlerTenantID)
	}
	if categories.createInput.Name != "Pedicures" || categories.createInput.SortOrder == nil || *categories.createInput.SortOrder != 1 {
		t.Fatalf("input passed through incorrectly: %+v", categories.createInput)
	}

	response := decodeBody(t, recorder)
	for _, key := range []string{"id", "name", "sort_order", "status", "created_at", "updated_at"} {
		if _, ok := response[key]; !ok {
			t.Fatalf("response missing %q: %s", key, recorder.Body.String())
		}
	}
	if _, present := response["tenant_id"]; present {
		t.Fatal("response exposed tenant_id, which is deliberately not part of the category DTO")
	}
}

// The mandatory DTO-protection proof, mirroring ServiceHandler's own: every
// server-owned field a client might try to smuggle in has nowhere to land.
func TestCategoryCreateIgnoresServerOwnedFieldsSmuggledIntoTheBody(t *testing.T) {
	categories := &fakeCategoryService{result: storedCategory()}
	handler := NewServiceCategoryHandler(categories)
	recorder := httptest.NewRecorder()

	body := `{
		"name":"Pedicures",
		"id":"11111111-1111-1111-1111-111111111111",
		"tenant_id":"22222222-2222-2222-2222-222222222222",
		"status":"ARCHIVED",
		"created_at":"2000-01-01T00:00:00Z"
	}`
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), handlerTenantID)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if categories.tenantID != handlerTenantID {
		t.Fatalf("tenant = %q, want the route's %q", categories.tenantID, handlerTenantID)
	}
	if categories.createInput != (service.CreateCategoryInput{Name: "Pedicures"}) {
		t.Fatalf("input = %+v, want only the accepted field", categories.createInput)
	}
	response := decodeBody(t, recorder)
	if response["status"] != string(model.StatusActive) {
		t.Fatalf("status = %v, want ACTIVE — a client-supplied status must have no effect", response["status"])
	}
}

// --- List / Get --------------------------------------------------------------

func TestCategoryListPassesTheStatusQueryThroughAndReturnsAnArray(t *testing.T) {
	categories := &fakeCategoryService{list: []*model.ServiceCategory{storedCategory()}}
	handler := NewServiceCategoryHandler(categories)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/?status=ARCHIVED", nil), handlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if categories.statusFilter != "ARCHIVED" {
		t.Fatalf("status filter = %q, want ARCHIVED", categories.statusFilter)
	}
	var result []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("returned %d categories, want 1", len(result))
	}
}

func TestCategoryListReturnsAnEmptyArrayRatherThanNull(t *testing.T) {
	handler := NewServiceCategoryHandler(&fakeCategoryService{list: nil})
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID)

	if got := strings.TrimSpace(recorder.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

func TestCategoryGetPassesBothIdentifiersThrough(t *testing.T) {
	categories := &fakeCategoryService{result: storedCategory()}
	handler := NewServiceCategoryHandler(categories)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerCategoryID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if categories.tenantID != handlerTenantID || categories.categoryID != handlerCategoryID {
		t.Fatalf("identifiers = %q/%q, want %q/%q", categories.tenantID, categories.categoryID, handlerTenantID, handlerCategoryID)
	}
}

func TestCategoryGetSurfacesNotFoundAs404(t *testing.T) {
	categories := &fakeCategoryService{err: apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", nil)}
	handler := NewServiceCategoryHandler(categories)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerCategoryID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertErrorCode(t, recorder, "CATEGORY_NOT_FOUND")
}

// --- Update ------------------------------------------------------------------

func TestCategoryUpdateAppliesOnlySuppliedFields(t *testing.T) {
	categories := &fakeCategoryService{result: storedCategory()}
	handler := NewServiceCategoryHandler(categories)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"name":"Deluxe Pedicures"}`)), handlerTenantID, handlerCategoryID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if categories.updateInput.Name == nil || *categories.updateInput.Name != "Deluxe Pedicures" {
		t.Fatalf("name = %v, want the supplied value", categories.updateInput.Name)
	}
	if categories.updateInput.SortOrder != nil {
		t.Fatalf("SortOrder = %v, want nil (omitted)", *categories.updateInput.SortOrder)
	}
}

func TestCategoryUpdateRejectsMalformedJSON(t *testing.T) {
	handler := NewServiceCategoryHandler(&fakeCategoryService{})
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader("{oops")), handlerTenantID, handlerCategoryID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

// --- Archive -----------------------------------------------------------------

func TestCategoryArchiveNeedsNoBodyAndReturnsTheArchivedCategory(t *testing.T) {
	archived := storedCategory()
	archived.Status = model.StatusArchived
	categories := &fakeCategoryService{result: archived}
	handler := NewServiceCategoryHandler(categories)
	recorder := httptest.NewRecorder()

	handler.Archive(recorder, httptest.NewRequest(http.MethodPost, "/", nil), handlerTenantID, handlerCategoryID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	response := decodeBody(t, recorder)
	if response["status"] != string(model.StatusArchived) {
		t.Fatalf("status = %v, want ARCHIVED", response["status"])
	}
}

func TestCategoryArchiveSurfacesNotFound(t *testing.T) {
	categories := &fakeCategoryService{err: apperrors.New(apperrors.CodeCategoryNotFound, "service category not found", nil)}
	handler := NewServiceCategoryHandler(categories)
	recorder := httptest.NewRecorder()

	handler.Archive(recorder, httptest.NewRequest(http.MethodPost, "/", nil), handlerTenantID, handlerCategoryID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertErrorCode(t, recorder, "CATEGORY_NOT_FOUND")
}

// compile-time guard: the fake must keep satisfying the real interface.
var _ service.CategoryService = (*fakeCategoryService)(nil)
