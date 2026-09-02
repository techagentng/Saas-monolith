package handler

import (
	"encoding/json"
	"net/http"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// WorkingHoursHandler serves a staff member's recurring weekly working hours.
//
// Every route it backs sits behind Authentication -> Tenant Context ->
// Authorization, wired in app.New, so this handler performs no permission
// check of its own — the same division StaffHandler already uses.
//
// This is never appointment slots, breaks, holidays, one-off exceptions, or
// an availability calculation: those are later features (S6/S7+) reading
// this table, not written here.
type WorkingHoursHandler struct {
	hours service.WorkingHoursService
}

func NewWorkingHoursHandler(hours service.WorkingHoursService) *WorkingHoursHandler {
	return &WorkingHoursHandler{hours: hours}
}

// PublicWorkingHourInterval is the tenant-facing representation of one
// interval. No id, tenant_id, staff_id, or timestamps: those are
// repository/database-only fields the API response does not need to expose.
type PublicWorkingHourInterval struct {
	DayOfWeek string `json:"day_of_week"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// PublicWorkingHours is the complete schedule response for one staff member.
type PublicWorkingHours struct {
	StaffID   string                      `json:"staff_id"`
	Intervals []PublicWorkingHourInterval `json:"intervals"`
}

func toPublicWorkingHours(staffID string, intervals []*model.WorkingHourInterval) PublicWorkingHours {
	// Intervals is built with make(..., 0, ...) rather than left nil, so an
	// empty schedule serializes as [] rather than null — a technician with no
	// configured hours yet is a successful, ordinary response.
	result := make([]PublicWorkingHourInterval, 0, len(intervals))
	for _, interval := range intervals {
		result = append(result, PublicWorkingHourInterval{
			DayOfWeek: string(interval.DayOfWeek), StartTime: interval.StartTime, EndTime: interval.EndTime,
		})
	}
	return PublicWorkingHours{StaffID: staffID, Intervals: result}
}

// Get handles GET /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours.
//
// A staff member with no configured hours yet returns a successful empty
// schedule. Only a nonexistent, or cross-tenant, staff profile is
// STAFF_NOT_FOUND — identically to every other staff sub-resource.
func (h *WorkingHoursHandler) Get(writer http.ResponseWriter, request *http.Request, tenantID string, staffID string) {
	intervals, err := h.hours.List(request.Context(), tenantID, staffID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicWorkingHours(staffID, intervals))
}

// Replace handles PUT /api/v1/tenants/{tenantID}/staff/{staffID}/working-hours.
//
// PUT rather than PATCH because the body is the complete weekly schedule:
// whatever is sent becomes the schedule, the same "the owner-facing operation
// is the complete set" reasoning ReplaceCapabilities already applies. A
// missing or null intervals array is treated as an explicit empty schedule —
// "this technician has no configured hours" is a legitimate state, including
// as the deliberate way to clear one, and guessing otherwise would make the
// endpoint's meaning depend on JSON subtleties.
func (h *WorkingHoursHandler) Replace(writer http.ResponseWriter, request *http.Request, tenantID string, staffID string) {
	var input struct {
		Intervals []struct {
			DayOfWeek string `json:"day_of_week"`
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
		} `json:"intervals"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err))
		return
	}

	intervals := make([]service.IntervalInput, 0, len(input.Intervals))
	for _, interval := range input.Intervals {
		intervals = append(intervals, service.IntervalInput{
			DayOfWeek: interval.DayOfWeek, StartTime: interval.StartTime, EndTime: interval.EndTime,
		})
	}

	result, err := h.hours.ReplaceWeeklySchedule(request.Context(), tenantID, staffID, intervals)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, toPublicWorkingHours(staffID, result))
}
