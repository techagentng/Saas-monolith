package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/suggestions"
)

// SuggestionService serves the backend-owned starter-service catalogue for
// the caller's own tenant.
//
// It is read-only and stateless: suggestions.NailTechnician (and any future
// per-vertical set) is a Go constant, never a database row, so there is
// nothing here to create, update or archive — see the suggestions package's
// own doc comment for why. Tenant access itself — membership and the
// service.read permission SC1 reuses for this route — is verified by the
// production middleware chain before List is ever reached.
type SuggestionService interface {
	// List returns the suggestion set for tenantID's own business type,
	// resolved server-side rather than accepted as a caller-supplied
	// parameter — a tenant only ever needs its own vertical's starter
	// catalogue, and taking business_type from the request body would let an
	// authenticated caller browse an unrelated vertical's suggestions for no
	// legitimate reason. A tenant with no business type yet, or one with no
	// starter catalogue defined, gets an empty slice — not an error.
	List(ctx context.Context, tenantID string) ([]suggestions.Suggestion, error)
}

type suggestionService struct {
	tenants TenantReader
}

func NewSuggestionService(tenants TenantReader) SuggestionService {
	return &suggestionService{tenants: tenants}
}

func (s *suggestionService) List(ctx context.Context, tenantID string) ([]suggestions.Suggestion, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid tenant id", err)
	}

	tenant, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if tenant.BusinessType == nil {
		// A tenant that has not declared a vertical yet has no starter
		// catalogue to offer — a normal, empty response, not a validation
		// failure: nothing about listing suggestions requires onboarding to
		// be complete.
		return []suggestions.Suggestion{}, nil
	}
	return suggestions.ForBusinessType(*tenant.BusinessType), nil
}

// compile-time guard: the implementation must keep satisfying its interface.
var _ SuggestionService = (*suggestionService)(nil)
