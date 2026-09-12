package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/money"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/receipt"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// BookingReceiptReader is the narrow slice of booking persistence this
// service needs: resolving one booking by its S12-BE receipt access token,
// tenant-scoped. Declared here, in the consumer — the same
// interface-segregation reasoning every other narrow reader in this package
// follows (AvailabilityServiceReader, StaffReader, ...).
type BookingReceiptReader interface {
	FindByTenantAndReceiptToken(ctx context.Context, tenantID string, token string) (*model.Booking, error)
}

// BookingReceiptService resolves the server-authoritative data for one
// booking's public receipt and hands it to a receipt.Generator to render.
// It never trusts anything but the slug/reference/token in the request —
// every displayed field is re-resolved from CURRENT persisted state (the
// service's current name/price/duration, the staff member's current display
// name, the tenant's current name/timezone/currency), the same
// non-negotiable rule BookingService.CreatePublicBooking already applies
// when it re-validates a slot against S7 rather than trusting what the
// client remembers choosing.
//
// Historical-accuracy limitation (documented, not silently worked around):
// migration 000016's own doc comment on model.Booking explains there is no
// price/service/duration snapshot — "S11+ can add a snapshot when a real
// money flow needs one." This receipt therefore shows the service's CURRENT
// price and name, which may differ from what was true at booking time if an
// owner edits the service afterward. Adding snapshot columns to fix this is
// explicitly out of scope for S12-BE (see the task's own "STOP and explain"
// instruction on schema changes for receipt correctness) and is reported
// here rather than silently introduced.
type BookingReceiptService interface {
	// GetReceipt validates slug/reference/token, resolves the receipt data,
	// and returns the rendered PDF bytes.
	//
	//   - hidden/reserved/non-canonical/unknown slug        → TENANT_NOT_FOUND / TENANT_SLUG_INVALID
	//   - a resolvable non-NAIL_TECHNICIAN tenant             → RESOURCE_NOT_FOUND
	//   - a blank token                                       → VALIDATION_FAILED
	//   - a wrong token, a token from another tenant, or a
	//     reference that does not match the token's own
	//     booking                                             → BOOKING_NOT_FOUND (all three indistinguishable — never disclose which)
	//   - PDF rendering failure                                → INTERNAL_ERROR
	//
	// A CANCELLED booking is NOT refused: its receipt is generated with
	// Status "CANCELLED" rendered plainly, per S12-BE's explicit preference
	// — this is display-only and grants no capability a valid token did not
	// already carry.
	GetReceipt(ctx context.Context, slug string, reference string, token string) ([]byte, error)
}

type bookingReceiptService struct {
	tenants   PublicTenantResolver
	bookings  BookingReceiptReader
	services  AvailabilityServiceReader
	staff     StaffReader
	generator receipt.Generator
}

// NewBookingReceiptService wires the receipt lookup/formatting layer over
// the existing S8 tenant resolver, S1 catalog reader, and S3 staff reader —
// reused exactly as BookingService.CreatePublicBooking already reuses them
// — plus an injected receipt.Generator (production: receipt.NewPDFGenerator).
func NewBookingReceiptService(
	tenants PublicTenantResolver,
	bookings BookingReceiptReader,
	services AvailabilityServiceReader,
	staff StaffReader,
	generator receipt.Generator,
) BookingReceiptService {
	return &bookingReceiptService{tenants: tenants, bookings: bookings, services: services, staff: staff, generator: generator}
}

