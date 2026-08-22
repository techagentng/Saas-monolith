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
	handler := NewTenantHandler(&tenantServiceFake{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(`{"name":"Salon","slug":"salon"}`))

	handler.Create(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestCreateTenantHandlerRejectsMalformedJSON(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{})
	recorder := httptest.NewRecorder()
	request := authenticatedTenantRequest(t, `not json`)

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestCreateTenantHandlerUsesAuthenticatedCreatorNeverRequestBody(t *testing.T) {
	fake := &tenantServiceFake{tenant: &model.Tenant{ID: "tenant-1", Name: "Salon", Slug: "salon", Status: model.StatusActive}}
	handler := NewTenantHandler(fake)
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
	handler := NewTenantHandler(&tenantServiceFake{tenant: tenant})
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

func TestCreateTenantHandlerPropagatesSlugTaken(t *testing.T) {
	handler := NewTenantHandler(&tenantServiceFake{err: apperrors.New(apperrors.CodeTenantSlugTaken, "tenant slug is already taken", nil)})
	recorder := httptest.NewRecorder()
	request := authenticatedTenantRequest(t, `{"name":"Salon","slug":"salon"}`)

	handler.Create(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
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
}

func (f *tenantServiceFake) Create(_ context.Context, input service.CreateTenantInput) (*model.Tenant, error) {
	f.receivedInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.tenant, nil
}
