package providers

// DefaultAdapterRegistry returns a registry with all built-in provider adapters.
// Used by Pipeline to create adapters for known provider types.
func DefaultAdapterRegistry() *AdapterRegistry {
	r := NewAdapterRegistry()
	r.Register("anthropic", NewAnthropicAdapter)
	r.Register("openai", NewOpenAIAdapter)
	r.Register("dashscope", NewDashScopeAdapter)
	r.Register("codex", NewCodexAdapter)
	return r
}
