package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
)

type fakeBookingReceiptService struct {
	slug, reference, token string
	pdfBytes               []byte
	err                    error
	calls                  int
}

func (f *fakeBookingReceiptService) GetReceipt(_ context.Context, slug, reference, token string) ([]byte, error) {
	f.calls++
	f.slug, f.reference, f.token = slug, reference, token
	if f.err != nil {
		return nil, f.err
	}
	return f.pdfBytes, nil
}

func TestPublicBookingReceiptGetStreamsThePDFWithSafeHeaders(t *testing.T) {
	fake := &fakeBookingReceiptService{pdfBytes: []byte("%PDF-1.4 fake receipt bytes")}
	handler := NewPublicBookingReceiptHandler(fake)
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/glamour-nails/bookings/NB-1A2B3C4D/receipt?token=abc123", nil)
	handler.Get(recorder, request, "glamour-nails", "NB-1A2B3C4D")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("Content-Type = %q, want application/pdf", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="booking-NB-1A2B3C4D.pdf"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	if recorder.Body.String() != "%PDF-1.4 fake receipt bytes" {
		t.Fatalf("body = %q, want the generator's exact bytes streamed through unmodified", recorder.Body.String())
	}

	if fake.slug != "glamour-nails" || fake.reference != "NB-1A2B3C4D" || fake.token != "abc123" {
		t.Fatalf("passed through = slug=%q reference=%q token=%q", fake.slug, fake.reference, fake.token)
	}
}

func TestPublicBookingReceiptGetRejectsAMissingTokenBeforeReachingTheService(t *testing.T) {
	fake := &fakeBookingReceiptService{pdfBytes: []byte("%PDF")}
	handler := NewPublicBookingReceiptHandler(fake)
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/glamour-nails/bookings/NB-1A2B3C4D/receipt", nil)
	handler.Get(recorder, request, "glamour-nails", "NB-1A2B3C4D")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "VALIDATION_FAILED")
	if fake.calls != 0 {
		t.Fatal("a request with no token reached the service")
	}
}

func TestPublicBookingReceiptGetMapsAWrongTokenToNotFound(t *testing.T) {
	fake := &fakeBookingReceiptService{err: apperrors.New(apperrors.CodeBookingNotFound, "booking not found", nil)}
	handler := NewPublicBookingReceiptHandler(fake)
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/glamour-nails/bookings/NB-1A2B3C4D/receipt?token=wrong", nil)
	handler.Get(recorder, request, "glamour-nails", "NB-1A2B3C4D")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "BOOKING_NOT_FOUND")
	if recorder.Header().Get("Content-Type") == "application/pdf" {
		t.Fatal("an error response must never claim to be a PDF")
	}
}

func TestPublicBookingReceiptGetMapsAnUnknownTenantToNotFound(t *testing.T) {
	fake := &fakeBookingReceiptService{err: apperrors.New(apperrors.CodeTenantNotFound, "tenant not found", nil)}
	handler := NewPublicBookingReceiptHandler(fake)
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/unknown-tenant/bookings/NB-1A2B3C4D/receipt?token=abc", nil)
	handler.Get(recorder, request, "unknown-tenant", "NB-1A2B3C4D")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertErrorCode(t, recorder, "TENANT_NOT_FOUND")
}

func TestPublicBookingReceiptGetMapsAGeneratorFailureToInternalError(t *testing.T) {
	fake := &fakeBookingReceiptService{err: apperrors.New(apperrors.CodeInternalError, "could not generate receipt", nil)}
	handler := NewPublicBookingReceiptHandler(fake)
	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/tenants/glamour-nails/bookings/NB-1A2B3C4D/receipt?token=abc", nil)
	handler.Get(recorder, request, "glamour-nails", "NB-1A2B3C4D")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	assertErrorCode(t, recorder, "INTERNAL_ERROR")
}
