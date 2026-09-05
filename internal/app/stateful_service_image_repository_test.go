package app

import (
	"context"
	"sort"

	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	schedulingmodel "github.com/techagentng/saas-monolith/internal/scheduling/model"
	schedulingrepository "github.com/techagentng/saas-monolith/internal/scheduling/repository"
)

// statefulServiceImageRepository is the Service Images analog of
// statefulServiceRepository/statefulCategoryRepository: a small in-memory
// repository.ServiceImageRepository backing the real production services
// through the real middleware chain, for route/RBAC tests in this package.
type statefulServiceImageRepository struct {
	images     map[string]*schedulingmodel.ServiceImage
	writeCalls int
}

func newStatefulServiceImageRepository() *statefulServiceImageRepository {
	return &statefulServiceImageRepository{images: map[string]*schedulingmodel.ServiceImage{}}
}

func (r *statefulServiceImageRepository) Create(_ context.Context, image *schedulingmodel.ServiceImage) (*schedulingmodel.ServiceImage, error) {
	r.writeCalls++
	stored := *image
	r.images[stored.ID] = &stored
	return &stored, nil
}

func (r *statefulServiceImageRepository) FindByID(_ context.Context, tenantID string, imageID string) (*schedulingmodel.ServiceImage, error) {
	stored, ok := r.images[imageID]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
	}
	return stored, nil
}

func (r *statefulServiceImageRepository) ListByService(_ context.Context, tenantID string, serviceID string) ([]*schedulingmodel.ServiceImage, error) {
	var result []*schedulingmodel.ServiceImage
	for _, stored := range r.images {
		if stored.TenantID == tenantID && stored.ServiceID == serviceID {
			result = append(result, stored)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SortOrder < result[j].SortOrder })
	return result, nil
}

func (r *statefulServiceImageRepository) ListByServiceIDs(_ context.Context, tenantID string, serviceIDs []string) ([]*schedulingmodel.ServiceImage, error) {
	wanted := make(map[string]bool, len(serviceIDs))
	for _, id := range serviceIDs {
		wanted[id] = true
	}
	var result []*schedulingmodel.ServiceImage
	for _, stored := range r.images {
		if stored.TenantID == tenantID && wanted[stored.ServiceID] {
			result = append(result, stored)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ServiceID != result[j].ServiceID {
			return result[i].ServiceID < result[j].ServiceID
		}
		return result[i].SortOrder < result[j].SortOrder
	})
	return result, nil
}

func (r *statefulServiceImageRepository) Update(_ context.Context, tenantID string, imageID string, update schedulingrepository.ServiceImageUpdate) (*schedulingmodel.ServiceImage, error) {
	r.writeCalls++
	stored, ok := r.images[imageID]
	if !ok || stored.TenantID != tenantID {
		return nil, apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
	}
	if update.AltText != nil {
		stored.AltText = update.AltText
	}
	if update.IsPrimary != nil {
		stored.IsPrimary = *update.IsPrimary
	}
	return stored, nil
}

func (r *statefulServiceImageRepository) ClearPrimary(_ context.Context, tenantID string, serviceID string) error {
	for _, stored := range r.images {
		if stored.TenantID == tenantID && stored.ServiceID == serviceID {
			stored.IsPrimary = false
		}
	}
	return nil
}

func (r *statefulServiceImageRepository) Delete(_ context.Context, tenantID string, imageID string) error {
	r.writeCalls++
	stored, ok := r.images[imageID]
	if !ok || stored.TenantID != tenantID {
		return apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
	}
	delete(r.images, imageID)
	return nil
}

func (r *statefulServiceImageRepository) SetSortOrders(_ context.Context, tenantID string, serviceID string, orderedImageIDs []string) error {
	r.writeCalls++
	for index, id := range orderedImageIDs {
		stored, ok := r.images[id]
		if !ok || stored.TenantID != tenantID || stored.ServiceID != serviceID {
			return apperrors.New(apperrors.CodeImageNotFound, "service image not found", nil)
		}
		stored.SortOrder = index
	}
	return nil
}

var _ schedulingrepository.ServiceImageRepository = (*statefulServiceImageRepository)(nil)
