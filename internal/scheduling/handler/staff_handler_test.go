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
	handlerStaffID = "550e8400-e29b-41d4-a716-446655449001"
	handlerUserID  = "550e8400-e29b-41d4-a716-446655449002"
)

// fakeStaffService records exactly what the handler passed through, which is how
// the DTO-protection tests prove a smuggled field never reached the service.
type fakeStaffService struct {
	createInput  service.CreateStaffInput
	updateInput  service.UpdateStaffInput
	serviceIDs   []string
	statusFilter string
	tenantID     string
	staffID      string

	result     *model.StaffProfile
	list       []*model.StaffProfile
	capability []string
	err        error
}

func (f *fakeStaffService) Create(_ context.Context, tenantID string, input service.CreateStaffInput) (*model.StaffProfile, error) {
	f.tenantID, f.createInput = tenantID, input
	return f.result, f.err
}

func (f *fakeStaffService) Get(_ context.Context, tenantID string, staffID string) (*model.StaffProfile, error) {
	f.tenantID, f.staffID = tenantID, staffID
	return f.result, f.err
}

func (f *fakeStaffService) List(_ context.Context, tenantID string, statusFilter string) ([]*model.StaffProfile, error) {
	f.tenantID, f.statusFilter = tenantID, statusFilter
	return f.list, f.err
}

func (f *fakeStaffService) Update(_ context.Context, tenantID string, staffID string, input service.UpdateStaffInput) (*model.StaffProfile, error) {
	f.tenantID, f.staffID, f.updateInput = tenantID, staffID, input
	return f.result, f.err
}

func (f *fakeStaffService) Archive(_ context.Context, tenantID string, staffID string) (*model.StaffProfile, error) {
	f.tenantID, f.staffID = tenantID, staffID
	return f.result, f.err
}

func (f *fakeStaffService) ListCapabilities(_ context.Context, tenantID string, staffID string) ([]string, error) {
	f.tenantID, f.staffID = tenantID, staffID
	return f.capability, f.err
}

func (f *fakeStaffService) ReplaceCapabilities(_ context.Context, tenantID string, staffID string, serviceIDs []string) ([]string, error) {
	f.tenantID, f.staffID, f.serviceIDs = tenantID, staffID, serviceIDs
	return f.capability, f.err
}

func storedStaff() *model.StaffProfile {
	bio := "Ten years of gel work."
	userID := handlerUserID
	return &model.StaffProfile{
		ID: handlerStaffID, TenantID: handlerTenantID, UserID: &userID,
		DisplayName: "Ada", Bio: &bio, IsBookable: true, Status: model.StatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

// --- create ------------------------------------------------------------------

func TestStaffCreateRejectsMalformedJSON(t *testing.T) {
	handler := NewStaffHandler(&fakeStaffService{})
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json")), handlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestStaffCreateReturns201AndTheProfileShape(t *testing.T) {
	staff := &fakeStaffService{result: storedStaff()}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	body := `{"display_name":"Ada","bio":"Ten years of gel work.","user_id":"` + handlerUserID + `","is_bookable":true}`
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), handlerTenantID)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if staff.tenantID != handlerTenantID {
		t.Fatalf("tenant = %q, want the route's %q", staff.tenantID, handlerTenantID)
	}
	if staff.createInput.DisplayName != "Ada" || staff.createInput.UserID == nil || *staff.createInput.UserID != handlerUserID {
		t.Fatalf("input passed through incorrectly: %+v", staff.createInput)
	}

	response := decodeBody(t, recorder)
	for _, key := range []string{"id", "user_id", "display_name", "bio", "is_bookable", "status", "created_at", "updated_at"} {
		if _, ok := response[key]; !ok {
			t.Fatalf("response missing %q: %s", key, recorder.Body.String())
		}
	}
	// tenant_id is not echoed; it is already the caller's own tenant.
	if _, present := response["tenant_id"]; present {
		t.Fatal("response exposed tenant_id")
	}
}

