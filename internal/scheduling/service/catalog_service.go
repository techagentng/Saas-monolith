package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/money"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// CreateServiceInput carries a transport-validated service creation request.
//
// TenantID is deliberately absent: it comes from the trusted tenant context
// resolved by the middleware chain and is passed as a separate argument, never
// read from a request body. Status, currency and the timestamps are absent for
// the same reason — the server owns all of them.
type CreateServiceInput struct {
	Name            string
	Description     *string
	DurationMinutes int
	PriceMinor      int64
}

// UpdateServiceInput carries a partial update. Only non-nil fields change.
//
// The set is limited to genuinely editable catalog data. Adding a field here
// makes it client-writable, so status (owned by Archive), tenant_id, id,
// currency (tenant-level and write-once) and the timestamps all stay absent
// by design.
type UpdateServiceInput struct {
	Name            *string
	Description     *string
	DurationMinutes *int
	PriceMinor      *int64
}

// TenantReader is the narrow slice of tenant persistence this module needs:
// enough to read a tenant's currency before pricing anything against it.
//
// It is declared here, in the consumer, rather than imported wholesale from
// the tenant module — the same interface-segregation reasoning that keeps
// OnboardingRepository separate from TenantRepository. Scheduling has no
// business knowing how to create or update a tenant.
type TenantReader interface {
	FindByID(ctx context.Context, id string) (*tenantmodel.Tenant, error)
}

// CatalogService owns the service-catalog rules: validation, the currency
// prerequisite, and the archive-idempotency decision.
//
// Tenant access itself — membership and the service.* permission — is verified
// by the production middleware chain before any method here is reached. This
// service does not re-derive or re-check authorization; it does, however, scope
// every repository call by tenantID, so a defect in the chain cannot turn into
// a cross-tenant read or write.
type CatalogService interface {
	Create(ctx context.Context, tenantID string, input CreateServiceInput) (*model.Service, error)
	Get(ctx context.Context, tenantID string, serviceID string) (*model.Service, error)
	// List returns the tenant's catalog. statusFilter accepts "ACTIVE",
	// "ARCHIVED" or "ALL"; an empty string means the default, ACTIVE.
	List(ctx context.Context, tenantID string, statusFilter string) ([]*model.Service, error)
	Update(ctx context.Context, tenantID string, serviceID string, input UpdateServiceInput) (*model.Service, error)
	// Archive moves a service ACTIVE -> ARCHIVED. Calling it on an already
	// archived service is idempotent: it returns the service unchanged without
	// writing, so a repeated call never disturbs updated_at.
	Archive(ctx context.Context, tenantID string, serviceID string) (*model.Service, error)
}

type catalogService struct {
	services repository.ServiceRepository
	tenants  TenantReader
}

func NewCatalogService(services repository.ServiceRepository, tenants TenantReader) CatalogService {
	return &catalogService{services: services, tenants: tenants}
}

// ParseStatusFilter converts the list endpoint's status query parameter into a
// repository filter. It rejects unrecognized values rather than silently
// falling back to a default, so a typo surfaces as a validation error instead
// of quietly returning the wrong catalog.
func ParseStatusFilter(raw string) (repository.ServiceListFilter, error) {
	switch raw {
	case "", string(model.StatusActive):
		// The default is ACTIVE: the catalog a caller almost always wants is
		// the one currently being offered.
		status := model.StatusActive
		return repository.ServiceListFilter{Status: &status}, nil
	case string(model.StatusArchived):
		status := model.StatusArchived
		return repository.ServiceListFilter{Status: &status}, nil
	case "ALL":
		return repository.ServiceListFilter{}, nil
	default:
		return repository.ServiceListFilter{}, apperrors.New(apperrors.CodeValidationFailed, "invalid status filter", nil)
	}
}

func (s *catalogService) Create(ctx context.Context, tenantID string, input CreateServiceInput) (*model.Service, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}

	name, err := model.ValidateName(input.Name)
	if err != nil {
		return nil, err
	}
	description, err := model.ValidateDescription(input.Description)
	if err != nil {
		return nil, err
	}
	if err := model.ValidateDurationMinutes(input.DurationMinutes); err != nil {
		return nil, err
	}
	if err := model.ValidatePriceMinor(input.PriceMinor); err != nil {
		return nil, err
	}

	// A price is meaningless without knowing what it is denominated in, so a
	// tenant must declare its currency before its first service exists. This is
	// checked after field validation so an obviously malformed request fails on
	// its own merits rather than on a prerequisite it also happens to be
	// missing.
	if _, err := s.resolveCurrency(ctx, tenantID, input.PriceMinor); err != nil {
		return nil, err
	}

	return s.services.Create(ctx, &model.Service{
		ID:              uuid.NewString(),
		TenantID:        tenantID,
		Name:            name,
		Description:     description,
		DurationMinutes: input.DurationMinutes,
		PriceMinor:      input.PriceMinor,
		// Status is left unset so the repository's own defaulting applies,
		// producing ACTIVE. There is no path through this method by which a
		// caller influences it.
	})
}

