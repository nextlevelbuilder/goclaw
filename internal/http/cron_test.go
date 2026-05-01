package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestCronHandlerCreatePersistsOverrides(t *testing.T) {
	fakeStore := newFakeCronStore()
	handler := NewCronHandler(fakeStore)

	req := httptest.NewRequest(http.MethodPost, "/v1/cron", strings.NewReader(`{
		"name":"daily-social-rollup",
		"schedule":{"kind":"cron","expr":"0 9 * * *","tz":"UTC"},
		"message":"Summarize social metrics",
		"enabled":false,
		"managed":{"by":"gillen","source":"configmap","key":"social-rollup","version":"v1","declaredHash":"abc"},
		"provider":"openai",
		"model":"gpt-5.5"
	}`))
	req = req.WithContext(store.WithRole(req.Context(), "admin"))
	rec := httptest.NewRecorder()

	handler.handleCreate(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	job := fakeStore.jobs["job-1"]
	if job == nil {
		t.Fatal("expected job to be created")
	}
	if job.Enabled {
		t.Fatal("expected enabled override to be persisted")
	}
	if job.Managed.By != "gillen" || job.Managed.Key != "social-rollup" {
		t.Fatalf("managed metadata not persisted: %+v", job.Managed)
	}
	if job.Provider != "openai" || job.Model != "gpt-5.5" {
		t.Fatalf("provider/model not persisted: provider=%q model=%q", job.Provider, job.Model)
	}
}

func TestCronHandlerListFiltersManagedJobs(t *testing.T) {
	fakeStore := newFakeCronStore()
	fakeStore.jobs["a"] = fakeJob("a", "gillen", "one")
	fakeStore.jobs["b"] = fakeJob("b", "other", "two")
	handler := NewCronHandler(fakeStore)

	req := httptest.NewRequest(http.MethodGet, "/v1/cron?includeDisabled=true&managedBy=gillen&managedKey=one", nil)
	req = req.WithContext(store.WithRole(req.Context(), "admin"))
	rec := httptest.NewRecorder()

	handler.handleList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Jobs []store.CronJob `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Jobs) != 1 || resp.Jobs[0].ID != "a" {
		t.Fatalf("unexpected jobs: %+v", resp.Jobs)
	}
}

func TestCronHandlerListScopesOperatorToContextUser(t *testing.T) {
	fakeStore := newFakeCronStore()
	fakeStore.jobs["a"] = fakeJobWithUser("a", "alice")
	fakeStore.jobs["b"] = fakeJobWithUser("b", "bob")
	handler := NewCronHandler(fakeStore)

	req := httptest.NewRequest(http.MethodGet, "/v1/cron?includeDisabled=true&userId=bob", nil)
	ctx := store.WithUserID(store.WithRole(req.Context(), "operator"), "alice")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.handleList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Jobs []store.CronJob `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Jobs) != 1 || resp.Jobs[0].UserID != "alice" {
		t.Fatalf("unexpected scoped jobs: %+v", resp.Jobs)
	}
}

