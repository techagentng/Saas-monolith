package handler

import (
	"net/http"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// PublicAvailabilityHandler serves the anonymous, customer-facing availability
// query (Scheduling S9, step 3) — the S7 engine behind the S8 public tenant
// gate.
//
// Deliberately separate from AvailabilityHandler, whose route is behind the
// full authenticated middleware chain. This one is registered as a bare mux
// entry, before the auth middleware exists, exactly like PublicServiceHandler.
//
// The wire DTO is the same PublicAvailability / PublicAvailabilitySlot the
// tenant-facing handler already defines in this package — the customer-safe
// projection was correct there and is reused verbatim rather than copied.
type PublicAvailabilityHandler struct {
	availability service.PublicAvailabilityService
}

func NewPublicAvailabilityHandler(availability service.PublicAvailabilityService) *PublicAvailabilityHandler {
	return &PublicAvailabilityHandler{availability: availability}
}

// Get handles
// GET /api/v1/public/tenants/{slug}/availability?service_id=&staff_id=&date=.
//
// service_id, staff_id and date are all required. The date is a calendar date
// (YYYY-MM-DD) interpreted in the tenant's own timezone; a caller-supplied
// timezone is ignored by design. An empty slot list is a 200, not an error.
func (h *PublicAvailabilityHandler) Get(writer http.ResponseWriter, request *http.Request, slug string) {
	query := request.URL.Query()
	serviceID := query.Get("service_id")
	staffID := query.Get("staff_id")
	date := query.Get("date")
	if serviceID == "" || staffID == "" || date == "" {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeValidationFailed, "service_id, staff_id and date are required", nil))
		return
	}

	result, err := h.availability.GetPublicAvailability(request.Context(), slug, serviceID, staffID, date)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	slots := make([]PublicAvailabilitySlot, len(result.Slots))
	for i, slot := range result.Slots {
		slots[i] = PublicAvailabilitySlot{Start: slot.Start, End: slot.End}
	}
	writeJSON(writer, http.StatusOK, PublicAvailability{
		Date:      result.Date,
		Timezone:  result.Timezone,
		ServiceID: result.ServiceID,
		StaffID:   result.StaffID,
		Slots:     slots,
	})
}
