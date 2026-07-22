package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeProvider is a minimal providers.Provider for goclaw_llm_complete tests.
type fakeProvider struct {
	name    string
	model   string
	reply   string
	chatErr error
}

func (p *fakeProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	if p.chatErr != nil {
		return nil, p.chatErr
	}
	return &providers.ChatResponse{Content: p.reply}, nil
}

func (p *fakeProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: p.reply}, nil
}

func (p *fakeProvider) DefaultModel() string { return p.model }
func (p *fakeProvider) Name() string         { return p.name }

func newLLMRegistry(prov *fakeProvider) *providers.Registry {
	reg := providers.NewRegistry(store.TenantIDFromContext)
	reg.Register(prov)
	return reg
}

func TestLLMComplete_HappyPath(t *testing.T) {
	prov := &fakeProvider{name: "anthropic", model: "claude", reply: "42"}
	reg := newLLMRegistry(prov)
	srv := newTestMCPServer()
	registerLLMCRUDTool(srv, reg, LLMDefaults{})

	result := callTool(t, srv, "goclaw_llm_complete", map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "what is 6*7?"},
		},
	})
	require.False(t, toolIsError(result), toolResultText(result))
	assert.Contains(t, toolResultText(result), "42")
}

func TestLLMComplete_RequiresMessages(t *testing.T) {
	prov := &fakeProvider{name: "anthropic", model: "claude"}
	reg := newLLMRegistry(prov)
	srv := newTestMCPServer()
	registerLLMCRUDTool(srv, reg, LLMDefaults{})

	result := callTool(t, srv, "goclaw_llm_complete", map[string]any{"messages": []any{}})
	assert.True(t, toolIsError(result))
}

func TestLLMComplete_UnknownProvider(t *testing.T) {
	prov := &fakeProvider{name: "anthropic", model: "claude"}
	reg := newLLMRegistry(prov)
	srv := newTestMCPServer()
	registerLLMCRUDTool(srv, reg, LLMDefaults{})

	result := callTool(t, srv, "goclaw_llm_complete", map[string]any{
		"provider": "does-not-exist",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	assert.True(t, toolIsError(result))
}

func TestLLMComplete_NilRegistry(t *testing.T) {
	srv := newTestMCPServer()
	registerLLMCRUDTool(srv, nil, LLMDefaults{})

	result := callTool(t, srv, "goclaw_llm_complete", map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	assert.True(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "no providers configured")
}
