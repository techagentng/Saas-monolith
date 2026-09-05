package handler

import (
	"context"
	"encoding/json"
	"errors"
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
	handlerTenantID  = "550e8400-e29b-41d4-a716-446655442001"
	handlerServiceID = "550e8400-e29b-41d4-a716-446655442002"
)

// categoryID is a var, not a const: some cases below need its address to
// populate model.Service.CategoryID (*string).
var categoryID = "550e8400-e29b-41d4-a716-446655442003"

// fakeCatalogService records exactly what the handler passed through, which is
// how the DTO-protection tests below prove a smuggled field never reached the
// service layer.
type fakeCatalogService struct {
	createInput  service.CreateServiceInput
	updateInput  service.UpdateServiceInput
	statusFilter string
	tenantID     string
	serviceID    string

	result *model.Service
	list   []*model.Service
	err    error
}

func (f *fakeCatalogService) Create(_ context.Context, tenantID string, input service.CreateServiceInput) (*model.Service, error) {
	f.tenantID, f.createInput = tenantID, input
	return f.result, f.err
}

func (f *fakeCatalogService) Get(_ context.Context, tenantID string, serviceID string) (*model.Service, error) {
	f.tenantID, f.serviceID = tenantID, serviceID
	return f.result, f.err
}

func (f *fakeCatalogService) List(_ context.Context, tenantID string, statusFilter string) ([]*model.Service, error) {
	f.tenantID, f.statusFilter = tenantID, statusFilter
	return f.list, f.err
}

func (f *fakeCatalogService) Update(_ context.Context, tenantID string, serviceID string, input service.UpdateServiceInput) (*model.Service, error) {
	f.tenantID, f.serviceID, f.updateInput = tenantID, serviceID, input
	return f.result, f.err
}

func (f *fakeCatalogService) Archive(_ context.Context, tenantID string, serviceID string) (*model.Service, error) {
	f.tenantID, f.serviceID = tenantID, serviceID
	return f.result, f.err
}

func storedService() *model.Service {
	description := "Long-lasting gel finish."
	return &model.Service{
		ID: handlerServiceID, TenantID: handlerTenantID, Name: "Gel Manicure",
		Description: &description, DurationMinutes: 45, PriceMinor: 1999,
		Status: model.StatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v (body = %s)", err, recorder.Body.String())
	}
	return body
}

func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error response is not the standard envelope: %v (body = %s)", err, recorder.Body.String())
	}
	if envelope.Error.Code != want {
		t.Fatalf("error code = %q, want %q (body = %s)", envelope.Error.Code, want, recorder.Body.String())
	}
	if envelope.Error.Message == "" {
		t.Fatal("error response carried no message")
	}
}

// --- Create ------------------------------------------------------------------

func TestCreateRejectsMalformedJSON(t *testing.T) {
	handler := NewServiceHandler(&fakeCatalogService{})
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json")), handlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestCreateReturns201AndTheServiceShape(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	body := `{"name":"Gel Manicure","description":"Long-lasting gel finish.","duration_minutes":45,"price_minor":1999}`
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), handlerTenantID)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if catalog.tenantID != handlerTenantID {
		t.Fatalf("tenant passed to the service = %q, want the route's %q", catalog.tenantID, handlerTenantID)
	}
	if catalog.createInput.Name != "Gel Manicure" || catalog.createInput.DurationMinutes != 45 || catalog.createInput.PriceMinor != 1999 {
		t.Fatalf("input passed through incorrectly: %+v", catalog.createInput)
	}

	response := decodeBody(t, recorder)
	for _, key := range []string{"id", "name", "description", "duration_minutes", "price_minor", "status", "created_at", "updated_at"} {
		if _, ok := response[key]; !ok {
			t.Fatalf("response missing %q: %s", key, recorder.Body.String())
		}
	}
	// tenant_id is not echoed: it is already the caller's own tenant, named in
	// the route. currency lives on the tenant, not on each catalog row.
	for _, key := range []string{"tenant_id", "currency"} {
		if _, present := response[key]; present {
			t.Fatalf("response exposed %q, which is deliberately not part of the service DTO", key)
		}
	}
	if response["price_minor"].(float64) != 1999 {
		t.Fatalf("price_minor = %v, want 1999", response["price_minor"])
	}
}