// A non-login worker omits user_id entirely, and the handler must pass that
// absence through as nil rather than an empty string.
func TestStaffCreateSupportsANonLoginWorker(t *testing.T) {
	profile := storedStaff()
	profile.UserID = nil
	staff := &fakeStaffService{result: profile}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"display_name":"Chioma"}`)), handlerTenantID)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if staff.createInput.UserID != nil {
		t.Fatalf("UserID = %v, want nil when omitted", *staff.createInput.UserID)
	}
	if staff.createInput.IsBookable != nil {
		t.Fatal("IsBookable was populated when omitted — the default belongs to the service layer")
	}
	response := decodeBody(t, recorder)
	if response["user_id"] != nil {
		t.Fatalf("user_id = %v, want null for a non-login worker", response["user_id"])
	}
}

// The mandatory DTO-protection proof.
func TestStaffCreateIgnoresServerOwnedFieldsSmuggledIntoTheBody(t *testing.T) {
	staff := &fakeStaffService{result: storedStaff()}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	body := `{
		"display_name":"Ada",
		"id":"11111111-1111-1111-1111-111111111111",
		"tenant_id":"22222222-2222-2222-2222-222222222222",
		"status":"ARCHIVED",
		"created_at":"2000-01-01T00:00:00Z",
		"updated_at":"2000-01-01T00:00:00Z"
	}`
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), handlerTenantID)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if staff.tenantID != handlerTenantID {
		t.Fatalf("tenant = %q, want the route's %q — a body tenant_id must never be honored", staff.tenantID, handlerTenantID)
	}
	// CreateStaffInput carries exactly four fields; status, id and the
	// timestamps have structurally nowhere to land.
	if staff.createInput.DisplayName != "Ada" || staff.createInput.Bio != nil || staff.createInput.UserID != nil || staff.createInput.IsBookable != nil {
		t.Fatalf("a smuggled field populated the input: %+v", staff.createInput)
	}
	response := decodeBody(t, recorder)
	if response["status"] != string(model.StatusActive) {
		t.Fatalf("status = %v, want ACTIVE — a client-supplied status must have no effect", response["status"])
	}
}

func TestStaffCreateNeverLeaksAnInternalError(t *testing.T) {
	staff := &fakeStaffService{err: errors.New("pq: connection reset by peer")}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"display_name":"Ada"}`)), handlerTenantID)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	assertErrorCode(t, recorder, "INTERNAL_ERROR")
	if strings.Contains(recorder.Body.String(), "connection reset") {
		t.Fatalf("the raw driver error leaked: %s", recorder.Body.String())
	}
}

// --- list / get --------------------------------------------------------------

func TestStaffListPassesTheStatusQueryThroughAndReturnsAnArray(t *testing.T) {
	staff := &fakeStaffService{list: []*model.StaffProfile{storedStaff()}}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/?status=ARCHIVED", nil), handlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if staff.statusFilter != "ARCHIVED" {
		t.Fatalf("status filter = %q, want ARCHIVED", staff.statusFilter)
	}
	var profiles []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &profiles); err != nil {
		t.Fatalf("response is not a JSON array: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("returned %d profiles, want 1", len(profiles))
	}
}

