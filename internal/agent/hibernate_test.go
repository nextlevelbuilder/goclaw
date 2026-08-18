package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// hibernationRunsStore is a RunsStore double that records the checkpoint/status
// writes Loop.SuspendRun makes, so the pause lifecycle is asserted without a DB.
type hibernationRunsStore struct {
	mu               sync.Mutex
	run              *store.AgentRun
	err              error
	checkpointCalls  int
	checkpointRunID  string
	checkpointStatus string
}

func (s *hibernationRunsStore) CreateRun(context.Context, *store.AgentRun) error { return nil }

func (s *hibernationRunsStore) UpdateRunStatus(context.Context, string, string) error {
	return nil
}

func (s *hibernationRunsStore) UpdateRunCheckpoint(_ context.Context, runID, status string, _ json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpointCalls++
	s.checkpointRunID = runID
	s.checkpointStatus = status
	return s.err
}

func (s *hibernationRunsStore) UpdateRunTerminal(context.Context, string, string, string, time.Time) error {
	return nil
}

func (s *hibernationRunsStore) TouchHeartbeat(context.Context, string) error { return nil }

func (s *hibernationRunsStore) GetRun(context.Context, string) (*store.AgentRun, error) {
	return s.run, s.err
}

func (s *hibernationRunsStore) ListRuns(context.Context, store.RunListOpts) ([]store.AgentRun, error) {
	return nil, nil
}

func (s *hibernationRunsStore) RecoverStaleRuns(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

// suspendableAgent is an Agent that also implements loopSuspender. Its ID() is
// the agent_key used by Router.Register/Get.
type suspendableAgent struct {
	stubAgent
	suspendFn func(ctx context.Context, runID string) error
}

func (a *suspendableAgent) SuspendRun(ctx context.Context, runID string) error {
	if a.suspendFn == nil {
		return nil
	}
	return a.suspendFn(ctx, runID)
}

func TestLoopSuspendRunUnavailableWithoutStore(t *testing.T) {
	l := &Loop{}
	if err := l.SuspendRun(context.Background(), "run-1"); !errors.Is(err, ErrRunSuspendUnavailable) {
		t.Fatalf("err = %v, want ErrRunSuspendUnavailable", err)
	}
}

func TestLoopSuspendRunRequiresRunID(t *testing.T) {
	l := &Loop{runsStore: &hibernationRunsStore{}}
	if err := l.SuspendRun(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty run_id")
	}
}

func TestLoopSuspendRunNotFound(t *testing.T) {
	l := &Loop{runsStore: &hibernationRunsStore{run: nil}}
	if err := l.SuspendRun(context.Background(), "run-1"); !errors.Is(err, ErrRunSuspendNotFound) {
		t.Fatalf("err = %v, want ErrRunSuspendNotFound", err)
	}
}

func TestLoopSuspendRunWritesCheckpointAndPausedStatus(t *testing.T) {
	run := &store.AgentRun{
		RunID:      "run-1",
		Status:     store.AgentRunStatusRunning,
		Checkpoint: json.RawMessage(`{"iteration":3,"input":{"content":"hi"}}`),
	}
	stores := &hibernationRunsStore{run: run}

	var gotEvent *AgentEvent
	l := &Loop{
		id:        "agent-1",
		runsStore: stores,
		onEvent:   func(e AgentEvent) { gotEvent = &e },
	}
	if err := l.SuspendRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("SuspendRun: %v", err)
	}

	stores.mu.Lock()
	defer stores.mu.Unlock()
	if stores.checkpointCalls != 1 {
		t.Fatalf("checkpointCalls = %d, want 1", stores.checkpointCalls)
	}
	if stores.checkpointRunID != "run-1" {
		t.Fatalf("checkpointRunID = %q, want run-1", stores.checkpointRunID)
	}
	if stores.checkpointStatus != store.RunTimelineStatusPaused {
		t.Fatalf("checkpointStatus = %q, want %q", stores.checkpointStatus, store.RunTimelineStatusPaused)
	}
	if gotEvent == nil {
		t.Fatal("expected AgentEventRunPaused event, got none")
	}
	if gotEvent.Type != protocol.AgentEventRunPaused {
		t.Fatalf("event type = %q, want %q", gotEvent.Type, protocol.AgentEventRunPaused)
	}
	if gotEvent.AgentID != "agent-1" || gotEvent.RunID != "run-1" {
		t.Fatalf("event agent/run = %q/%q", gotEvent.AgentID, gotEvent.RunID)
	}
	payload, ok := gotEvent.Payload.(*protocol.RunPausedPayload)
	if !ok {
		t.Fatalf("payload type = %T", gotEvent.Payload)
	}
	if payload.RunID != "run-1" || payload.Iteration != 3 {
		t.Fatalf("payload = %+v, want RunID run-1 Iteration 3", payload)
	}
}

func TestLoopSuspendRunIsIdempotentOnPaused(t *testing.T) {
	run := &store.AgentRun{RunID: "run-1", Status: store.RunTimelineStatusPaused}
	stores := &hibernationRunsStore{run: run}
	l := &Loop{runsStore: stores}
	if err := l.SuspendRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("SuspendRun: %v", err)
	}
	if stores.checkpointCalls != 0 {
		t.Fatalf("checkpointCalls = %d, want 0 (idempotent no-op)", stores.checkpointCalls)
	}
}

