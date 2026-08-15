package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

type recordingSubagentProvider struct {
	model        string
	systemPrompt string
	response     string
	calls        int
}

func (p *recordingSubagentProvider) Name() string         { return "recording" }
func (p *recordingSubagentProvider) DefaultModel() string { return "provider-default" }
func (p *recordingSubagentProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.calls++
	p.model = req.Model
	if len(req.Messages) > 0 {
		p.systemPrompt = req.Messages[0].Content
	}
	content := p.response
	if content == "" {
		content = "done"
	}
	return &providers.ChatResponse{Content: content, FinishReason: "stop"}, nil
}

func TestRunSyncKeepsParentAgentWorkspace(t *testing.T) {
	provider := &recordingSubagentProvider{}
	manager := NewSubagentManager(provider, nil, "manager-default", nil, NewRegistry, SubagentConfig{
		MaxConcurrent:       20,
		MaxSpawnDepth:       1,
		MaxChildrenPerAgent: 5,
	})
	workspace := t.TempDir()
	ctx := WithToolWorkspace(subagentTestContext("parent"), workspace)

	if _, _, _, err := manager.RunSync(
		ctx, "parent", 0, "test workspace", "workspace", "", "test", "chat",
	); err != nil {
		t.Fatalf("RunSync() error = %v", err)
	}
	if !strings.Contains(provider.systemPrompt, workspace) {
		t.Fatalf("subagent prompt omitted parent workspace %q: %s", workspace, provider.systemPrompt)
	}
}

func TestDelegatedAgentStartsOwnSpawnTree(t *testing.T) {
	provider := &recordingSubagentProvider{}
	manager := NewSubagentManager(provider, nil, "manager-default", nil, NewRegistry, SubagentConfig{
		MaxConcurrent:       20,
		MaxSpawnDepth:       1,
		MaxChildrenPerAgent: 5,
	})

	tenantID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	ctx = store.WithAgentID(ctx, agentA)
	ctx = store.WithAgentKey(ctx, "agent-a")
	ctx = WithToolAgentKey(ctx, "agent-a")
	ctx = withSubagentExecution(ctx, TaskScope{
		TenantID: tenantID, RootAgentID: agentA, RootAgentKey: "agent-a",
	}, "agent-a-leaf", 1, nil)
	ctx = WithSubagentConfig(ctx, &config.SubagentsConfig{
		MaxConcurrent:       2,
		MaxSpawnDepth:       4,
		MaxChildrenPerAgent: 3,
	})

	ctx = withDelegatedAgentExecution(ctx, nil)
	if inherited := SubagentConfigFromCtx(ctx); inherited != nil {
		t.Fatalf("delegated agent inherited source config: %#v", inherited)
	}
	ctx = store.WithAgentID(ctx, agentB)
	ctx = store.WithAgentKey(ctx, "agent-b")
	ctx = store.WithAgentContextWindow(ctx, 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)
	ctx = WithToolAgentKey(ctx, "agent-b")
	ctx = WithSubagentConfig(ctx, &config.SubagentsConfig{
		MaxConcurrent:       20,
		MaxSpawnDepth:       1,
		MaxChildrenPerAgent: 5,
	})

	if _, _, _, err := manager.RunSync(
		ctx, "agent-b", 0, "run as B child", "b-child", "", "delegate", "chat",
	); err != nil {
		t.Fatalf("B spawn failed after delegation boundary: %v", err)
	}

	scope := TaskScope{TenantID: tenantID, RootAgentID: agentB, RootAgentKey: "agent-b"}
	tasks := manager.ListTasks(scope, "")
	if len(tasks) != 1 {
		t.Fatalf("B task count = %d, want 1", len(tasks))
	}
	if task := tasks[0]; task.Depth != 1 || task.ParentTaskID != "" ||
		task.RootAgentID != agentB || task.RootAgentKey != "agent-b" {
		t.Fatalf("B spawn tree task = %#v", task)
	}
}

