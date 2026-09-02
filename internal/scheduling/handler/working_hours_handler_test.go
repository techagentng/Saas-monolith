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

// fakeWorkingHoursService records exactly what the handler passed through,
// the same DTO-protection discipline fakeStaffService already applies.
type fakeWorkingHoursService struct {
	tenantID  string
	staffID   string
	intervals []service.IntervalInput

	result []*model.WorkingHourInterval
	err    error
}

func (f *fakeWorkingHoursService) List(_ context.Context, tenantID string, staffID string) ([]*model.WorkingHourInterval, error) {
	f.tenantID, f.staffID = tenantID, staffID
	return f.result, f.err
}

func (f *fakeWorkingHoursService) ReplaceWeeklySchedule(_ context.Context, tenantID string, staffID string, intervals []service.IntervalInput) ([]*model.WorkingHourInterval, error) {
	f.tenantID, f.staffID, f.intervals = tenantID, staffID, intervals
	return f.result, f.err
}

func storedInterval(day model.DayOfWeek, start, end string) *model.WorkingHourInterval {
	return &model.WorkingHourInterval{
		ID: "550e8400-e29b-41d4-a716-446655460001", TenantID: handlerTenantID, StaffID: handlerStaffID,
		DayOfWeek: day, StartTime: start, EndTime: end,
	}
}

// --- GET -----------------------------------------------------------------

func TestWorkingHoursGetReturnsTheScheduleShape(t *testing.T) {
	fake := &fakeWorkingHoursService{result: []*model.WorkingHourInterval{
		storedInterval(model.Monday, "09:00", "17:00"),
		storedInterval(model.Tuesday, "09:00", "17:00"),
	}}
	handler := NewWorkingHoursHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.tenantID != handlerTenantID || fake.staffID != handlerStaffID {
		t.Fatalf("identifiers = %q/%q, want %q/%q", fake.tenantID, fake.staffID, handlerTenantID, handlerStaffID)
	}
	response := decodeBody(t, recorder)
	if response["staff_id"] != handlerStaffID {
		t.Fatalf("staff_id = %v, want %q", response["staff_id"], handlerStaffID)
	}
	intervals, ok := response["intervals"].([]any)
	if !ok || len(intervals) != 2 {
		t.Fatalf("intervals = %v, want 2 entries", response["intervals"])
	}
	first := intervals[0].(map[string]any)
	for _, key := range []string{"day_of_week", "start_time", "end_time"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("interval missing %q: %s", key, recorder.Body.String())
		}
	}
	// repository/database-only fields must not leak into the response.
	for _, key := range []string{"id", "tenant_id", "staff_id", "created_at", "updated_at"} {
		if _, present := first[key]; present {
			t.Fatalf("interval exposed repository-only field %q", key)
		}
	}
}

// A technician with no configured hours yet is a successful empty schedule,
// never a 404 — only a missing staff profile is.
func TestWorkingHoursGetReturnsAnEmptyScheduleAsSuccessNotNotFound(t *testing.T) {
	fake := &fakeWorkingHoursService{result: nil}
	handler := NewWorkingHoursHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200 for an empty schedule", recorder.Code, recorder.Body.String())
	}
	response := decodeBody(t, recorder)
	intervals, ok := response["intervals"].([]any)
	if !ok || len(intervals) != 0 {
		t.Fatalf("intervals = %v, want an empty array, not null", response["intervals"])
	}
	if !strings.Contains(recorder.Body.String(), `"intervals":[]`) {
		t.Fatalf("body = %s, want intervals serialized as [] rather than null", recorder.Body.String())
	}
}

func TestWorkingHoursGetHandlesNonexistentStaffAsStaffNotFound(t *testing.T) {
	fake := &fakeWorkingHoursService{err: apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)}
	handler := NewWorkingHoursHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/", nil), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "STAFF_NOT_FOUND")
}

// --- PUT -------------------------------------------------------------------

func TestWorkingHoursReplaceRejectsMalformedJSON(t *testing.T) {
	handler := NewWorkingHoursHandler(&fakeWorkingHoursService{})
	recorder := httptest.NewRecorder()

	handler.Replace(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader("{not json")), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestWorkingHoursReplacePassesIntervalsThroughToTheService(t *testing.T) {
	fake := &fakeWorkingHoursService{result: []*model.WorkingHourInterval{storedInterval(model.Monday, "09:00", "17:00")}}
	handler := NewWorkingHoursHandler(fake)
	recorder := httptest.NewRecorder()

	body := `{"intervals":[{"day_of_week":"MONDAY","start_time":"09:00","end_time":"12:00"},{"day_of_week":"MONDAY","start_time":"13:00","end_time":"17:00"}]}`
	handler.Replace(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if fake.tenantID != handlerTenantID || fake.staffID != handlerStaffID {
		t.Fatalf("identifiers = %q/%q, want %q/%q", fake.tenantID, fake.staffID, handlerTenantID, handlerStaffID)
	}
	if len(fake.intervals) != 2 {
		t.Fatalf("intervals passed to the service = %+v, want 2", fake.intervals)
	}
	if fake.intervals[0].DayOfWeek != "MONDAY" || fake.intervals[0].StartTime != "09:00" || fake.intervals[0].EndTime != "12:00" {
		t.Fatalf("first interval passed through incorrectly: %+v", fake.intervals[0])
	}
}

// A missing/null intervals array is an explicit empty schedule, not a
// decoding failure — the deliberate way to clear a technician's hours.
func TestWorkingHoursReplaceTreatsAMissingIntervalsArrayAsEmpty(t *testing.T) {
	fake := &fakeWorkingHoursService{result: nil}
	handler := NewWorkingHoursHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Replace(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{}`)), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(fake.intervals) != 0 {
		t.Fatalf("intervals passed to the service = %+v, want none", fake.intervals)
	}
}

func TestWorkingHoursReplaceReturnsTheValidationFailureFromTheService(t *testing.T) {
	fake := &fakeWorkingHoursService{err: apperrors.New(apperrors.CodeValidationFailed, "overlapping intervals on MONDAY", nil)}
	handler := NewWorkingHoursHandler(fake)
	recorder := httptest.NewRecorder()

	body := `{"intervals":[{"day_of_week":"MONDAY","start_time":"09:00","end_time":"13:00"},{"day_of_week":"MONDAY","start_time":"12:00","end_time":"17:00"}]}`
	handler.Replace(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "VALIDATION_FAILED")
}

func TestWorkingHoursReplaceHandlesNonexistentStaffAsStaffNotFound(t *testing.T) {
	fake := &fakeWorkingHoursService{err: apperrors.New(apperrors.CodeStaffNotFound, "staff profile not found", nil)}
	handler := NewWorkingHoursHandler(fake)
	recorder := httptest.NewRecorder()

	handler.Replace(recorder, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"intervals":[]}`)), handlerTenantID, handlerStaffID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "STAFF_NOT_FOUND")
}
