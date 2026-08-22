package tenant

import "context"

type contextKey struct{}

type TenantContext struct {
	TenantID string `json:"tenant_id"`
}

func WithContext(ctx context.Context, tenantContext TenantContext) context.Context {
	return context.WithValue(ctx, contextKey{}, tenantContext)
}
func FromContext(ctx context.Context) (TenantContext, bool) {
	value, ok := ctx.Value(contextKey{}).(TenantContext)
	return value, ok
}
