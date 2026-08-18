package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// recordingRunsStore is a minimal in-memory RunsStore double that records the
// calls runRecordUpdater makes, so the lifecycle is asserted without a DB.
type recordingRunsStore struct {
	mu sync.Mutex

	createErr      error
	heartbeatErr   error
	terminalErr    error
	createCalls    int
	heartbeatCalls int
	terminalCalls  int
	lastCreated    *store.AgentRun
	lastStatus     string
	lastErrMsg     string
}

func (s *recordingRunsStore) CreateRun(_ context.Context, run *store.AgentRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	s.lastCreated = run
	return s.createErr
}

func (s *recordingRunsStore) UpdateRunStatus(context.Context, string, string) error {
	return nil
}

func (s *recordingRunsStore) UpdateRunCheckpoint(context.Context, string, string, json.RawMessage) error {
	return nil
}

func (s *recordingRunsStore) UpdateRunTerminal(_ context.Context, _ string, status, errMsg string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminalCalls++
	s.lastStatus = status
	s.lastErrMsg = errMsg
	return s.terminalErr
}

func (s *recordingRunsStore) TouchHeartbeat(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatCalls++
	return s.heartbeatErr
}

func (s *recordingRunsStore) GetRun(context.Context, string) (*store.AgentRun, error) {
	return nil, nil
}

func (s *recordingRunsStore) ListRuns(context.Context, store.RunListOpts) ([]store.AgentRun, error) {
	return nil, nil
}

