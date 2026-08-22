package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	"github.com/techagentng/saas-monolith/internal/tenant/model"
	"github.com/techagentng/saas-monolith/internal/tenant/repository"
)

type CreateMembershipInput struct{ TenantID, UserID string }

type MembershipService interface {
	Create(ctx context.Context, input CreateMembershipInput) (*model.TenantMembership, error)
	Find(ctx context.Context, tenantID, userID string) (*model.TenantMembership, error)
	ListByUser(ctx context.Context, userID string) ([]model.TenantMembership, error)
	Revoke(ctx context.Context, tenantID, userID string) error
}

type membershipService struct {
	users       identityrepository.UserRepository
	tenants     repository.TenantRepository
	memberships repository.MembershipRepository
}

func NewMembershipService(users identityrepository.UserRepository, tenants repository.TenantRepository, memberships repository.MembershipRepository) MembershipService {
	return &membershipService{users: users, tenants: tenants, memberships: memberships}
}

func (s *membershipService) Create(ctx context.Context, input CreateMembershipInput) (*model.TenantMembership, error) {
	if err := validateIDs(input.TenantID, input.UserID); err != nil {
		return nil, err
	}
	if _, err := s.users.FindByID(ctx, input.UserID); err != nil {
		return nil, fmt.Errorf("validating membership user: %w", err)
	}
	if _, err := s.tenants.FindByID(ctx, input.TenantID); err != nil {
		return nil, fmt.Errorf("validating membership tenant: %w", err)
	}
	now := time.Now()
	return s.memberships.Create(ctx, model.TenantMembership{ID: uuid.NewString(), TenantID: input.TenantID, UserID: input.UserID, Status: model.MembershipStatusActive, CreatedAt: now, UpdatedAt: now})
}

func (s *membershipService) Find(ctx context.Context, tenantID, userID string) (*model.TenantMembership, error) {
	if err := validateIDs(tenantID, userID); err != nil {
		return nil, err
	}
	membership, err := s.memberships.FindByTenantAndUser(ctx, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("finding membership: %w", err)
	}
	return membership, nil
}

func (s *membershipService) ListByUser(ctx context.Context, userID string) ([]model.TenantMembership, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}
	memberships, err := s.memberships.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("listing memberships: %w", err)
	}
	return memberships, nil
}

func (s *membershipService) Revoke(ctx context.Context, tenantID, userID string) error {
	if err := validateIDs(tenantID, userID); err != nil {
		return err
	}
	if err := s.memberships.Disable(ctx, tenantID, userID, time.Now()); err != nil {
		return fmt.Errorf("revoking membership: %w", err)
	}
	return nil
}

func validateIDs(tenantID, userID string) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}
	if _, err := uuid.Parse(userID); err != nil {
		return apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}
	return nil
}
