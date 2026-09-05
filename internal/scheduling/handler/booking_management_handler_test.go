package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

type fakeBookingManagementService struct {
	tenantID  string
	bookingID string
	filter    service.BookingListFilter
	list      []service.BookingSummary
	detail    *service.BookingDetail
	err       error
}

func (f *fakeBookingManagementService) List(_ context.Context, tenantID string, filter service.BookingListFilter) ([]service.BookingSummary, error) {
	f.tenantID, f.filter = tenantID, filter
	return f.list, f.err
}

func (f *fakeBookingManagementService) Get(_ context.Context, tenantID, bookingID string) (*service.BookingDetail, error) {
	f.tenantID, f.bookingID = tenantID, bookingID
	return f.detail, f.err
}

func (f *fakeBookingManagementService) Cancel(_ context.Context, tenantID, bookingID string) (*service.BookingDetail, error) {
	f.tenantID, f.bookingID = tenantID, bookingID
	return f.detail, f.err
}

func sampleSummary() service.BookingSummary {
	phone := "+2348001112222"
	return service.BookingSummary{
		ID: "550e8400-e29b-41d4-a716-4466554e0001", Reference: "NB-550E8400",
		Status: model.BookingConfirmed, ServiceID: "svc", ServiceName: "Gel Manicure",
		StaffID: "stf", StaffName: "Ada", CustomerName: "Jane Doe", CustomerPhone: &phone,
		StartAt: time.Date(2026, 9, 12, 8, 30, 0, 0, time.UTC), EndAt: time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC),
		DurationMinutes: 30, CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

const mgmtTenantID = "550e8400-e29b-41d4-a716-446655442001"

func TestBookingListShapesResponseAndParsesQuery(t *testing.T) {
	fake := &fakeBookingManagementService{list: []service.BookingSummary{sampleSummary()}}
	h := NewBookingManagementHandler(fake)
	rec := httptest.NewRecorder()

	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+mgmtTenantID+"/bookings?view=past&staff_id=STF-1&service_id=SVC-1", nil), mgmtTenantID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.filter.View != service.BookingViewPast || fake.filter.StaffID == nil || *fake.filter.StaffID != "STF-1" || fake.filter.ServiceID == nil {
		t.Fatalf("filter not parsed: %+v", fake.filter)
	}
	body := rec.Body.String()
	for _, want := range []string{`"reference":"NB-550E8400"`, `"customer_name":"Jane Doe"`, `"service":{`, `"duration_minutes":30`, `"start":"2026-09-12T08:30:00Z"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	for _, forbidden := range []string{"tenant_id", "updated_at", "TenantID"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("list leaked %q: %s", forbidden, body)
		}
	}
}

func TestBookingListEmptyIsEmptyArray(t *testing.T) {
	h := NewBookingManagementHandler(&fakeBookingManagementService{list: nil})
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/", nil), mgmtTenantID)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("body = %q, want []", rec.Body.String())
	}
}

func TestBookingListRejectsInvalidView(t *testing.T) {
	h := NewBookingManagementHandler(&fakeBookingManagementService{err: apperrors.New(apperrors.CodeValidationFailed, "invalid booking view", nil)})
	// The handler itself parses the view; a bad value never reaches the service.
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/?view=sideways", nil), mgmtTenantID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertErrorCode(t, rec, "VALIDATION_FAILED")
}

func TestBookingDetailIncludesTimezone(t *testing.T) {
	d := &service.BookingDetail{BookingSummary: sampleSummary(), Timezone: "Africa/Lagos"}
	h := NewBookingManagementHandler(&fakeBookingManagementService{detail: d})
	rec := httptest.NewRecorder()

	h.Get(rec, httptest.NewRequest(http.MethodGet, "/", nil), mgmtTenantID, d.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"timezone":"Africa/Lagos"`) {
		t.Fatalf("detail missing timezone: %s", rec.Body.String())
	}
}

func TestBookingDetailNotFoundMapping(t *testing.T) {
	h := NewBookingManagementHandler(&fakeBookingManagementService{err: apperrors.New(apperrors.CodeBookingNotFound, "booking not found", nil)})
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/", nil), mgmtTenantID, "x")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	assertErrorCode(t, rec, "BOOKING_NOT_FOUND")
}

func TestBookingCancelReturnsUpdatedDetail(t *testing.T) {
	summary := sampleSummary()
	summary.Status = model.BookingCancelled
	h := NewBookingManagementHandler(&fakeBookingManagementService{detail: &service.BookingDetail{BookingSummary: summary, Timezone: "Africa/Lagos"}})
	rec := httptest.NewRecorder()

	h.Cancel(rec, httptest.NewRequest(http.MethodPost, "/", nil), mgmtTenantID, summary.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"CANCELLED"`) {
		t.Fatalf("cancel response = %s", rec.Body.String())
	}
}

func TestBookingCancelErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		code apperrors.ErrorCode
		want int
		body string
	}{
		{apperrors.CodeBookingNotFound, http.StatusNotFound, "BOOKING_NOT_FOUND"},
		{apperrors.CodeInvalidRequest, http.StatusBadRequest, "INVALID_REQUEST"},
		{apperrors.CodePermissionDenied, http.StatusForbidden, "PERMISSION_DENIED"},
	} {
		t.Run(string(tc.code), func(t *testing.T) {
			h := NewBookingManagementHandler(&fakeBookingManagementService{err: apperrors.New(tc.code, "x", nil)})
			rec := httptest.NewRecorder()
			h.Cancel(rec, httptest.NewRequest(http.MethodPost, "/", nil), mgmtTenantID, "x")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			assertErrorCode(t, rec, tc.body)
		})
	}
}
