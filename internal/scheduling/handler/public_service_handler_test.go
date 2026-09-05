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

// SC1: the public DTO exposes category (the name) and never category_id.
func TestPublicServiceListExposesCategoryNameNeverCategoryID(t *testing.T) {
	category := "Pedicures"
	fake := &fakePublicCatalogService{result: &service.PublicCatalog{
		Currency: strp("NGN"),
		Services: []service.PublicServiceView{
			{ID: "s1", Name: "Spa Pedicure", DurationMinutes: 60, PriceMinor: 2999, Category: &category},
			{ID: "s2", Name: "Uncategorised Add-On", DurationMinutes: 15, PriceMinor: 999, Category: nil},
		},
	}}
	handler := NewPublicServiceHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "glamour-nails")

	body := decodeBody(t, recorder)
	services := body["services"].([]any)
	categorised := services[0].(map[string]any)
	if categorised["category"] != "Pedicures" {
		t.Fatalf("category = %v, want %q", categorised["category"], "Pedicures")
	}
	if _, present := categorised["category_id"]; present {
		t.Fatal("public catalog item exposed category_id — an internal identifier that must never reach an anonymous customer")
	}

	uncategorised := services[1].(map[string]any)
	if uncategorised["category"] != nil {
		t.Fatalf("category = %v, want null for an uncategorised service", uncategorised["category"])
	}
}

// Service Images: the public catalogue exposes url/alt_text/sort_order/
// is_primary and nothing else — no storage_key, no tenant_id, no service_id.
// An existing service with no images returns images: [].
func TestPublicServiceListExposesImagesWithNoStorageKey(t *testing.T) {
	alt := "Finished gel set"
	fake := &fakePublicCatalogService{result: &service.PublicCatalog{
		Currency: strp("NGN"),
		Services: []service.PublicServiceView{
			{
				ID: "s1", Name: "Gel Manicure", DurationMinutes: 45, PriceMinor: 1999,
				Images: []service.PublicServiceImageView{
					{ID: "img1", URL: "https://cdn.test.local/media/img1.jpg", AltText: &alt, SortOrder: 0, IsPrimary: true},
				},
			},
			{ID: "s2", Name: "No-Photo Service", DurationMinutes: 30, PriceMinor: 999, Images: []service.PublicServiceImageView{}},
		},
	}}
	handler := NewPublicServiceHandler(fake)
	recorder := httptest.NewRecorder()

	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "glamour-nails")

	body := decodeBody(t, recorder)
	services := body["services"].([]any)

	withImage := services[0].(map[string]any)
	images := withImage["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images = %v, want 1", images)
	}
	image := images[0].(map[string]any)
	for _, key := range []string{"id", "url", "alt_text", "sort_order", "is_primary"} {
		if _, ok := image[key]; !ok {
			t.Fatalf("public image missing %q: %s", key, recorder.Body.String())
		}
	}
	for _, forbidden := range []string{"storage_key", "tenant_id", "service_id", "created_at", "updated_at"} {
		if _, present := image[forbidden]; present {
			t.Fatalf("public image exposed internal field %q", forbidden)
		}
	}

	// An existing service without images must continue to work.
	withoutImage := services[1].(map[string]any)
	if imgs, ok := withoutImage["images"].([]any); !ok || len(imgs) != 0 {
		t.Fatalf("images for a photo-less service = %v, want []", withoutImage["images"])
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
