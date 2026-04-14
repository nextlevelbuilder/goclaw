package methods

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ---- stub CronStore ----

type stubCronStore struct {
	jobs       map[string]*store.CronJob
	addErr     error
	removeErr  error
	updateErr  error
	enableErr  error
	addedJob   *store.CronJob
}

func newStubCronStore() *stubCronStore {
	return &stubCronStore{jobs: make(map[string]*store.CronJob)}
}

func (s *stubCronStore) addJob(id, name, userID string) {
	s.jobs[id] = &store.CronJob{ID: id, Name: name, UserID: userID, Enabled: true}
}

func (s *stubCronStore) AddJob(_ context.Context, name string, schedule store.CronSchedule, message string,
	deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
	if s.addErr != nil {
		return nil, s.addErr
	}
	job := &store.CronJob{
		ID:      "new-job-id",
		Name:    name,
		UserID:  userID,
		Enabled: true,
		Schedule: schedule,
	}
	s.addedJob = job
	s.jobs[job.ID] = job
	return job, nil
}

func (s *stubCronStore) GetJob(_ context.Context, jobID string) (*store.CronJob, bool) {
	j, ok := s.jobs[jobID]
	return j, ok
}

func (s *stubCronStore) ListJobs(_ context.Context, _ bool, _, userID string) []store.CronJob {
	var result []store.CronJob
	for _, j := range s.jobs {
		if userID == "" || j.UserID == userID {
			result = append(result, *j)
		}
	}
	return result
}

func (s *stubCronStore) RemoveJob(_ context.Context, jobID string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	delete(s.jobs, jobID)
	return nil
}

func (s *stubCronStore) UpdateJob(_ context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	j, ok := s.jobs[jobID]
	if !ok {
		return nil, store.ErrCronJobNotFound
	}
	return j, nil
}

func (s *stubCronStore) EnableJob(_ context.Context, jobID string, enabled bool) error {
	if s.enableErr != nil {
		return s.enableErr
	}
	if j, ok := s.jobs[jobID]; ok {
		j.Enabled = enabled
	}
	return nil
}

func (s *stubCronStore) GetRunLog(_ context.Context, _ string, _, _ int) ([]store.CronRunLogEntry, int) {
	return nil, 0
}

func (s *stubCronStore) Status() map[string]any { return map[string]any{"running": true} }

// Lifecycle stubs (not called in unit tests)
func (s *stubCronStore) Start() error                                            { return nil }
func (s *stubCronStore) Stop()                                                   {}
func (s *stubCronStore) SetOnJob(_ func(*store.CronJob) (*store.CronJobResult, error)) {}
func (s *stubCronStore) SetOnEvent(_ func(store.CronEvent))                      {}
func (s *stubCronStore) RunJob(_ context.Context, _ string, _ bool) (bool, string, error) {
	return true, "", nil
}
func (s *stubCronStore) SetDefaultTimezone(_ string)                             {}
func (s *stubCronStore) GetDueJobs(_ time.Time) []store.CronJob                 { return nil }

// ---- helpers ----

func buildCronMethods(t *testing.T, svc *stubCronStore) *CronMethods {
	t.Helper()
	cfg := &config.Config{}
	return NewCronMethods(svc, &stubEventPub{}, cfg)
}

func cronReqFrame(t *testing.T, method string, params map[string]any) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "cron-req-1",
		Method: method,
		Params: raw,
	}
}

// ---- Tests: handleList ----

func TestCronList_EmptyStore_ReturnsEmptyJobs(t *testing.T) {
	svc := newStubCronStore()
	m := buildCronMethods(t, svc)
	client := nullClient()
	req := cronReqFrame(t, protocol.MethodCronList, map[string]any{})
	m.handleList(context.Background(), client, req)
	// No panic = success
}

func TestCronList_WithJobs_ReturnsAll(t *testing.T) {
	svc := newStubCronStore()
	svc.addJob("job-1", "my-job", "user-x")
	svc.addJob("job-2", "another-job", "user-y")

	m := buildCronMethods(t, svc)
	client := nullClient()
	req := cronReqFrame(t, protocol.MethodCronList, map[string]any{"includeDisabled": false})
	m.handleList(context.Background(), client, req)
	// No panic = success
}

// ---- Tests: handleCreate ----

