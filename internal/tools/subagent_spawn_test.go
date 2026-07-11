package tools

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type recordingSubagentProvider struct {
	model string
}

func (p *recordingSubagentProvider) Name() string         { return "recording" }
func (p *recordingSubagentProvider) DefaultModel() string { return "provider-default" }
func (p *recordingSubagentProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.model = req.Model
	return &providers.ChatResponse{Content: "done", FinishReason: "stop"}, nil
}
func (p *recordingSubagentProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func TestRunSyncHonorsPerTaskModelOverride(t *testing.T) {
	provider := &recordingSubagentProvider{}
	manager := NewSubagentManager(provider, nil, "manager-default", nil, NewRegistry, SubagentConfig{
		MaxConcurrent:       4,
		MaxSpawnDepth:       3,
		MaxChildrenPerAgent: 8,
		Model:               "configured-model",
	})

	ctx := store.WithTenantID(context.Background(), uuid.New())
	ctx = WithParentModel(ctx, "parent-model")

	result, _, err := manager.RunSync(ctx, "parent", 0, "test task", "test", "requested-model", "test", "chat")
	if err != nil {
		t.Fatalf("RunSync() error = %v", err)
	}
	if result != "done" {
		t.Fatalf("RunSync() result = %q, want %q", result, "done")
	}
	if provider.model != "requested-model" {
		t.Fatalf("provider model = %q, want per-task override %q", provider.model, "requested-model")
	}
}
