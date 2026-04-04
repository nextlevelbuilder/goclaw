package browser

import "context"

// browserTenantKey is a context key for passing tenant ID to browser operations.
type browserTenantKey struct{}

// WithTenantID returns a context with the browser tenant ID set.
// This is used to isolate browser pages per tenant via incognito contexts.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, browserTenantKey{}, tenantID)
}

// tenantIDFromCtx extracts the tenant ID from context.
func tenantIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(browserTenantKey{}).(string); ok {
		return v
	}
	return ""
}
