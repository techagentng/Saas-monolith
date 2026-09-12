package handler

import (
	"net/http"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/service"
)

// PublicBookingReceiptHandler serves the anonymous, customer-facing PDF
// receipt download (S12-BE). Registered as a bare mux entry, before the auth
// middleware exists, exactly like every other public handler in this
// package — it must never be wrapped into a private chain.
type PublicBookingReceiptHandler struct {
	receipts service.BookingReceiptService
}

func NewPublicBookingReceiptHandler(receipts service.BookingReceiptService) *PublicBookingReceiptHandler {
	return &PublicBookingReceiptHandler{receipts: receipts}
}

// Get handles GET /api/v1/public/tenants/{slug}/bookings/{reference}/receipt?token=.
//
// token is the sole access secret (see model.Booking.ReceiptAccessToken); an
// absent token is rejected here, before even reaching the service. reference
// is a readability nicety the service re-validates against the token's own
// booking — never authoritative alone.
//
// The filename built below is safe from header injection specifically
// because it is only reached AFTER GetReceipt succeeds, which requires
// reference to have exactly equaled the server-computed "NB-XXXXXXXX" value
// for that booking (see BookingReceiptService.GetReceipt) — a caller cannot
// reach this line with an arbitrary reference string.
func (h *PublicBookingReceiptHandler) Get(writer http.ResponseWriter, request *http.Request, slug string, reference string) {
	token := request.URL.Query().Get("token")
	if token == "" {
		writeSchedulingError(writer, apperrors.New(apperrors.CodeValidationFailed, "a receipt token is required", nil))
		return
	}

	pdfBytes, err := h.receipts.GetReceipt(request.Context(), slug, reference, token)
	if err != nil {
		writeSchedulingError(writer, err)
		return
	}

	writer.Header().Set("Content-Type", "application/pdf")
	writer.Header().Set("Content-Disposition", `attachment; filename="booking-`+reference+`.pdf"`)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(pdfBytes)
}
