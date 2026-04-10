package tracing

import (
	"math"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func floatEquals(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestCalculateCost_NilInputs(t *testing.T) {
	if got := CalculateCost(nil, nil); got != 0 {
		t.Errorf("nil pricing + nil usage: got %v, want 0", got)
	}
	if got := CalculateCost(&config.ModelPricing{InputPerMillion: 1}, nil); got != 0 {
		t.Errorf("nil usage: got %v, want 0", got)
	}
	if got := CalculateCost(nil, &providers.Usage{PromptTokens: 100}); got != 0 {
		t.Errorf("nil pricing: got %v, want 0", got)
	}
}

func TestCalculateCost_PromptAndCompletion(t *testing.T) {
	pricing := &config.ModelPricing{
		InputPerMillion:  3.0,  // $3/M input tokens
		OutputPerMillion: 15.0, // $15/M output tokens
	}
	usage := &providers.Usage{
		PromptTokens:     1_000_000,
		CompletionTokens: 500_000,
	}
	// 1M * 3 + 0.5M * 15 = 3 + 7.5 = 10.5
	want := 10.5
	if got := CalculateCost(pricing, usage); !floatEquals(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCalculateCost_CacheTokens(t *testing.T) {
	pricing := &config.ModelPricing{
		InputPerMillion:       3.0,
		OutputPerMillion:      15.0,
		CacheReadPerMillion:   0.3,   // 10% of input
		CacheCreatePerMillion: 3.75,  // 25% premium
	}
	usage := &providers.Usage{
		PromptTokens:        1_000_000,
		CompletionTokens:    500_000,
		CacheReadTokens:     2_000_000,
		CacheCreationTokens: 100_000,
	}
	// 1M*3 + 0.5M*15 + 2M*0.3 + 0.1M*3.75 = 3 + 7.5 + 0.6 + 0.375 = 11.475
	want := 11.475
	if got := CalculateCost(pricing, usage); !floatEquals(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCalculateCost_ThinkingTokens_SubCountNoDoubleCount verifies that when
// ReasoningPerMillion is NOT set, thinking tokens are already included in
// CompletionTokens and must not be double-charged. This is the critical
// regression gate for OpenAI/Codex/GPT-5 where CompletionTokens already
// includes reasoning as a sub-count.
func TestCalculateCost_ThinkingTokens_SubCountNoDoubleCount(t *testing.T) {
	pricing := &config.ModelPricing{
		InputPerMillion:  3.0,
		OutputPerMillion: 15.0,
		// ReasoningPerMillion intentionally unset.
	}
	// Simulate an OpenAI o4-mini response: 1000 completion tokens of which
	// 800 are reasoning. Provider billing = 1000 * output_rate (reasoning
	// is inside the completion bucket already).
	usage := &providers.Usage{
		PromptTokens:     100,
		CompletionTokens: 1000,
		ThinkingTokens:   800,
	}
	// 100*3 + 1000*15 = 0.0003 + 0.015 = 0.0153
	want := 100.0*3.0/1_000_000 + 1000.0*15.0/1_000_000
	if got := CalculateCost(pricing, usage); !floatEquals(got, want) {
		t.Errorf("double-count regression: got %v, want %v", got, want)
	}
}

// TestCalculateCost_ThinkingTokens_WithDistinctReasoningRate verifies that
// when a distinct ReasoningPerMillion is configured, the completion bucket
// is split into visible output + thinking, and each priced independently.
// This supports pricing tiers where reasoning is cheaper or more expensive
// than the visible output (e.g. dev-economy pricing).
func TestCalculateCost_ThinkingTokens_WithDistinctReasoningRate(t *testing.T) {
	pricing := &config.ModelPricing{
		InputPerMillion:     3.0,
		OutputPerMillion:    15.0,
		ReasoningPerMillion: 10.0, // thinking cheaper than visible output
	}
	usage := &providers.Usage{
		PromptTokens:     100_000,
		CompletionTokens: 250_000, // 50k visible + 200k thinking
		ThinkingTokens:   200_000,
	}
	// 100k*3 + 50k*15 + 200k*10 = 0.3 + 0.75 + 2.0 = 3.05
	want := 3.05
	if got := CalculateCost(pricing, usage); !floatEquals(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCalculateCost_ThinkingTokens_AnthropicEstimateOverrun verifies the
// defensive clamp when Anthropic's thinkingChars/4 estimate exceeds the
// API's reported OutputTokens (which can happen under unusual streaming).
func TestCalculateCost_ThinkingTokens_AnthropicEstimateOverrun(t *testing.T) {
	pricing := &config.ModelPricing{
		InputPerMillion:     3.0,
		OutputPerMillion:    15.0,
		ReasoningPerMillion: 20.0,
	}
	usage := &providers.Usage{
		PromptTokens:     100,
		CompletionTokens: 100, // API says 100 output
		ThinkingTokens:   150, // our estimate says 150 (overrun)
	}
	// visible clamped to 0, thinking priced at 20/M:
	// 100*3 + 0*15 + 150*20 = 0.0003 + 0 + 0.003 = 0.0033
	want := 100.0*3.0/1_000_000 + 150.0*20.0/1_000_000
	if got := CalculateCost(pricing, usage); !floatEquals(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCalculateCost_ZeroThinking_BackwardCompat verifies no behavior change
// for models without thinking tokens (non-reasoning models).
func TestCalculateCost_ZeroThinking_BackwardCompat(t *testing.T) {
	pricing := &config.ModelPricing{
		InputPerMillion:     3.0,
		OutputPerMillion:    15.0,
		ReasoningPerMillion: 20.0, // set but no thinking tokens used
	}
	usage := &providers.Usage{
		PromptTokens:     1_000_000,
		CompletionTokens: 500_000,
		ThinkingTokens:   0,
	}
	// Same as TestCalculateCost_PromptAndCompletion: 10.5
	want := 10.5
	if got := CalculateCost(pricing, usage); !floatEquals(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCalculateCost_OriginalBehaviorUnchanged ensures non-reasoning models
// with no ReasoningPerMillion configured get exactly the same cost as before
// Phase 1 (guards against regressions in the common path).
func TestCalculateCost_OriginalBehaviorUnchanged(t *testing.T) {
	pricing := &config.ModelPricing{
		InputPerMillion:  3.0,
		OutputPerMillion: 15.0,
	}
	// GPT-4o style call with no reasoning at all.
	usage := &providers.Usage{
		PromptTokens:     1_000_000,
		CompletionTokens: 500_000,
	}
	want := 10.5 // 3 + 7.5
	if got := CalculateCost(pricing, usage); !floatEquals(got, want) {
		t.Errorf("original-path regression: got %v, want %v", got, want)
	}
}

func TestLookupPricing_ProviderQualified(t *testing.T) {
	m := map[string]*config.ModelPricing{
		"anthropic/claude-opus-4": {InputPerMillion: 15.0, OutputPerMillion: 75.0},
		"claude-opus-4":           {InputPerMillion: 10.0, OutputPerMillion: 50.0},
	}
	// Provider-qualified takes precedence
	p := LookupPricing(m, "anthropic", "claude-opus-4")
	if p == nil || p.InputPerMillion != 15.0 {
		t.Errorf("expected provider-qualified match, got %+v", p)
	}
	// Fallback to bare model name
	p = LookupPricing(m, "unknown", "claude-opus-4")
	if p == nil || p.InputPerMillion != 10.0 {
		t.Errorf("expected bare model fallback, got %+v", p)
	}
	// Not found
	if p := LookupPricing(m, "unknown", "unknown"); p != nil {
		t.Errorf("expected nil, got %+v", p)
	}
	// Nil map
	if p := LookupPricing(nil, "x", "y"); p != nil {
		t.Errorf("expected nil for nil map, got %+v", p)
	}
}
