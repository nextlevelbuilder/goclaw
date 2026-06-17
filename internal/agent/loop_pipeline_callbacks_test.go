package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type finalThinkingStreamProvider struct{}

func (p finalThinkingStreamProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: "final", Thinking: "non-stream thinking"}, nil
}

func (p finalThinkingStreamProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: "final", Thinking: "final streamed thinking"}, nil
}

func (p finalThinkingStreamProvider) DefaultModel() string { return "test-model" }
func (p finalThinkingStreamProvider) Name() string         { return "test-provider" }

func TestMakeCallLLM_StreamsFinalThinkingWhenNoThinkingChunkArrives(t *testing.T) {
	col := &eventCollector{}
	loop := &Loop{id: "test-agent", onEvent: col.onEvent}
	req := &RunRequest{
		RunID:      "run-1",
		SessionKey: "sess-1",
		Channel:    "telegram",
		Stream:     true,
	}
	state := &pipeline.RunState{
		Provider:  finalThinkingStreamProvider{},
		Model:     "test-model",
		Iteration: 0,
	}

	resp, err := loop.makeCallLLM(req, col.onEvent)(context.Background(), state, providers.ChatRequest{})
	if err != nil {
		t.Fatalf("makeCallLLM returned error: %v", err)
	}
	if resp == nil || resp.Thinking != "final streamed thinking" {
		t.Fatalf("stream response = %+v, want final thinking preserved", resp)
	}

	thinking := col.filter(protocol.ChatEventThinking)
	if len(thinking) != 1 {
		t.Fatalf("thinking events = %+v, want exactly one final thinking event", thinking)
	}
	payload, ok := thinking[0].Payload.(map[string]string)
	if !ok || payload["content"] != "final streamed thinking" {
		t.Fatalf("thinking payload = %+v", thinking[0].Payload)
	}
}
