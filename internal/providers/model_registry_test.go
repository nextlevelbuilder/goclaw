package providers

import (
	"testing"
)

func TestInMemoryRegistryRegisterAndResolve(t *testing.T) {
	registry := &InMemoryRegistry{}

	spec := ModelSpec{
		ID:            "gpt-4o",
		Provider:      "openai",
		ContextWindow: 128_000,
		MaxTokens:     16_384,
		Reasoning:     false,
		Vision:        true,
		TokenizerID:   "o200k_base",
	}

	registry.Register(spec)
	resolved := registry.Resolve("openai", "gpt-4o")

	if resolved == nil {
		t.Fatal("expected resolved spec, got nil")
	}
	if resolved.ID != "gpt-4o" {
		t.Errorf("expected ID=gpt-4o, got %s", resolved.ID)
	}
	if resolved.ContextWindow != 128_000 {
		t.Errorf("expected ContextWindow=128000, got %d", resolved.ContextWindow)
	}
	if resolved.Vision != true {
		t.Errorf("expected Vision=true, got %v", resolved.Vision)
	}
}

func TestInMemoryRegistryResolveUnknownReturnsNil(t *testing.T) {
	registry := &InMemoryRegistry{}

	resolved := registry.Resolve("openai", "unknown-model")

	if resolved != nil {
		t.Errorf("expected nil for unknown model, got %v", resolved)
	}
}

func TestInMemoryRegistryCatalog(t *testing.T) {
	registry := &InMemoryRegistry{}

	specs := []ModelSpec{
		{ID: "gpt-4o", Provider: "openai", ContextWindow: 128_000},
		{ID: "gpt-5.4", Provider: "openai", ContextWindow: 1_000_000},
		{ID: "claude-opus-4-6", Provider: "anthropic", ContextWindow: 200_000},
	}

	for _, spec := range specs {
		registry.Register(spec)
	}

	openaiCatalog := registry.Catalog("openai")

	if len(openaiCatalog) != 2 {
		t.Errorf("expected 2 OpenAI models, got %d", len(openaiCatalog))
	}

	for _, spec := range openaiCatalog {
		if spec.Provider != "openai" {
			t.Errorf("expected provider=openai, got %s", spec.Provider)
		}
	}
}

func TestInMemoryRegistryCatalogEmpty(t *testing.T) {
	registry := &InMemoryRegistry{}

	catalog := registry.Catalog("openai")

	if len(catalog) != 0 {
		t.Errorf("expected empty catalog, got %d models", len(catalog))
	}
}

func TestInMemoryRegistryCatalogMultipleProviders(t *testing.T) {
	registry := &InMemoryRegistry{}

	specs := []ModelSpec{
		{ID: "gpt-4o", Provider: "openai"},
		{ID: "claude-opus-4-6", Provider: "anthropic"},
		{ID: "claude-sonnet-4-6", Provider: "anthropic"},
	}

	for _, spec := range specs {
		registry.Register(spec)
	}

	anthropicCatalog := registry.Catalog("anthropic")

	if len(anthropicCatalog) != 2 {
		t.Errorf("expected 2 Anthropic models, got %d", len(anthropicCatalog))
	}

	for _, spec := range anthropicCatalog {
		if spec.Provider != "anthropic" {
			t.Errorf("expected provider=anthropic, got %s", spec.Provider)
		}
	}
}

func TestNewInMemoryRegistrySeeded(t *testing.T) {
	registry := NewInMemoryRegistry()

	// Should have default models from SeedDefaultModels
	resolved := registry.Resolve("anthropic", "claude-opus-4-6")
	if resolved == nil {
		t.Fatal("expected seeded claude-opus-4-6, got nil")
	}

	resolved = registry.Resolve("openai", "gpt-4o")
	if resolved == nil {
		t.Fatal("expected seeded gpt-4o, got nil")
	}
}

