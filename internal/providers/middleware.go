package providers

// RequestMiddleware transforms a provider request body after buildRequestBody().
// Returns the (possibly modified) body. Must be nil-safe on config fields.
type RequestMiddleware func(body map[string]any, cfg MiddlewareConfig) map[string]any

// MiddlewareConfig provides context for middleware decisions.
type MiddlewareConfig struct {
	Provider string               // "openai", "anthropic", "codex", etc.
	Model    string               // resolved model ID
	Caps     ProviderCapabilities // from CapabilitiesAware
	AuthType string               // "api_key", "oauth"
	APIBase  string               // provider base URL
	Options  map[string]any       // ChatRequest.Options passthrough
}

// ComposeMiddlewares chains middlewares left-to-right. Nil entries skipped.
// Returns nil if all entries are nil (zero-alloc fast path).
func ComposeMiddlewares(middlewares ...RequestMiddleware) RequestMiddleware {
	var active []RequestMiddleware
	for _, mw := range middlewares {
		if mw != nil {
			active = append(active, mw)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return func(body map[string]any, cfg MiddlewareConfig) map[string]any {
		for _, mw := range active {
			body = mw(body, cfg)
		}
		return body
	}
}

// ApplyMiddlewares applies a composed middleware to a request body.
// No-op if mw is nil (zero-alloc when no middleware registered).
func ApplyMiddlewares(body map[string]any, mw RequestMiddleware, cfg MiddlewareConfig) map[string]any {
	if mw == nil {
		return body
	}
	return mw(body, cfg)
}
