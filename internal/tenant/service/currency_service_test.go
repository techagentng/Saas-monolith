package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
)

const currencyTenantID = "550e8400-e29b-41d4-a716-446655443001"

// fakeCurrencyRepository satisfies repository.CurrencyRepository and records
// whether a write actually happened, which is how the idempotency and
// write-once tests below prove the service refused to touch persistence.
type fakeCurrencyRepository struct {
	tenant     *model.Tenant
	findCalls  int
	writeCalls int
	findErr    error
}

func (r *fakeCurrencyRepository) FindByID(_ context.Context, id string) (*model.Tenant, error) {
	r.findCalls++
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.tenant == nil || r.tenant.ID != id {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	return r.tenant, nil
}

func (r *fakeCurrencyRepository) SetCurrency(_ context.Context, tenantID string, currency string) (*model.Tenant, error) {
	r.writeCalls++
	if r.tenant == nil || r.tenant.ID != tenantID {
		return nil, apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)
	}
	r.tenant.Currency = &currency
	r.tenant.UpdatedAt = time.Now().UTC()
	return r.tenant, nil
}

func legacyTenant() *fakeCurrencyRepository {
	// A tenant created before Scheduling S1: currency is NULL and no value was
	// inferred for it by the migration.
	return &fakeCurrencyRepository{tenant: &model.Tenant{
		ID: currencyTenantID, Name: "Acme Nails", Slug: "acme-nails", Status: model.StatusActive,
	}}
}

func tenantWithStoredCurrency(code string) *fakeCurrencyRepository {
	currency := code
	return &fakeCurrencyRepository{tenant: &model.Tenant{
		ID: currencyTenantID, Name: "Acme Nails", Slug: "acme-nails", Status: model.StatusActive, Currency: &currency,
	}}
}

func assertCurrencyErrorCode(t *testing.T, err error, want apperrors.ErrorCode, context string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != want {
		t.Fatalf("%s: error = %v, want %s", context, err, want)
	}
}

func TestSetCurrencyOnALegacyTenantWithNullCurrency(t *testing.T) {
	tenants := legacyTenant()
	currencies := NewCurrencyService(tenants)

	updated, err := currencies.Set(context.Background(), currencyTenantID, "NGN")
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if updated.Currency == nil || *updated.Currency != "NGN" {
		t.Fatalf("Currency = %v, want NGN", updated.Currency)
	}
	if tenants.writeCalls != 1 {
		t.Fatalf("write calls = %d, want 1", tenants.writeCalls)
	}
}

func TestSetCurrencyIsIdempotentForTheSameValue(t *testing.T) {
	tenants := tenantWithStoredCurrency("NGN")
	currencies := NewCurrencyService(tenants)

	updated, err := currencies.Set(context.Background(), currencyTenantID, "NGN")
	if err != nil {
		t.Fatalf("Set() error = %v, want idempotent success", err)
	}
	if updated.Currency == nil || *updated.Currency != "NGN" {
		t.Fatalf("Currency = %v, want NGN", updated.Currency)
	}
	if tenants.writeCalls != 0 {
		t.Fatal("Set() re-persisted an unchanged currency — a retried request must not bump updated_at")
	}
}

func TestSetCurrencyRefusesToChangeAnAlreadySetCurrency(t *testing.T) {
	// Changing it would silently reinterpret every stored amount: 1999 minor
	// units meaning NGN 19.99 one day and USD 19.99 the next, with no
	// conversion in this system to make that transition correct.
	tenants := tenantWithStoredCurrency("NGN")
	currencies := NewCurrencyService(tenants)

	_, err := currencies.Set(context.Background(), currencyTenantID, "USD")
	assertCurrencyErrorCode(t, err, apperrors.CodeValidationFailed, "Set(different currency)")
	if tenants.writeCalls != 0 {
		t.Fatal("Set() wrote a changed currency")
	}
	if *tenants.tenant.Currency != "NGN" {
		t.Fatalf("stored currency = %q, want it unchanged at NGN", *tenants.tenant.Currency)
	}
}

