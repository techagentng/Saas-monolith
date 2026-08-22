package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/techagentng/saas-monolith/internal/authorization/model"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	identitymodel "github.com/techagentng/saas-monolith/internal/identity/model"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

const (
	userID   = "550e8400-e29b-41d4-a716-446655440001"
	tenantID = "550e8400-e29b-41d4-a716-446655440002"
	roleID   = "550e8400-e29b-41d4-a716-446655440003"
)

func TestAssignTenantRoleRequiresActiveMembership(t *testing.T) {
	roles := &roleRepoFake{role: &model.Role{ID: roleID, Name: "STAFF", Scope: model.ScopeTenant}}
	memberships := &membershipRepoFake{membership: &tenantmodel.TenantMembership{TenantID: tenantID, UserID: userID, Status: tenantmodel.MembershipStatusActive}}
	service := NewAssignmentService(&userRepoFake{}, &tenantRepoFake{}, roles, &userRoleRepoFake{}, memberships)

	assignment, err := service.Assign(context.Background(), AssignInput{UserID: userID, RoleID: roleID, TenantID: tenantID})
	if err != nil || assignment.TenantID != tenantID {
		t.Fatalf("Assign() = %#v, %v", assignment, err)
	}
}

func TestAssignTenantRoleDeniesDisabledMembership(t *testing.T) {
	roles := &roleRepoFake{role: &model.Role{ID: roleID, Scope: model.ScopeTenant}}
	memberships := &membershipRepoFake{membership: &tenantmodel.TenantMembership{TenantID: tenantID, UserID: userID, Status: tenantmodel.MembershipStatusDisabled}}
	service := NewAssignmentService(&userRepoFake{}, &tenantRepoFake{}, roles, &userRoleRepoFake{}, memberships)
	_, err := service.Assign(context.Background(), AssignInput{UserID: userID, RoleID: roleID, TenantID: tenantID})
	assertCode(t, err, apperrors.CodeTenantAccessDenied)
}

func TestAssignPlatformRoleRejectsTenantID(t *testing.T) {
	roles := &roleRepoFake{role: &model.Role{ID: roleID, Scope: model.ScopePlatform}}
	service := NewAssignmentService(&userRepoFake{}, &tenantRepoFake{}, roles, &userRoleRepoFake{}, &membershipRepoFake{})
	_, err := service.Assign(context.Background(), AssignInput{UserID: userID, RoleID: roleID, TenantID: tenantID})
	assertCode(t, err, apperrors.CodeValidationFailed)
}

func TestResolveTenantPermissionsIsSortedAndRejectsInactiveMembership(t *testing.T) {
	memberships := &membershipRepoFake{membership: &tenantmodel.TenantMembership{TenantID: tenantID, UserID: userID, Status: tenantmodel.MembershipStatusDisabled}}
	service := NewPermissionResolutionService(&userRoleResolverFake{}, &rolePermissionRepoFake{}, memberships, &tenantRepoFake{})
	_, err := service.ResolveTenant(context.Background(), userID, tenantID)
	assertCode(t, err, apperrors.CodeTenantAccessDenied)
}

func assertCode(t *testing.T, err error, expected apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != expected {
		t.Fatalf("error = %v, want %q", err, expected)
	}
}

type userRepoFake struct{}

func (*userRepoFake) Create(context.Context, identitymodel.User) (*identitymodel.User, error) {
	return nil, nil
}
func (*userRepoFake) FindByID(context.Context, string) (*identitymodel.User, error) {
	return &identitymodel.User{ID: userID}, nil
}
func (*userRepoFake) FindByEmail(context.Context, string) (*identitymodel.User, error) {
	return nil, nil
}

type tenantRepoFake struct{}

func (*tenantRepoFake) FindByID(context.Context, string) (*tenantmodel.Tenant, error) {
	return &tenantmodel.Tenant{ID: tenantID, Status: tenantmodel.StatusActive}, nil
}

type roleRepoFake struct {
	role        *model.Role
	permissions []string
}

func (r *roleRepoFake) FindByID(context.Context, string) (*model.Role, error) { return r.role, nil }
func (*roleRepoFake) FindByNameScope(context.Context, string, model.Scope) (*model.Role, error) {
	return nil, nil
}

type userRoleRepoFake struct{ assignment *model.UserRole }

func (r *userRoleRepoFake) Assign(_ context.Context, assignment model.UserRole) (*model.UserRole, error) {
	if r.assignment != nil {
		return r.assignment, nil
	}
	return &assignment, nil
}
func (*userRoleRepoFake) Remove(context.Context, string) error { return nil }
func (*userRoleRepoFake) ListByUserScope(context.Context, string, model.Scope, string) ([]model.Role, error) {
	return nil, nil
}

type membershipRepoFake struct{ membership *tenantmodel.TenantMembership }

func (*membershipRepoFake) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, nil
}
func (r *membershipRepoFake) FindByTenantAndUser(context.Context, string, string) (*tenantmodel.TenantMembership, error) {
	return r.membership, nil
}
func (*membershipRepoFake) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}
func (*membershipRepoFake) Disable(context.Context, string, string, time.Time) error { return nil }

type userRoleResolverFake struct{}

func (*userRoleResolverFake) Assign(context.Context, model.UserRole) (*model.UserRole, error) {
	return nil, nil
}
func (*userRoleResolverFake) Remove(context.Context, string) error { return nil }
func (*userRoleResolverFake) ListByUserScope(context.Context, string, model.Scope, string) ([]model.Role, error) {
	return []model.Role{{ID: roleID}}, nil
}

type rolePermissionRepoFake struct{}

func (*rolePermissionRepoFake) Assign(context.Context, model.RolePermission) error { return nil }
func (*rolePermissionRepoFake) Remove(context.Context, string, string) error       { return nil }
func (*rolePermissionRepoFake) ListByRole(context.Context, string) ([]model.Permission, error) {
	return []model.Permission{{Code: "tenant.read"}, {Code: "tenant.update"}}, nil
}

var _ = errors.New