func TestRunSyncRedactsDelegationArtifactWorkspace(t *testing.T) {
	physicalOutputs := filepath.Join(t.TempDir(), "collaboration", "delegations", uuid.NewString(), "outputs")
	if err := os.MkdirAll(physicalOutputs, 0750); err != nil {
		t.Fatal(err)
	}
	provider := &recordingSubagentProvider{response: "result from " + physicalOutputs}
	manager := NewSubagentManager(provider, nil, "manager-default", nil, NewRegistry, SubagentConfig{
		MaxConcurrent:       20,
		MaxSpawnDepth:       2,
		MaxChildrenPerAgent: 5,
	})
	ctx := delegationArtifactTestContext()
	ctx = store.WithAgentID(ctx, uuid.New())
	ctx = store.WithAgentContextWindow(ctx, 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)
	ctx = WithToolWorkspace(ctx, physicalOutputs)
	ctx = WithToolAgentKey(ctx, "parent")
	ctx = tracing.WithTextRedactor(ctx, strings.NewReplacer(physicalOutputs, "outputs").Replace)

	result, _, _, err := manager.RunSync(
		ctx, "parent", 0, "inspect outputs", "workspace", "", "test", "chat",
	)
	if err != nil {
		t.Fatalf("RunSync() error = %v", err)
	}
	if strings.Contains(provider.systemPrompt, physicalOutputs) ||
		!strings.Contains(provider.systemPrompt, "outputs/") {
		t.Fatalf("artifact prompt leaked physical workspace: %s", provider.systemPrompt)
	}
	if strings.Contains(result, physicalOutputs) || !strings.Contains(result, "outputs") {
		t.Fatalf("artifact result leaked physical workspace: %q", result)
	}
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

	ctx := subagentTestContext("parent")
	ctx = WithParentModel(ctx, "parent-model")
	// The subagent's internal LLM call is agent-scoped; the guard requires a
	// budget in ctx (propagated from the calling agent via injectContext).
	ctx = store.WithAgentContextWindow(ctx, 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	result, _, _, err := manager.RunSync(ctx, "parent", 0, "test task", "test", "requested-model", "test", "chat")
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

func TestRunSyncRetainedCompletedTaskDoesNotConsumeActiveQuota(t *testing.T) {
	provider := &recordingSubagentProvider{}
	manager := NewSubagentManager(provider, nil, "manager-default", nil, NewRegistry, SubagentConfig{
		MaxConcurrent:       1,
		MaxSpawnDepth:       3,
		MaxChildrenPerAgent: 1,
	})

	ctx := subagentTestContext("parent")

	for i := range 2 {
		if _, _, _, err := manager.RunSync(
			ctx, "parent", 0, "test task", "test", "", "test", "chat",
		); err != nil {
			t.Fatalf("RunSync() attempt %d error = %v", i+1, err)
		}
	}

	tasks := manager.ListTasks(subagentScopeFromContext(ctx), "")
	if len(tasks) != 2 {
		t.Fatalf("retained tasks = %d, want 2", len(tasks))
	}
	for _, task := range tasks {
		if task.Status != TaskStatusCompleted {
			t.Fatalf("retained task status = %q, want completed", task.Status)
		}
	}
}

func TestSubagentTaskScopeRejectsOtherRootTree(t *testing.T) {
	tenantID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()
	manager := NewSubagentManager(nil, nil, "", nil, nil, SubagentConfig{})
	manager.tasks["running-a"] = &SubagentTask{
		ID: "running-a", Status: TaskStatusRunning, RootAgentID: agentA,
		RootAgentKey: "agent-a", OriginTenantID: tenantID,
	}
	if _, ok := manager.GetTask(TaskScope{
		TenantID: tenantID, RootAgentID: agentB, RootAgentKey: "agent-b",
	}, "running-a"); ok {
		t.Fatal("other root tree read task by ID")
	}
	if manager.CancelTask(TaskScope{
		TenantID: tenantID, RootAgentID: agentB, RootAgentKey: "agent-b",
	}, "running-a") {
		t.Fatal("other root tree cancelled task")
	}
}

type mediaSubagentProvider struct {
	calls int
}

func (p *mediaSubagentProvider) Name() string         { return "media-test" }
func (p *mediaSubagentProvider) DefaultModel() string { return "media-test-model" }
func (p *mediaSubagentProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	p.calls++
	if p.calls == 1 {
		return &providers.ChatResponse{
			ToolCalls: []providers.ToolCall{{
				ID:        "create-1",
				Name:      "create_test_media",
				Arguments: map[string]any{},
			}},
		}, nil
	}
	return &providers.ChatResponse{Content: "media created", FinishReason: "stop"}, nil
}
func (p *mediaSubagentProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

type subagentMediaTool struct{}

func (subagentMediaTool) Name() string               { return "create_test_media" }
func (subagentMediaTool) Description() string        { return "Create test media" }
func (subagentMediaTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (subagentMediaTool) Execute(context.Context, map[string]any) *Result {
	return &Result{
		ForLLM: "created",
		Media: []bus.MediaFile{{
			Path:     "/tmp/subagent-generated.png",
			MimeType: "image/png",
			Filename: "subagent-generated.png",
		}},
	}
}

func TestRunSyncReturnsGeneratedMedia(t *testing.T) {
	provider := &mediaSubagentProvider{}
	manager := newMediaSubagentManager(provider)

	ctx := subagentTestContext("parent")
	result, media, _, err := manager.RunSync(ctx, "parent", 0, "create media", "media", "", "delegate", "chat")
	if err != nil {
		t.Fatalf("RunSync() error = %v", err)
	}
	if result != "media created" {
		t.Fatalf("RunSync() result = %q, want %q", result, "media created")
	}
	if len(media) != 1 || media[0].Path != "/tmp/subagent-generated.png" {
		t.Fatalf("RunSync() media = %#v, want generated media", media)
	}
}

func TestSpawnToolSyncForwardsGeneratedMedia(t *testing.T) {
	provider := &mediaSubagentProvider{}
	tool := NewSpawnTool(newMediaSubagentManager(provider), "parent", 0)
	ctx := subagentTestContext("parent")

	result := tool.Execute(ctx, map[string]any{
		"task": "create media",
		"mode": "sync",
	})

	if result.IsError {
		t.Fatalf("SpawnTool.Execute() error = %s", result.ForLLM)
	}
	if len(result.Media) != 1 || result.Media[0].Path != "/tmp/subagent-generated.png" {
		t.Fatalf("SpawnTool.Execute() media = %#v, want generated media", result.Media)
	}
}

func subagentTestContext(rootKey string) context.Context {
	ctx := store.WithTenantID(context.Background(), uuid.New())
	ctx = store.WithAgentID(ctx, uuid.New())
	ctx = store.WithAgentContextWindow(ctx, 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)
	return WithToolAgentKey(ctx, rootKey)
}

func newMediaSubagentManager(provider providers.Provider) *SubagentManager {
	return NewSubagentManager(provider, nil, "media-test-model", nil, func() *Registry {
		registry := NewRegistry()
		registry.Register(subagentMediaTool{})
		return registry
	}, SubagentConfig{
		MaxConcurrent:       4,
		MaxSpawnDepth:       3,
		MaxChildrenPerAgent: 8,
	})
}
