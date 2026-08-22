package service

import (
	"context"
	"errors"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

const (
	authzUserID  = "550e8400-e29b-41d4-a716-446655440101"
	authzTenantA = "550e8400-e29b-41d4-a716-446655440102"
	authzTenantB = "550e8400-e29b-41d4-a716-446655440103"
)

// --- Tenant-scoped authorization -------------------------------------------------

func TestRequireTenantPermissionAllowsWhenGranted(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"user.read"}}}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequireTenantPermission(context.Background(), auth.Principal{UserID: authzUserID}, tenant.TenantContext{TenantID: authzTenantA}, "user.read")
	if err != nil {
		t.Fatalf("RequireTenantPermission() = %v, want nil", err)
	}
}

func TestRequireTenantPermissionDeniesWhenMissing(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"user.read"}}}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequireTenantPermission(context.Background(), auth.Principal{UserID: authzUserID}, tenant.TenantContext{TenantID: authzTenantA}, "tenant.update")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestRequireTenantPermissionDoesNotLeakAcrossTenants(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"user.read"}}}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequireTenantPermission(context.Background(), auth.Principal{UserID: authzUserID}, tenant.TenantContext{TenantID: authzTenantB}, "user.read")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestRequireTenantPermissionDeniesMissingTenantContext(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"user.read"}}}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequireTenantPermission(context.Background(), auth.Principal{UserID: authzUserID}, tenant.TenantContext{}, "user.read")
	assertAuthzCode(t, err, apperrors.CodeTenantAccessDenied)
}

func TestRequireTenantPermissionDeniesMissingPrincipal(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"user.read"}}}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequireTenantPermission(context.Background(), auth.Principal{}, tenant.TenantContext{TenantID: authzTenantA}, "user.read")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestRequireTenantPermissionPropagatesTenantAccessDenied(t *testing.T) {
	resolver := &resolverFake{tenantErr: apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequireTenantPermission(context.Background(), auth.Principal{UserID: authzUserID}, tenant.TenantContext{TenantID: authzTenantA}, "user.read")
	assertAuthzCode(t, err, apperrors.CodeTenantAccessDenied)
}

func TestRequireTenantPermissionFailsClosedOnResolverError(t *testing.T) {
	resolver := &resolverFake{tenantErr: errors.New("db unavailable")}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequireTenantPermission(context.Background(), auth.Principal{UserID: authzUserID}, tenant.TenantContext{TenantID: authzTenantA}, "user.read")
	if err == nil {
		t.Fatal("resolver error must not authorize")
	}
	assertAuthzCode(t, err, apperrors.CodeServiceUnavailable)
}

func TestRequireTenantPermissionReflectsRevokedMembershipOnNextCall(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"tenant.update"}}}
	authorizer := NewAuthorizer(resolver)
	principal := auth.Principal{UserID: authzUserID}
	tenantCtx := tenant.TenantContext{TenantID: authzTenantA}

	if err := authorizer.RequireTenantPermission(context.Background(), principal, tenantCtx, "tenant.update"); err != nil {
		t.Fatalf("first authorization = %v, want nil", err)
	}

	resolver.tenantErr = apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
	resolver.tenantPermissions = nil

	err := authorizer.RequireTenantPermission(context.Background(), principal, tenantCtx, "tenant.update")
	assertAuthzCode(t, err, apperrors.CodeTenantAccessDenied)
}

func TestRequireTenantPermissionReflectsRoleRemovalOnNextCall(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"role.assign"}}}
	authorizer := NewAuthorizer(resolver)
	principal := auth.Principal{UserID: authzUserID}
	tenantCtx := tenant.TenantContext{TenantID: authzTenantA}

	if err := authorizer.RequireTenantPermission(context.Background(), principal, tenantCtx, "role.assign"); err != nil {
		t.Fatalf("first authorization = %v, want nil", err)
	}

	resolver.tenantPermissions = map[string][]string{authzTenantA: {}}

	err := authorizer.RequireTenantPermission(context.Background(), principal, tenantCtx, "role.assign")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestRequireTenantPermissionReflectsPermissionRemovalOnNextCall(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"tenant.update", "tenant.read"}}}
	authorizer := NewAuthorizer(resolver)
	principal := auth.Principal{UserID: authzUserID}
	tenantCtx := tenant.TenantContext{TenantID: authzTenantA}

	if err := authorizer.RequireTenantPermission(context.Background(), principal, tenantCtx, "tenant.update"); err != nil {
		t.Fatalf("first authorization = %v, want nil", err)
	}

	resolver.tenantPermissions = map[string][]string{authzTenantA: {"tenant.read"}}

	err := authorizer.RequireTenantPermission(context.Background(), principal, tenantCtx, "tenant.update")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

