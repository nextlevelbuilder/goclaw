package hooks

import "context"

type ctxKey string

const skipHooksKey ctxKey = "skip_hooks"

// WithSkipHooks returns a context that signals hook evaluation should be skipped.
// Used by agent evaluator to prevent recursive hook firing.
func WithSkipHooks(ctx context.Context, skip bool) context.Context {
	return context.WithValue(ctx, skipHooksKey, skip)
}

// SkipHooksFromContext returns true if hooks should be skipped for this context.
func SkipHooksFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(skipHooksKey).(bool)
	return v
}
