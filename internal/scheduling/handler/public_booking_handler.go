package handler

import (
	"encoding/json"
	"net/http"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// PublicBookingHandler serves anonymous appointment booking creation
// (Scheduling S10) — the step that turns an S9 slot selection into a persisted
// booking.
//
// Deliberately separate from every authenticated handler and registered as a
// bare mux entry before the auth middleware exists, exactly like the other
// public handlers. It accepts customer PII in the body; that PII is never
// logged and never echoed back in an error.
type PublicBookingHandler struct {
	bookings service.BookingService
}

func NewPublicBookingHandler(bookings service.BookingService) *PublicBookingHandler {
	return &PublicBookingHandler{bookings: bookings}
}

// publicBookingRequest is the decode target. It carries exactly the fields the
// customer supplies. There is no field for tenant_id, duration, end, price,
// currency, service name, staff name or timezone — a client that sends them
// has them silently discarded, the structural protection every write endpoint
// in this codebase relies on.
type publicBookingRequest struct {
	ServiceID string `json:"service_id"`
	StaffID   string `json:"staff_id"`
	Date      string `json:"date"`
	Start     string `json:"start"`
	Customer  struct {
		Name  string  `json:"name"`
		Phone *string `json:"phone"`
		Email *string `json:"email"`
	} `json:"customer"`
}

// PublicBookingService is the {id, name} of the booked service in the response.
type PublicBookingService struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PublicBookingStaff is the {id, name} of the booked technician in the response.
type PublicBookingStaff struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PublicBooking is the customer-safe confirmation payload. No tenant_id, no
// customer PII echoed back, no timestamps, no internal scheduling data.
type PublicBooking struct {
	ID        string               `json:"id"`
	Reference string               `json:"reference"`
	Status    string               `json:"status"`
	Service   PublicBookingService `json:"service"`
	Staff     PublicBookingStaff   `json:"staff"`
	Date      string               `json:"date"`
	Start     string               `json:"start"`
	End       string               `json:"end"`
	Timezone  string               `json:"timezone"`
}

// PublicBookingResponse wraps the booking under a "booking" key, matching the
// S10 conceptual response shape.
type PublicBookingResponse struct {
	Booking PublicBooking `json:"booking"`
}

// Create handles POST /api/v1/public/tenants/{slug}/bookings.
//
// The slug comes from the route. The body is decoded into publicBookingRequest;
// a malformed body is INVALID_REQUEST. Everything else — vertical enforcement,
// slot re-validation against S7, the concurrency-safe insert — is the service's.
func (h *PublicBookingHandler) Create(writer http.ResponseWriter, request *http.Request, slug string) {
	var body publicBookingRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	result, err := h.bookings.CreatePublicBooking(request.Context(), slug, service.CreateBookingInput{
		ServiceID: body.ServiceID,
		StaffID:   body.StaffID,
		Date:      body.Date,
		Start:     body.Start,
		Customer: service.CustomerInput{
			Name:  body.Customer.Name,
			Phone: body.Customer.Phone,
			Email: body.Customer.Email,
		},
	})
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	writeJSON(writer, http.StatusCreated, PublicBookingResponse{Booking: PublicBooking{
		ID:        result.ID,
		Reference: result.Reference,
		Status:    string(result.Status),
		Service:   PublicBookingService{ID: result.ServiceID, Name: result.ServiceName},
		Staff:     PublicBookingStaff{ID: result.StaffID, Name: result.StaffName},
		Date:      result.Date,
		Start:     result.Start,
		End:       result.End,
		Timezone:  result.Timezone,
	}})
}
