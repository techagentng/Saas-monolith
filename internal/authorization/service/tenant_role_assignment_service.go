package service

import (
	"context"

	"github.com/techagentng/saas-monolith/internal/auth"
	"github.com/techagentng/saas-monolith/internal/authorization/model"
	"github.com/techagentng/saas-monolith/internal/authorization/repository"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

// assignableTenantRoleName is the only role name a tenant-scoped role.assign
// operation may target. This is the approved business rule: BUSINESS_OWNER
// (the only role holding role.assign) may assign STAFF only. It may never
// assign BUSINESS_OWNER (privilege duplication) or SUPER_ADMIN (a platform
// role; tenant operations must never grant platform authority).
//
// This is a deliberate, approved exception to "authorize via permission
// codes, not role names": the target role's identity is itself the business
// invariant being enforced here, not a shortcut around permission checks.
const assignableTenantRoleName = "STAFF"

// TenantRoleAssignmentService performs tenant role assignment as a
// service-level authorization boundary: the required role.assign permission
// and the target-role business restriction are enforced here so they cannot
// be bypassed by invoking the underlying Feature 5 AssignmentService from
// another entry point.
type TenantRoleAssignmentService interface {
	AssignTenantRole(ctx context.Context, actor auth.Principal, tenantContext tenant.TenantContext, targetUserID, targetRoleID string) (*model.UserRole, error)
}

type tenantRoleAssignmentService struct {
	authorizer  Authorizer
	roles       repository.RoleRepository
	assignments AssignmentService
}

func NewTenantRoleAssignmentService(authorizer Authorizer, roles repository.RoleRepository, assignments AssignmentService) TenantRoleAssignmentService {
	return &tenantRoleAssignmentService{authorizer: authorizer, roles: roles, assignments: assignments}
}

func (s *tenantRoleAssignmentService) AssignTenantRole(ctx context.Context, actor auth.Principal, tenantContext tenant.TenantContext, targetUserID, targetRoleID string) (*model.UserRole, error) {
	if err := s.authorizer.RequireTenantPermission(ctx, actor, tenantContext, "role.assign"); err != nil {
		return nil, err
	}
	role, err := s.roles.FindByID(ctx, targetRoleID)
	if err != nil {
		return nil, err
	}
	if role == nil || role.Scope != model.ScopeTenant || role.Name != assignableTenantRoleName {
		return nil, apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)
	}
	return s.assignments.Assign(ctx, AssignInput{UserID: targetUserID, RoleID: targetRoleID, TenantID: tenantContext.TenantID})
}
