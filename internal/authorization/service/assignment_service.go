package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/techagentng/saas-monolith/internal/authorization/model"
	"github.com/techagentng/saas-monolith/internal/authorization/repository"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identityrepository "github.com/techagentng/saas-monolith/internal/identity/repository"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
)

type AssignInput struct{ UserID, RoleID, TenantID string }
type AssignmentService interface {
	Assign(context.Context, AssignInput) (*model.UserRole, error)
	Remove(context.Context, string) error
}
type assignmentService struct {
	users       identityrepository.UserRepository
	tenants     tenantrepository.TenantRepository
	roles       repository.RoleRepository
	assignments repository.UserRoleRepository
	memberships tenantrepository.MembershipRepository
}

func NewAssignmentService(users identityrepository.UserRepository, tenants tenantrepository.TenantRepository, roles repository.RoleRepository, assignments repository.UserRoleRepository, memberships tenantrepository.MembershipRepository) AssignmentService {
	return &assignmentService{users, tenants, roles, assignments, memberships}
}
func (s *assignmentService) Assign(ctx context.Context, input AssignInput) (*model.UserRole, error) {
	if _, err := uuid.Parse(input.UserID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}
	if _, err := uuid.Parse(input.RoleID); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
	}
	user, err := s.users.FindByID(ctx, input.UserID)
	if err != nil || user == nil {
		return nil, fmt.Errorf("validating role user: %w", err)
	}
	role, err := s.roles.FindByID(ctx, input.RoleID)
	if err != nil || role == nil {
		return nil, fmt.Errorf("finding role: %w", err)
	}
	assignment := model.UserRole{ID: uuid.NewString(), UserID: input.UserID, RoleID: input.RoleID}
	assignment.CreatedAt = time.Now().UTC()
	if role.Scope == model.ScopePlatform {
		if input.TenantID != "" {
			return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid platform assignment", nil)
		}
		assignment.Platform = true
	} else if role.Scope == model.ScopeTenant {
		if _, err := uuid.Parse(input.TenantID); err != nil {
			return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid request", err)
		}
		tenant, err := s.tenants.FindByID(ctx, input.TenantID)
		if err != nil || tenant == nil {
			return nil, fmt.Errorf("finding assignment tenant: %w", err)
		}
		if tenant.Status != tenantmodel.StatusActive {
			return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
		}
		membership, err := s.memberships.FindByTenantAndUser(ctx, input.TenantID, input.UserID)
		if err != nil {
			return nil, fmt.Errorf("finding membership: %w", err)
		}
		if membership == nil || membership.Status != tenantmodel.MembershipStatusActive {
			return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
		}
		assignment.TenantID = input.TenantID
	} else {
		return nil, apperrors.New(apperrors.CodeValidationFailed, "invalid role scope", nil)
	}
	return s.assignments.Assign(ctx, assignment)
}
func (s *assignmentService) Remove(ctx context.Context, id string) error {
	return s.assignments.Remove(ctx, id)
}