func TestCronHandlerCreateUsesOperatorContextUser(t *testing.T) {
	fakeStore := newFakeCronStore()
	handler := NewCronHandler(fakeStore)

	req := httptest.NewRequest(http.MethodPost, "/v1/cron", strings.NewReader(`{
		"name":"owned",
		"userId":"bob",
		"schedule":{"kind":"cron","expr":"0 9 * * *","tz":"UTC"},
		"message":"run"
	}`))
	ctx := store.WithUserID(store.WithRole(req.Context(), "operator"), "alice")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.handleCreate(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := fakeStore.jobs["job-1"].UserID; got != "alice" {
		t.Fatalf("expected context user alice, got %q", got)
	}
}

func TestCronHandlerRejectsOperatorMutatingOtherUserJob(t *testing.T) {
	fakeStore := newFakeCronStore()
	fakeStore.jobs["job-1"] = fakeJobWithUser("job-1", "bob")
	handler := NewCronHandler(fakeStore)

	req := httptest.NewRequest(http.MethodDelete, "/v1/cron/job-1", nil)
	req.SetPathValue("id", "job-1")
	ctx := store.WithUserID(store.WithRole(req.Context(), "operator"), "alice")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.handleDelete(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if fakeStore.jobs["job-1"] == nil {
		t.Fatal("expected unauthorized delete to leave job intact")
	}
}

func TestCronHandlerCreateRollsBackFailedOverridePatch(t *testing.T) {
	fakeStore := newFakeCronStore()
	fakeStore.updateErr = errors.New("patch failed")
	handler := NewCronHandler(fakeStore)

	req := httptest.NewRequest(http.MethodPost, "/v1/cron", strings.NewReader(`{
		"name":"rollback",
		"schedule":{"kind":"cron","expr":"0 9 * * *","tz":"UTC"},
		"message":"run",
		"provider":"provider-name-that-fails"
	}`))
	req = req.WithContext(store.WithRole(req.Context(), "admin"))
	rec := httptest.NewRecorder()

	handler.handleCreate(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if fakeStore.jobs["job-1"] != nil {
		t.Fatal("expected failed override patch to roll back created job")
	}
	if fakeStore.removeCalls != 1 {
		t.Fatalf("expected one rollback remove, got %d", fakeStore.removeCalls)
	}
}

func fakeJob(id, by, key string) *store.CronJob {
	return &store.CronJob{
		ID:      id,
		Name:    id,
		Enabled: true,
		Managed: store.CronManaged{By: by, Key: key},
		Schedule: store.CronSchedule{
			Kind: "cron",
			Expr: "0 9 * * *",
		},
		Payload: store.CronPayload{Kind: "agent_turn", Message: "run"},
	}
}

func fakeJobWithUser(id, userID string) *store.CronJob {
	job := fakeJob(id, "gillen", id)
	job.UserID = userID
	return job
}

type fakeCronStore struct {
	jobs        map[string]*store.CronJob
	onJob       func(job *store.CronJob) (*store.CronJobResult, error)
	updateErr   error
	removeCalls int
}

func newFakeCronStore() *fakeCronStore {
	return &fakeCronStore{jobs: make(map[string]*store.CronJob)}
}

func (s *fakeCronStore) AddJob(_ context.Context, name string, schedule store.CronSchedule, message string, deliver bool, channel, to, agentID, userID string) (*store.CronJob, error) {
	job := store.CronJob{
		ID:             "job-1",
		Name:           name,
		AgentID:        agentID,
		UserID:         userID,
		Enabled:        true,
		Schedule:       schedule,
		Payload:        store.CronPayload{Kind: "agent_turn", Message: message},
		Deliver:        deliver,
		DeliverChannel: channel,
		DeliverTo:      to,
	}
	s.jobs[job.ID] = &job
	return &job, nil
}

func (s *fakeCronStore) GetJob(_ context.Context, jobID string) (*store.CronJob, bool) {
	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *fakeCronStore) ListJobs(_ context.Context, includeDisabled bool, agentID, userID string) []store.CronJob {
	var jobs []store.CronJob
	for _, job := range s.jobs {
		if !includeDisabled && !job.Enabled {
			continue
		}
		if agentID != "" && job.AgentID != agentID {
			continue
		}
		if userID != "" && job.UserID != userID {
			continue
		}
		jobs = append(jobs, *job)
	}
	return jobs
}

func (s *fakeCronStore) RemoveJob(_ context.Context, jobID string) error {
	s.removeCalls++
	delete(s.jobs, jobID)
	return nil
}

func (s *fakeCronStore) UpdateJob(_ context.Context, jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	job := s.jobs[jobID]
	if job == nil {
		return nil, store.ErrCronJobNotFound
	}
	if patch.Enabled != nil {
		job.Enabled = *patch.Enabled
	}
	if patch.Managed != nil {
		job.Managed = *patch.Managed
	}
	if patch.Provider != nil {
		job.Provider = *patch.Provider
	}
	if patch.Model != nil {
		job.Model = *patch.Model
	}
	if patch.Stateless != nil {
		job.Stateless = *patch.Stateless
	}
	if patch.WakeHeartbeat != nil {
		job.WakeHeartbeat = *patch.WakeHeartbeat
	}
	if patch.DeleteAfterRun != nil {
		job.DeleteAfterRun = *patch.DeleteAfterRun
	}
	return job, nil
}

func (s *fakeCronStore) EnableJob(_ context.Context, jobID string, enabled bool) error {
	job := s.jobs[jobID]
	if job == nil {
		return store.ErrCronJobNotFound
	}
	job.Enabled = enabled
	return nil
}

func (s *fakeCronStore) GetRunLog(context.Context, string, int, int) ([]store.CronRunLogEntry, int) {
	return nil, 0
}

func (s *fakeCronStore) Status() map[string]any { return map[string]any{"jobs": len(s.jobs)} }
func (s *fakeCronStore) Start() error           { return nil }
func (s *fakeCronStore) Stop()                  {}
func (s *fakeCronStore) SetOnJob(handler func(job *store.CronJob) (*store.CronJobResult, error)) {
	s.onJob = handler
}
func (s *fakeCronStore) SetOnEvent(func(event store.CronEvent)) {}
func (s *fakeCronStore) RunJob(context.Context, string, bool) (bool, string, error) {
	return true, "ran", nil
}
func (s *fakeCronStore) SetDefaultTimezone(string) {}
func (s *fakeCronStore) GetDueJobs(time.Time) []store.CronJob {
	return nil
}
