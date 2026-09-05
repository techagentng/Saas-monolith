package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

type fakeBookingService struct {
	slug   string
	input  service.CreateBookingInput
	result *service.BookedAppointment
	err    error
}

func (f *fakeBookingService) CreatePublicBooking(_ context.Context, slug string, input service.CreateBookingInput) (*service.BookedAppointment, error) {
	f.slug, f.input = slug, input
	return f.result, f.err
}

func okBooking() *service.BookedAppointment {
	return &service.BookedAppointment{
		ID: "b1c2d3e4-0000-0000-0000-000000000000", Reference: "NB-B1C2D3E4",
		Status:    model.BookingConfirmed,
		ServiceID: "svc-1", ServiceName: "Gel Manicure",
		StaffID: "stf-1", StaffName: "Ada",
		Date: "2026-09-07", Start: "09:30", End: "10:00", Timezone: "Africa/Lagos",
	}
}

func TestPublicBookingCreateShapesTheResponseAndPassesIdentifiersThrough(t *testing.T) {
	fake := &fakeBookingService{result: okBooking()}
	handler := NewPublicBookingHandler(fake)
	recorder := httptest.NewRecorder()

	body := `{"service_id":"svc-1","staff_id":"stf-1","date":"2026-09-07","start":"09:30",
		"customer":{"name":"Jane Doe","phone":"+2348001112222","email":"jane@example.com"},
		"end":"11:59","duration_minutes":999,"price_minor":1,"tenant_id":"HACK"}`
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/public/tenants/glamour-nails/bookings", strings.NewReader(body)), "glamour-nails")

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", recorder.Code, recorder.Body.String())
	}
	// Identifiers + selected start reached the service; the smuggled
	// end/duration/price/tenant_id have nowhere to land in CreateBookingInput.
	if fake.slug != "glamour-nails" || fake.input.ServiceID != "svc-1" || fake.input.StaffID != "stf-1" ||
		fake.input.Date != "2026-09-07" || fake.input.Start != "09:30" || fake.input.Customer.Name != "Jane Doe" {
		t.Fatalf("passed through = %+v", fake.input)
	}

	body2 := decodeBody(t, recorder)
	booking, ok := body2["booking"].(map[string]any)
	if !ok {
		t.Fatalf("response has no booking object: %s", recorder.Body.String())
	}
	for _, key := range []string{"id", "reference", "status", "service", "staff", "date", "start", "end", "timezone"} {
		if _, present := booking[key]; !present {
			t.Fatalf("booking missing %q: %s", key, recorder.Body.String())
		}
	}
	if booking["status"] != "CONFIRMED" || booking["end"] != "10:00" {
		t.Fatalf("booking fields wrong: %v", booking)
	}
	// No internal fields, and no customer PII echoed back.
	for _, forbidden := range []string{"tenant_id", "customer", "created_at", "updated_at", "start_at", "end_at", "Jane Doe", "jane@example.com"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestPublicBookingCreateRejectsMalformedJSON(t *testing.T) {
	handler := NewPublicBookingHandler(&fakeBookingService{})
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{not json")), "glamour-nails")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestPublicBookingCreateMapsServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		code apperrors.ErrorCode
		want int
		body string
	}{
		{apperrors.CodeTenantNotFound, http.StatusNotFound, "TENANT_NOT_FOUND"},
		{apperrors.CodeResourceNotFound, http.StatusNotFound, "RESOURCE_NOT_FOUND"},
		{apperrors.CodeServiceNotFound, http.StatusNotFound, "SERVICE_NOT_FOUND"},
		{apperrors.CodeStaffNotFound, http.StatusNotFound, "STAFF_NOT_FOUND"},
		{apperrors.CodeValidationFailed, http.StatusBadRequest, "VALIDATION_FAILED"},
		{apperrors.CodeBookingSlotUnavailable, http.StatusConflict, "BOOKING_SLOT_UNAVAILABLE"},
	} {
		t.Run(string(tc.code), func(t *testing.T) {
			fake := &fakeBookingService{err: apperrors.New(tc.code, "boom", nil)}
			handler := NewPublicBookingHandler(fake)
			recorder := httptest.NewRecorder()

			handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)), "glamour-nails")

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			assertErrorCode(t, recorder, tc.body)
		})
	}
}

// The 409 body carries the friendly slot-taken message and nothing else — no
// other customer's data, no constraint text.
func TestPublicBookingConflictBodyIsSafe(t *testing.T) {
	fake := &fakeBookingService{err: apperrors.New(apperrors.CodeBookingSlotUnavailable, "internal detail", nil)}
	handler := NewPublicBookingHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)), "glamour-nails")

	body := recorder.Body.String()
	if !strings.Contains(body, "no longer available") {
		t.Fatalf("409 body = %s, want the friendly slot-taken message", body)
	}
	for _, leak := range []string{"internal detail", "bookings_no_overlap", "23P01", "constraint"} {
		if strings.Contains(body, leak) {
			t.Fatalf("409 body leaked %q: %s", leak, body)
		}
	}
}