// resolveCurrency loads the tenant's currency and proves the requested price is
// a constructible money.Amount in it. The returned Amount is not persisted —
// price and currency live in separate places by design — but building it is
// what makes the money boundary real rather than decorative: an amount that
// money would refuse never reaches the database.
func (s *catalogService) resolveCurrency(ctx context.Context, tenantID string, priceMinor int64) (money.Amount, error) {
	tenant, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return money.Amount{}, err
	}
	if tenant.Currency == nil {
		return money.Amount{}, apperrors.New(apperrors.CodeValidationFailed, "tenant currency must be set before a service can be priced", nil)
	}
	// Re-validated rather than trusted: the column is written only through
	// CurrencyService, which validates, but a value that is somehow stored and
	// unsupported must not silently become the denomination of a real price.
	currency, err := money.ValidateCurrency(*tenant.Currency)
	if err != nil {
		return money.Amount{}, err
	}
	return money.New(priceMinor, currency)
}

func (s *catalogService) Get(ctx context.Context, tenantID string, serviceID string) (*model.Service, error) {
	if err := validateIdentifiers(tenantID, serviceID); err != nil {
		return nil, err
	}
	return s.services.FindByID(ctx, tenantID, serviceID)
}

func (s *catalogService) List(ctx context.Context, tenantID string, statusFilter string) ([]*model.Service, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	filter, err := ParseStatusFilter(statusFilter)
	if err != nil {
		return nil, err
	}
	return s.services.ListByTenant(ctx, tenantID, filter)
}

func (s *catalogService) Update(ctx context.Context, tenantID string, serviceID string, input UpdateServiceInput) (*model.Service, error) {
	if err := validateIdentifiers(tenantID, serviceID); err != nil {
		return nil, err
	}

	update := repository.ServiceUpdate{}

	if input.Name != nil {
		name, err := model.ValidateName(*input.Name)
		if err != nil {
			return nil, err
		}
		update.Name = &name
	}
	if input.Description != nil {
		description, err := model.ValidateDescription(input.Description)
		if err != nil {
			return nil, err
		}
		update.Description = description
	}
	if input.DurationMinutes != nil {
		if err := model.ValidateDurationMinutes(*input.DurationMinutes); err != nil {
			return nil, err
		}
		update.DurationMinutes = input.DurationMinutes
	}
	if input.PriceMinor != nil {
		if err := model.ValidatePriceMinor(*input.PriceMinor); err != nil {
			return nil, err
		}
		// Re-priced through the same money boundary a create goes through, so
		// an edit cannot bypass a check a creation could not.
		if _, err := s.resolveCurrency(ctx, tenantID, *input.PriceMinor); err != nil {
			return nil, err
		}
		update.PriceMinor = input.PriceMinor
	}

	if update.IsEmpty() {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "no fields to update", nil)
	}

	return s.services.Update(ctx, tenantID, serviceID, update)
}

func (s *catalogService) Archive(ctx context.Context, tenantID string, serviceID string) (*model.Service, error) {
	if err := validateIdentifiers(tenantID, serviceID); err != nil {
		return nil, err
	}

	current, err := s.services.FindByID(ctx, tenantID, serviceID)
	if err != nil {
		return nil, err
	}
	// Idempotent: an already archived service is returned as-is, without a
	// write, so repeating the call never bumps updated_at. This mirrors
	// OnboardingService.Complete's treatment of an already-COMPLETED tenant.
	if current.Status == model.StatusArchived {
		return current, nil
	}
	return s.services.Archive(ctx, tenantID, serviceID)
}

// validateIdentifiers refuses a malformed tenant or service id before any query
// runs. Both are reported as INVALID_REQUEST rather than SERVICE_NOT_FOUND:
// a syntactically impossible id is a broken request, not a missing resource.
func validateIdentifiers(tenantID string, serviceID string) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}
	if _, err := uuid.Parse(serviceID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid service id", err)
	}
	return nil
}

// compile-time guard: the implementation must keep satisfying its interface.
var _ CatalogService = (*catalogService)(nil)
