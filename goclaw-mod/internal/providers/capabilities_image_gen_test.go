package providers

import "testing"

// TestCodexProvider_ImageGenCapability verifies CodexProvider.Capabilities()
// reports ImageGeneration=true.
func TestCodexProvider_ImageGenCapability(t *testing.T) {
	ts := &staticTokenSource{token: "tok"}
	p := NewCodexProvider("codex", ts, "", "")
	caps := p.Capabilities()
	if !caps.ImageGeneration {
		t.Error("CodexProvider.Capabilities().ImageGeneration must be true")
	}
}

// TestCodexAdapter_ImageGenCapability verifies CodexAdapter.Capabilities()
// also reports ImageGeneration=true (adapter must mirror provider).
func TestCodexAdapter_ImageGenCapability(t *testing.T) {
	a, err := NewCodexAdapter(ProviderConfig{})
	if err != nil {
		t.Fatalf("NewCodexAdapter: %v", err)
	}
	caps := a.Capabilities()
	if !caps.ImageGeneration {
		t.Error("CodexAdapter.Capabilities().ImageGeneration must be true")
	}
}

// TestOtherProviders_NoImageGenCapability verifies providers that do NOT support
// image_generation return ImageGeneration=false (the zero value). This protects
// against accidentally attaching the tool to non-Codex providers.
func TestOtherProviders_NoImageGenCapability(t *testing.T) {
	// Anthropic
	ap := &AnthropicProvider{}
	if ap.Capabilities().ImageGeneration {
		t.Error("AnthropicProvider must not advertise ImageGeneration")
	}

	// DashScope
	dp := &DashScopeProvider{}
	if dp.Capabilities().ImageGeneration {
		t.Error("DashScopeProvider must not advertise ImageGeneration")
	}

	// OpenAI (via OpenAIProvider using compat layer)
	op := &OpenAIProvider{}
	if op.Capabilities().ImageGeneration {
		t.Error("OpenAIProvider must not advertise ImageGeneration")
	}
}