// --- Platform-scoped authorization -----------------------------------------------

func TestRequirePlatformPermissionAllowsSuperAdmin(t *testing.T) {
	resolver := &resolverFake{platformPermissions: []string{"role.read", "permission.assign"}}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequirePlatformPermission(context.Background(), auth.Principal{UserID: authzUserID}, "permission.assign")
	if err != nil {
		t.Fatalf("RequirePlatformPermission() = %v, want nil", err)
	}
}

func TestRequirePlatformPermissionDeniesTenantOnlyRole(t *testing.T) {
	resolver := &resolverFake{platformPermissions: nil}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequirePlatformPermission(context.Background(), auth.Principal{UserID: authzUserID}, "role.read")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestRequirePlatformPermissionDeniesNoPlatformRole(t *testing.T) {
	resolver := &resolverFake{}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequirePlatformPermission(context.Background(), auth.Principal{UserID: authzUserID}, "role.read")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestRequirePlatformPermissionDeniesMissingPrincipal(t *testing.T) {
	resolver := &resolverFake{platformPermissions: []string{"role.read"}}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequirePlatformPermission(context.Background(), auth.Principal{}, "role.read")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestRequirePlatformPermissionFailsClosedOnResolverError(t *testing.T) {
	resolver := &resolverFake{platformErr: errors.New("db unavailable")}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequirePlatformPermission(context.Background(), auth.Principal{UserID: authzUserID}, "role.read")
	assertAuthzCode(t, err, apperrors.CodeServiceUnavailable)
}

func TestTenantPermissionNeverSatisfiesPlatformPermission(t *testing.T) {
	// BUSINESS_OWNER: rich tenant permission set, zero platform assignments.
	resolver := &resolverFake{
		tenantPermissions:   map[string][]string{authzTenantA: {"user.read", "user.create", "user.update", "user.disable", "tenant.read", "tenant.update", "role.read", "role.assign", "permission.read"}},
		platformPermissions: nil,
	}
	authorizer := NewAuthorizer(resolver)

	if err := authorizer.RequireTenantPermission(context.Background(), auth.Principal{UserID: authzUserID}, tenant.TenantContext{TenantID: authzTenantA}, "tenant.update"); err != nil {
		t.Fatalf("tenant permission should be granted: %v", err)
	}
	err := authorizer.RequirePlatformPermission(context.Background(), auth.Principal{UserID: authzUserID}, "tenant.update")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func TestTenantMembershipNeverCreatesPlatformAuthority(t *testing.T) {
	// STAFF: tenant permissions present, no platform role assigned.
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"tenant.read", "user.read", "role.read", "permission.read"}}}
	authorizer := NewAuthorizer(resolver)

	for _, code := range []string{"tenant.read", "user.read", "role.read", "permission.read"} {
		if err := authorizer.RequirePlatformPermission(context.Background(), auth.Principal{UserID: authzUserID}, code); err == nil {
			t.Fatalf("platform permission %q must not be satisfied by tenant membership", code)
		}
	}
}

func TestTenantContextPresenceDoesNotGrantPlatformAuthority(t *testing.T) {
	resolver := &resolverFake{tenantPermissions: map[string][]string{authzTenantA: {"user.read"}}}
	authorizer := NewAuthorizer(resolver)

	err := authorizer.RequirePlatformPermission(context.Background(), auth.Principal{UserID: authzUserID}, "user.read")
	assertAuthzCode(t, err, apperrors.CodePermissionDenied)
}

func assertAuthzCode(t *testing.T, err error, expected apperrors.ErrorCode) {
	t.Helper()
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != expected {
		t.Fatalf("error = %v, want code %q", err, expected)
	}
}

// --- fakes -------------------------------------------------------------------------

type resolverFake struct {
	tenantPermissions   map[string][]string
	tenantErr           error
	platformPermissions []string
	platformErr         error
}

func (r *resolverFake) ResolveTenant(_ context.Context, _ string, tenantID string) ([]string, error) {
	if r.tenantErr != nil {
		return nil, r.tenantErr
	}
	return r.tenantPermissions[tenantID], nil
}

func (r *resolverFake) ResolvePlatform(_ context.Context, _ string) ([]string, error) {
	if r.platformErr != nil {
		return nil, r.platformErr
	}
	return r.platformPermissions, nil
}
