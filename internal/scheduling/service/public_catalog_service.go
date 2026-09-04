package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/scheduling/model"
	"github.com/techagentng/saas-monolith/internal/scheduling/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantservice "github.com/techagentng/saas-monolith/internal/tenant/service"
)

// PublicTenantResolver resolves a public slug to the internal context S8 needs,
// applying the same visibility gate the anonymous tenant-identity endpoint
// uses. Declared here and satisfied by tenant/service's PublicTenantService —
// the catalog does not re-implement "is this tenant publicly visible", and it
// never touches the tenant repository directly.
type PublicTenantResolver interface {
	ResolvePublicTenant(ctx context.Context, slug string) (*tenantservice.PublicTenantContext, error)
}

// PublicServiceView is the customer-safe projection of one catalog service.
//
// It is a plain transport DTO with no coupling to any web component, so the
// Next.js public booking page and the existing React Native customer app can
// both consume the same shape. Deliberately absent: tenant_id, status,
// created_at, updated_at — a customer never sees internal lifecycle or audit
// data. PriceMinor is paired with the tenant-level Currency on the envelope
// (PublicCatalog), not repeated on every row.
type PublicServiceView struct {
	ID              string
	Name            string
	Description     *string
	DurationMinutes int
	PriceMinor      int64
	// Category is the category's Name, resolved server-side from the
	// service's internal CategoryID — never the id itself, which is an
	// internal identifier with no meaning to a customer and is never part of
	// this DTO. nil for an uncategorised service, and also for a service
	// whose category row cannot be resolved (defensive: a missing category
	// must degrade the display, never break the whole public catalog).
	Category *string
}

// PublicCatalog is the whole public-catalog response: the bookable services
// plus the ISO 4217 currency their prices are denominated in. Currency is a
// tenant-level property in Scheduling S1 (one business, one currency), so it
// lives on the envelope; it is nil for a tenant that has not declared one yet.
type PublicCatalog struct {
	Currency *string
	Services []PublicServiceView
}

// PublicCatalogService serves the anonymous, customer-facing view of a
// NAIL_TECHNICIAN tenant's service catalog — the first screen of the public
// booking journey (Catalog S8). It is read-only and never exposes an
// owner/admin field.
//
// It is strictly the appointment-service catalog for the appointment vertical.
// A hotel's rooms, a restaurant's tables and a transport company's routes are
// different resource models with their own future public endpoints; this
// service must never stand in for them, so a resolvable tenant of any other
// business type is refused rather than served an empty or mislabelled catalog.
type PublicCatalogService interface {
	// GetCatalog resolves the slug, enforces public visibility (delegated) and
	// the NAIL_TECHNICIAN vertical, then returns only ACTIVE services in the
	// catalog's deterministic order.
	//
	//   - unknown / reserved / non-canonical / not-publicly-visible slug
	//       → TENANT_NOT_FOUND (all identical — no disclosure)
	//   - a resolvable tenant of any non-NAIL_TECHNICIAN business type
	//       → RESOURCE_NOT_FOUND (there is no public appointment catalog here)
	//   - a nail tenant with no ACTIVE services
	//       → an empty slice, not an error
	GetCatalog(ctx context.Context, slug string) (*PublicCatalog, error)
}

type publicCatalogService struct {
	tenants    PublicTenantResolver
	services   repository.ServiceRepository
	categories repository.ServiceCategoryRepository
}

func NewPublicCatalogService(tenants PublicTenantResolver, services repository.ServiceRepository, categories repository.ServiceCategoryRepository) PublicCatalogService {
	return &publicCatalogService{tenants: tenants, services: services, categories: categories}
}

func (s *publicCatalogService) GetCatalog(ctx context.Context, slug string) (*PublicCatalog, error) {
	resolved, err := s.tenants.ResolvePublicTenant(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Vertical guard. business_type is already part of the public tenant
	// identity, so refusing here discloses nothing new — it just stops the
	// appointment catalog being presented as though it were a hotel's or a
	// restaurant's public resource.
	if resolved.BusinessType == nil || *resolved.BusinessType != tenantmodel.BusinessTypeNailTechnician {
		return nil, apperrors.New(apperrors.CodeResourceNotFound, "no public service catalog for this business type", nil)
	}

	if _, err := uuid.Parse(resolved.TenantID); err != nil {
		// The resolver handed back a malformed internal id — a data fault, not
		// anything the anonymous caller did.
		return nil, apperrors.New(apperrors.CodeInternalError, "resolved tenant id is invalid", err)
	}

	// Only ACTIVE services are offered for booking; ARCHIVED ones have stopped
	// being sold and must never appear on the public page. There is no
	// separate "active but internal-only" flag in S1 — every ACTIVE service is
	// considered publicly bookable (see the S9 contract notes).
	active := model.StatusActive
	stored, err := s.services.ListByTenant(ctx, resolved.TenantID, repository.ServiceListFilter{Status: &active})
	if err != nil {
		return nil, err
	}

	// Resolved once for the whole listing rather than per service, so an
	// N-service catalog costs one extra query, not N — and skipped entirely
	// when nothing in this listing references a category, so a tenant with
	// no categories yet (or a caller with no category repository configured)
	// never pays for or requires the lookup. Every status is included, not
	// just ACTIVE: a service can stay ACTIVE while its category is archived
	// (see model.ServiceCategory's own doc comment), and its name should
	// still resolve for that service's row.
	var categoryNames map[string]string
	if hasCategorizedService(stored) {
		categoryNames, err = s.categoryNamesByID(ctx, resolved.TenantID)
		if err != nil {
			return nil, err
		}
	}

	views := make([]PublicServiceView, 0, len(stored))
	for _, svc := range stored {
		var category *string
		if svc.CategoryID != nil {
			if name, ok := categoryNames[*svc.CategoryID]; ok {
				category = &name
			}
			// A miss here means a category referenced by a service could not
			// be resolved — defensively treated as uncategorised for display
			// rather than failing the whole public catalog.
		}
		views = append(views, PublicServiceView{
			ID:              svc.ID,
			Name:            svc.Name,
			Description:     svc.Description,
			DurationMinutes: svc.DurationMinutes,
			PriceMinor:      svc.PriceMinor,
			Category:        category,
		})
	}
	return &PublicCatalog{Currency: resolved.Currency, Services: views}, nil
}

// hasCategorizedService reports whether any service in the listing carries a
// CategoryID, so GetCatalog can skip the category lookup entirely when there
// is nothing for it to resolve.
func hasCategorizedService(stored []*model.Service) bool {
	for _, svc := range stored {
		if svc.CategoryID != nil {
			return true
		}
	}
	return false
}

// categoryNamesByID loads every one of the tenant's categories, regardless of
// status, into an id -> name lookup for GetCatalog's single pass over the
// service list.
func (s *publicCatalogService) categoryNamesByID(ctx context.Context, tenantID string) (map[string]string, error) {
	categories, err := s.categories.ListByTenant(ctx, tenantID, repository.ServiceCategoryListFilter{})
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(categories))
	for _, category := range categories {
		names[category.ID] = category.Name
	}
	return names, nil
}

// compile-time guards.
var (
	_ PublicCatalogService = (*publicCatalogService)(nil)
	_ PublicTenantResolver = tenantservice.PublicTenantService(nil)
)
