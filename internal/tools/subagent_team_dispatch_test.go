package tools

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// teamDispatchRecorder is a PostTurnProcessor that records what was dispatched.
type teamDispatchRecorder struct {
	mu        sync.Mutex
	processed map[uuid.UUID][]uuid.UUID
}

func (r *teamDispatchRecorder) ProcessPendingTasks(_ context.Context, teamID uuid.UUID, taskIDs []uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.processed == nil {
		r.processed = make(map[uuid.UUID][]uuid.UUID)
	}
	r.processed[teamID] = append(r.processed[teamID], taskIDs...)
	return nil
}

func (r *teamDispatchRecorder) DispatchUnblockedTasks(context.Context, uuid.UUID) {}

func (r *teamDispatchRecorder) dispatched(teamID uuid.UUID) []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uuid.UUID(nil), r.processed[teamID]...)
}

// teamTaskCreatingProvider stands in for an agent whose turn calls
// team_tasks(action="create"): it records the tracker it saw and registers a
// task on it, exactly like executeCreate does.
type teamTaskCreatingProvider struct {
	teamID uuid.UUID
	taskID uuid.UUID

	mu     sync.Mutex
	seen   *PendingTeamDispatch
	called bool
}

func (p *teamTaskCreatingProvider) Name() string         { return "team-task-creating" }
func (p *teamTaskCreatingProvider) DefaultModel() string { return "provider-default" }

func (p *teamTaskCreatingProvider) Chat(ctx context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	ptd := PendingTeamDispatchFromCtx(ctx)
	p.mu.Lock()
	p.seen = ptd
	p.called = true
	p.mu.Unlock()
	if ptd != nil {
		ptd.Add(p.teamID, p.taskID)
	}
	return &providers.ChatResponse{Content: "done", FinishReason: "stop"}, nil
}

func (p *teamTaskCreatingProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func (p *teamTaskCreatingProvider) tracker() (*PendingTeamDispatch, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen, p.called
}

// An async spawn is detached from the parent's turn: by the time the child runs,
// the parent's post-turn drain has already fired. The child must therefore get
// its own tracker, drained when the child's own run ends — otherwise every team
// task it creates stays pending forever and is never dispatched.
func TestAsyncSpawnDispatchesTeamTasksCreatedAfterParentTurnEnded(t *testing.T) {
	recorder := &teamDispatchRecorder{}
	provider := &teamTaskCreatingProvider{teamID: uuid.New(), taskID: uuid.New()}
	manager := NewSubagentManager(provider, nil, "manager-default", nil, NewRegistry, SubagentConfig{
		MaxConcurrent:       20,
		MaxSpawnDepth:       1,
		MaxChildrenPerAgent: 5,
	})
	manager.SetPostTurnProcessor(recorder)

	parentCtx, parentDrain := InjectTeamDispatch(subagentTestContext("parent"), recorder)
	parentTracker := PendingTeamDispatchFromCtx(parentCtx)
	if parentTracker == nil {
		t.Fatal("parent context has no pending team dispatch tracker")
	}
	// The parent's turn ends before the detached child ever starts.
	parentDrain()

	if _, err := manager.Spawn(
		parentCtx, "parent", 0, "create a team task", "child", "", "test", "chat", "direct", nil,
	); err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if len(recorder.dispatched(provider.teamID)) > 0 {
			break
		}
		if time.Now().After(deadline) {
			_, called := provider.tracker()
			t.Fatalf("team task was never dispatched (child ran = %v)", called)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := recorder.dispatched(provider.teamID); len(got) != 1 || got[0] != provider.taskID {
		t.Fatalf("dispatched tasks = %v, want [%v]", got, provider.taskID)
	}
	childTracker, _ := provider.tracker()
	if childTracker == nil {
		t.Fatal("child run had no pending team dispatch tracker")
	}
	if childTracker == parentTracker {
		t.Fatal("child run reused the parent's tracker; its tasks would never be drained")
	}
	if leftover := parentTracker.Drain(); len(leftover) != 0 {
		t.Fatalf("parent tracker collected child tasks: %v", leftover)
	}
}
