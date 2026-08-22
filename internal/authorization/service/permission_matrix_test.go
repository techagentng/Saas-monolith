package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	authmodel "github.com/techagentng/saas-monolith/internal/authorization/model"
	tenantmodel "github.com/techagentng/saas-monolith/internal/tenant/model"
)

func TestApprovedPermissionMatrixResolvesExactSets(t *testing.T) {
	resolution := NewPermissionResolutionService(&matrixUserRoles{}, &matrixRolePermissions{}, &matrixMemberships{active: true}, &matrixTenants{})

	tests := []struct {
		name, tenant string
		want         []string
	}{
		{"business owner tenant A", "tenant-a", []string{"permission.read", "role.assign", "role.read", "tenant.read", "tenant.update", "user.create", "user.disable", "user.read", "user.update"}},
		{"staff tenant A", "tenant-a-staff", []string{"permission.read", "role.read", "tenant.read", "user.read"}},
		{"staff tenant B", "tenant-b", []string{"permission.read", "role.read", "tenant.read", "user.read"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolution.ResolveTenant(context.Background(), userID, test.tenant)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ResolveTenant() = %#v, %v; want %#v", got, err, test.want)
			}
		})
	}
}

func TestApprovedPermissionMatrixExcludesForbiddenMutations(t *testing.T) {
	resolution := NewPermissionResolutionService(&matrixUserRoles{}, &matrixRolePermissions{}, &matrixMemberships{active: true}, &matrixTenants{})
	got, err := resolution.ResolveTenant(context.Background(), userID, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"role.create", "role.update", "role.delete", "permission.assign"} {
		for _, code := range got {
			if code == forbidden {
				t.Fatalf("business owner received %q", forbidden)
			}
		}
	}
}

func TestRevokedMembershipRemovesTenantPermissions(t *testing.T) {
	memberships := &matrixMemberships{active: false}
	resolution := NewPermissionResolutionService(&matrixUserRoles{}, &matrixRolePermissions{}, memberships, &matrixTenants{})
	if _, err := resolution.ResolveTenant(context.Background(), userID, "tenant-a"); err == nil {
		t.Fatal("revoked membership returned permissions")
	}
}

type matrixUserRoles struct{}

func (*matrixUserRoles) Assign(context.Context, authmodel.UserRole) (*authmodel.UserRole, error) {
	return nil, nil
}
func (*matrixUserRoles) Remove(context.Context, string) error { return nil }
func (*matrixUserRoles) ListByUserScope(_ context.Context, _ string, _ authmodel.Scope, tenant string) ([]authmodel.Role, error) {
	if tenant == "tenant-a" {
		return []authmodel.Role{{ID: "owner-role"}}, nil
	}
	return []authmodel.Role{{ID: "staff-role"}}, nil
}

type matrixRolePermissions struct{}

func (*matrixRolePermissions) Assign(context.Context, authmodel.RolePermission) error { return nil }
func (*matrixRolePermissions) Remove(context.Context, string, string) error           { return nil }
func (*matrixRolePermissions) ListByRole(_ context.Context, roleID string) ([]authmodel.Permission, error) {
	if roleID == "owner-role" {
		return []authmodel.Permission{{Code: "user.read"}, {Code: "user.create"}, {Code: "user.update"}, {Code: "user.disable"}, {Code: "tenant.read"}, {Code: "tenant.update"}, {Code: "role.read"}, {Code: "role.assign"}, {Code: "permission.read"}}, nil
	}
	return []authmodel.Permission{{Code: "tenant.read"}, {Code: "user.read"}, {Code: "role.read"}, {Code: "permission.read"}}, nil
}

type matrixMemberships struct{ active bool }

func (m *matrixMemberships) Create(context.Context, tenantmodel.TenantMembership) (*tenantmodel.TenantMembership, error) {
	return nil, nil
}
func (m *matrixMemberships) FindByTenantAndUser(context.Context, string, string) (*tenantmodel.TenantMembership, error) {
	if !m.active {
		return nil, nil
	}
	return &tenantmodel.TenantMembership{Status: tenantmodel.MembershipStatusActive}, nil
}
func (*matrixMemberships) ListByUser(context.Context, string) ([]tenantmodel.TenantMembership, error) {
	return nil, nil
}
func (*matrixMemberships) Disable(context.Context, string, string, time.Time) error { return nil }

type matrixTenants struct{}

func (*matrixTenants) FindByID(context.Context, string) (*tenantmodel.Tenant, error) {
	return &tenantmodel.Tenant{Status: tenantmodel.StatusActive}, nil
}