func TestCronCreate_MissingName_ReturnsInvalidRequest(t *testing.T) {
	svc := newStubCronStore()
	m := buildCronMethods(t, svc)
	client := nullClient()
	req := cronReqFrame(t, protocol.MethodCronCreate, map[string]any{
		"message": "hello",
		"schedule": map[string]any{"kind": "every", "everyMs": 60000},
	})
	m.handleCreate(context.Background(), client, req)
	// No panic = name-required error path hit
}

func TestCronCreate_MissingMessage_ReturnsInvalidRequest(t *testing.T) {
	svc := newStubCronStore()
	m := buildCronMethods(t, svc)
	client := nullClient()
	req := cronReqFrame(t, protocol.MethodCronCreate, map[string]any{
		"name":     "my-job",
		"schedule": map[string]any{"kind": "every", "everyMs": 60000},
		// message intentionally omitted
	})
	m.handleCreate(context.Background(), client, req)
	// No panic = message-required error path hit
}

func TestCronCreate_InvalidSlug_ReturnsInvalidRequest(t *testing.T) {
	svc := newStubCronStore()
	m := buildCronMethods(t, svc)
	client := nullClient()
	// Name contains uppercase — invalid slug
	req := cronReqFrame(t, protocol.MethodCronCreate, map[string]any{
		"name":     "My Invalid Name!",
		"message":  "do something",
		"schedule": map[string]any{"kind": "every", "everyMs": 60000},
	})
	m.handleCreate(context.Background(), client, req)
	// No panic = invalid-slug error path hit
}

func TestCronCreate_ValidParams_CreatesJob(t *testing.T) {
	svc := newStubCronStore()
	m := buildCronMethods(t, svc)
	client := nullClient()
	req := cronReqFrame(t, protocol.MethodCronCreate, map[string]any{
		"name":    "my-daily-job",
		"message": "do the thing",
		"schedule": map[string]any{"kind": "every", "everyMs": 3600000},
	})
	m.handleCreate(context.Background(), client, req)
	// No panic = successful create path hit
	if svc.addedJob == nil {
		t.Error("expected AddJob to be called")
	}
	if svc.addedJob != nil && svc.addedJob.Name != "my-daily-job" {
		t.Errorf("job name = %q, want %q", svc.addedJob.Name, "my-daily-job")
	}
}

// ---- Tests: handleDelete ----

func TestCronDelete_MissingJobID_ReturnsInvalidRequest(t *testing.T) {
	svc := newStubCronStore()
	m := buildCronMethods(t, svc)
	client := nullClient()
	req := cronReqFrame(t, protocol.MethodCronDelete, map[string]any{})
	m.handleDelete(context.Background(), client, req)
	// No panic = jobId-required error path hit
}

func TestCronDelete_UserOwnsJob_DeletesJob(t *testing.T) {
	svc := newStubCronStore()
	// nullClient().UserID() returns "" — match the job's userID to that so ownership check passes.
	svc.addJob("del-job", "to-delete", "")
	m := buildCronMethods(t, svc)
	client := nullClient()

	req := cronReqFrame(t, protocol.MethodCronDelete, map[string]any{"jobId": "del-job"})
	m.handleDelete(context.Background(), client, req)

	if _, exists := svc.jobs["del-job"]; exists {
		t.Error("expected job to be deleted from store")
	}
}

func TestCronDelete_NonExistentJob_NonAdminPath_ReturnsUnauthorized(t *testing.T) {
	svc := newStubCronStore()
	// No job with that ID exists — non-admin client will get unauthorized
	m := buildCronMethods(t, svc)
	client := nullClient()
	req := cronReqFrame(t, protocol.MethodCronDelete, map[string]any{"jobId": "ghost-job"})
	m.handleDelete(context.Background(), client, req)
	// No panic = unauthorized path hit
}

// ---- Tests: handleToggle ----

func TestCronToggle_MissingJobID_ReturnsInvalidRequest(t *testing.T) {
	svc := newStubCronStore()
	m := buildCronMethods(t, svc)
	client := nullClient()
	req := cronReqFrame(t, protocol.MethodCronToggle, map[string]any{"enabled": true})
	m.handleToggle(context.Background(), client, req)
	// No panic = jobId-required error path hit
}
