package service

import (
	"context"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// PublicAvailabilityService is the anonymous adapter over the S7 availability
// engine — step 3 of the public booking journey (Scheduling S9).
//
// It is a thin gate, nothing more: it resolves the public slug through the
// SAME PublicTenantResolver S8 uses (reserved / canonical / ACTIVE+COMPLETED),
// enforces the NAIL_TECHNICIAN vertical, and hands the resolved internal
// tenant id to S7's GetAvailability. Every rule that matters — service and
// staff existence, cross-tenant non-disclosure, the staff/service capability
// check, tenant-timezone resolution, past-slot filtering, deterministic slot
// generation — already lives in AvailabilityService and is not re-implemented
// or duplicated here.
type PublicAvailabilityService interface {
	// GetPublicAvailability returns the bookable slots for one service with one
	// technician on one tenant-local calendar date (YYYY-MM-DD). A
	// caller-supplied timezone is never consulted — the tenant's own is
	// authoritative, resolved inside S7.
	//
	// Errors mirror S8's public semantics plus S7's:
	//   - hidden / reserved / non-canonical / non-existent slug → TENANT_NOT_FOUND (or TENANT_SLUG_INVALID)
	//   - a resolvable non-NAIL_TECHNICIAN tenant                → RESOURCE_NOT_FOUND
	//   - archived / missing / cross-tenant service or staff     → SERVICE_NOT_FOUND / STAFF_NOT_FOUND
	//   - a technician not assigned the service                  → VALIDATION_FAILED
	//   - a malformed date                                       → VALIDATION_FAILED
	//   - no working hours / all past / not bookable             → an empty slot list, 200, no error
	GetPublicAvailability(ctx context.Context, slug string, serviceID string, staffID string, date string) (*AvailabilityResult, error)
}

type publicAvailabilityService struct {
	tenants PublicTenantResolver
	engine  AvailabilityService
}

// NewPublicAvailabilityService wires the S9 gate over the S8 tenant resolver
// and the existing S7 engine.
func NewPublicAvailabilityService(tenants PublicTenantResolver, engine AvailabilityService) PublicAvailabilityService {
	return &publicAvailabilityService{tenants: tenants, engine: engine}
}

func (s *publicAvailabilityService) GetPublicAvailability(ctx context.Context, slug string, serviceID string, staffID string, date string) (*AvailabilityResult, error) {
	resolved, err := s.tenants.ResolvePublicTenant(ctx, slug)
	if err != nil {
		return nil, err
	}
	if resolved.BusinessType == nil || *resolved.BusinessType != tenantmodel.BusinessTypeNailTechnician {
		return nil, apperrors.New(apperrors.CodeResourceNotFound, "no public availability for this business type", nil)
	}

	// The internal tenant id comes only from the resolved slug — never from
	// the client. S7 scopes every one of its own repository calls by it.
	return s.engine.GetAvailability(ctx, resolved.TenantID, serviceID, staffID, date)
}

// compile-time guard.
var _ PublicAvailabilityService = (*publicAvailabilityService)(nil)
