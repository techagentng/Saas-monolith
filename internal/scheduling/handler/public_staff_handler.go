package handler

import (
	"net/http"

	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// PublicStaffHandler serves the anonymous, customer-facing list of technicians
// who can perform a given service (Scheduling S9, step 2).
//
// It is deliberately not part of StaffHandler: every StaffHandler route sits
// behind Authentication -> Tenant Context -> Authorization and its DTO carries
// the linked user id, which must never reach an anonymous caller — the same
// separation PublicServiceHandler keeps from ServiceHandler.
type PublicStaffHandler struct {
	staff service.PublicStaffService
}

func NewPublicStaffHandler(staff service.PublicStaffService) *PublicStaffHandler {
	return &PublicStaffHandler{staff: staff}
}

// PublicStaffItem is the wire representation of one customer-facing technician:
// an id to book against and a name to show. Nothing else — no user id, no
// status, no bookable flag, no bio, no timestamps, no role.
type PublicStaffItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PublicStaffListResponse echoes the service the list is for, then the staff.
type PublicStaffListResponse struct {
	ServiceID string            `json:"service_id"`
	Staff     []PublicStaffItem `json:"staff"`
}

// List handles GET /api/v1/public/tenants/{slug}/services/{serviceID}/staff.
//
// The slug and service id come from the route; this endpoint takes no body and
// reads no principal. A tenant that is not publicly visible, or a slug that
// does not resolve, is TENANT_NOT_FOUND — identical to a nonexistent one. A
// resolvable non-NAIL_TECHNICIAN tenant is RESOURCE_NOT_FOUND. An archived,
// missing or cross-tenant service is SERVICE_NOT_FOUND. A valid service with
// no bookable capable staff is a successful empty list.
func (h *PublicStaffHandler) List(writer http.ResponseWriter, request *http.Request, slug string, serviceID string) {
	views, err := h.staff.ListCapableStaff(request.Context(), slug, serviceID)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	// Built with make(..., len) so an empty result serializes as [] rather
	// than null — "no technicians available for this service" is an ordinary,
	// successful response the client renders as an empty state.
	items := make([]PublicStaffItem, len(views))
	for i, v := range views {
		items[i] = PublicStaffItem{ID: v.ID, Name: v.Name}
	}
	writeJSON(writer, http.StatusOK, PublicStaffListResponse{ServiceID: serviceID, Staff: items})
}