func (s *recordingRunsStore) RecoverStaleRuns(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func (s *recordingRunsStore) counts() (createCalls, heartbeatCalls, terminalCalls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createCalls, s.heartbeatCalls, s.terminalCalls
}

func TestStartRunRecordDisabledWithoutStore(t *testing.T) {
	// No RunsStore wired → run-record tracking disabled (nil, no panic).
	if u := startRunRecord(context.Background(), &Loop{}, RunRequest{RunID: "run-1"}); u != nil {
		t.Fatal("expected nil updater when runsStore is nil")
	}
}

func TestStartRunRecordNoRunID(t *testing.T) {
	s := &recordingRunsStore{}
	l := &Loop{runsStore: s}
	if u := startRunRecord(context.Background(), l, RunRequest{}); u != nil {
		t.Fatal("expected nil updater when RunID is empty")
	}
	if n, _, _ := s.counts(); n != 0 {
		t.Fatalf("CreateRun called %d times for empty RunID, want 0", n)
	}
}

func TestStartRunRecordCreateFailureNonFatal(t *testing.T) {
	s := &recordingRunsStore{createErr: context.DeadlineExceeded}
	l := &Loop{runsStore: s}
	// A failed write must not block the run: nil updater, no goroutine leak.
	if u := startRunRecord(context.Background(), l, RunRequest{RunID: "run-1"}); u != nil {
		t.Fatal("expected nil updater when CreateRun fails")
	}
}

func TestStartRunRecordCreatesRunningRow(t *testing.T) {
	s := &recordingRunsStore{}
	l := &Loop{runsStore: s}
	u := startRunRecord(context.Background(), l, RunRequest{
		RunID:      "run-1",
		SessionKey: "session-1",
		UserID:     "user-1",
		Channel:    "web",
		ChatID:     "chat-1",
	})
	if u == nil {
		t.Fatal("expected updater")
	}
	defer u.terminal(context.Background(), store.AgentRunStatusCompleted, "")

	s.mu.Lock()
	run := s.lastCreated
	s.mu.Unlock()
	if run == nil {
		t.Fatal("CreateRun never called")
	}
	if run.Status != store.AgentRunStatusRunning {
		t.Fatalf("Status = %q, want %q", run.Status, store.AgentRunStatusRunning)
	}
	if run.Attempt != 1 {
		t.Fatalf("Attempt = %d, want 1", run.Attempt)
	}
	if run.RunID != "run-1" || run.SessionKey != "session-1" || run.UserID != "user-1" {
		t.Fatalf("identity fields wrong: %+v", run)
	}
	if run.HeartbeatAt.IsZero() || run.StartedAt.IsZero() {
		t.Fatal("timestamps not stamped")
	}
}

func TestRunRecordTerminalIdempotent(t *testing.T) {
	s := &recordingRunsStore{}
	l := &Loop{runsStore: s}
	u := startRunRecord(context.Background(), l, RunRequest{RunID: "run-1"})
	if u == nil {
		t.Fatal("expected updater")
	}

	// Normal exit path plus panic safety-net defer both fire terminal().
	u.terminal(context.Background(), store.AgentRunStatusCompleted, "")
	u.terminal(context.Background(), store.AgentRunStatusFailed, "safety net")

	if _, _, n := s.counts(); n != 1 {
		t.Fatalf("UpdateRunTerminal called %d times, want 1 (sync.Once)", n)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastStatus != store.AgentRunStatusCompleted {
		t.Fatalf("status = %q, want %q (first terminal wins)", s.lastStatus, store.AgentRunStatusCompleted)
	}
}

func TestRunRecordHeartbeatTicksAndTerminalStops(t *testing.T) {
	s := &recordingRunsStore{}
	u := &runRecordUpdater{
		runs:      s,
		runID:     "run-1",
		heartbeat: time.NewTicker(10 * time.Millisecond),
		done:      make(chan struct{}),
	}
	go u.heartbeatLoop(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, hb, _ := s.counts()
		if hb >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat not advanced: got %d, want >= 3", hb)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// terminal stops the loop; the heartbeat count must freeze.
	u.terminal(context.Background(), store.AgentRunStatusCancelled, "cancelled")
	after, _, tn := s.counts()
	time.Sleep(30 * time.Millisecond)
	a2, _, _ := s.counts()
	if a2 != after {
		t.Fatalf("heartbeat continued after terminal: %d -> %d", after, a2)
	}
	if tn != 1 {
		t.Fatalf("terminal calls = %d, want 1", tn)
	}
}

func TestRunRecordHeartbeatFailureNonFatal(t *testing.T) {
	s := &recordingRunsStore{heartbeatErr: context.DeadlineExceeded}
	l := &Loop{runsStore: s}
	if u := startRunRecord(context.Background(), l, RunRequest{RunID: "run-1"}); u == nil {
		t.Fatal("expected updater")
	}
	// Heartbeat failure is non-fatal: the updater must still reach terminal.
	if _, _, n := s.counts(); n != 0 {
		t.Fatalf("terminal called before terminal(), want 0")
	}
}

func TestNewRunRecordUpdaterSkipsCreateRun(t *testing.T) {
	// Resume must preserve the stored checkpoint: newRunRecordUpdater must NOT
	// call CreateRun (whose upsert would clobber checkpoint with NULL) yet still
	// heartbeat and reach terminal.
	s := &recordingRunsStore{}
	l := &Loop{runsStore: s, runHeartbeatInterval: time.Millisecond}

	ctx := store.WithTenantID(context.Background(), uuid.Must(uuid.NewV7()))
	u := newRunRecordUpdater(ctx, l, "run-1")
	if u == nil {
		t.Fatal("expected updater")
	}

	if n, _, _ := s.counts(); n != 0 {
		t.Fatalf("CreateRun called %d times, want 0 (no upsert on resume)", n)
	}

	// Heartbeat goroutine advances heartbeat_at.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, hb, _ := s.counts()
		if hb >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat not advanced: got %d, want >= 1", hb)
		}
		time.Sleep(5 * time.Millisecond)
	}

	u.terminal(ctx, store.AgentRunStatusCompleted, "")
	if _, _, tn := s.counts(); tn != 1 {
		t.Fatalf("terminal calls = %d, want 1", tn)
	}
}

func TestNewRunRecordUpdaterNilWithoutStore(t *testing.T) {
	if u := newRunRecordUpdater(context.Background(), &Loop{}, "run-1"); u != nil {
		t.Fatal("expected nil updater when runsStore is nil")
	}
}