func TestStaffListReturnsAnEmptyArrayRatherThanNull(t *testing.T) {
	handler := NewStaffHandler(&fakeStaffService{list: nil})
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID)

	if got := strings.TrimSpace(recorder.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

func TestStaffGetSurfacesNotFoundWithoutDisclosingTenancy(t *testing.T) {
	staff := &fakeStaffService{err: apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertErrorCode(t, recorder, "STAFF_NOT_FOUND")
	if strings.Contains(strings.ToLower(recorder.Body.String()), "tenant") {
		t.Fatalf("the 404 body mentions tenancy, disclosing why the lookup failed: %s", recorder.Body.String())
	}
}

// --- update / archive --------------------------------------------------------

func TestStaffUpdateAppliesOnlySuppliedFields(t *testing.T) {
	staff := &fakeStaffService{result: storedStaff()}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"display_name":"Ada Obi"}`)), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if staff.updateInput.DisplayName == nil || *staff.updateInput.DisplayName != "Ada Obi" {
		t.Fatalf("display_name = %v, want the supplied value", staff.updateInput.DisplayName)
	}
	if staff.updateInput.Bio != nil || staff.updateInput.IsBookable != nil {
		t.Fatalf("omitted fields were populated: %+v", staff.updateInput)
	}
}

// user_id has no field on the update DTO: re-pointing a profile at a different
// person is not an edit, and status belongs to archiving.
func TestStaffUpdateCannotChangeTheLinkedUserOrStatus(t *testing.T) {
	staff := &fakeStaffService{result: storedStaff()}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	body := `{
		"display_name":"Ada Obi",
		"user_id":"33333333-3333-3333-3333-333333333333",
		"tenant_id":"22222222-2222-2222-2222-222222222222",
		"status":"ARCHIVED",
		"id":"11111111-1111-1111-1111-111111111111"
	}`
	handler.Update(recorder, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if staff.staffID != handlerStaffID {
		t.Fatalf("staff = %q, want the route's %q — a body id must never be honored", staff.staffID, handlerStaffID)
	}
	if staff.updateInput.Bio != nil || staff.updateInput.IsBookable != nil {
		t.Fatalf("a smuggled field populated the update input: %+v", staff.updateInput)
	}
	response := decodeBody(t, recorder)
	if response["status"] != string(model.StatusActive) {
		t.Fatalf("status = %v, want ACTIVE — status is never client-writable through PATCH", response["status"])
	}
	if response["user_id"] != handlerUserID {
		t.Fatalf("user_id = %v, want it unchanged", response["user_id"])
	}
}

func TestStaffArchiveNeedsNoBody(t *testing.T) {
	archived := storedStaff()
	archived.Status = model.StatusArchived
	staff := &fakeStaffService{result: archived}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	handler.Archive(recorder, httptest.NewRequest(http.MethodPost, "/", nil), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	response := decodeBody(t, recorder)
	if response["status"] != string(model.StatusArchived) {
		t.Fatalf("status = %v, want ARCHIVED", response["status"])
	}
}

// --- capabilities ------------------------------------------------------------

func TestReplaceCapabilitiesPassesTheFullSetThrough(t *testing.T) {
	staff := &fakeStaffService{capability: []string{"aaa", "bbb"}}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	handler.ReplaceCapabilities(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"service_ids":["aaa","bbb"]}`)), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(staff.serviceIDs) != 2 || staff.serviceIDs[0] != "aaa" {
		t.Fatalf("service ids = %v, want the full submitted set", staff.serviceIDs)
	}

	var body StaffCapabilities
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body.ServiceIDs) != 2 {
		t.Fatalf("response service_ids = %v, want the committed set", body.ServiceIDs)
	}
}

// An omitted or null service_ids means "this technician performs nothing",
// which is a legitimate state rather than a malformed request.
func TestReplaceCapabilitiesTreatsAnAbsentSetAsEmpty(t *testing.T) {
	staff := &fakeStaffService{capability: []string{}}
	handler := NewStaffHandler(staff)

	for _, body := range []string{`{}`, `{"service_ids":null}`, `{"service_ids":[]}`} {
		recorder := httptest.NewRecorder()
		handler.ReplaceCapabilities(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), handlerTenantID, handlerStaffID)

		if recorder.Code != http.StatusOK {
			t.Fatalf("body %s: status = %d, want 200", body, recorder.Code)
		}
		if len(staff.serviceIDs) != 0 {
			t.Fatalf("body %s: service ids = %v, want an empty set", body, staff.serviceIDs)
		}
	}
}

func TestReplaceCapabilitiesRejectsMalformedJSON(t *testing.T) {
	handler := NewStaffHandler(&fakeStaffService{})
	recorder := httptest.NewRecorder()

	handler.ReplaceCapabilities(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader("{oops")), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestListCapabilitiesReturnsServiceIDsOnly(t *testing.T) {
	staff := &fakeStaffService{capability: []string{"aaa"}}
	handler := NewStaffHandler(staff)
	recorder := httptest.NewRecorder()

	handler.ListCapabilities(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	response := decodeBody(t, recorder)
	if _, present := response["service_ids"]; !present {
		t.Fatalf("response missing service_ids: %s", recorder.Body.String())
	}
	// Service definitions are the catalog's to serve; duplicating them here
	// would create a second copy that could disagree with it.
	for _, leaked := range []string{"name", "price_minor", "duration_minutes"} {
		if _, present := response[leaked]; present {
			t.Fatalf("capability response leaked catalog field %q", leaked)
		}
	}
}

// The tenant-facing DTO must never carry credential or account material. It
// exposes user_id (a reference an owner needs) and nothing else about the user.
func TestStaffDTOCarriesNoCredentialOrAccountData(t *testing.T) {
	encoded, err := json.Marshal(toPublicStaff(storedStaff()))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"email", "password", "hash", "role", "membership", "token"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("staff DTO exposes %q: %s", forbidden, encoded)
		}
	}
}

// compile-time guard: the fake must keep satisfying the real interface.
var _ service.StaffService = (*fakeStaffService)(nil)
