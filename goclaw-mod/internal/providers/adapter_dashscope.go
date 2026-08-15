package providers

import (
	"fmt"
	"net/http"
)

// DashScopeAdapter implements ProviderAdapter for Alibaba DashScope.
// Thin wrapper around OpenAIAdapter — same wire format, different capabilities.
// Critical: StreamWithTools=false (DashScope falls back to non-streaming for tool calls).
type DashScopeAdapter struct {
	inner *OpenAIAdapter
	caps  ProviderCapabilities
}

// NewDashScopeAdapter creates a DashScope adapter from ProviderConfig.
func NewDashScopeAdapter(cfg ProviderConfig) (ProviderAdapter, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = dashscopeDefaultBase
	}
	if cfg.Model == "" {
		cfg.Model = dashscopeDefaultModel
	}
	inner, err := NewOpenAIAdapter(cfg)
	if err != nil {
		return nil, err
	}
	oa, ok := inner.(*OpenAIAdapter)
	if !ok {
		return nil, fmt.Errorf("dashscope adapter: unexpected inner type %T", inner)
	}
	// Override StreamWithTools from OpenAI's true to false
	caps := oa.Capabilities()
	caps.StreamWithTools = false
	return &DashScopeAdapter{inner: oa, caps: caps}, nil
}

func (a *DashScopeAdapter) Name() string { return "dashscope" }

// Capabilities returns OpenAI capabilities with StreamWithTools overridden to false.
func (a *DashScopeAdapter) Capabilities() ProviderCapabilities {
	return a.caps
}

// ToRequest delegates to OpenAI adapter (same Chat Completions wire format).
func (a *DashScopeAdapter) ToRequest(req ChatRequest) ([]byte, http.Header, error) {
	return a.inner.ToRequest(req)
}

// FromResponse delegates to OpenAI adapter.
func (a *DashScopeAdapter) FromResponse(data []byte) (*ChatResponse, error) {
	return a.inner.FromResponse(data)
}

// FromStreamChunk delegates to OpenAI adapter (same SSE format).
func (a *DashScopeAdapter) FromStreamChunk(data []byte) (*StreamChunk, error) {
	return a.inner.FromStreamChunk(data)
}
