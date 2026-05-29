package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type testCronStore struct {
	jobs      map[string]*store.CronJob
	updateCnt int
	runCnt    int
	lastForce bool
	updateErr error
	runErr    error
}

func newTestCronStore(job *store.CronJob) *testCronStore {
	jobs := map[string]*store.CronJob{}
	if job != nil {
		jobs[job.ID] = job
	}
	return &testCronStore{jobs: jobs}
}

func (s *testCronStore) AddJob(context.Context, string, store.CronSchedule, string, bool, string, string, string, string) (*store.CronJob, error) {
	return nil, nil
}

func (s *testCronStore) GetJob(_ context.Context, jobID string) (*store.CronJob, bool) {
	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *testCronStore) ListJobs(context.Context, bool, string, string) []store.CronJob {
	var jobs []store.CronJob
	for _, job := range s.jobs {
		jobs = append(jobs, *job)
	}
	return jobs
}

func (s *testCronStore) RemoveJob(context.Context, string) error { return nil }

func (s *testCronStore) UpdateJob(_ context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	s.updateCnt++
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	job := s.jobs[jobID]
	if patch.Message != "" {
		job.Payload.Message = patch.Message
	}
	return job, nil
}

func (s *testCronStore) EnableJob(context.Context, string, bool) error { return nil }
func (s *testCronStore) GetRunLog(context.Context, string, int, int) ([]store.CronRunLogEntry, int) {
	return nil, 0
}
func (s *testCronStore) Status() map[string]any                                      { return map[string]any{} }
func (s *testCronStore) Start() error                                                { return nil }
func (s *testCronStore) Stop()                                                       {}
func (s *testCronStore) SetOnJob(func(*store.CronJob) (*store.CronJobResult, error)) {}
func (s *testCronStore) SetOnEvent(func(store.CronEvent))                            {}

func (s *testCronStore) RunJob(_ context.Context, _ string, force bool) (bool, string, error) {
	s.runCnt++
	s.lastForce = force
	return true, "", s.runErr
}

func (s *testCronStore) GetDueJobs(time.Time) []store.CronJob { return nil }
func (s *testCronStore) SetDefaultTimezone(string)            {}

func TestCronToolBlocksCredentialBoundUpdateByDifferentUser(t *testing.T) {
	cronStore := newTestCronStore(&store.CronJob{
		ID:     "job-1",
		UserID: "group:telegram:-100123",
		Payload: store.CronPayload{
			Message:          "old",
			CredentialUserID: "tenant-user-a",
		},
	})
	tool := NewCronTool(cronStore)
	ctx := store.WithUserID(context.Background(), "group:telegram:-100123")
	ctx = store.WithCredentialUserID(ctx, "tenant-user-b")

	result := tool.Execute(ctx, map[string]any{
		"action": "update",
		"jobId":  "job-1",
		"patch":  map[string]any{"message": "run gh issue list"},
	})

	if !result.IsError || !strings.Contains(result.ForLLM, "credential context") {
		t.Fatalf("expected credential context error, got %#v", result)
	}
	if cronStore.updateCnt != 0 {
		t.Fatalf("UpdateJob called %d times, want 0", cronStore.updateCnt)
	}
}

func TestCronToolListRedactsCredentialUserID(t *testing.T) {
	cronStore := newTestCronStore(&store.CronJob{
		ID:     "job-1",
		UserID: "group:telegram:-100123",
		Payload: store.CronPayload{
			Message:          "run gh issue list",
			CredentialUserID: "tenant-user-a",
		},
	})
	tool := NewCronTool(cronStore)
	ctx := store.WithUserID(context.Background(), "group:telegram:-100123")

	result := tool.Execute(ctx, map[string]any{"action": "list", "includeDisabled": true})

	if result.IsError {
		t.Fatalf("list returned error: %#v", result)
	}
	if strings.Contains(result.ForLLM, "tenant-user-a") || strings.Contains(result.ForLLM, "credentialUserId") {
		t.Fatalf("credential identity leaked in list response: %s", result.ForLLM)
	}
}

func TestCronToolBlocksCredentialBoundRunByDifferentUser(t *testing.T) {
	cronStore := newTestCronStore(&store.CronJob{
		ID:     "job-1",
		UserID: "group:telegram:-100123",
		Payload: store.CronPayload{
			Message:          "run gh issue list",
			CredentialUserID: "tenant-user-a",
		},
	})
	tool := NewCronTool(cronStore)
	ctx := store.WithUserID(context.Background(), "group:telegram:-100123")
	ctx = store.WithCredentialUserID(ctx, "tenant-user-b")

	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"jobId":   "job-1",
		"runMode": "force",
	})

	if !result.IsError || !strings.Contains(result.ForLLM, "credential context") {
		t.Fatalf("expected credential context error, got %#v", result)
	}
	if cronStore.runCnt != 0 {
		t.Fatalf("RunJob called %d times, want 0", cronStore.runCnt)
	}
}
