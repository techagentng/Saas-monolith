package service

import (
	"context"
	"sort"

	"github.com/techagentng/saas-monolith/internal/authorization/model"
	"github.com/techagentng/saas-monolith/internal/authorization/repository"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
	tenantrepository "github.com/techagentng/saas-monolith/internal/tenant/repository"
)

type PermissionResolutionService interface {
	ResolvePlatform(context.Context, string) ([]string, error)
	ResolveTenant(context.Context, string, string) ([]string, error)
}
type permissionResolutionService struct {
	roles           repository.UserRoleRepository
	rolePermissions repository.RolePermissionRepository
	memberships     tenantrepository.MembershipRepository
	tenants         tenantrepository.TenantRepository
}

func NewPermissionResolutionService(roles repository.UserRoleRepository, rolePermissions repository.RolePermissionRepository, memberships tenantrepository.MembershipRepository, tenants tenantrepository.TenantRepository) PermissionResolutionService {
	return &permissionResolutionService{roles, rolePermissions, memberships, tenants}
}
func (s *permissionResolutionService) ResolvePlatform(ctx context.Context, userID string) ([]string, error) {
	return s.resolve(ctx, userID, model.ScopePlatform, "")
}
func (s *permissionResolutionService) ResolveTenant(ctx context.Context, userID, tenantID string) ([]string, error) {
	tenant, err := s.tenants.FindByID(ctx, tenantID)
	if err != nil || tenant == nil || tenant.Status != tenantmodel.StatusActive {
		return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", err)
	}
	membership, err := s.memberships.FindByTenantAndUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if membership == nil || membership.Status != tenantmodel.MembershipStatusActive {
		return nil, apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
	}
	return s.resolve(ctx, userID, model.ScopeTenant, tenantID)
}
func (s *permissionResolutionService) resolve(ctx context.Context, userID string, scope model.Scope, tenantID string) ([]string, error) {
	roles, err := s.roles.ListByUserScope(ctx, userID, scope, tenantID)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, role := range roles {
		permissions, err := s.rolePermissions.ListByRole(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		for _, permission := range permissions {
			set[permission.Code] = true
		}
	}
	result := make([]string, 0, len(set))
	for code := range set {
		result = append(result, code)
	}
	sort.Strings(result)
	return result, nil
}
