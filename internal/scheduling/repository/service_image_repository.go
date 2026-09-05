package repository

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/scheduling/model"
)

// ServiceImageUpdate carries a partial update to an image's own fields.
// SortOrder is deliberately absent — ordering is a whole-collection concern
// owned by ReorderImages, never a single-row PATCH, so there is nowhere for a
// client to express "move this one image" outside the bulk reorder endpoint.
type ServiceImageUpdate struct {
	AltText *string
	// IsPrimary, when non-nil and true, requests promoting this image.
	// ServiceImageRepository.Update does not itself demote whichever image
	// was previously primary — that unset-then-set sequencing is
	// ServiceImageService's responsibility (SetPrimary), the same layering
	// CatalogService owns archive-idempotency above ServiceRepository.Archive.
	// A false value is accepted structurally but is never sent by the
	// service layer today: "make this one NOT primary" with no replacement
	// is not a supported operation (see ServiceImageService.SetPrimary's doc
	// comment for why).
	IsPrimary *bool
}

// IsEmpty reports whether no field is set for update.
func (u *ServiceImageUpdate) IsEmpty() bool {
	return u.AltText == nil && u.IsPrimary == nil
}

// ServiceImageRepository is the persistence boundary for one service's
// uploaded images.
//
// Every method takes tenantID, and every implementation must filter on it —
// the same isolation mechanism ServiceRepository and ServiceCategoryRepository
// use. An image belonging to another tenant is reported exactly as a
// nonexistent one.
type ServiceImageRepository interface {
	Create(ctx context.Context, image *model.ServiceImage) (*model.ServiceImage, error)
	// FindByID resolves one image within one tenant (and, transitively via
	// the row's own ServiceID, one service — callers additionally check
	// ServiceID against the service named in the route, since a tenant can
	// have more than one service).
	FindByID(ctx context.Context, tenantID string, imageID string) (*model.ServiceImage, error)
	// ListByService returns one service's images in display order
	// (sort_order, then id for a deterministic tiebreak).
	ListByService(ctx context.Context, tenantID string, serviceID string) ([]*model.ServiceImage, error)
	// ListByServiceIDs is ListByService's batched form: every image for
	// every named service, in one query, so a caller rendering a whole
	// catalog (PublicCatalogService.GetCatalog) pays one extra query rather
	// than one per service. A nil/empty serviceIDs returns an empty slice
	// without querying — the same "skip the query when there is nothing to
	// resolve" discipline PublicCatalogService already applies to category
	// names.
	ListByServiceIDs(ctx context.Context, tenantID string, serviceIDs []string) ([]*model.ServiceImage, error)
	Update(ctx context.Context, tenantID string, imageID string, update ServiceImageUpdate) (*model.ServiceImage, error)
	// ClearPrimary unsets is_primary for every image on the service. Used by
	// ServiceImageService.SetPrimary immediately before setting the new
	// primary, inside one transaction, so the partial unique index
	// (service_images_service_primary_unique) is never asked to hold two
	// primaries at once even transiently.
	ClearPrimary(ctx context.Context, tenantID string, serviceID string) error
	// Delete removes the row. It does not touch the underlying storage
	// object — that is ServiceImageService's job (delete the DB row first,
	// then best-effort delete the object; see its doc comment for why that
	// order is the safe one).
	Delete(ctx context.Context, tenantID string, imageID string) error
	// SetSortOrders applies a full reorder atomically: order[i] is the image
	// id that should occupy position i. The caller (ServiceImageService) has
	// already validated the id set exactly matches the service's current
	// images — this method trusts that and simply writes.
	SetSortOrders(ctx context.Context, tenantID string, serviceID string, orderedImageIDs []string) error
}