func TestCloneFromTemplateFound(t *testing.T) {
	registry := &InMemoryRegistry{}

	template := ModelSpec{
		ID:            "gpt-4o",
		Provider:      "openai",
		ContextWindow: 128_000,
		MaxTokens:     16_384,
		Reasoning:     false,
		Vision:        true,
		TokenizerID:   "o200k_base",
		Cost: ModelCost{
			InputPer1M:     5.0,
			OutputPer1M:    15.0,
			CacheReadPer1M: 1.25,
		},
	}
	registry.Register(template)

	cloned := CloneFromTemplate(registry, "openai", "gpt-4o-turbo", []string{"gpt-4o"}, nil)

	if cloned == nil {
		t.Fatal("expected cloned spec, got nil")
	}
	if cloned.ID != "gpt-4o-turbo" {
		t.Errorf("expected ID=gpt-4o-turbo, got %s", cloned.ID)
	}
	if cloned.Provider != "openai" {
		t.Errorf("expected Provider=openai, got %s", cloned.Provider)
	}
	if cloned.ContextWindow != 128_000 {
		t.Errorf("expected ContextWindow=128000, got %d", cloned.ContextWindow)
	}
	if cloned.Vision != true {
		t.Errorf("expected Vision=true, got %v", cloned.Vision)
	}
	if cloned.Cost.InputPer1M != 5.0 {
		t.Errorf("expected InputPer1M=5.0, got %f", cloned.Cost.InputPer1M)
	}
}

func TestCloneFromTemplateNotFound(t *testing.T) {
	registry := &InMemoryRegistry{}

	cloned := CloneFromTemplate(registry, "openai", "gpt-999", []string{"unknown"}, nil)

	if cloned != nil {
		t.Errorf("expected nil for unknown template, got %v", cloned)
	}
}

func TestCloneFromTemplateWithPatch(t *testing.T) {
	registry := &InMemoryRegistry{}

	template := ModelSpec{
		ID:            "gpt-4o",
		Provider:      "openai",
		ContextWindow: 128_000,
		MaxTokens:     16_384,
		Reasoning:     false,
		Vision:        true,
		TokenizerID:   "o200k_base",
		Cost: ModelCost{
			InputPer1M:  5.0,
			OutputPer1M: 15.0,
		},
	}
	registry.Register(template)

	patch := &ModelSpec{
		ContextWindow: 200_000,
		MaxTokens:     100_000,
		Cost: ModelCost{
			InputPer1M:  10.0,
			OutputPer1M: 30.0,
		},
	}

	cloned := CloneFromTemplate(registry, "openai", "gpt-5.5", []string{"gpt-4o"}, patch)

	if cloned == nil {
		t.Fatal("expected cloned spec, got nil")
	}
	if cloned.ID != "gpt-5.5" {
		t.Errorf("expected ID=gpt-5.5, got %s", cloned.ID)
	}
	if cloned.ContextWindow != 200_000 {
		t.Errorf("expected patched ContextWindow=200000, got %d", cloned.ContextWindow)
	}
	if cloned.MaxTokens != 100_000 {
		t.Errorf("expected patched MaxTokens=100000, got %d", cloned.MaxTokens)
	}
	if cloned.Cost.InputPer1M != 10.0 {
		t.Errorf("expected patched InputPer1M=10.0, got %f", cloned.Cost.InputPer1M)
	}
	// Vision should be preserved from template
	if cloned.Vision != true {
		t.Errorf("expected Vision=true from template, got %v", cloned.Vision)
	}
}

func TestCloneFromTemplateMultipleTemplates(t *testing.T) {
	registry := &InMemoryRegistry{}

	t1 := ModelSpec{ID: "template1", Provider: "openai", ContextWindow: 100}
	t2 := ModelSpec{ID: "template2", Provider: "openai", ContextWindow: 200}

	registry.Register(t1)
	registry.Register(t2)

	// First template found should be used
	cloned := CloneFromTemplate(registry, "openai", "new", []string{"template1", "template2"}, nil)

	if cloned == nil {
		t.Fatal("expected cloned spec, got nil")
	}
	if cloned.ContextWindow != 100 {
		t.Errorf("expected ContextWindow=100 from first template, got %d", cloned.ContextWindow)
	}
}

func TestCloneFromTemplateSkipsMissingTemplates(t *testing.T) {
	registry := &InMemoryRegistry{}

	template := ModelSpec{ID: "found", Provider: "openai", ContextWindow: 300}
	registry.Register(template)

	// Should skip unknown templates and use the one that exists
	cloned := CloneFromTemplate(registry, "openai", "new", []string{"missing1", "missing2", "found"}, nil)

	if cloned == nil {
		t.Fatal("expected cloned spec, got nil")
	}
	if cloned.ContextWindow != 300 {
		t.Errorf("expected ContextWindow=300, got %d", cloned.ContextWindow)
	}
}