func TestSetCurrencyRejectsUnsupportedCodesBeforeLoadingTheTenant(t *testing.T) {
	for _, candidate := range []string{"", "XXX", "JPY", "US", "USDD"} {
		tenants := legacyTenant()
		currencies := NewCurrencyService(tenants)

		_, err := currencies.Set(context.Background(), currencyTenantID, candidate)
		assertCurrencyErrorCode(t, err, apperrors.CodeValidationFailed, "Set("+candidate+")")
		if tenants.findCalls != 0 {
			t.Fatalf("Set(%q) loaded the tenant before validating the code", candidate)
		}
		if tenants.writeCalls != 0 {
			t.Fatalf("Set(%q) wrote an unsupported currency", candidate)
		}
	}
}

func TestSetCurrencyRejectsRatherThanNormalizesCase(t *testing.T) {
	// Reject-over-normalize, matching ValidateSlug and ValidateBusinessType.
	for _, candidate := range []string{"ngn", "Ngn", " NGN", "NGN "} {
		tenants := legacyTenant()
		currencies := NewCurrencyService(tenants)

		_, err := currencies.Set(context.Background(), currencyTenantID, candidate)
		assertCurrencyErrorCode(t, err, apperrors.CodeValidationFailed, "Set("+candidate+")")
		if tenants.writeCalls != 0 {
			t.Fatalf("Set(%q) normalized a non-canonical code instead of rejecting it", candidate)
		}
	}
}

func TestSetCurrencyRejectsMalformedTenantID(t *testing.T) {
	tenants := legacyTenant()
	currencies := NewCurrencyService(tenants)

	_, err := currencies.Set(context.Background(), "not-a-uuid", "NGN")
	assertCurrencyErrorCode(t, err, apperrors.CodeInvalidRequest, "Set(malformed tenant id)")
	if tenants.writeCalls != 0 {
		t.Fatal("Set() wrote against a malformed tenant id")
	}
}

func TestSetCurrencyPropagatesTenantNotFound(t *testing.T) {
	tenants := &fakeCurrencyRepository{}
	currencies := NewCurrencyService(tenants)

	_, err := currencies.Set(context.Background(), currencyTenantID, "NGN")
	assertCurrencyErrorCode(t, err, apperrors.CodeTenantNotFound, "Set(unknown tenant)")
	if tenants.writeCalls != 0 {
		t.Fatal("Set() wrote against a nonexistent tenant")
	}
}

// The profile endpoint must not be a second, unguarded route to the currency
// column. UpdateTenantProfileRequest is the decode target for
// PATCH /api/v1/tenants/{tenantID}, and the protection is structural: there is
// no field for a client to populate.
func TestUpdateTenantProfileRequestHasNoCurrencyField(t *testing.T) {
	var request UpdateTenantProfileRequest
	requestType := reflect.TypeOf(request)

	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		if field.Name == "Currency" || field.Tag.Get("json") == "currency" {
			t.Fatalf("UpdateTenantProfileRequest exposes %q — currency is write-once and must only be reachable through CurrencyService", field.Name)
		}
	}

	// Guard the intended shape too, so a future field addition is a deliberate
	// decision rather than an accident.
	want := map[string]bool{"name": true, "description": true, "contact_email": true, "contact_phone": true, "timezone": true}
	if requestType.NumField() != len(want) {
		t.Fatalf("UpdateTenantProfileRequest has %d fields, want %d — adding one makes it client-writable", requestType.NumField(), len(want))
	}
	for i := 0; i < requestType.NumField(); i++ {
		tag := requestType.Field(i).Tag.Get("json")
		if !want[tag] {
			t.Fatalf("UpdateTenantProfileRequest carries an unexpected writable field %q", tag)
		}
	}
}
