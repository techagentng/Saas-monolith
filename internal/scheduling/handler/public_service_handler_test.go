package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

type fakePublicCatalogService struct {
	slug   string
	result *service.PublicCatalog
	err    error
}

func (f *fakePublicCatalogService) GetCatalog(_ context.Context, slug string) (*service.PublicCatalog, error) {
	f.slug = slug
	return f.result, f.err
}

func strp(v string) *string { return &v }

func TestPublicServiceListShapesTheResponse(t *testing.T) {
	desc := "Long-lasting gel finish."
	currency := "NGN"
	fake := &fakePublicCatalogService{result: &service.PublicCatalog{
		Currency: &currency,
		Services: []service.PublicServiceView{
			{ID: "s1", Name: "Gel Manicure", Description: &desc, DurationMinutes: 45, PriceMinor: 1999},
			{ID: "s2", Name: "Pedicure", Description: nil, DurationMinutes: 60, PriceMinor: 2999},
		},
	}}
	handler := NewPublicServiceHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/glamour-nails/services", nil), "glamour-nails")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.slug != "glamour-nails" {
		t.Fatalf("slug passed through = %q", fake.slug)
	}

	body := decodeBody(t, recorder)
	if body["currency"] != "NGN" {
		t.Fatalf("currency = %v, want NGN", body["currency"])
	}
	services, ok := body["services"].([]any)
	if !ok || len(services) != 2 {
		t.Fatalf("services = %v, want 2 entries", body["services"])
	}
	first := services[0].(map[string]any)
	for _, key := range []string{"id", "name", "description", "duration_minutes", "price_minor"} {
		if _, present := first[key]; !present {
			t.Fatalf("service missing %q: %s", key, recorder.Body.String())
		}
	}
	// Owner/admin internals must never reach a customer.
	for _, forbidden := range []string{"tenant_id", "status", "created_at", "updated_at"} {
		if _, present := first[forbidden]; present {
			t.Fatalf("public service exposed %q", forbidden)
		}
	}
}

func TestPublicServiceListSerializesAnEmptyCatalogAsEmptyArray(t *testing.T) {
	fake := &fakePublicCatalogService{result: &service.PublicCatalog{Currency: strp("NGN"), Services: nil}}
	handler := NewPublicServiceHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "glamour-nails")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"services":[]`) {
		t.Fatalf("body = %s, want services serialized as []", recorder.Body.String())
	}
}

func TestPublicServiceListEmitsANullCurrencyWhenUndeclared(t *testing.T) {
	fake := &fakePublicCatalogService{result: &service.PublicCatalog{Currency: nil, Services: []service.PublicServiceView{}}}
	handler := NewPublicServiceHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "glamour-nails")

	if !strings.Contains(recorder.Body.String(), `"currency":null`) {
		t.Fatalf("body = %s, want currency:null", recorder.Body.String())
	}
}

func TestPublicServiceListMapsErrorsThroughTheSanitizer(t *testing.T) {
	for _, tc := range []struct {
		code apperrors.ErrorCode
		want int
		body string
	}{
		{apperrors.CodeTenantNotFound, http.StatusNotFound, "TENANT_NOT_FOUND"},
		{apperrors.CodeResourceNotFound, http.StatusNotFound, "RESOURCE_NOT_FOUND"},
		{apperrors.CodeTenantSlugInvalid, http.StatusBadRequest, "TENANT_SLUG_INVALID"},
	} {
		t.Run(string(tc.code), func(t *testing.T) {
			fake := &fakePublicCatalogService{err: apperrors.New(tc.code, "boom", nil)}
			handler := NewPublicServiceHandler(fake)
			recorder := httptest.NewRecorder()

			handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "glamour-nails")

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			assertErrorCode(t, recorder, tc.body)
		})
	}
}