func TestCloneFromTemplatePatchBooleanFields(t *testing.T) {
	registry := &InMemoryRegistry{}

	template := ModelSpec{
		ID:       "base",
		Provider: "openai",
		Reasoning: false,
		Vision:   false,
	}
	registry.Register(template)

	patch := &ModelSpec{
		Reasoning: true,
		Vision:    true,
	}

	cloned := CloneFromTemplate(registry, "openai", "new", []string{"base"}, patch)

	if !cloned.Reasoning {
		t.Errorf("expected Reasoning=true from patch, got %v", cloned.Reasoning)
	}
	if !cloned.Vision {
		t.Errorf("expected Vision=true from patch, got %v", cloned.Vision)
	}
}

func TestSeedDefaultModels(t *testing.T) {
	registry := &InMemoryRegistry{}

	SeedDefaultModels(registry)

	// Check Anthropic models
	claude := registry.Resolve("anthropic", "claude-opus-4-6")
	if claude == nil {
		t.Fatal("expected claude-opus-4-6, got nil")
	}
	if claude.ContextWindow != 200_000 {
		t.Errorf("expected ContextWindow=200000, got %d", claude.ContextWindow)
	}
	if !claude.Vision {
		t.Errorf("expected Vision=true, got %v", claude.Vision)
	}

	// Check OpenAI models
	gpt := registry.Resolve("openai", "gpt-4o")
	if gpt == nil {
		t.Fatal("expected gpt-4o, got nil")
	}
	if gpt.ContextWindow != 128_000 {
		t.Errorf("expected ContextWindow=128000, got %d", gpt.ContextWindow)
	}
}

func TestAnthropicForwardCompatResolveVersioned(t *testing.T) {
	registry := NewInMemoryRegistry()
	resolver := &AnthropicForwardCompat{}
	registry.RegisterResolver("anthropic", resolver)

	// Try to resolve claude-opus-4-7 which doesn't exist directly
	// Should find claude-opus-4-6 as a template
	resolved := registry.Resolve("anthropic", "claude-opus-4-7")

	if resolved == nil {
		t.Fatal("expected forward-compat resolution, got nil")
	}
	if resolved.ID != "claude-opus-4-7" {
		t.Errorf("expected ID=claude-opus-4-7, got %s", resolved.ID)
	}
	// Should inherit from claude-opus-4-6
	if resolved.ContextWindow != 200_000 {
		t.Errorf("expected ContextWindow=200000 from template, got %d", resolved.ContextWindow)
	}
}

func TestAnthropicForwardCompatResolveWithSuffix(t *testing.T) {
	registry := NewInMemoryRegistry()
	resolver := &AnthropicForwardCompat{}
	registry.RegisterResolver("anthropic", resolver)

	// Try to resolve versioned model with date suffix
	resolved := registry.Resolve("anthropic", "claude-opus-4-7-20260501")

	if resolved == nil {
		t.Fatal("expected forward-compat resolution with suffix, got nil")
	}
	if resolved.ID != "claude-opus-4-7-20260501" {
		t.Errorf("expected ID=claude-opus-4-7-20260501, got %s", resolved.ID)
	}
	// Should still inherit context window
	if resolved.ContextWindow != 200_000 {
		t.Errorf("expected ContextWindow=200000, got %d", resolved.ContextWindow)
	}
}

func TestAnthropicForwardCompatNoMatch(t *testing.T) {
	registry := NewInMemoryRegistry()
	resolver := &AnthropicForwardCompat{}
	registry.RegisterResolver("anthropic", resolver)

	// Non-matching format should return nil (and not crash)
	resolved := registry.Resolve("anthropic", "invalid-format")

	if resolved != nil {
		t.Errorf("expected nil for non-matching format, got %v", resolved)
	}
}

