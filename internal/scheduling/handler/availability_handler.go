package handler

import (
	"net/http"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// AvailabilityHandler serves the tenant-facing availability query for the
// appointment vertical.
//
// The route it backs sits behind Authentication -> Tenant Context ->
// Authorization, wired in app.New, so this handler performs no permission
// check of its own — the same division ServiceHandler and StaffHandler use.
//
// There is deliberately no anonymous variant. The public availability API is
// S9 and will be a separate handler with its own visibility gate, for the same
// reason PublicTenantHandler is separate from TenantHandler.
type AvailabilityHandler struct {
	availability service.AvailabilityService
}

func NewAvailabilityHandler(availability service.AvailabilityService) *AvailabilityHandler {
	return &AvailabilityHandler{availability: availability}
}

// PublicAvailabilitySlot is one bookable window, as tenant-local wall-clock
// "HH:MM" strings — the same representation working hours are returned in.
type PublicAvailabilitySlot struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// PublicAvailability is the availability response. It echoes the resolved
// query context (notably the authoritative timezone the slots are expressed
// in) and the slot list. Internal scheduling maths — instants, offsets,
// occupied intervals — is not exposed.
type PublicAvailability struct {
	Date      string                   `json:"date"`
	Timezone  string                   `json:"timezone"`
	ServiceID string                   `json:"service_id"`
	StaffID   string                   `json:"staff_id"`
	Slots     []PublicAvailabilitySlot `json:"slots"`
}

// Get handles GET /api/v1/tenants/{tenantID}/availability?service_id=&staff_id=&date=.
//
// service_id, staff_id and date are all required query parameters. A missing
// one is VALIDATION_FAILED rather than a guess. The date is a calendar date
// (YYYY-MM-DD) interpreted in the tenant's own timezone; a timezone supplied
// by the caller is ignored by design.
func (h *AvailabilityHandler) Get(writer http.ResponseWriter, request *http.Request, tenantID string) {
	query := request.URL.Query()
	serviceID := query.Get("service_id")
	staffID := query.Get("staff_id")
	date := query.Get("date")
	if serviceID == "" || staffID == "" || date == "" {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeValidationFailed, "service_id, staff_id and date are required", nil))
		return
	}

	result, err := h.availability.GetAvailability(request.Context(), tenantID, serviceID, staffID, date)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	// Slots is built with make(..., len) so an empty result serializes as []
	// rather than null — a day with no availability is an ordinary, successful
	// response, the same treatment working hours give an empty schedule.
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
