package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/availability"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// fakeAvailabilityService records exactly what the handler passed through and
// returns a canned result or error.
type fakeAvailabilityService struct {
	tenantID  string
	serviceID string
	staffID   string
	date      string

	result *service.AvailabilityResult
	err    error
}

func (f *fakeAvailabilityService) GetAvailability(_ context.Context, tenantID, serviceID, staffID, date string) (*service.AvailabilityResult, error) {
	f.tenantID, f.serviceID, f.staffID, f.date = tenantID, serviceID, staffID, date
	return f.result, f.err
}

const (
	availHandlerServiceID = "550e8400-e29b-41d4-a716-446655443001"
	availHandlerStaffID   = "550e8400-e29b-41d4-a716-446655443002"
)

func availabilityRequest(query string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+handlerTenantID+"/availability?"+query, nil)
}

func fullQuery() string {
	return "service_id=" + availHandlerServiceID + "&staff_id=" + availHandlerStaffID + "&date=2026-09-07"
}

func TestAvailabilityGetPassesTheQueryThroughAndShapesTheResponse(t *testing.T) {
	fake := &fakeAvailabilityService{result: &service.AvailabilityResult{
		Date: "2026-09-07", Timezone: "Africa/Lagos",
		ServiceID: availHandlerServiceID, StaffID: availHandlerStaffID,
		Slots: []availability.Slot{{Start: "09:00", End: "09:30"}, {Start: "09:30", End: "10:00"}},
	}}
	handler := NewAvailabilityHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, availabilityRequest(fullQuery()), handlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.tenantID != handlerTenantID || fake.serviceID != availHandlerServiceID || fake.staffID != availHandlerStaffID || fake.date != "2026-09-07" {
		t.Fatalf("passed through = %q/%q/%q/%q", fake.tenantID, fake.serviceID, fake.staffID, fake.date)
	}

	body := decodeBody(t, recorder)
	if body["date"] != "2026-09-07" || body["timezone"] != "Africa/Lagos" {
		t.Fatalf("response context wrong: %s", recorder.Body.String())
	}
	slots, ok := body["slots"].([]any)
	if !ok || len(slots) != 2 {
		t.Fatalf("slots = %v, want 2 entries", body["slots"])
	}
	first := slots[0].(map[string]any)
	if first["start"] != "09:00" || first["end"] != "09:30" {
		t.Fatalf("first slot = %v", first)
	}
	for _, forbidden := range []string{"occupied", "instant", "offset", "now"} {
		if _, present := first[forbidden]; present {
			t.Fatalf("slot leaked internal field %q", forbidden)
		}
	}
}

func TestAvailabilityGetSerializesAnEmptyResultAsEmptyArray(t *testing.T) {
	fake := &fakeAvailabilityService{result: &service.AvailabilityResult{
		Date: "2026-09-07", Timezone: "Africa/Lagos",
		ServiceID: availHandlerServiceID, StaffID: availHandlerStaffID,
		Slots: nil,
	}}
	handler := NewAvailabilityHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, availabilityRequest(fullQuery()), handlerTenantID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"slots":[]`) {
		t.Fatalf("body = %s, want slots serialized as []", recorder.Body.String())
	}
}

func TestAvailabilityGetRequiresEveryQueryParameter(t *testing.T) {
	cases := map[string]string{
		"missing service_id": "staff_id=" + availHandlerStaffID + "&date=2026-09-07",
		"missing staff_id":   "service_id=" + availHandlerServiceID + "&date=2026-09-07",
		"missing date":       "service_id=" + availHandlerServiceID + "&staff_id=" + availHandlerStaffID,
		"empty":              "",
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeAvailabilityService{}
			handler := NewAvailabilityHandler(fake)
			recorder := httptest.NewRecorder()

			handler.Get(recorder, availabilityRequest(query), handlerTenantID)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
			}
			assertErrorCode(t, recorder, "VALIDATION_FAILED")
			if fake.tenantID != "" {
				t.Fatal("an incomplete query still reached the service")
			}
		})
	}
}

func TestAvailabilityGetMapsAServiceLayerErrorThroughTheSanitizer(t *testing.T) {
	for _, tc := range []struct {
		code apperrors.ErrorCode
		want int
		body string
	}{
		{apperrors.CodeValidationFailed, http.StatusBadRequest, "VALIDATION_FAILED"},
		{apperrors.CodeServiceNotFound, http.StatusNotFound, "SERVICE_NOT_FOUND"},
		{apperrors.CodeStaffNotFound, http.StatusNotFound, "STAFF_NOT_FOUND"},
		{apperrors.CodeInvalidRequest, http.StatusBadRequest, "INVALID_REQUEST"},
		{apperrors.CodeInternalError, http.StatusInternalServerError, "INTERNAL_ERROR"},
	} {
		t.Run(string(tc.code), func(t *testing.T) {
			fake := &fakeAvailabilityService{err: apperrors.New(tc.code, "boom", nil)}
			handler := NewAvailabilityHandler(fake)
			recorder := httptest.NewRecorder()

			handler.Get(recorder, availabilityRequest(fullQuery()), handlerTenantID)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			assertErrorCode(t, recorder, tc.body)
		})
	}
}
