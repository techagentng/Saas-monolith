package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

type TenantContext struct{ TenantID string }

type TenantContextService interface {
	Resolve(ctx context.Context, principal auth.Principal, candidateTenantID string) (*TenantContext, error)
}

type tenantContextService struct {
	tenants     repository.TenantRepository
	memberships repository.MembershipRepository
}

func NewTenantContextService(tenants repository.TenantRepository, memberships repository.MembershipRepository) TenantContextService {
	return &tenantContextService{tenants: tenants, memberships: memberships}
}

func (s *tenantContextService) Resolve(ctx context.Context, principal auth.Principal, candidateTenantID string) (*TenantContext, error) {
	if _, err := uuid.Parse(principal.UserID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidCredentials, "invalid credentials", err)
	}
	if _, err := uuid.Parse(candidateTenantID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}
	tenant, err := s.tenants.FindByID(ctx, candidateTenantID)
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) && appErr.Code == apperrors.CodeTenantNotFound {
			return nil, accessDenied(err)
		}
		return nil, fmt.Errorf("finding tenant for context: %w", err)
	}
	if tenant == nil || tenant.Status != model.StatusActive {
		return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", err)
	}
	membership, err := s.memberships.FindByTenantAndUser(ctx, candidateTenantID, principal.UserID)
	if err != nil {
		return nil, fmt.Errorf("finding membership for context: %w", err)
	}
	if membership == nil || membership.TenantID != candidateTenantID || membership.UserID != principal.UserID || membership.Status != model.MembershipStatusActive {
		return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
	}
	return &TenantContext{TenantID: candidateTenantID}, nil
}

func accessDenied(cause error) error {
	if cause == nil {
		return apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
	}
	return fmt.Errorf("tenant context verification: %w", apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", cause))
}
