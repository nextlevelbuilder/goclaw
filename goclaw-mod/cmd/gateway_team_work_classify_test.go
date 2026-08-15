package cmd

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type teamWorkGateTestEmbedder struct {
	called bool
}

func (e *teamWorkGateTestEmbedder) Name() string  { return "test-embedder" }
func (e *teamWorkGateTestEmbedder) Model() string { return "test-embedding" }
func (e *teamWorkGateTestEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.called = true
	return [][]float32{{1, 0}}, nil
}

type teamWorkGateTestProvider struct {
	called bool
}

func (p *teamWorkGateTestProvider) Name() string         { return "test-provider" }
func (p *teamWorkGateTestProvider) DefaultModel() string { return "test-model" }
func (p *teamWorkGateTestProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	p.called = true
	return &providers.ChatResponse{Content: `{"decision":"team","mode":"team","required_tool":"team_tasks"}`}, nil
}
func (p *teamWorkGateTestProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	p.called = true
	return nil, nil
}

func TestApplyTeamWorkGateForInboundSkipsAgentWithoutTeamOrDelegateLink(t *testing.T) {
	enabled := true
	embedder := &teamWorkGateTestEmbedder{}
	provider := &teamWorkGateTestProvider{}

	out := applyTeamWorkGateForInbound(context.Background(), &ConsumerDeps{
		Cfg:              &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}},
		TeamWorkEmbedder: embedder,
	}, bus.InboundMessage{
		Content:  "lập kế hoạch content và chiến lược cho chiến dịch mới",
		Metadata: map[string]string{},
	}, "session:test", "bao-an", "direct", uuid.New(), nil, provider, "test-model")

	if out.Message != "lập kế hoạch content và chiến lược cho chiến dịch mới" {
		t.Fatalf("Message = %q, want original message", out.Message)
	}
	if out.Directive != nil {
		t.Fatalf("Directive = %+v, want nil", out.Directive)
	}
	if embedder.called {
		t.Fatal("embedder was called even though agent has no team/delegate capability")
	}
	if provider.called {
		t.Fatal("provider was called even though agent has no team/delegate capability")
	}
}
