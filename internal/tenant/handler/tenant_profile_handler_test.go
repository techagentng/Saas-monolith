package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

const profileRouteTenantID = "550e8400-e29b-41d4-a716-446655440950"

func profilePatchRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+profileRouteTenantID, strings.NewReader(body))
}

func stringPointer(value string) *string { return &value }

func TestUpdateProfileHandlerRejectsMalformedJSON(t *testing.T) {
	fake := &tenantServiceFake{}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(`not json`), profileRouteTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if fake.updateCalls != 0 {
		t.Fatalf("service called %d times for malformed JSON", fake.updateCalls)
	}
}

// The handler forwards an empty patch; the service owns the empty-patch rule,
// and the handler must map its VALIDATION_FAILED to 400.
func TestUpdateProfileHandlerRejectsEmptyPatch(t *testing.T) {
	fake := &tenantServiceFake{updateErr: apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(`{}`), profileRouteTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestUpdateProfileHandlerReturns200WithUpdatedTenant(t *testing.T) {
	now := time.Now().UTC()
	fake := &tenantServiceFake{updateTenant: &model.Tenant{
		ID: profileRouteTenantID, Name: "Acme Beauty Studio", Slug: "acme-salon", Status: model.StatusActive,
		Description: stringPointer("Best salon"), ContactEmail: stringPointer("hi@acme.test"),
		ContactPhone: stringPointer("+2348012345678"), Timezone: stringPointer("Africa/Lagos"),
		CreatedAt: now, UpdatedAt: now,
	}}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(`{"name":"Acme Beauty Studio"}`), profileRouteTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["name"] != "Acme Beauty Studio" {
		t.Fatalf("name = %v", body["name"])
	}
	if body["slug"] != "acme-salon" {
		t.Fatalf("slug = %v, want unchanged", body["slug"])
	}
	if body["description"] != "Best salon" || body["timezone"] != "Africa/Lagos" {
		t.Fatalf("profile fields missing from response: %s", recorder.Body.String())
	}
}

// The tenant target comes from the route argument, never the request body.
func TestUpdateProfileHandlerUsesRouteTenantIDNotBody(t *testing.T) {
	fake := &tenantServiceFake{updateTenant: &model.Tenant{ID: profileRouteTenantID, Name: "Acme", Slug: "acme"}}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(`{"name":"Acme","id":"attacker-id","tenant_id":"attacker-id"}`), profileRouteTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if fake.updateTenantID != profileRouteTenantID {
		t.Fatalf("service received tenant %q, want route value %q", fake.updateTenantID, profileRouteTenantID)
	}
}

// Protected fields are absent from the DTO, so a lenient decoder cannot bind
// them; only the approved profile fields reach the service.
func TestUpdateProfileHandlerIgnoresProtectedBodyFields(t *testing.T) {
	fake := &tenantServiceFake{updateTenant: &model.Tenant{ID: profileRouteTenantID, Name: "New Name", Slug: "acme-salon", Status: model.StatusActive}}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(
		`{"name":"New Name","slug":"attacker-slug","status":"DISABLED","id":"attacker-id","created_at":"2020-01-01T00:00:00Z"}`,
	), profileRouteTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if fake.updateInput.Name == nil || *fake.updateInput.Name != "New Name" {
		t.Fatalf("Name = %v, want forwarded", fake.updateInput.Name)
	}
	if fake.updateInput.Description != nil || fake.updateInput.ContactEmail != nil ||
		fake.updateInput.ContactPhone != nil || fake.updateInput.Timezone != nil {
		t.Fatalf("unexpected fields bound from protected keys: %#v", fake.updateInput)
	}
}

func TestUpdateProfileHandlerMapsServiceValidationError(t *testing.T) {
	fake := &tenantServiceFake{updateErr: apperrors.New(apperrors.CodeValidationFailed, "invalid timezone identifier", nil)}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(`{"timezone":"Mars/Olympus"}`), profileRouteTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), string(apperrors.CodeValidationFailed)) {
		t.Fatalf("body = %s, want structured VALIDATION_FAILED", recorder.Body.String())
	}
}

func TestUpdateProfileHandlerMapsPermissionDenied(t *testing.T) {
	fake := &tenantServiceFake{updateErr: apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(`{"name":"Acme"}`), profileRouteTenantID)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

// An unexpected system failure must not leak internals to the caller.
func TestUpdateProfileHandlerMapsSystemErrorSafely(t *testing.T) {
	fake := &tenantServiceFake{updateErr: errors.New("pq: connection reset by peer")}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(`{"name":"Acme"}`), profileRouteTenantID)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "connection reset") {
		t.Fatalf("internal error detail leaked: %s", recorder.Body.String())
	}
}

// Unset optional profile fields serialize as explicit null, keeping the
// tenant JSON shape stable for frontend consumers.
func TestUpdateProfileHandlerSerializesUnsetProfileFieldsAsNull(t *testing.T) {
	fake := &tenantServiceFake{updateTenant: &model.Tenant{
		ID: profileRouteTenantID, Name: "Acme", Slug: "acme", Status: model.StatusActive,
	}}
	handler := NewTenantHandler(fake, &fakeRetrievalService{})
	recorder := httptest.NewRecorder()

	handler.UpdateProfile(recorder, profilePatchRequest(`{"name":"Acme"}`), profileRouteTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, field := range []string{"description", "contact_email", "contact_phone", "timezone"} {
		raw, present := body[field]
		if !present {
			t.Fatalf("field %q absent; optional profile fields must stay in the response shape", field)
		}
		if string(raw) != "null" {
			t.Fatalf("field %q = %s, want null", field, raw)
		}
	}
}
