package service

import (
	"context"
	stderrors "errors"

	"github.com/techagentng/saas-monolith/internal/auth"
	apperrors "github.com/techagentng/saas-monolith/internal/errors"
	"github.com/techagentng/saas-monolith/internal/tenant"
)

// Authorizer answers: may this authenticated principal perform an action
// requiring the given permission code, within the given scope?
//
// Authorization is default-deny: any incomplete security fact (missing
// principal, missing tenant context, resolver failure) results in denial.
type Authorizer interface {
	RequireTenantPermission(ctx context.Context, principal auth.Principal, tenantContext tenant.TenantContext, permission string) error
	RequirePlatformPermission(ctx context.Context, principal auth.Principal, permission string) error
}

type authorizer struct {
	permissions PermissionResolutionService
}

// NewAuthorizer builds an Authorizer backed by Feature 5's server-side
// effective permission resolution. It resolves permissions on every call;
// it never caches a decision.
func NewAuthorizer(permissions PermissionResolutionService) Authorizer {
	return &authorizer{permissions: permissions}
}

func (a *authorizer) RequireTenantPermission(ctx context.Context, principal auth.Principal, tenantContext tenant.TenantContext, permission string) error {
	if principal.UserID == "" {
		return apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)
	}
	if tenantContext.TenantID == "" {
		return apperrors.New(apperrors.CodeTenantAccessDenied, "tenant access denied", nil)
	}
	granted, err := a.permissions.ResolveTenant(ctx, principal.UserID, tenantContext.TenantID)
	if err != nil {
		return failClosed(err)
	}
	if !containsPermission(granted, permission) {
		return apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)
	}
	return nil
}

func (a *authorizer) RequirePlatformPermission(ctx context.Context, principal auth.Principal, permission string) error {
	if principal.UserID == "" {
		return apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)
	}
	granted, err := a.permissions.ResolvePlatform(ctx, principal.UserID)
	if err != nil {
		return failClosed(err)
	}
	if !containsPermission(granted, permission) {
		return apperrors.New(apperrors.CodePermissionDenied, "permission denied", nil)
	}
	return nil
}

// failClosed preserves a known application error (e.g. TENANT_ACCESS_DENIED)
// as the safe deny reason, and maps any unexpected/infrastructure failure to
// a generic non-disclosing service error. It never allows access.
func failClosed(err error) error {
	var appErr *apperrors.AppError
	if stderrors.As(err, &appErr) {
		return err
	}
	return apperrors.New(apperrors.CodeServiceUnavailable, "authorization unavailable", err)
}

func containsPermission(granted []string, permission string) bool {
	for _, code := range granted {
		if code == permission {
			return true
		}
	}
	return false
}
