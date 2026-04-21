package http

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// webhookCtxKey is the unexported context key type for webhook-layer values.
// Uses a distinct struct type (not contextKey string) to avoid collision with
// store-layer keys while following the same struct-key pattern.
type webhookCtxKey struct{}

// WithWebhookData returns a new context carrying the resolved WebhookData.
// Call store.WithTenantID separately to propagate tenant to downstream stores.
func WithWebhookData(ctx context.Context, w *store.WebhookData) context.Context {
	return context.WithValue(ctx, webhookCtxKey{}, w)
}

// WebhookDataFromContext extracts the resolved webhook from context.
// Returns nil if not set (pre-auth or non-webhook request paths).
func WebhookDataFromContext(ctx context.Context) *store.WebhookData {
	v, _ := ctx.Value(webhookCtxKey{}).(*store.WebhookData)
	return v
}
