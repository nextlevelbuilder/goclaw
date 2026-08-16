package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type stubRunTimelineStore struct {
	opts  []store.RunTimelineListOpts
	items []store.RunTimelineItem
}

func (s *stubRunTimelineStore) AppendRunTimelineItem(context.Context, *store.RunTimelineItem) error {
	return nil
}

func (s *stubRunTimelineStore) ListRunTimelineItems(_ context.Context, opts store.RunTimelineListOpts) ([]store.RunTimelineItem, error) {
	s.opts = append(s.opts, opts)
	return s.items, nil
}

func (s *stubRunTimelineStore) RecoverInterruptedRuns(context.Context) (int64, error) {
	return 0, nil
}

func TestRunTimelineHTTPScopesViewerByUser(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	timeline := &stubRunTimelineStore{
		items: []store.RunTimelineItem{
			{RunID: "run-1", UserID: "caller", Seq: 1, Preview: "visible"},
			{RunID: "run-1", UserID: "other", Seq: 2, Preview: "hidden"},
		},
	}
	mux := http.NewServeMux()
	NewTracesHandler(&mockTracingStore{}, timeline).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1/timeline?session_key=session-1&limit=20", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []store.RunTimelineItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].UserID != "caller" {
		t.Fatalf("items = %+v", body.Items)
	}
	if len(timeline.opts) != 1 {
		t.Fatalf("List calls = %d, want 1", len(timeline.opts))
	}
	if timeline.opts[0].RunID != "run-1" || timeline.opts[0].SessionKey != "session-1" {
		t.Fatalf("opts = %+v", timeline.opts[0])
	}
}

func TestRunTimelineHTTPAdminSeesTenantItems(t *testing.T) {
	token := "timeline-admin-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {ID: uuid.New(), Scopes: []string{"operator.admin"}, OwnerID: "admin"},
	})
	timeline := &stubRunTimelineStore{
		items: []store.RunTimelineItem{{RunID: "run-1", UserID: "other", Seq: 1}},
	}
	mux := http.NewServeMux()
	NewTracesHandler(&mockTracingStore{}, timeline).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1/timeline", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

type stubHTTPRunsStore struct {
	run   *store.AgentRun
	err   error
	calls []string
}

func (s *stubHTTPRunsStore) CreateRun(context.Context, *store.AgentRun) error { return s.err }

func (s *stubHTTPRunsStore) UpdateRunStatus(context.Context, string, string) error { return s.err }

func (s *stubHTTPRunsStore) UpdateRunTerminal(context.Context, string, string, string, time.Time) error {
	return s.err
}

func (s *stubHTTPRunsStore) TouchHeartbeat(context.Context, string) error { return s.err }

func (s *stubHTTPRunsStore) GetRun(_ context.Context, runID string) (*store.AgentRun, error) {
	s.calls = append(s.calls, runID)
	if s.err != nil {
		return nil, s.err
	}
	if s.run == nil {
		return nil, errors.New("not found")
	}
	return s.run, nil
}

func (s *stubHTTPRunsStore) ListRuns(context.Context, store.RunListOpts) ([]store.AgentRun, error) {
	return nil, s.err
}

func (s *stubHTTPRunsStore) RecoverStaleRuns(context.Context, time.Duration) (int64, error) {
	return 0, s.err
}

func TestRunGetHTTPScopesViewerByUser(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	runs := &stubHTTPRunsStore{run: &store.AgentRun{RunID: "run-1", UserID: "caller", Status: store.AgentRunStatusRunning}}
	timeline := &stubRunTimelineStore{}
	h := NewTracesHandler(&mockTracingStore{}, timeline)
	h.SetRunsStore(runs)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Run store.AgentRun `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Run.Status != store.AgentRunStatusRunning {
		t.Fatalf("status = %q, want running", body.Run.Status)
	}
	if len(runs.calls) != 1 || runs.calls[0] != "run-1" {
		t.Fatalf("GetRun calls = %v", runs.calls)
	}
}

func TestRunGetHTTPDeniesNonOwnerViewer(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	runs := &stubHTTPRunsStore{run: &store.AgentRun{RunID: "run-1", UserID: "other", Status: store.AgentRunStatusRunning}}
	timeline := &stubRunTimelineStore{}
	h := NewTracesHandler(&mockTracingStore{}, timeline)
	h.SetRunsStore(runs)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunGetHTTPUnavailableWhenStoreNil(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	mux := http.NewServeMux()
	NewTracesHandler(&mockTracingStore{}, &stubRunTimelineStore{}).RegisterRoutes(mux) // no SetRunsStore

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunGetHTTPNotFound(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	runs := &stubHTTPRunsStore{err: errors.New("sql: no rows")}
	timeline := &stubRunTimelineStore{}
	h := NewTracesHandler(&mockTracingStore{}, timeline)
	h.SetRunsStore(runs)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/nope", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRunEventsHTTPUsesAfterSeqCursor(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	timeline := &stubRunTimelineStore{
		items: []store.RunTimelineItem{
			{RunID: "run-1", UserID: "caller", Seq: 4, Preview: "event-4"},
			{RunID: "run-1", UserID: "caller", Seq: 5, Preview: "event-5"},
		},
	}
	mux := http.NewServeMux()
	NewTracesHandler(&mockTracingStore{}, timeline).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1/events?after=3&limit=20", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		After    int                       `json:"after"`
		Items    []store.RunTimelineItem   `json:"items"`
		NextAfter int                      `json:"next_after"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.After != 3 {
		t.Fatalf("after = %d, want 3", body.After)
	}
	if len(body.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(body.Items))
	}
	if body.NextAfter != 5 {
		t.Fatalf("next_after = %d, want 5", body.NextAfter)
	}
	if timeline.opts[0].AfterSeq != 3 {
		t.Fatalf("AfterSeq = %d, want 3", timeline.opts[0].AfterSeq)
	}
	if timeline.opts[0].Limit != 20 {
		t.Fatalf("Limit = %d, want 20", timeline.opts[0].Limit)
	}
}

func TestRunEventsHTTPRejectsInvalidAfter(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	timeline := &stubRunTimelineStore{}
	mux := http.NewServeMux()
	NewTracesHandler(&mockTracingStore{}, timeline).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1/events?after=-5", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if len(timeline.opts) != 0 {
		t.Fatalf("List calls = %d, want 0", len(timeline.opts))
	}
}
