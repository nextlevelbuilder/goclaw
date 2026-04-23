package tools

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeDispatcher records Fire calls and returns a preset decision.
type fakeDispatcher struct {
	decision hooks.Decision
	calls    int
}

func (f *fakeDispatcher) Fire(_ context.Context, _ hooks.Event) (hooks.FireResult, error) {
	f.calls++
	return hooks.FireResult{Decision: f.decision}, nil
}

// --- minimal noop stores to satisfy AgentLinkStore / AgentCRUDStore ---

type noopAgentLink struct{}

func (noopAgentLink) CreateLink(_ context.Context, _ *store.AgentLinkData) error { return nil }
func (noopAgentLink) DeleteLink(_ context.Context, _ uuid.UUID) error            { return nil }
func (noopAgentLink) UpdateLink(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (noopAgentLink) GetLink(_ context.Context, _ uuid.UUID) (*store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) ListLinksFrom(_ context.Context, _ uuid.UUID) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) ListLinksTo(_ context.Context, _ uuid.UUID) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) CanDelegate(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}
func (noopAgentLink) GetLinkBetween(_ context.Context, _, _ uuid.UUID) (*store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) DelegateTargets(_ context.Context, _ uuid.UUID) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) SearchDelegateTargets(_ context.Context, _ uuid.UUID, _ string, _ int) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) SearchDelegateTargetsByEmbedding(_ context.Context, _ uuid.UUID, _ []float32, _ int) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) DeleteTeamLinksForAgent(_ context.Context, _, _ uuid.UUID) error { return nil }

type noopAgentCRUD struct {
	keyToID map[string]uuid.UUID
}

func (n noopAgentCRUD) Create(_ context.Context, _ *store.AgentData) error { return nil }
func (n noopAgentCRUD) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	id, ok := n.keyToID[key]
	if !ok {
		id = uuid.New()
	}
	return &store.AgentData{BaseModel: store.BaseModel{ID: id}, AgentKey: key}, nil
}
func (n noopAgentCRUD) GetByID(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (n noopAgentCRUD) GetByIDUnscoped(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (n noopAgentCRUD) GetByKeys(_ context.Context, _ []string) ([]store.AgentData, error) {
	return nil, nil
}
func (n noopAgentCRUD) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	return nil, nil
}
func (n noopAgentCRUD) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error { return nil }
func (n noopAgentCRUD) Delete(_ context.Context, _ uuid.UUID) error                    { return nil }
func (n noopAgentCRUD) List(_ context.Context, _ string) ([]store.AgentData, error)    { return nil, nil }
func (n noopAgentCRUD) GetDefault(_ context.Context) (*store.AgentData, error)         { return nil, nil }
func (n noopAgentCRUD) ResetStuckSummoning(_ context.Context) (int64, error)            { return 0, nil }

// --- helpers ---

func makeDelegateCtx() context.Context {
	ctx := store.WithAgentID(context.Background(), uuid.New())
	ctx = store.WithTenantID(ctx, uuid.New())
	ctx = store.WithAgentKey(ctx, "parent-agent")
	return ctx
}

// --- tests ---

func TestDelegateTool_SubagentStartBlock_AbortsDispatch(t *testing.T) {
	runCalled := 0
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		runCalled++
		return DelegateResult{Content: "ok"}, nil
	}

	tool := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, runFn)
	disp := &fakeDispatcher{decision: hooks.DecisionBlock}
	tool.SetHookDispatcher(disp)

	result := tool.Execute(makeDelegateCtx(), map[string]any{
		"agent_key": "child-agent",
		"task":      "do something",
		"mode":      "sync",
	})

	if result == nil || !result.IsError {
		t.Fatal("expected error result when hook blocks")
	}
	if runCalled != 0 {
		t.Errorf("runFn must not be called; got %d calls", runCalled)
	}
	if disp.calls != 1 {
		t.Errorf("expected 1 dispatcher Fire call; got %d", disp.calls)
	}
}

func TestDelegateTool_SubagentStartAllow_ProceedsToRun(t *testing.T) {
	runCalled := 0
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		runCalled++
		return DelegateResult{Content: "done"}, nil
	}

	tool := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, runFn)
	disp := &fakeDispatcher{decision: hooks.DecisionAllow}
	tool.SetHookDispatcher(disp)

	result := tool.Execute(makeDelegateCtx(), map[string]any{
		"agent_key": "child-agent",
		"task":      "do something",
		"mode":      "sync",
	})

	if result != nil && result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if runCalled != 1 {
		t.Errorf("expected runFn called once; got %d", runCalled)
	}
}

func TestDelegateTool_NilDispatcher_SkipsHook(t *testing.T) {
	runCalled := 0
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		runCalled++
		return DelegateResult{Content: "done"}, nil
	}

	// No SetHookDispatcher — hookDispatcher stays nil.
	tool := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, runFn)

	result := tool.Execute(makeDelegateCtx(), map[string]any{
		"agent_key": "child-agent",
		"task":      "do something",
		"mode":      "sync",
	})

	if result != nil && result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if runCalled != 1 {
		t.Errorf("expected runFn called once; got %d", runCalled)
	}
}
