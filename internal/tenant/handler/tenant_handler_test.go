package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

const testCreatorID = "550e8400-e29b-41d4-a716-446655440777"

func TestCreateTenantHandlerRequiresAuthentication(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{}, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(`{"name":"Salon","slug":"salon"}`))

	handler.Create(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestCreateTenantHandlerRejectsMalformedJSON(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{}, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	request := authenticatedTenantRequest(t, `not json`)

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestCreateTenantHandlerUsesAuthenticatedCreatorNeverRequestBody(t *testing.T) {
	fake := &tenantServiceFake{tenant: &model.Tenant{ID: "tenant-1", Name: "Salon", Slug: "salon", Status: model.StatusActive}}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	// Body attempts to smuggle a different owner/user/role — the handler's
	// decode target has no field for any of them, so they cannot influence
	// the call regardless of decoder strictness.
	request := authenticatedTenantRequest(t, `{"name":"Salon","slug":"salon","owner_id":"someone-else","user_id":"someone-else","role":"SUPER_ADMIN"}`)

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if fake.receivedInput.CreatorUserID != testCreatorID {
		t.Fatalf("creator = %q, want authenticated principal %q", fake.receivedInput.CreatorUserID, testCreatorID)
	}
}

func TestCreateTenantHandlerReturns201OnSuccess(t *testing.T) {
	now := time.Now()
	tenant := &model.Tenant{ID: "tenant-1", Name: "Salon", Slug: "salon", Status: model.StatusActive, CreatedAt: now, UpdatedAt: now}
	handler := NewTenantHandler(&tenantServiceFake{tenant: tenant}, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	request := authenticatedTenantRequest(t, `{"name":"Salon","slug":"salon"}`)

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["id"] != "tenant-1" || body["name"] != "Salon" || body["slug"] != "salon" || body["status"] != "ACTIVE" {
		t.Fatalf("response = %s", recorder.Body.Bytes())
	}
}

// F1: the decode target only ever has a field for business_type — there is
// no field for onboarding_status, onboarding_step, or any owner/role
// identifier, so a client attempting to smuggle any of them in has them
// silently discarded regardless of decoder strictness, the same protection
// TestCreateTenantHandlerUsesAuthenticatedCreatorNeverRequestBody already
// proves for ownership.
func TestCreateTenantHandlerAcceptsBusinessTypeAndIgnoresOnboardingFields(t *testing.T) {
	fake := &tenantServiceFake{tenant: &model.Tenant{ID: "tenant-1", Name: "Hotel Co", Slug: "hotel-co", Status: model.StatusActive}}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	request := authenticatedTenantRequest(t, `{"name":"Hotel Co","slug":"hotel-co","business_type":"HOTEL","onboarding_status":"COMPLETED","onboarding_step":"review"}`)

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	if fake.receivedInput.BusinessType != "HOTEL" {
		t.Fatalf("BusinessType = %q, want %q from the request body", fake.receivedInput.BusinessType, "HOTEL")
	}
	// CreateTenantInput has no onboarding_status/onboarding_step field at
	// all, so there is nothing further to assert here beyond the fact that
	// this compiles and the request succeeds — the absence of those fields
	// on the struct itself is the enforcement.
}

func TestCreateTenantHandlerPropagatesInvalidBusinessType(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{err: apperrors.New(apperrors.CodeValidationFailed, "invalid business type", nil)}, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	request := authenticatedTenantRequest(t, `{"name":"Salon","slug":"salon","business_type":"BARBERSHOP"}`)

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 VALIDATION_FAILED, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateTenantHandlerPropagatesSlugTaken(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{err: apperrors.New(apperrors.CodeTenantSlugTaken, "tenant slug is already taken", nil)}, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	request := authenticatedTenantRequest(t, `{"name":"Salon","slug":"salon"}`)

	handler.Create(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestListTenantHandlerRequiresAuthentication(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{}, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)

	handler.List(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestListTenantHandlerReturnsServiceTenantsAs200(t *testing.T) {
	now := time.Now()
	tenants := []*model.Tenant{
		{ID: "tenant-1", Name: "Salon A", Slug: "salon-a", Status: model.StatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "tenant-2", Name: "Salon B", Slug: "salon-b", Status: model.StatusActive, CreatedAt: now, UpdatedAt: now},
	}
	retrieval := &fakeRetrievalService{tenants: tenants}
	handler := NewTenantHandler(&tenantServiceFake{}, retrieval)
	recorder := httptest.NewRecorder()
	request := authenticatedGetRequest(t, http.MethodGet, "/api/v1/tenants")

	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", recorder.Code, recorder.Body.String())
	}
	var body []PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v, body=%s", err, recorder.Body.String())
	}
	if len(body) != 2 || body[0].ID != "tenant-1" || body[1].ID != "tenant-2" {
		t.Fatalf("response = %#v", body)
	}
	if retrieval.receivedListUser != testCreatorID {
		t.Fatalf("service received userID = %q, want authenticated principal %q", retrieval.receivedListUser, testCreatorID)
	}
}

func TestListTenantHandlerReturnsEmptyArrayNot404(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{}, &fakeRetrievalService{tenants: []*model.Tenant{}})
	recorder := httptest.NewRecorder()
	request := authenticatedGetRequest(t, http.MethodGet, "/api/v1/tenants")

	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for empty list, body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.TrimSpace(recorder.Body.String()) != "[]" {
		t.Fatalf("body = %q, want empty JSON array", recorder.Body.String())
	}
}

func TestListTenantHandlerPropagatesServiceError(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{}, &fakeRetrievalService{listErr: apperrors.New(apperrors.CodeInternalError, "db down", nil)})
	recorder := httptest.NewRecorder()
	request := authenticatedGetRequest(t, http.MethodGet, "/api/v1/tenants")

	handler.List(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestGetByIDTenantHandlerRequiresAuthentication(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{}, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-1", nil)

	handler.GetByID(recorder, request, "tenant-1")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestGetByIDTenantHandlerReturnsServiceTenantAs200(t *testing.T) {
	now := time.Now()
	tenant := &model.Tenant{ID: "tenant-1", Name: "Salon", Slug: "salon", Status: model.StatusActive, CreatedAt: now, UpdatedAt: now}
	retrieval := &fakeRetrievalService{tenant: tenant}
	handler := NewTenantHandler(&tenantServiceFake{}, retrieval)
	recorder := httptest.NewRecorder()
	request := authenticatedGetRequest(t, http.MethodGet, "/api/v1/tenants/tenant-1")

	handler.GetByID(recorder, request, "tenant-1")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", recorder.Code, recorder.Body.String())
	}
	var body PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v, body=%s", err, recorder.Body.String())
	}
	if body.ID != "tenant-1" || body.Name != "Salon" || body.Slug != "salon" {
		t.Fatalf("response = %#v", body)
	}
	if retrieval.receivedGetUser != testCreatorID || retrieval.receivedGetID != "tenant-1" {
		t.Fatalf("service received userID=%q tenantID=%q, want principal=%q tenantID=%q", retrieval.receivedGetUser, retrieval.receivedGetID, testCreatorID, "tenant-1")
	}
}

// F1: GetByID's response (an owner/authenticated representation, distinct
// from the anonymous public tenant identity endpoint) must actually carry
// business_type/onboarding_status/onboarding_step when the tenant has them
// — this is what future F4/F7 will read.
func TestGetByIDTenantHandlerIncludesBusinessTypeAndOnboardingState(t *testing.T) {
	now := time.Now()
	businessType := model.BusinessTypeHotel
	step := "room_types"
	tenant := &model.Tenant{
		ID: "tenant-1", Name: "Hotel Co", Slug: "hotel-co", Status: model.StatusActive,
		BusinessType: &businessType, OnboardingStatus: model.OnboardingStatusInProgress, OnboardingStep: &step,
		CreatedAt: now, UpdatedAt: now,
	}
	retrieval := &fakeRetrievalService{tenant: tenant}
	handler := NewTenantHandler(&tenantServiceFake{}, retrieval)
	recorder := httptest.NewRecorder()
	request := authenticatedGetRequest(t, http.MethodGet, "/api/v1/tenants/tenant-1")

	handler.GetByID(recorder, request, "tenant-1")

	var body PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v, body=%s", err, recorder.Body.String())
	}
	if body.BusinessType == nil || *body.BusinessType != model.BusinessTypeHotel {
		t.Fatalf("BusinessType = %v, want HOTEL", body.BusinessType)
	}
	if body.OnboardingStatus != model.OnboardingStatusInProgress {
		t.Fatalf("OnboardingStatus = %q, want IN_PROGRESS", body.OnboardingStatus)
	}
	if body.OnboardingStep == nil || *body.OnboardingStep != "room_types" {
		t.Fatalf("OnboardingStep = %v, want room_types", body.OnboardingStep)
	}
}

// F1: a legacy tenant with no business_type must serialize as a null field,
// not omit the key or fail — the majority case for every pre-F1 tenant.
func TestGetByIDTenantHandlerSerializesNilBusinessTypeAsNull(t *testing.T) {
	tenant := &model.Tenant{ID: "tenant-1", Name: "Legacy", Slug: "legacy", Status: model.StatusActive, OnboardingStatus: model.OnboardingStatusCompleted}
	retrieval := &fakeRetrievalService{tenant: tenant}
	handler := NewTenantHandler(&tenantServiceFake{}, retrieval)
	recorder := httptest.NewRecorder()
	request := authenticatedGetRequest(t, http.MethodGet, "/api/v1/tenants/tenant-1")

	handler.GetByID(recorder, request, "tenant-1")

	if !strings.Contains(recorder.Body.String(), `"business_type":null`) {
		t.Fatalf("body = %s, want business_type key present and null", recorder.Body.String())
	}
}

func TestGetByIDTenantHandlerPropagatesAccessDeniedError(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{}, &fakeRetrievalService{getErr: apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)})
	recorder := httptest.NewRecorder()
	request := authenticatedGetRequest(t, http.MethodGet, "/api/v1/tenants/tenant-2")

	handler.GetByID(recorder, request, "tenant-2")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetByIDTenantHandlerPropagatesInvalidRequestError(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{}, &fakeRetrievalService{getErr: apperrors.New(apperrors.CodeInvalidRequest, "invalid request", nil)})
	recorder := httptest.NewRecorder()
	request := authenticatedGetRequest(t, http.MethodGet, "/api/v1/tenants/not-a-uuid")

	handler.GetByID(recorder, request, "not-a-uuid")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", recorder.Code, recorder.Body.String())
	}
}

func authenticatedGetRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	ctx := auth.WithPrincipal(request.Context(), auth.Principal{UserID: testCreatorID, SessionID: "session"})
	return request.WithContext(ctx)
}

func authenticatedTenantRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(body))
	ctx := auth.WithPrincipal(request.Context(), auth.Principal{UserID: testCreatorID, SessionID: "session"})
	return request.WithContext(ctx)
}

type tenantServiceFake struct {
	tenant        *model.Tenant
	err           error
	receivedInput service.CreateTenantInput

	updateErr      error
	updateTenant   *model.Tenant
	updateCalls    int
	updateTenantID string
	updateInput    service.UpdateTenantProfileRequest
}

func (f *tenantServiceFake) Create(_ context.Context, input service.CreateTenantInput) (*model.Tenant, error) {
	f.receivedInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.tenant, nil
}

func (f *tenantServiceFake) UpdateProfile(_ context.Context, tenantID string, input service.UpdateTenantProfileRequest) (*model.Tenant, error) {
	f.updateCalls++
	f.updateTenantID = tenantID
	f.updateInput = input
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.updateTenant, nil
}

type fakeRetrievalService struct {
	tenants          []*model.Tenant
	tenant           *model.Tenant
	listErr          error
	getErr           error
	receivedListUser string
	receivedGetUser  string
	receivedGetID    string
}

func (f *fakeRetrievalService) ListAccessible(_ context.Context, userID string) ([]*model.Tenant, error) {
	f.receivedListUser = userID
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.tenants == nil {
		return []*model.Tenant{}, nil
	}
	return f.tenants, nil
}

func (f *fakeRetrievalService) GetAccessible(_ context.Context, userID, tenantID string) (*model.Tenant, error) {
	f.receivedGetUser = userID
	f.receivedGetID = tenantID
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.tenant != nil {
		return f.tenant, nil
	}
	return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "access denied", nil)
}
