package service

import (
	"context"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	authmodel "github.com/techagentng/saas-monolith/internal/authorization/model"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

const (
	assignActorID  = "550e8400-e29b-41d4-a716-446655440201"
	assignTargetID = "550e8400-e29b-41d4-a716-446655440202"
	assignTenantID = "550e8400-e29b-41d4-a716-446655440203"
	staffRoleID    = "650e8400-e29b-41d4-a716-446655440003"
	ownerRoleID    = "650e8400-e29b-41d4-a716-446655440002"
	adminRoleID    = "650e8400-e29b-41d4-a716-446655440001"
)

func TestBusinessOwnerCanAssignStaff(t *testing.T) {
	svc := newTenantRoleAssignmentServiceForTest(t, "role.assign", &roleRepoFake{role: &authmodel.Role{ID: staffRoleID, Name: "STAFF", Scope: authmodel.ScopeTenant}})

	assignment, err := svc.AssignTenantRole(context.Background(), auth.Principal{UserID: assignActorID}, tenant.TenantContext{TenantID: assignTenantID}, assignTargetID, staffRoleID)
	if err != nil || assignment == nil {
		t.Fatalf("AssignTenantRole() = %#v, %v; want success", assignment, err)
	}
}

func TestBusinessOwnerCannotAssignBusinessOwner(t *testing.T) {
	svc := newTenantRoleAssignmentServiceForTest(t, "role.assign", &roleRepoFake{role: &authmodel.Role{ID: ownerRoleID, Name: "BUSINESS_OWNER", Scope: authmodel.ScopeTenant}})

	_, err := svc.AssignTenantRole(context.Background(), auth.Principal{UserID: assignActorID}, tenant.TenantContext{TenantID: assignTenantID}, assignTargetID, ownerRoleID)
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestBusinessOwnerCannotAssignSuperAdmin(t *testing.T) {
	svc := newTenantRoleAssignmentServiceForTest(t, "role.assign", &roleRepoFake{role: &authmodel.Role{ID: adminRoleID, Name: "SUPER_ADMIN", Scope: authmodel.ScopePlatform}})

	_, err := svc.AssignTenantRole(context.Background(), auth.Principal{UserID: assignActorID}, tenant.TenantContext{TenantID: assignTenantID}, assignTargetID, adminRoleID)
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestAssignTenantRoleRequiresRoleAssignPermission(t *testing.T) {
	// STAFF: no role.assign permission granted -> must be denied before role lookup happens.
	svc := newTenantRoleAssignmentServiceForTest(t, "", &roleRepoFake{role: &authmodel.Role{ID: staffRoleID, Name: "STAFF", Scope: authmodel.ScopeTenant}})

	_, err := svc.AssignTenantRole(context.Background(), auth.Principal{UserID: assignActorID}, tenant.TenantContext{TenantID: assignTenantID}, assignTargetID, staffRoleID)
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestAssignTenantRoleDeniesCrossTenantPermission(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{"other-tenant": {"role.assign"}}}
	svc := NewTenantRoleAssignmentService(NewAuthorizer(resolver), &roleRepoFake{role: &authmodel.Role{ID: staffRoleID, Name: "STAFF", Scope: authmodel.ScopeTenant}}, newTestAssignmentService())

	_, err := svc.AssignTenantRole(context.Background(), auth.Principal{UserID: assignActorID}, tenant.TenantContext{TenantID: assignTenantID}, assignTargetID, staffRoleID)
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func newTenantRoleAssignmentServiceForTest(t *testing.T, grantedPermission string, roles *roleRepoFake) TenantRoleAssignmentService {
	t.Helper()
	var permissions []string
	if grantedPermission != "" {
		permissions = []string{grantedPermission}
	}
	resolver := &resolverFake{tenantPermissions: map[string][]string{assignTenantID: permissions}}
	return NewTenantRoleAssignmentService(NewAuthorizer(resolver), roles, newTestAssignmentService())
}

func newTestAssignmentService() AssignmentService {
	roles := &roleRepoFake{role: &authmodel.Role{ID: staffRoleID, Name: "STAFF", Scope: authmodel.ScopeTenant}}
	memberships := &membershipRepoFake{membership: &tenantmodel.TenantMembership{TenantID: assignTenantID, UserID: assignTargetID, Status: tenantmodel.MembershipStatusActive}}
	return NewAssignmentService(&userRepoFake{}, &tenantRepoFake{}, roles, &userRoleRepoFake{}, memberships)
}
