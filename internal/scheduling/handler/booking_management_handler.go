package handler

import (
	"encoding/json"
	"net/http"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// BookingManagementHandler serves the authenticated, tenant-scoped owner/staff
// booking views (Scheduling S11). Every route it backs sits behind
// Authentication -> Tenant Context -> Authorization (booking.read /
// booking.update), wired in app.New, so this handler performs no permission
// check of its own — the same division ServiceHandler and StaffHandler use.
type BookingManagementHandler struct {
	bookings service.BookingManagementService
}

func NewBookingManagementHandler(bookings service.BookingManagementService) *BookingManagementHandler {
	return &BookingManagementHandler{bookings: bookings}
}

// bookingParty is the {id, name} of a service or technician on a booking.
type bookingParty struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TenantBooking is one dashboard list row.
//
// start/end/created_at are absolute RFC3339 UTC instants — the backend's
// storage and API convention; the dashboard formats them in the tenant
// timezone. Customer phone/email are present because contacting the customer
// is the operational purpose of this list, and the caller is already
// authorized for booking.read in this tenant. Absent: tenant_id, raw
// updated_at, and any auth/session/audit data.
type TenantBooking struct {
	ID              string       `json:"id"`
	Reference       string       `json:"reference"`
	Status          string       `json:"status"`
	Service         bookingParty `json:"service"`
	Staff           bookingParty `json:"staff"`
	CustomerName    string       `json:"customer_name"`
	CustomerPhone   *string      `json:"customer_phone"`
	CustomerEmail   *string      `json:"customer_email"`
	Start           time.Time    `json:"start"`
	End             time.Time    `json:"end"`
	DurationMinutes int          `json:"duration_minutes"`
	CreatedAt       time.Time    `json:"created_at"`
}

// TenantBookingDetail is a single booking with the tenant timezone attached so
// the detail view is self-contained.
type TenantBookingDetail struct {
	TenantBooking
	Timezone string `json:"timezone"`
}

func toTenantBooking(s service.BookingSummary) TenantBooking {
	return TenantBooking{
		ID:              s.ID,
		Reference:       s.Reference,
		Status:          string(s.Status),
		Service:         bookingParty{ID: s.ServiceID, Name: s.ServiceName},
		Staff:           bookingParty{ID: s.StaffID, Name: s.StaffName},
		CustomerName:    s.CustomerName,
		CustomerPhone:   s.CustomerPhone,
		CustomerEmail:   s.CustomerEmail,
		Start:           s.StartAt,
		End:             s.EndAt,
		DurationMinutes: s.DurationMinutes,
		CreatedAt:       s.CreatedAt,
	}
}

func toTenantBookingDetail(d *service.BookingDetail) TenantBookingDetail {
	return TenantBookingDetail{TenantBooking: toTenantBooking(d.BookingSummary), Timezone: d.Timezone}
}

// List handles GET /api/v1/tenants/{tenantID}/bookings?view=&staff_id=&service_id=&date=.
//
// view is one of upcoming (default) / past / cancelled / all. staff_id and
// service_id are optional exact filters. date, if given, is a calendar date
// in YYYY-MM-DD form interpreted in the TENANT's own timezone (never the
// server's) — the same date semantics the public availability endpoint
// uses. All filtering happens in the database.
func (h *BookingManagementHandler) List(writer http.ResponseWriter, request *http.Request, tenantID string) {
	query := request.URL.Query()
	view, err := service.ParseBookingView(query.Get("view"))
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	summaries, err := h.bookings.List(request.Context(), tenantID, service.BookingListFilter{
		View:      view,
		StaffID:   optionalQuery(query.Get("staff_id")),
		ServiceID: optionalQuery(query.Get("service_id")),
		Date:      optionalQuery(query.Get("date")),
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	// An empty result serializes as [] rather than null.
	result := make([]TenantBooking, len(summaries))
	for i, s := range summaries {
		result[i] = toTenantBooking(s)
	}
	writeJSON(writer, http.StatusOK, result)
}

// Get handles GET /api/v1/tenants/{tenantID}/bookings/{bookingID}. A missing or
// cross-tenant booking id is BOOKING_NOT_FOUND, identically.
func (h *BookingManagementHandler) Get(writer http.ResponseWriter, request *http.Request, tenantID string, bookingID string) {
	detail, err := h.bookings.Get(request.Context(), tenantID, bookingID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toTenantBookingDetail(detail))
}

// Cancel handles POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/cancel.
//
// The request carries no body: cancellation is a server-decided state
// transition (CONFIRMED -> CANCELLED), never a client-supplied status value —
// the same shape service/{id}/archive and staff/{id}/archive already use. It
// is idempotent on an already-cancelled booking. The response is the updated
// booking detail.
func (h *BookingManagementHandler) Cancel(writer http.ResponseWriter, request *http.Request, tenantID string, bookingID string) {
	detail, err := h.bookings.Cancel(request.Context(), tenantID, bookingID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toTenantBookingDetail(detail))
}

// rescheduleRequest is the decode target for reschedule. There is no field
// for end, duration, service id, staff id, customer or price — a client
// that sends them has them silently discarded, the same structural
// protection every other write endpoint in this codebase relies on.
type rescheduleRequest struct {
	Date  string `json:"date"`
	Start string `json:"start"`
}

// Reschedule handles POST /api/v1/tenants/{tenantID}/bookings/{bookingID}/reschedule.
//
// Body: {"date": "YYYY-MM-DD", "start": "HH:MM"}, both interpreted in the
// TENANT's own timezone (never the caller's). The response is the same
// booking-detail DTO List/Get/Cancel already use — no second
// reschedule-specific shape.
func (h *BookingManagementHandler) Reschedule(writer http.ResponseWriter, request *http.Request, tenantID string, bookingID string) {
	var body rescheduleRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	detail, err := h.bookings.Reschedule(request.Context(), tenantID, bookingID, service.RescheduleBookingInput{
		Date:  body.Date,
		Start: body.Start,
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toTenantBookingDetail(detail))
}

func optionalQuery(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