func TestLoopSuspendRunLeavesTerminalRunUntouched(t *testing.T) {
	for _, status := range []string{
		store.AgentRunStatusCompleted,
		store.AgentRunStatusFailed,
		store.AgentRunStatusCancelled,
	} {
		run := &store.AgentRun{RunID: "run-1", Status: status}
		stores := &hibernationRunsStore{run: run}
		l := &Loop{runsStore: stores}
		if err := l.SuspendRun(context.Background(), "run-1"); err != nil {
			t.Fatalf("SuspendRun(status=%q): %v", status, err)
		}
		if stores.checkpointCalls != 0 {
			t.Fatalf("checkpointCalls(status=%q) = %d, want 0", status, stores.checkpointCalls)
		}
	}
}

func TestLoopSuspendRunPropagatesStoreError(t *testing.T) {
	storeErr := errors.New("db down")
	run := &store.AgentRun{RunID: "run-1", Status: store.AgentRunStatusRunning}
	stores := &hibernationRunsStore{run: run, err: storeErr}
	l := &Loop{runsStore: stores}
	if err := l.SuspendRun(context.Background(), "run-1"); !errors.Is(err, storeErr) {
		t.Fatalf("err = %v, want store error propagated", err)
	}
}

// TestPackageSuspendRunResolvesViaRouter verifies the router-based package-level
// entrypoint: it resolves the owning Agent by the run's AgentID (a UUID, resolved
// to the agent_key through the router resolver) and delegates to the loop's
// SuspendRun.
func TestPackageSuspendRunResolvesViaRouter(t *testing.T) {
	agentUUID := uuid.Must(uuid.NewV7())
	target := &suspendableAgent{stubAgent: stubAgent{id: "agent-1"}}
	router := NewRouter()
	router.SetResolver(func(_ context.Context, _ string) (Agent, error) {
		return target, nil
	})
	run := &store.AgentRun{RunID: "run-1", AgentID: &agentUUID}
	stores := &hibernationRunsStore{run: run}

	called := false
	target.suspendFn = func(_ context.Context, runID string) error {
		called = true
		if runID != "run-1" {
			t.Fatalf("runID = %q, want run-1", runID)
		}
		return nil
	}

	if err := SuspendRun(context.Background(), router, stores, "run-1"); err != nil {
		t.Fatalf("SuspendRun: %v", err)
	}
	if !called {
		t.Fatal("expected delegated SuspendRun call")
	}
}

func TestPackageSuspendRunUnavailableWithoutWiring(t *testing.T) {
	if err := SuspendRun(context.Background(), nil, nil, "run-1"); !errors.Is(err, ErrRunSuspendUnavailable) {
		t.Fatalf("err = %v, want ErrRunSuspendUnavailable", err)
	}
}

func TestPackageSuspendRunNotSupportedByAgent(t *testing.T) {
	// stubAgent lacks SuspendRun → assert unavailable.
	agentUUID := uuid.Must(uuid.NewV7())
	router := NewRouter()
	router.SetResolver(func(_ context.Context, _ string) (Agent, error) {
		return &stubAgent{id: "agent-1"}, nil
	})
	run := &store.AgentRun{RunID: "run-1", AgentID: &agentUUID}
	stores := &hibernationRunsStore{run: run}
	if err := SuspendRun(context.Background(), router, stores, "run-1"); !errors.Is(err, ErrRunSuspendUnavailable) {
		t.Fatalf("err = %v, want ErrRunSuspendUnavailable", err)
	}
}

func TestWakeRunDelegatesToResumer(t *testing.T) {
	called := false
	resume := func(_ context.Context, runID string) (*RunResult, error) {
		called = true
		if runID != "run-1" {
			t.Fatalf("runID = %q, want run-1", runID)
		}
		return &RunResult{RunID: "run-1"}, nil
	}
	res, err := WakeRun(context.Background(), resume, "run-1")
	if err != nil {
		t.Fatalf("WakeRun: %v", err)
	}
	if !called {
		t.Fatal("expected resume closure call")
	}
	if res == nil || res.RunID != "run-1" {
		t.Fatalf("result = %+v, want RunID run-1", res)
	}
}

func TestWakeRunUnavailableWithoutResumer(t *testing.T) {
	// WakeRun with a nil resume closure reports the resume-unavailable sentinel.
	if _, err := WakeRun(context.Background(), nil, "run-1"); !errors.Is(err, ErrRunResumeUnavailable) {
		t.Fatalf("err = %v, want ErrRunResumeUnavailable", err)
	}
}

func TestCheckpointIterationExtractsSavedIteration(t *testing.T) {
	if got := checkpointIteration(json.RawMessage(`{"iteration":7}`)); got != 7 {
		t.Fatalf("got %d, want 7", got)
	}
	if got := checkpointIteration(json.RawMessage(`{}`)); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
	if got := checkpointIteration(json.RawMessage(`not-json`)); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
	if got := checkpointIteration(nil); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

// Compile-time assertions for the double's surface.
var _ loopSuspender = (*suspendableAgent)(nil)
var _ Agent = (*suspendableAgent)(nil)