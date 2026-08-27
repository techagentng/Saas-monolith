package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/service"
)

const currencyHandlerTenantID = "550e8400-e29b-41d4-a716-446655444001"

type fakeCurrencyService struct {
	tenantID string
	code     string
	result   *model.Tenant
	err      error
}

func (f *fakeCurrencyService) Set(_ context.Context, tenantID string, code string) (*model.Tenant, error) {
	f.tenantID, f.code = tenantID, code
	return f.result, f.err
}

func tenantWithCurrency(code string) *model.Tenant {
	currency := code
	businessType := model.BusinessTypeNailTechnician
	return &model.Tenant{
		ID: currencyHandlerTenantID, Name: "Acme Nails", Slug: "acme-nails", Status: model.StatusActive,
		BusinessType: &businessType, OnboardingStatus: model.OnboardingStatusCompleted, Currency: &currency,
	}
}

func assertCurrencyErrorEnvelope(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
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
}

func TestCurrencySetRejectsMalformedJSON(t *testing.T) {
	handler := NewCurrencyHandler(&fakeCurrencyService{})
	recorder := httptest.NewRecorder()

	handler.Set(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader("{nope")), currencyHandlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertCurrencyErrorEnvelope(t, recorder, "INVALID_REQUEST")
}

func TestCurrencySetPassesTheCodeThroughAndReturnsTheTenant(t *testing.T) {
	currencies := &fakeCurrencyService{result: tenantWithCurrency("NGN")}
	handler := NewCurrencyHandler(currencies)
	recorder := httptest.NewRecorder()

	handler.Set(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"currency":"NGN"}`)), currencyHandlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if currencies.tenantID != currencyHandlerTenantID {
		t.Fatalf("tenant = %q, want the route's %q", currencies.tenantID, currencyHandlerTenantID)
	}
	if currencies.code != "NGN" {
		t.Fatalf("code = %q, want NGN", currencies.code)
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["currency"] != "NGN" {
		t.Fatalf("currency = %v, want NGN in the response", body["currency"])
	}
}

// The decode target has exactly one field, so nothing else a client sends can
// land — the same structural protection every other write endpoint here uses.
func TestCurrencySetIgnoresEverythingButTheCurrencyField(t *testing.T) {
	currencies := &fakeCurrencyService{result: tenantWithCurrency("NGN")}
	handler := NewCurrencyHandler(currencies)
	recorder := httptest.NewRecorder()

	body := `{
		"currency":"NGN",
		"id":"11111111-1111-1111-1111-111111111111",
		"tenant_id":"22222222-2222-2222-2222-222222222222",
		"status":"DISABLED",
		"name":"Hijacked",
		"slug":"hijacked",
		"business_type":"HOTEL",
		"onboarding_status":"IN_PROGRESS"
	}`
	handler.Set(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), currencyHandlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if currencies.tenantID != currencyHandlerTenantID {
		t.Fatalf("tenant = %q, want the route's %q — a body tenant_id must never be honored", currencies.tenantID, currencyHandlerTenantID)
	}
	if currencies.code != "NGN" {
		t.Fatalf("code = %q, want NGN", currencies.code)
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if response["name"] != "Acme Nails" || response["slug"] != "acme-nails" {
		t.Fatalf("a smuggled field changed the tenant: name=%v slug=%v", response["name"], response["slug"])
	}
	if response["status"] != string(model.StatusActive) {
		t.Fatalf("status = %v, want it untouched", response["status"])
	}
}

func TestCurrencySetSurfacesTheWriteOnceRefusal(t *testing.T) {
	currencies := &fakeCurrencyService{err: apperrors.New(apperrors.CodeValidationFailed, "tenant currency cannot be changed once set", nil)}
	handler := NewCurrencyHandler(currencies)
	recorder := httptest.NewRecorder()

	handler.Set(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"currency":"USD"}`)), currencyHandlerTenantID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertCurrencyErrorEnvelope(t, recorder, "VALIDATION_FAILED")
}

func TestCurrencySetNeverLeaksAnInternalError(t *testing.T) {
	currencies := &fakeCurrencyService{err: errors.New("pq: connection reset by peer")}
	handler := NewCurrencyHandler(currencies)
	recorder := httptest.NewRecorder()

	handler.Set(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"currency":"NGN"}`)), currencyHandlerTenantID)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	assertCurrencyErrorEnvelope(t, recorder, "INTERNAL_ERROR")
	if strings.Contains(recorder.Body.String(), "connection reset") {
		t.Fatalf("the raw driver error leaked: %s", recorder.Body.String())
	}
}

// The owner-facing tenant DTO must expose currency so a client can format
// prices, while the anonymous DTO (PublicTenantIdentity) deliberately does not.
func TestPublicTenantDTOCarriesCurrency(t *testing.T) {
	tenant := tenantWithCurrency("NGN")
	dto := toPublicTenant(tenant)

	if dto.Currency == nil || *dto.Currency != "NGN" {
		t.Fatalf("PublicTenant.Currency = %v, want NGN", dto.Currency)
	}

	encoded, err := json.Marshal(PublicTenantIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "currency") {
		t.Fatalf("the anonymous PublicTenantIdentity exposes currency: %s", encoded)
	}
}

// compile-time guard: the fake must keep satisfying the real interface.
var _ service.CurrencyService = (*fakeCurrencyService)(nil)
