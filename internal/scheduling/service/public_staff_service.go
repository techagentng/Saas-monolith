package service

import (
	"context"
	"sort"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

// PublicStaffCapabilityReader is the reverse capability lookup S9's technician
// discovery needs: which staff can perform one service. Declared here, in the
// consumer, satisfied by repository.PostgresCapabilityRepository.
type PublicStaffCapabilityReader interface {
	ListStaffIDsForService(ctx context.Context, tenantID string, serviceID string) ([]string, error)
}

// PublicStaffView is the customer-safe projection of one technician.
//
// It is a plain transport DTO with no coupling to any web component, so the
// Next.js booking page and the React Native customer app consume the same
// shape. Deliberately absent — none of it belongs to a customer choosing who
// to book with, and some of it must never reach an anonymous caller:
// tenant_id, user_id, status, is_bookable, bio, timestamps, and any
// authorization role. A BUSINESS_OWNER who performs services appears here as
// an ordinary technician, never labelled as an owner.
type PublicStaffView struct {
	ID   string
	Name string
}

// PublicStaffService serves the anonymous, customer-facing list of technicians
// who can perform a given service — step 2 of the public booking journey
// (Scheduling S9). Read-only; NAIL_TECHNICIAN only.
//
// It reuses PublicTenantResolver (the identical reserved / canonical /
// ACTIVE+COMPLETED visibility gate S8 uses) rather than re-checking visibility,
// and it validates the service against the resolved tenant through the S1
// catalog before trusting the id from the URL.
type PublicStaffService interface {
	// ListCapableStaff returns the ACTIVE, bookable technicians assigned to
	// serviceID, sorted by name. Non-disclosure at every edge:
	//   - hidden / reserved / non-canonical / non-existent slug → TENANT_NOT_FOUND (or TENANT_SLUG_INVALID)
	//   - a resolvable non-NAIL_TECHNICIAN tenant                → RESOURCE_NOT_FOUND
	//   - a service that is archived, missing, or another tenant's → SERVICE_NOT_FOUND
	//   - a nail tenant + valid service with no capable/bookable staff → an empty slice, not an error
	ListCapableStaff(ctx context.Context, slug string, serviceID string) ([]PublicStaffView, error)
}

type publicStaffService struct {
	tenants      PublicTenantResolver
	services     AvailabilityServiceReader
	staff        StaffReader
	capabilities PublicStaffCapabilityReader
}

// NewPublicStaffService wires technician discovery over the S8 tenant resolver
// and the existing S1/S3 repositories.
func NewPublicStaffService(
	tenants PublicTenantResolver,
	services AvailabilityServiceReader,
	staff StaffReader,
	capabilities PublicStaffCapabilityReader,
) PublicStaffService {
	return &publicStaffService{tenants: tenants, services: services, staff: staff, capabilities: capabilities}
}

func (s *publicStaffService) ListCapableStaff(ctx context.Context, slug string, serviceID string) ([]PublicStaffView, error) {
	// A syntactically impossible service id is reported as a missing service,
	// not INVALID_REQUEST — a customer following a stale or hand-edited link
	// should see the same "unavailable" as any other bad id, and the response
	// must not disclose whether such an id exists anywhere.
	if _, err := uuid.Parse(serviceID); err != nil {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}

	resolved, err := s.tenants.ResolvePublicTenant(ctx, slug)
	if err != nil {
		return nil, err
	}
	if resolved.BusinessType == nil || *resolved.BusinessType != tenantmodel.BusinessTypeNailTechnician {
		return nil, apperrors.New(apperrors.CodeResourceNotFound, "no public technician list for this business type", nil)
	}

	// The service must exist in THIS tenant. FindByID is tenant-scoped, so a
	// cross-tenant or nonexistent id is indistinguishable — SERVICE_NOT_FOUND
	// in both cases, disclosing nothing about other tenants.
	svc, err := s.services.FindByID(ctx, resolved.TenantID, serviceID)
	if err != nil {
		return nil, err
	}
	if svc.Status == model.StatusArchived {
		return nil, apperrors.New(apperrors.CodeServiceNotFound, "service not found", nil)
	}

	staffIDs, err := s.capabilities.ListStaffIDsForService(ctx, resolved.TenantID, serviceID)
	if err != nil {
		return nil, err
	}

	views := make([]PublicStaffView, 0, len(staffIDs))
	for _, id := range staffIDs {
		profile, err := s.staff.FindByID(ctx, resolved.TenantID, id)
		if err != nil {
			return nil, err
		}
		// Only someone a customer can actually be booked with: ACTIVE (still
		// works here) and bookable (currently taking appointments). An archived
		// or paused technician is simply absent, never surfaced as disabled.
		if profile.Status != model.StatusActive || !profile.IsBookable {
			continue
		}
		views = append(views, PublicStaffView{ID: profile.ID, Name: profile.DisplayName})
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].Name != views[j].Name {
			return views[i].Name < views[j].Name
		}
		return views[i].ID < views[j].ID
	})
	return views, nil
}

// compile-time guard.
var _ PublicStaffService = (*publicStaffService)(nil)
