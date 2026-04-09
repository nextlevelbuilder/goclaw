package consolidation

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// mockExtractor implements EntityExtractor for testing.
type mockExtractor struct {
	result *knowledgegraph.ExtractionResult
	err    error
}

func (m *mockExtractor) Extract(_ context.Context, _ string) (*knowledgegraph.ExtractionResult, error) {
	return m.result, m.err
}

// mockProvider implements providers.Provider for testing LLM calls.
type mockProvider struct {
	chatResp *providers.ChatResponse
	chatErr  error
}

func (m *mockProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return m.chatResp, m.chatErr
}

func (m *mockProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return m.chatResp, m.chatErr
}

func (m *mockProvider) Name() string         { return "mock" }
func (m *mockProvider) DefaultModel() string  { return "mock-model" }
