package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/money"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// CurrencyService owns the tenant currency rule (Scheduling S1): a tenant
// declares the currency all of its prices are denominated in, exactly once.
//
// This is a dedicated service and a dedicated endpoint rather than a field on
// UpdateTenantProfileRequest. Profile fields are freely re-editable; currency
// is not, and mixing a write-once value into a partial-update endpoint would
// give one request shape two different mutability rules.
type CurrencyService interface {
	// Set records a tenant's currency.
	//
	//   currency is nil                    -> set it
	//   currency already equals the value  -> idempotent success, no write
	//   currency already set to something else -> refused
	Set(ctx context.Context, tenantID string, code string) (*model.Tenant, error)
}

type currencyService struct {
	tenants repository.CurrencyRepository
}

func NewCurrencyService(tenants repository.CurrencyRepository) CurrencyService {
	return &currencyService{tenants: tenants}
}

func (s *currencyService) Set(ctx context.Context, tenantID string, code string) (*model.Tenant, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	// Validated before the tenant is even loaded, so an unsupported code never
	// reaches persistence. money.ValidateCurrency rejects rather than
	// normalizes: "ngn" and " NGN" are refused, not silently upcased or
	// trimmed, matching how ValidateSlug and ValidateBusinessType treat their
	// own controlled values.
	currency, err := money.ValidateCurrency(code)
	if err != nil {
		return nil, err
	}

	current, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	if current.Currency != nil {
		// Idempotent: re-sending the value the tenant already has succeeds
		// without a write, so a retried request never bumps updated_at.
		if *current.Currency == string(currency) {
			return current, nil
		}
		// Write-once. Changing a tenant's currency after prices exist would
		// silently reinterpret every stored amount — 1999 minor units meaning
		// ₦19.99 one day and $19.99 the next — with no conversion in this
		// system to make that transition correct.
		return nil, apperrors.New(apperrors.CodeValidationFailed, "tenant currency cannot be changed once set", nil)
	}

	return s.tenants.SetCurrency(ctx, tenantID, string(currency))
}

// compile-time guard: the implementation must keep satisfying its interface.
var _ CurrencyService = (*currencyService)(nil)
