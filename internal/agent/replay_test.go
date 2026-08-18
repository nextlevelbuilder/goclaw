package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubReplayAgent implements both Agent and loopReplayer so ReplayRun can
// resolve it via a Router and assert the replay capability (the same path the
// gateway wiring uses in production). id must equal the owning run's AgentID
// string so the router cache hit matches the Get lookup.
type stubReplayAgent struct {
	id        string
	runResult *RunResult
	runErr    error
	resumed   *json.RawMessage
}

func (a *stubReplayAgent) ID() string { return a.id }
func (a *stubReplayAgent) UUID() uuid.UUID                   { return uuid.New() }
func (a *stubReplayAgent) OtherConfig() json.RawMessage      { return nil }
func (a *stubReplayAgent) Run(context.Context, RunRequest) (*RunResult, error) {
	return a.runResult, a.runErr
}
func (a *stubReplayAgent) IsRunning() bool                    { return false }
func (a *stubReplayAgent) Model() string                      { return "test" }
func (a *stubReplayAgent) ProviderName() string               { return "test" }
func (a *stubReplayAgent) Provider() providers.Provider       { return nil }
func (a *stubReplayAgent) ResumeRunFrom(_ context.Context, _ string, cp json.RawMessage) (*RunResult, error) {
	a.resumed = &cp
	return a.runResult, a.runErr
}

// stubSpySnapshots implements store.CheckpointSnapshotStore for ReplayRun tests.
type stubSpySnapshots struct {
	snap *store.CheckpointSnapshot
	err  error
}

func (s *stubSpySnapshots) AppendCheckpointSnapshot(context.Context, *store.CheckpointSnapshot) error {
	return nil
}
func (s *stubSpySnapshots) ListCheckpointSnapshots(context.Context, store.CheckpointSnapshotListOpts) ([]store.CheckpointSnapshot, error) {
	return nil, nil
}
func (s *stubSpySnapshots) GetCheckpointSnapshot(context.Context, string, int) (*store.CheckpointSnapshot, error) {
	return s.snap, s.err
}

func TestReplayRunUnavailableWhenCapabilitiesMissing(t *testing.T) {
	// All three of router/runs/snaps are required; any missing → unavailable.
	if _, err := ReplayRun(context.Background(), nil, nil, nil, "run-1", 1); !errors.Is(err, ErrRunReplayUnavailable) {
		t.Fatalf("err = %v, want ErrRunReplayUnavailable", err)
	}
}

func TestReplayRunRequiresRunID(t *testing.T) {
	router := NewRouter()
	router.Register(&stubReplayAgent{})
	runs := &stubResumeRunsStore{run: &store.AgentRun{RunID: "run-1", AgentID: nil}}
	snaps := &stubSpySnapshots{snap: &store.CheckpointSnapshot{Snapshot: json.RawMessage(`{}`), Iteration: 2}}
	if _, err := ReplayRun(context.Background(), router, runs, snaps, "", 1); err == nil {
		t.Fatal("expected error for empty run_id")
	}
}

func TestReplayRunNotFoundWithoutRun(t *testing.T) {
	router := NewRouter()
	runs := &stubResumeRunsStore{run: nil}
	snaps := &stubSpySnapshots{}
	_, err := ReplayRun(context.Background(), router, runs, snaps, "run-1", 1)
	if !errors.Is(err, ErrRunReplayNotFound) {
		t.Fatalf("err = %v, want ErrRunReplayNotFound", err)
	}
}

func TestReplayRunNotFoundWhenSnapshotMissing(t *testing.T) {
	router := NewRouter()
	agentID := uuid.New()
	runs := &stubResumeRunsStore{run: &store.AgentRun{RunID: "run-1", AgentID: &agentID}}
	snaps := &stubSpySnapshots{err: errors.New("no rows")}
	_, err := ReplayRun(context.Background(), router, runs, snaps, "run-1", 3)
	if !errors.Is(err, ErrRunReplayNotFound) {
		t.Fatalf("err = %v, want ErrRunReplayNotFound", err)
	}
}

func TestReplayRunDispatchesToOwnerLoop(t *testing.T) {
	router := NewRouter()
	agentID := uuid.New()
	replayAgent := &stubReplayAgent{id: agentID.String(), runResult: &RunResult{Content: "replayed answer"}}
	router.Register(replayAgent)
	runs := &stubResumeRunsStore{run: &store.AgentRun{
		RunID:   "run-1",
		AgentID: &agentID,
		Status:  store.RunTimelineStatusPaused,
	}}
	cp := json.RawMessage(`{"version":1,"run_id":"run-1","iteration":4}`)
	snaps := &stubSpySnapshots{snap: &store.CheckpointSnapshot{RunID: "run-1", Seq: 4, Snapshot: cp, Iteration: 4}}

	res, err := ReplayRun(context.Background(), router, runs, snaps, "run-1", 4)
	if err != nil {
		t.Fatalf("ReplayRun: %v", err)
	}
	if res == nil || res.Content != "replayed answer" {
		t.Fatalf("result = %+v, want replayed answer", res)
	}
	if replayAgent.resumed == nil || string(*replayAgent.resumed) != string(cp) {
		t.Fatalf("resumed checkpoint = %v, want %s", replayAgent.resumed, cp)
	}
}

func TestResumeRunFromUnavailableWithoutStore(t *testing.T) {
	l := &Loop{}
	if _, err := l.ResumeRunFrom(context.Background(), "run-1", json.RawMessage(`{}`)); !errors.Is(err, ErrRunResumeUnavailable) {
		t.Fatalf("err = %v, want ErrRunResumeUnavailable", err)
	}
}

func TestResumeRunFromNotFound(t *testing.T) {
	l := &Loop{runsStore: &stubResumeRunsStore{run: nil}}
	if _, err := l.ResumeRunFrom(context.Background(), "run-1", json.RawMessage(`{}`)); !errors.Is(err, ErrRunResumeNotFound) {
		t.Fatalf("err = %v, want ErrRunResumeNotFound", err)
	}
}

func TestResumeRunFromRequiresRunID(t *testing.T) {
	l := &Loop{runsStore: &stubResumeRunsStore{}}
	if _, err := l.ResumeRunFrom(context.Background(), "", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for empty run_id")
	}
}

func TestResumeRunFromPropagatesStoreError(t *testing.T) {
	storeErr := errors.New("db down")
	l := &Loop{runsStore: &stubResumeRunsStore{err: storeErr}}
	if _, err := l.ResumeRunFrom(context.Background(), "run-1", json.RawMessage(`{}`)); !errors.Is(err, storeErr) {
		t.Fatalf("err = %v, want store error propagated", err)
	}
}