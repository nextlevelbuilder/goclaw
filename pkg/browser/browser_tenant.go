package browser

import (
	"context"
	"strings"
)

// browserTenantKey is a context key for passing browser scope to operations.
type browserTenantKey struct{}

// BrowserScope identifies one isolated server-side browser context.
type BrowserScope struct {
	TenantID string
	UserID   string
	AgentID  string
}

// WithTenantID returns a context with the browser tenant ID set.
// This is used to isolate browser pages per tenant via incognito contexts.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	scope := scopeFromCtx(ctx)
	scope.TenantID = tenantID
	return context.WithValue(ctx, browserTenantKey{}, scope)
}

// WithScope returns a context with the full browser isolation scope set.
func WithScope(ctx context.Context, scope BrowserScope) context.Context {
	return context.WithValue(ctx, browserTenantKey{}, scope)
}

// Key returns the stable page/context owner key for this browser scope.
func (s BrowserScope) Key() string {
	tenant := strings.TrimSpace(s.TenantID)
	user := strings.TrimSpace(s.UserID)
	agent := strings.TrimSpace(s.AgentID)
	if user == "" && agent == "" {
		return tenant
	}
	return "tenant=" + tenant + "|user=" + user + "|agent=" + agent
}

func (s BrowserScope) usesMainBrowser() bool {
	key := s.Key()
	return key == "" || (s.TenantID == MasterTenantID && strings.TrimSpace(s.UserID) == "" && strings.TrimSpace(s.AgentID) == "")
}

// scopeFromCtx extracts the browser isolation scope from context.
func scopeFromCtx(ctx context.Context) BrowserScope {
	switch v := ctx.Value(browserTenantKey{}).(type) {
	case BrowserScope:
		return v
	case string:
		return BrowserScope{TenantID: v}
	default:
		return BrowserScope{}
	}
}

// tenantIDFromCtx returns the effective browser owner key for legacy callers.
func tenantIDFromCtx(ctx context.Context) string {
	return scopeFromCtx(ctx).Key()
}
