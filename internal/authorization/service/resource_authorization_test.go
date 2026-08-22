package service

import (
	"context"
	"testing"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

const (
	idorUserID   = "550e8400-e29b-41d4-a716-446655440301"
	idorTenantA  = "550e8400-e29b-41d4-a716-446655440302"
	idorTenantB  = "550e8400-e29b-41d4-a716-446655440303"
	idorResource = "550e8400-e29b-41d4-a716-446655440304"
)

// TestRequireResourceTenantAllowsSameTenantResource proves that permission
// alone is not the whole authorization decision for tenant-owned resources:
// the resource's own tenant must match the trusted scope.
func TestRequireResourceTenantAllowsSameTenantResource(t *testing.T) {
	if err := RequireResourceTenant(idorTenantA, idorTenantA); err != nil {
		t.Fatalf("RequireResourceTenant() = %v, want nil", err)
	}
}

// TestRequireResourceTenantDeniesCrossTenantResourceAsNotFound proves the
// approved non-disclosure policy: a resource that exists but belongs to
// another tenant is reported as RESOURCE_NOT_FOUND, not as a permission
// failure that would confirm its existence.
func TestRequireResourceTenantDeniesCrossTenantResourceAsNotFound(t *testing.T) {
	err := RequireResourceTenant(idorTenantB, idorTenantA)
	assertAuthzCode(t, err, apperrors.CodeResourceNotFound)
}

func TestRequireResourceTenantDeniesEmptyResourceTenant(t *testing.T) {
	err := RequireResourceTenant("", idorTenantA)
	assertAuthzCode(t, err, apperrors.CodeResourceNotFound)
}

// TestIDORResourceLookupUsesTrustedTenantScope demonstrates the full,
// required contract for a tenant-owned resource: knowing the resource's
// UUID alone must never grant access. A repository that scopes its lookup
// by trusted tenant ID (FindByIDAndTenant) returns nothing for a
// cross-tenant ID even though the row exists.
func TestIDORResourceLookupUsesTrustedTenantScope(t *testing.T) {
	repo := &fakeTenantResourceRepository{resources: map[string]fakeTenantResource{
		idorResource: {ID: idorResource, TenantID: idorTenantB, Name: "secret"},
	}}
	resolver := &resolverFake{tenantPermissions: map[string][]string{idorTenantA: {"user.read"}}}
	authorizer := NewAuthorizer(resolver)
	principal := auth.Principal{UserID: idorUserID}
	trusted := tenant.TenantContext{TenantID: idorTenantA}

	if err := authorizer.RequireTenantPermission(context.Background(), principal, trusted, "user.read"); err != nil {
		t.Fatalf("actor should have the required permission: %v", err)
	}

	resource, err := repo.FindByIDAndTenant(context.Background(), idorResource, trusted.TenantID)
	if err != nil {
		t.Fatalf("FindByIDAndTenant() error = %v", err)
	}
	if resource != nil {
		t.Fatalf("cross-tenant resource must not be returned: %#v", resource)
	}
}

func TestIDORResourceLookupSucceedsForOwningTenant(t *testing.T) {
	repo := &fakeTenantResourceRepository{resources: map[string]fakeTenantResource{
		idorResource: {ID: idorResource, TenantID: idorTenantA, Name: "visible"},
	}}

	resource, err := repo.FindByIDAndTenant(context.Background(), idorResource, idorTenantA)
	if err != nil || resource == nil || resource.Name != "visible" {
		t.Fatalf("FindByIDAndTenant() = %#v, %v; want the owning tenant's resource", resource, err)
	}
}

type fakeTenantResource struct {
	ID, TenantID, Name string
}

// fakeTenantResourceRepository is a stand-in for a future tenant-owned
// repository. It exists only to prove the required architectural pattern:
// resource lookups must be scoped by trusted tenant ID at the query layer,
// never validated only after an unscoped global lookup.
type fakeTenantResourceRepository struct {
	resources map[string]fakeTenantResource
}

func (r *fakeTenantResourceRepository) FindByIDAndTenant(_ context.Context, id, tenantID string) (*fakeTenantResource, error) {
	resource, ok := r.resources[id]
	if !ok || resource.TenantID != tenantID {
		return nil, nil
	}
	return &resource, nil
}