func TestOpenAIForwardCompatResolveExactMatch(t *testing.T) {
	registry := NewInMemoryRegistry()
	resolver := &OpenAIForwardCompat{}
	registry.RegisterResolver("openai", resolver)

	// Try to resolve gpt-5.5 which should use gpt-5.4 as template with patch
	resolved := registry.Resolve("openai", "gpt-5.5")

	if resolved == nil {
		t.Fatal("expected forward-compat resolution for gpt-5.5, got nil")
	}
	if resolved.ID != "gpt-5.5" {
		t.Errorf("expected ID=gpt-5.5, got %s", resolved.ID)
	}
	// Should have patched values from the map
	if resolved.ContextWindow != 1_000_000 {
		t.Errorf("expected ContextWindow=1000000 from patch, got %d", resolved.ContextWindow)
	}
	if resolved.MaxTokens != 200_000 {
		t.Errorf("expected MaxTokens=200000 from patch, got %d", resolved.MaxTokens)
	}
}

func TestOpenAIForwardCompatResolvePrefixMatch(t *testing.T) {
	registry := NewInMemoryRegistry()
	resolver := &OpenAIForwardCompat{}
	registry.RegisterResolver("openai", resolver)

	// Try to resolve gpt-5.5-turbo which should match gpt-5.5 prefix
	resolved := registry.Resolve("openai", "gpt-5.5-turbo")

	if resolved == nil {
		t.Fatal("expected forward-compat resolution for gpt-5.5-turbo, got nil")
	}
	if resolved.ID != "gpt-5.5-turbo" {
		t.Errorf("expected ID=gpt-5.5-turbo, got %s", resolved.ID)
	}
	// Should use gpt-5.4 as template with gpt-5.5 patch
	if resolved.MaxTokens != 200_000 {
		t.Errorf("expected MaxTokens=200000 from patch, got %d", resolved.MaxTokens)
	}
}

func TestOpenAIForwardCompatResolveNoMatch(t *testing.T) {
	registry := NewInMemoryRegistry()
	resolver := &OpenAIForwardCompat{}
	registry.RegisterResolver("openai", resolver)

	// Non-matching model should return nil
	resolved := registry.Resolve("openai", "gpt-999")

	if resolved != nil {
		t.Errorf("expected nil for unknown model, got %v", resolved)
	}
}

func TestRegistryForwardCompatCaching(t *testing.T) {
	registry := NewInMemoryRegistry()
	resolver := &OpenAIForwardCompat{}
	registry.RegisterResolver("openai", resolver)

	// First resolve
	resolved1 := registry.Resolve("openai", "gpt-5.5")
	if resolved1 == nil {
		t.Fatal("expected first resolution to work")
	}

	// Second resolve should be cached
	resolved2 := registry.Resolve("openai", "gpt-5.5")
	if resolved2 == nil {
		t.Fatal("expected second resolution to work")
	}

	// Should be the same object (or at least have same values)
	if resolved1.ID != resolved2.ID {
		t.Errorf("expected same ID from cache, got %s vs %s", resolved1.ID, resolved2.ID)
	}
}

func TestCloneFromTemplatePatchZeroValuesIgnored(t *testing.T) {
	registry := &InMemoryRegistry{}

	template := ModelSpec{
		ID:            "base",
		Provider:      "openai",
		ContextWindow: 100,
		MaxTokens:     50,
		TokenizerID:   "original",
		Cost: ModelCost{
			InputPer1M: 1.0,
		},
	}
	registry.Register(template)

	// Patch with zero values should be ignored
	patch := &ModelSpec{
		ContextWindow: 0, // Should be ignored
		MaxTokens:     0, // Should be ignored
		TokenizerID:   "", // Should be ignored
		Cost: ModelCost{
			InputPer1M: 0, // Should be ignored
		},
	}

	cloned := CloneFromTemplate(registry, "openai", "new", []string{"base"}, patch)

	if cloned.ContextWindow != 100 {
		t.Errorf("expected ContextWindow=100 (unchanged), got %d", cloned.ContextWindow)
	}
	if cloned.MaxTokens != 50 {
		t.Errorf("expected MaxTokens=50 (unchanged), got %d", cloned.MaxTokens)
	}
	if cloned.TokenizerID != "original" {
		t.Errorf("expected TokenizerID=original (unchanged), got %s", cloned.TokenizerID)
	}
	if cloned.Cost.InputPer1M != 1.0 {
		t.Errorf("expected InputPer1M=1.0 (unchanged), got %f", cloned.Cost.InputPer1M)
	}
}
