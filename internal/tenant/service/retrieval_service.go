package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

// RetrievalService provides tenant discovery and retrieval operations.
type RetrievalService interface {
	// ListAccessible returns all ACTIVE tenants where the user has an ACTIVE membership,
	// ordered by created_at, id. Does not require any specific permissions.
	ListAccessible(ctx context.Context, userID string) ([]*model.Tenant, error)

	// GetAccessible returns a single tenant if the user has an ACTIVE membership in it.
	// Both the tenant and membership must be ACTIVE to succeed.
	GetAccessible(ctx context.Context, userID, tenantID string) (*model.Tenant, error)
}

type retrievalService struct {
	tenants repository.TenantRepository
}

func NewRetrievalService(tenants repository.TenantRepository) RetrievalService {
	return &retrievalService{tenants: tenants}
}

func (s *retrievalService) ListAccessible(ctx context.Context, userID string) ([]*model.Tenant, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}
	tenants, err := s.tenants.ListAccessibleByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("listing accessible tenants: %w", err)
	}
	// Return empty slice (not nil) for consistency with JSON marshaling
	if tenants == nil {
		tenants = []*model.Tenant{}
	}
	return tenants, nil
}

func (s *retrievalService) GetAccessible(ctx context.Context, userID, tenantID string) (*model.Tenant, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}

	// The repository's ListAccessibleByUserID already enforces both tenant and membership
	// access control. We retrieve the specific tenant from that list to verify access.
	tenants, err := s.tenants.ListAccessibleByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("verifying tenant access: %w", err)
	}

	for _, t := range tenants {
		if t.ID == tenantID {
			return t, nil
		}
	}

	// User either has no membership, membership is inactive, or tenant doesn't exist/is disabled.
	// Return 403 for all cases (enumeration protection).
	return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
}
