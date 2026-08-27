package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

const onboardingHandlerTenantID = "tenant-onboarding-1"

func TestOnboardingHandlerSaveProgressReturns200OnSuccess(t *testing.T) {
	tenant := &model.Tenant{ID: onboardingHandlerTenantID, Name: "Hotel Co", Slug: "hotel-co", Status: model.StatusActive, OnboardingStatus: model.OnboardingStatusInProgress}
	fake := &onboardingServiceFake{tenant: tenant}
	handler := NewOnboardingHandler(fake)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+onboardingHandlerTenantID+"/onboarding", strings.NewReader(`{"current_step":"business_profile"}`))

	handler.SaveProgress(recorder, request, onboardingHandlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.saveInput.CurrentStep != "business_profile" {
		t.Fatalf("CurrentStep = %q, want business_profile", fake.saveInput.CurrentStep)
	}
	if fake.saveTenantID != onboardingHandlerTenantID {
		t.Fatalf("tenantID = %q, want %q", fake.saveTenantID, onboardingHandlerTenantID)
	}
}

func TestOnboardingHandlerSaveProgressRejectsMalformedJSON(t *testing.T) {
	handler := NewOnboardingHandler(&onboardingServiceFake{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+onboardingHandlerTenantID+"/onboarding", strings.NewReader(`not json`))

	handler.SaveProgress(recorder, request, onboardingHandlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestOnboardingHandlerSaveProgressPropagatesInvalidStep(t *testing.T) {
	handler := NewOnboardingHandler(&onboardingServiceFake{err: apperrors.New(apperrors.CodeValidationFailed, "invalid onboarding step", nil)})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+onboardingHandlerTenantID+"/onboarding", strings.NewReader(`{"current_step":"rooms"}`))

	handler.SaveProgress(recorder, request, onboardingHandlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 VALIDATION_FAILED, body=%s", recorder.Code, recorder.Body.String())
	}
}

// The decode target has a field only for current_step — a client attempting
// to smuggle business_type/onboarding_status/status/owner/role has them
// silently discarded regardless of decoder strictness.
func TestOnboardingHandlerSaveProgressIgnoresProtectedFields(t *testing.T) {
	tenant := &model.Tenant{ID: onboardingHandlerTenantID, Name: "Hotel Co", Slug: "hotel-co", Status: model.StatusActive}
	fake := &onboardingServiceFake{tenant: tenant}
	handler := NewOnboardingHandler(fake)
	recorder := httptest.NewRecorder()
	body := `{"current_step":"business_profile","business_type":"RESTAURANT","onboarding_status":"COMPLETED","status":"DISABLED","owner_id":"attacker","role":"SUPER_ADMIN"}`
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+onboardingHandlerTenantID+"/onboarding", strings.NewReader(body))

	handler.SaveProgress(recorder, request, onboardingHandlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.saveInput.CurrentStep != "business_profile" {
		t.Fatalf("CurrentStep = %q, want business_profile (only the recognized field should decode)", fake.saveInput.CurrentStep)
	}
}

func TestOnboardingHandlerCompleteReturns200OnSuccess(t *testing.T) {
	tenant := &model.Tenant{ID: onboardingHandlerTenantID, Name: "Hotel Co", Slug: "hotel-co", Status: model.StatusActive, OnboardingStatus: model.OnboardingStatusCompleted}
	fake := &onboardingServiceFake{tenant: tenant}
	handler := NewOnboardingHandler(fake)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+onboardingHandlerTenantID+"/onboarding/complete", nil)

	handler.Complete(recorder, request, onboardingHandlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var body PublicTenant
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.OnboardingStatus != model.OnboardingStatusCompleted {
		t.Fatalf("OnboardingStatus = %q, want COMPLETED", body.OnboardingStatus)
	}
	if fake.completeTenantID != onboardingHandlerTenantID {
		t.Fatalf("tenantID = %q, want %q", fake.completeTenantID, onboardingHandlerTenantID)
	}
}

// This is the propagation proof for the mandatory completion-prerequisite
// check: the handler must surface the service's denial as-is, not swallow
// or reinterpret it as success.
func TestOnboardingHandlerCompletePropagatesPrerequisiteDenial(t *testing.T) {
	handler := NewOnboardingHandler(&onboardingServiceFake{err: apperrors.New(apperrors.CodeValidationFailed, "onboarding cannot be completed before any progress has been saved", nil)})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+onboardingHandlerTenantID+"/onboarding/complete", nil)

	handler.Complete(recorder, request, onboardingHandlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400 VALIDATION_FAILED", recorder.Code, recorder.Body.String())
	}
	assertOnboardingHandlerBodyCode(t, recorder, "VALIDATION_FAILED")
}

func TestOnboardingHandlerCompletePropagatesTenantNotFound(t *testing.T) {
	handler := NewOnboardingHandler(&onboardingServiceFake{err: apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+onboardingHandlerTenantID+"/onboarding/complete", nil)

	handler.Complete(recorder, request, onboardingHandlerTenantID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 TENANT_NOT_FOUND", recorder.Code)
	}
}

func assertOnboardingHandlerBodyCode(t *testing.T, recorder *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid error JSON: %v, body=%s", err, recorder.Body.String())
	}
	if body.Error.Code != expected {
		t.Fatalf("error code = %q, want %q", body.Error.Code, expected)
	}
}

type onboardingServiceFake struct {
	tenant *model.Tenant
	err    error

	saveTenantID     string
	saveInput        service.SaveOnboardingProgressInput
	completeTenantID string
}

func (f *onboardingServiceFake) SaveProgress(_ context.Context, tenantID string, input service.SaveOnboardingProgressInput) (*model.Tenant, error) {
	f.saveTenantID = tenantID
	f.saveInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.tenant, nil
}

func (f *onboardingServiceFake) Complete(_ context.Context, tenantID string) (*model.Tenant, error) {
	f.completeTenantID = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return f.tenant, nil
}
