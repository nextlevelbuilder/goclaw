package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubResumeRunsStore implements store.RunsStore for ResumeRun guard-path tests.
// Full pipeline execution is exercised by PG integration tests (needs a real
// provider); here we assert the availability/not-found/validation early exits.
type stubResumeRunsStore struct {
	run *store.AgentRun
	err error
}

func (s *stubResumeRunsStore) CreateRun(context.Context, *store.AgentRun) error { return nil }
func (s *stubResumeRunsStore) UpdateRunStatus(context.Context, string, string) error {
	return nil
}
func (s *stubResumeRunsStore) UpdateRunCheckpoint(context.Context, string, string, json.RawMessage) error {
	return nil
}
func (s *stubResumeRunsStore) UpdateRunTerminal(context.Context, string, string, string, time.Time) error {
	return nil
}
func (s *stubResumeRunsStore) TouchHeartbeat(context.Context, string) error { return nil }
func (s *stubResumeRunsStore) GetRun(context.Context, string) (*store.AgentRun, error) {
	return s.run, s.err
}
func (s *stubResumeRunsStore) ListRuns(context.Context, store.RunListOpts) ([]store.AgentRun, error) {
	return nil, nil
}
func (s *stubResumeRunsStore) RecoverStaleRuns(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func TestResumeRunUnavailableWithoutStore(t *testing.T) {
	l := &Loop{}
	if _, err := l.ResumeRun(context.Background(), "run-1"); !errors.Is(err, ErrRunResumeUnavailable) {
		t.Fatalf("err = %v, want ErrRunResumeUnavailable", err)
	}
}

func TestResumeRunRequiresRunID(t *testing.T) {
	l := &Loop{runsStore: &stubResumeRunsStore{}}
	if _, err := l.ResumeRun(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty run_id")
	}
}

func TestResumeRunNotFound(t *testing.T) {
	l := &Loop{runsStore: &stubResumeRunsStore{run: nil}}
	if _, err := l.ResumeRun(context.Background(), "run-1"); !errors.Is(err, ErrRunResumeNotFound) {
		t.Fatalf("err = %v, want ErrRunResumeNotFound", err)
	}
}

func TestResumeRunPropagatesStoreError(t *testing.T) {
	storeErr := errors.New("db down")
	l := &Loop{runsStore: &stubResumeRunsStore{err: storeErr}}
	if _, err := l.ResumeRun(context.Background(), "run-1"); !errors.Is(err, storeErr) {
		t.Fatalf("err = %v, want store error propagated", err)
	}
}

func TestRunRequestFromRunRecordMergesIdentityAndInput(t *testing.T) {
	run := &store.AgentRun{
		RunID:      "run-1",
		SessionKey: "session-1",
		UserID:     "user-1",
		Channel:    "telegram",
		ChatID:     "chat-1",
	}
	req := runRequestFromRunRecord(run, nil)
	if req.RunID != "run-1" || req.SessionKey != "session-1" || req.UserID != "user-1" ||
		req.Channel != "telegram" || req.ChatID != "chat-1" {
		t.Fatalf("identity fields not merged: %+v", req)
	}
}
