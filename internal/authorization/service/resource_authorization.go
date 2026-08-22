package service

import apperrors "github.com/techagentng/saas-monolith/internal/errors"

// RequireResourceTenant enforces the mandatory second half of tenant-owned
// resource authorization: permission alone is not sufficient. The resource's
// actual tenant ownership must match the trusted tenant scope the request is
// operating within.
//
// A mismatch (including an unresolved/empty resource tenant) is reported as
// RESOURCE_NOT_FOUND rather than PERMISSION_DENIED so that cross-tenant
// resource existence is never disclosed to a caller who lacks access to it.
func RequireResourceTenant(resourceTenantID, trustedTenantID string) error {
	if resourceTenantID == "" || trustedTenantID == "" || resourceTenantID != trustedTenantID {
		return apperrors.New(apperrors.CodeResourceNotFound, "resource not found", nil)
	}
	return nil
}