// The mandatory DTO-protection proof: a client sends every server-owned field
// it might hope to control, and none of them can land, because the decode
// target has no field to receive them.
func TestCreateIgnoresServerOwnedFieldsSmuggledIntoTheBody(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	body := `{
		"name":"Gel Manicure",
		"duration_minutes":45,
		"price_minor":1999,
		"id":"11111111-1111-1111-1111-111111111111",
		"tenant_id":"22222222-2222-2222-2222-222222222222",
		"status":"ARCHIVED",
		"currency":"USD",
		"created_at":"2000-01-01T00:00:00Z",
		"updated_at":"2000-01-01T00:00:00Z"
	}`
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), handlerTenantID)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	// The trusted tenant is the route's, never the body's.
	if catalog.tenantID != handlerTenantID {
		t.Fatalf("tenant = %q, want the route's %q — a body tenant_id must never be honored", catalog.tenantID, handlerTenantID)
	}
	// CreateServiceInput carries exactly four fields; there is structurally
	// nowhere for status, currency, id or the timestamps to have gone.
	if catalog.createInput != (service.CreateServiceInput{Name: "Gel Manicure", DurationMinutes: 45, PriceMinor: 1999}) {
		t.Fatalf("input = %+v, want only the four accepted fields", catalog.createInput)
	}
	// The response reflects the server's state, not the client's wishes.
	response := decodeBody(t, recorder)
	if response["status"] != string(model.StatusActive) {
		t.Fatalf("status = %v, want ACTIVE — a client-supplied status must have no effect", response["status"])
	}
	if response["id"] == "11111111-1111-1111-1111-111111111111" {
		t.Fatal("a client-supplied id was honored")
	}
}

func TestCreateSerializesServiceErrorsAsTheStandardEnvelope(t *testing.T) {
	catalog := &fakeCatalogService{err: apperrors.New(apperrors.CodeValidationFailed, "service duration must be greater than zero", nil)}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x","duration_minutes":0,"price_minor":0}`)), handlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "VALIDATION_FAILED")
}

func TestCreateNeverLeaksAnInternalError(t *testing.T) {
	catalog := &fakeCatalogService{err: errors.New("pq: connection reset by peer")}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Gel Manicure","duration_minutes":45,"price_minor":1999}`)), handlerTenantID)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	assertErrorCode(t, recorder, "INTERNAL_ERROR")
	if strings.Contains(recorder.Body.String(), "connection reset") {
		t.Fatalf("the raw driver error leaked to the client: %s", recorder.Body.String())
	}
}

// --- List / Get --------------------------------------------------------------

func TestListPassesTheStatusQueryThroughAndReturnsAnArray(t *testing.T) {
	catalog := &fakeCatalogService{list: []*model.Service{storedService()}}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/?status=ARCHIVED", nil), handlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if catalog.statusFilter != "ARCHIVED" {
		t.Fatalf("status filter = %q, want ARCHIVED", catalog.statusFilter)
	}
	var services []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &services); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("returned %d services, want 1", len(services))
	}
}

func TestListReturnsAnEmptyArrayRatherThanNull(t *testing.T) {
	// A client iterating the catalog should not have to special-case "no
	// services yet".
	handler := NewServiceHandler(&fakeCatalogService{list: nil})
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID)

	if got := strings.TrimSpace(recorder.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

func TestGetPassesBothIdentifiersThrough(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerServiceID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if catalog.tenantID != handlerTenantID || catalog.serviceID != handlerServiceID {
		t.Fatalf("identifiers = %q/%q, want %q/%q", catalog.tenantID, catalog.serviceID, handlerTenantID, handlerServiceID)
	}
}

func TestGetSurfacesServiceNotFoundAs404(t *testing.T) {
	catalog := &fakeCatalogService{err: apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerServiceID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertErrorCode(t, recorder, "SERVICE_NOT_FOUND")
	// The message must not distinguish "belongs to another tenant" from
	// "does not exist".
	if strings.Contains(strings.ToLower(recorder.Body.String()), "tenant") {
		t.Fatalf("the 404 body mentions tenancy, which discloses why the lookup failed: %s", recorder.Body.String())
	}
}

// --- Update ------------------------------------------------------------------

func TestUpdateAppliesOnlySuppliedFields(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"name":"Gel Manicure Deluxe"}`)), handlerTenantID, handlerServiceID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if catalog.updateInput.Name == nil || *catalog.updateInput.Name != "Gel Manicure Deluxe" {
		t.Fatalf("name = %v, want the supplied value", catalog.updateInput.Name)
	}
	// Omitted fields stay nil, which is what distinguishes "not supplied" from
	// "set to zero".
	if catalog.updateInput.DurationMinutes != nil || catalog.updateInput.PriceMinor != nil || catalog.updateInput.Description != nil {
		t.Fatalf("omitted fields were populated: %+v", catalog.updateInput)
	}
}

func TestUpdateIgnoresServerOwnedFieldsSmuggledIntoTheBody(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	body := `{
		"name":"Gel Manicure Deluxe",
		"id":"11111111-1111-1111-1111-111111111111",
		"tenant_id":"22222222-2222-2222-2222-222222222222",
		"status":"ARCHIVED",
		"currency":"USD",
		"created_at":"2000-01-01T00:00:00Z"
	}`
	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)), handlerTenantID, handlerServiceID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if catalog.tenantID != handlerTenantID {
		t.Fatalf("tenant = %q, want the route's %q", catalog.tenantID, handlerTenantID)
	}
	if catalog.serviceID != handlerServiceID {
		t.Fatalf("service = %q, want the route's %q — a body id must never be honored", catalog.serviceID, handlerServiceID)
	}
	// UpdateServiceInput has exactly four pointer fields. status, currency, id
	// and tenant_id have nowhere to land, so the only populated field is name.
	if catalog.updateInput.DurationMinutes != nil || catalog.updateInput.PriceMinor != nil || catalog.updateInput.Description != nil {
		t.Fatalf("a smuggled field populated the update input: %+v", catalog.updateInput)
	}
	response := decodeBody(t, recorder)
	if response["status"] != string(model.StatusActive) {
		t.Fatalf("status = %v, want ACTIVE — status is never client-writable through PATCH", response["status"])
	}
}

// --- category_id tri-state (SC1) ---------------------------------------------

func TestCreatePassesThroughASuppliedCategoryID(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	body := `{"name":"Gel Manicure","duration_minutes":45,"price_minor":1999,"category_id":"` + categoryID + `"}`
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), handlerTenantID)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if catalog.createInput.CategoryID == nil || *catalog.createInput.CategoryID != categoryID {
		t.Fatalf("CategoryID = %v, want %q", catalog.createInput.CategoryID, categoryID)
	}
}

func TestCreateWithoutCategoryIDLeavesItNil(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Gel Manicure","duration_minutes":45,"price_minor":1999}`)), handlerTenantID)

	if catalog.createInput.CategoryID != nil {
		t.Fatalf("CategoryID = %v, want nil when the key is omitted", *catalog.createInput.CategoryID)
	}
}

