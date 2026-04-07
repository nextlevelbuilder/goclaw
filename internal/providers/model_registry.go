package providers

import "sync"

// ModelSpec describes a model's capabilities and cost.
type ModelSpec struct {
	ID            string
	Provider      string
	ContextWindow int
	MaxTokens     int
	Reasoning     bool
	Vision        bool
	TokenizerID   string
	Cost          ModelCost
}

// ModelCost tracks per-1M-token pricing.
type ModelCost struct {
	InputPer1M     float64
	OutputPer1M    float64
	CacheReadPer1M float64
}

// ModelRegistry resolves model IDs to specs with forward-compatibility support.
type ModelRegistry interface {
	Resolve(provider, modelID string) *ModelSpec
	Register(spec ModelSpec)
	Catalog(provider string) []ModelSpec
}

// ForwardCompatResolver is implemented by providers to handle unknown models.
type ForwardCompatResolver interface {
	ResolveForwardCompat(modelID string, registry ModelRegistry) *ModelSpec
}

// InMemoryRegistry is a thread-safe in-memory ModelRegistry.
type InMemoryRegistry struct {
	models    sync.Map // key: "provider:modelID" → *ModelSpec
	resolvers sync.Map // key: provider → ForwardCompatResolver
}

// NewInMemoryRegistry creates a registry and seeds it with known models.
func NewInMemoryRegistry() *InMemoryRegistry {
	r := &InMemoryRegistry{}
	SeedDefaultModels(r)
	return r
}

func registryKey(provider, modelID string) string {
	return provider + ":" + modelID
}

// Register adds or updates a model spec.
func (r *InMemoryRegistry) Register(spec ModelSpec) {
	r.models.Store(registryKey(spec.Provider, spec.ID), &spec)
}

// RegisterResolver sets the forward-compat resolver for a provider.
func (r *InMemoryRegistry) RegisterResolver(provider string, resolver ForwardCompatResolver) {
	r.resolvers.Store(provider, resolver)
}

// Resolve looks up a model: direct hit → forward-compat → nil.
func (r *InMemoryRegistry) Resolve(provider, modelID string) *ModelSpec {
	// Direct cache hit
	if v, ok := r.models.Load(registryKey(provider, modelID)); ok {
		return v.(*ModelSpec)
	}
	// Forward-compat resolver
	if v, ok := r.resolvers.Load(provider); ok {
		if resolver, ok := v.(ForwardCompatResolver); ok {
			if spec := resolver.ResolveForwardCompat(modelID, r); spec != nil {
				// Cache for next lookup
				r.Register(*spec)
				return spec
			}
		}
	}
	return nil
}

// Catalog returns all known specs for a provider.
func (r *InMemoryRegistry) Catalog(provider string) []ModelSpec {
	var specs []ModelSpec
	prefix := provider + ":"
	r.models.Range(func(key, value any) bool {
		k := key.(string)
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			specs = append(specs, *value.(*ModelSpec))
		}
		return true
	})
	return specs
}

// CloneFromTemplate finds the first matching template and clones it with overrides.
// Returns nil if no template found.
func CloneFromTemplate(registry ModelRegistry, provider, modelID string, templateIDs []string, patch *ModelSpec) *ModelSpec {
	for _, tmplID := range templateIDs {
		tmpl := registry.Resolve(provider, tmplID)
		if tmpl == nil {
			continue
		}
		// Clone template
		spec := *tmpl
		spec.ID = modelID
		spec.Provider = provider

		// Apply non-zero patch fields
		if patch != nil {
			if patch.ContextWindow > 0 {
				spec.ContextWindow = patch.ContextWindow
			}
			if patch.MaxTokens > 0 {
				spec.MaxTokens = patch.MaxTokens
			}
			if patch.TokenizerID != "" {
				spec.TokenizerID = patch.TokenizerID
			}
			if patch.Cost.InputPer1M > 0 {
				spec.Cost.InputPer1M = patch.Cost.InputPer1M
			}
			if patch.Cost.OutputPer1M > 0 {
				spec.Cost.OutputPer1M = patch.Cost.OutputPer1M
			}
			if patch.Cost.CacheReadPer1M > 0 {
				spec.Cost.CacheReadPer1M = patch.Cost.CacheReadPer1M
			}
			// Boolean fields: patch overrides if true
			if patch.Reasoning {
				spec.Reasoning = true
			}
			if patch.Vision {
				spec.Vision = true
			}
		}
		return &spec
	}
	return nil
}

// SeedDefaultModels registers well-known models into the registry.
func SeedDefaultModels(r *InMemoryRegistry) {
	// Anthropic models
	for _, s := range []ModelSpec{
		// TokenizerID "cl100k_base" is an approximation — Claude uses a proprietary tokenizer.
		// Used for rough token estimation; actual counting should use provider-specific logic.
		{ID: "claude-opus-4-6", Provider: "anthropic", ContextWindow: 200_000, MaxTokens: 32_000, Reasoning: true, Vision: true, TokenizerID: "cl100k_base"},
		{ID: "claude-sonnet-4-6", Provider: "anthropic", ContextWindow: 200_000, MaxTokens: 16_000, Reasoning: true, Vision: true, TokenizerID: "cl100k_base"},
		{ID: "claude-haiku-4-5-20251001", Provider: "anthropic", ContextWindow: 200_000, MaxTokens: 8_192, Reasoning: false, Vision: true, TokenizerID: "cl100k_base"},
	} {
		r.Register(s)
	}

	// OpenAI models
	for _, s := range []ModelSpec{
		{ID: "gpt-5.4", Provider: "openai", ContextWindow: 1_000_000, MaxTokens: 100_000, Reasoning: true, Vision: true, TokenizerID: "o200k_base"},
		{ID: "gpt-5.2", Provider: "openai", ContextWindow: 256_000, MaxTokens: 64_000, Reasoning: true, Vision: true, TokenizerID: "o200k_base"},
		{ID: "gpt-4o", Provider: "openai", ContextWindow: 128_000, MaxTokens: 16_384, Reasoning: false, Vision: true, TokenizerID: "o200k_base"},
		{ID: "o3", Provider: "openai", ContextWindow: 200_000, MaxTokens: 100_000, Reasoning: true, Vision: true, TokenizerID: "o200k_base"},
		{ID: "o4-mini", Provider: "openai", ContextWindow: 200_000, MaxTokens: 100_000, Reasoning: true, Vision: true, TokenizerID: "o200k_base"},
	} {
		r.Register(s)
	}
}