func (s *bookingReceiptService) GetReceipt(ctx context.Context, slug string, reference string, token string) ([]byte, error) {
	if strings.TrimSpace(token) == "" {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "a receipt token is required", nil)
	}

	resolved, err := s.tenants.ResolvePublicTenant(ctx, slug)
	if err != nil {
		return nil, err
	}
	if resolved.BusinessType == nil || *resolved.BusinessType != tenantmodel.BusinessTypeNailTechnician {
		return nil, apperrors.New(apperrors.CodeResourceNotFound, "no public receipts for this business type", nil)
	}

	booking, err := s.bookings.FindByTenantAndReceiptToken(ctx, resolved.TenantID, token)
	if err != nil {
		return nil, err
	}
	// The reference in the URL is a readability nicety, not the access
	// secret (see model.Booking.ReceiptAccessToken's doc comment) — but it
	// must still name THIS booking. A mismatch is reported identically to a
	// wrong token: never disclose which part failed.
	if bookingReference(booking.ID) != reference {
		return nil, apperrors.New(apperrors.CodeBookingNotFound, "booking not found", nil)
	}

	svc, err := s.services.FindByID(ctx, resolved.TenantID, booking.ServiceID)
	if err != nil {
		return nil, err
	}
	staffProfile, err := s.staff.FindByID(ctx, resolved.TenantID, booking.StaffID)
	if err != nil {
		return nil, err
	}
	location, err := resolveReceiptLocation(resolved.Identity.Timezone)
	if err != nil {
		return nil, err
	}

	data := receipt.Data{
		BusinessName: resolved.Identity.Name,
		// Always nil: no tenant-logo storage exists yet (see
		// receipt.HTTPLogoFetcher's own doc comment). Left as an explicit
		// field, not omitted, so wiring a real source later is a one-line
		// change here rather than a new parameter threaded through this
		// whole call chain.
		LogoURL: nil,

		Reference: reference,
		Status:    string(booking.Status),

		ServiceName:     svc.Name,
		DurationMinutes: svc.DurationMinutes,
		TechnicianName:  staffProfile.DisplayName,

		Date:     booking.StartAt.In(location).Format("2006-01-02"),
		Start:    booking.StartAt.In(location).Format("15:04"),
		End:      booking.EndAt.In(location).Format("15:04"),
		Timezone: location.String(),

		FormattedPrice: formatReceiptPrice(svc.PriceMinor, resolved.Currency),

		CustomerName:  booking.Customer.Name,
		CustomerEmail: booking.Customer.Email,
		CustomerPhone: booking.Customer.Phone,
	}

	pdfBytes, err := s.generator.Generate(ctx, data)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "could not generate receipt", err)
	}
	return pdfBytes, nil
}

// resolveReceiptLocation mirrors availability_service.go's
// resolveTenantLocation, adapted to PublicTenantIdentity.Timezone
// (*string) rather than a full *tenantmodel.Tenant — this service
// deliberately never holds a full Tenant record, only what
// PublicTenantResolver already exposes. Kept as its own small function
// rather than forcing a shared helper across two different input types.
func resolveReceiptLocation(timezone *string) (*time.Location, error) {
	if timezone == nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "tenant has no configured timezone", nil)
	}
	name := strings.TrimSpace(*timezone)
	if name == "" {
		return nil, apperrors.New(apperrors.CodeInternalError, "tenant has no configured timezone", nil)
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInternalError, "tenant timezone is not a loadable IANA identifier", err)
	}
	return location, nil
}

// receiptCurrencySymbols mirrors the frontend's src/lib/money/currency.ts
// CURRENCY_SYMBOLS table exactly (same 7-currency allow-list as
// internal/money, "diffable by eye" per that file's own comment) —
// presentation-only. The authoritative integer minor-unit value and
// currency validation both still come from internal/money and the service
// catalog; this table never influences either.
var receiptCurrencySymbols = map[money.Currency]string{
	money.CurrencyNGN: "₦",
	money.CurrencyUSD: "$",
	money.CurrencyEUR: "€",
	money.CurrencyGBP: "£",
	money.CurrencyGHS: "₵",
	money.CurrencyKES: "KSh",
	money.CurrencyZAR: "R",
}

// formatReceiptPrice renders a service's current price for display. A
// tenant with no currency configured (currency nil) is a real, if rare,
// state — S1's contract makes it nullable — and must not make receipt
// generation fail; it falls back to a bare decimal amount with no symbol.
// An unsupported/invalid currency code falls back to the code itself,
// mirroring the frontend's currencyPrefix's own "show the code rather than a
// wrong symbol" rule.
func formatReceiptPrice(minor int64, currency *string) string {
	major, cents := minor/100, minor%100
	if currency == nil {
		return fmt.Sprintf("%s.%02d", groupThousands(major), cents)
	}
	amount, err := money.New(minor, money.Currency(*currency))
	if err != nil {
		return fmt.Sprintf("%s %s.%02d", *currency, groupThousands(major), cents)
	}
	if symbol, ok := receiptCurrencySymbols[amount.Currency()]; ok {
		return fmt.Sprintf("%s%s.%02d", symbol, groupThousands(major), cents)
	}
	return fmt.Sprintf("%s %s.%02d", string(amount.Currency()), groupThousands(major), cents)
}

// groupThousands inserts comma separators into a non-negative integer's
// decimal string using only integer/string operations — no floats,
// matching internal/money's own "exact integer arithmetic only" discipline.
func groupThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

// compile-time guard.
var _ BookingReceiptService = (*bookingReceiptService)(nil)