// PATCH's category_id must distinguish three wire states: omitted (leave
// unchanged), present and null (clear), and present with a value (assign) —
// a distinction a plain *string cannot make. These three tests prove the
// nullableCategoryID wrapper reaches UpdateServiceInput.CategoryID's
// **string tri-state correctly in each case.
func TestUpdateOmittedCategoryIDLeavesItUnwired(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"name":"Renamed"}`)), handlerTenantID, handlerServiceID)

	if catalog.updateInput.CategoryID != nil {
		t.Fatalf("CategoryID = %v, want nil (unwired) when the key is omitted entirely", catalog.updateInput.CategoryID)
	}
}

func TestUpdateExplicitNullCategoryIDRequestsClear(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"category_id":null}`)), handlerTenantID, handlerServiceID)

	if catalog.updateInput.CategoryID == nil {
		t.Fatal("CategoryID = nil, want a non-nil outer pointer for a present key")
	}
	if *catalog.updateInput.CategoryID != nil {
		t.Fatalf("CategoryID inner value = %v, want nil (clear) for an explicit null", **catalog.updateInput.CategoryID)
	}
}

func TestUpdateCategoryIDValueRequestsAssignment(t *testing.T) {
	catalog := &fakeCatalogService{result: storedService()}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"category_id":"`+categoryID+`"}`)), handlerTenantID, handlerServiceID)

	if catalog.updateInput.CategoryID == nil || *catalog.updateInput.CategoryID == nil {
		t.Fatalf("CategoryID = %v, want a set value", catalog.updateInput.CategoryID)
	}
	if **catalog.updateInput.CategoryID != categoryID {
		t.Fatalf("CategoryID inner value = %q, want %q", **catalog.updateInput.CategoryID, categoryID)
	}
}

// The authenticated owner-facing DTO exposes the raw category_id — unlike the
// anonymous public catalog, this caller is managing the assignment itself.
func TestPublicServiceExposesCategoryID(t *testing.T) {
	stored := storedService()
	stored.CategoryID = &categoryID
	catalog := &fakeCatalogService{result: stored}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerServiceID)

	response := decodeBody(t, recorder)
	if response["category_id"] != categoryID {
		t.Fatalf("category_id = %v, want %q", response["category_id"], categoryID)
	}
}

func TestUpdateRejectsMalformedJSON(t *testing.T) {
	handler := NewServiceHandler(&fakeCatalogService{})
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader("{oops")), handlerTenantID, handlerServiceID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

// --- Archive -----------------------------------------------------------------

func TestArchiveNeedsNoBodyAndReturnsTheArchivedService(t *testing.T) {
	archived := storedService()
	archived.Status = model.StatusArchived
	catalog := &fakeCatalogService{result: archived}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Archive(recorder, httptest.NewRequest(http.MethodPost, "/", nil), handlerTenantID, handlerServiceID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if catalog.tenantID != handlerTenantID || catalog.serviceID != handlerServiceID {
		t.Fatalf("identifiers = %q/%q", catalog.tenantID, catalog.serviceID)
	}
	response := decodeBody(t, recorder)
	if response["status"] != string(model.StatusArchived) {
		t.Fatalf("status = %v, want ARCHIVED", response["status"])
	}
}

func TestArchiveSurfacesNotFound(t *testing.T) {
	catalog := &fakeCatalogService{err: apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)}
	handler := NewServiceHandler(catalog)
	recorder := httptest.NewRecorder()

	handler.Archive(recorder, httptest.NewRequest(http.MethodPost, "/", nil), handlerTenantID, handlerServiceID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertErrorCode(t, recorder, "SERVICE_NOT_FOUND")
}

// compile-time guard: the fake must keep satisfying the real interface, so a
// change to CatalogService cannot leave these tests exercising a stale shape.
var _ service.CatalogService = (*fakeCatalogService)(nil)
