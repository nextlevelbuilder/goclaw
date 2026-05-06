package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeStore is a minimal SubagentTaskStore for the handler tests. We
// only need Get + UpdateStatus; the rest are no-ops to satisfy the
// interface.
type fakeStore struct {
	mu       sync.Mutex
	rows     map[uuid.UUID]*store.SubagentTaskData
	statuses []recordedStatus
}

type recordedStatus struct {
	id       uuid.UUID
	status   string
	result   string
	tenantID uuid.UUID
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[uuid.UUID]*store.SubagentTaskData{}} }

func (f *fakeStore) put(row *store.SubagentTaskData) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[row.ID] = row
}

func (f *fakeStore) Get(_ context.Context, id uuid.UUID) (*store.SubagentTaskData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (f *fakeStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, result *string, _ int, _, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	upd := recordedStatus{id: id, status: status, tenantID: store.TenantIDFromContext(ctx)}
	if result != nil {
		upd.result = *result
	}
	f.statuses = append(f.statuses, upd)
	if r, ok := f.rows[id]; ok {
		r.Status = status
	}
	return nil
}

func (f *fakeStore) Create(context.Context, *store.SubagentTaskData) error { return nil }
func (f *fakeStore) ListByParent(context.Context, string, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeStore) ListBySession(context.Context, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeStore) ListRunningAcrossTenants(context.Context, int) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeStore) Archive(context.Context, time.Duration) (int64, error) { return 0, nil }
func (f *fakeStore) UpdateMetadata(context.Context, uuid.UUID, map[string]any) error {
	return nil
}

// fakeSender is a ChannelSender that records every SendToChannel call.
type fakeSender struct {
	mu    sync.Mutex
	calls []sendCall
}

type sendCall struct {
	channel, chatID, content string
}

func (f *fakeSender) SendToChannel(_ context.Context, ch, chatID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sendCall{ch, chatID, content})
	return nil
}

// runningRow seeds a fakeStore with a row in 'running' status, the
// shape spawn_job would have written. Returns the id for use in
// the URL path.
func runningRow(s *fakeStore, channel, threadID string) uuid.UUID {
	id := uuid.New()
	ch := channel
	tid := threadID
	s.put(&store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: id},
		TenantID:       uuid.New(),
		ParentAgentKey: "eng",
		Subject:        "test",
		Status:         "running",
		OriginChannel:  &ch,
		OriginChatID:   &tid,
	})
	return id
}

// signedRequest builds a POST with a valid HMAC signature header.
func signedRequest(t *testing.T, path string, body []byte, secret []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// serve builds a router with the JobsHandler routes and dispatches one
// request through it. Mirrors how server.go wires handlers in prod.
func serve(h *JobsHandler, req *http.Request) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestJobsHandler_ProgressHappyPath(t *testing.T) {
	s := newFakeStore()
	sender := &fakeSender{}
	id := runningRow(s, "discord-eng", "1217")
	h := NewJobsHandler(s, sender, []byte("topsecret"))

	body, _ := json.Marshal(progressRequest{
		Content:  "💭 reading the auth flow",
		Channel:  "discord-eng",
		ThreadID: "1217",
	})
	req := signedRequest(t, "/v1/agents/jobs/"+id.String()+"/progress", body, []byte("topsecret"))
	rec := serve(h, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 send, got %d", len(sender.calls))
	}
	if sender.calls[0].chatID != "1217" {
		t.Errorf("chatID: got %s want 1217", sender.calls[0].chatID)
	}
}

func TestJobsHandler_ProgressInvalidHMAC(t *testing.T) {
	s := newFakeStore()
	id := runningRow(s, "discord-eng", "1217")
	h := NewJobsHandler(s, &fakeSender{}, []byte("topsecret"))

	body, _ := json.Marshal(progressRequest{Content: "x", Channel: "discord-eng", ThreadID: "1217"})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents/jobs/"+id.String()+"/progress", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	rec := serve(h, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on bad sig, got %d", rec.Code)
	}
}

func TestJobsHandler_ProgressJobNotFound(t *testing.T) {
	// Security guard: HMAC valid + body parses, but no row in store.
	// Must 404, not silently post to whatever channel/thread the
	// caller specified.
	s := newFakeStore()
	h := NewJobsHandler(s, &fakeSender{}, []byte("k"))
	body, _ := json.Marshal(progressRequest{Content: "x", Channel: "ch", ThreadID: "t"})
	req := signedRequest(t, "/v1/agents/jobs/"+uuid.New().String()+"/progress", body, []byte("k"))
	rec := serve(h, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestJobsHandler_ProgressTargetMismatch(t *testing.T) {
	// Security guard: row exists, but caller asks to post to a
	// different channel/thread than the row was created with.
	// Refuse with 403 — defends against leaked HMAC + redirected post.
	s := newFakeStore()
	id := runningRow(s, "discord-eng", "1217")
	sender := &fakeSender{}
	h := NewJobsHandler(s, sender, []byte("k"))

	body, _ := json.Marshal(progressRequest{
		Content:  "leak attempt",
		Channel:  "discord-eng",
		ThreadID: "9999", // not the row's chat
	})
	req := signedRequest(t, "/v1/agents/jobs/"+id.String()+"/progress", body, []byte("k"))
	rec := serve(h, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.calls) != 0 {
		t.Errorf("must not have posted on mismatch, got %d calls", len(sender.calls))
	}
}

func TestJobsHandler_ProgressIgnoredWhenNotRunning(t *testing.T) {
	// A late progress post for a job already done/failed must not
	// error (race between stream-task's tail and the informer's
	// complete) but also must not re-post to the channel.
	s := newFakeStore()
	id := runningRow(s, "discord-eng", "1217")
	row, _ := s.Get(context.Background(), id)
	row.Status = "done"
	sender := &fakeSender{}
	h := NewJobsHandler(s, sender, []byte("k"))

	body, _ := json.Marshal(progressRequest{Content: "late", Channel: "discord-eng", ThreadID: "1217"})
	req := signedRequest(t, "/v1/agents/jobs/"+id.String()+"/progress", body, []byte("k"))
	rec := serve(h, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 silent ignore, got %d", rec.Code)
	}
	if len(sender.calls) != 0 {
		t.Errorf("must not have posted for done job, got %d", len(sender.calls))
	}
}

func TestJobsHandler_ProgressInvalidJSON(t *testing.T) {
	s := newFakeStore()
	h := NewJobsHandler(s, &fakeSender{}, []byte("k"))
	req := signedRequest(t, "/v1/agents/jobs/"+uuid.New().String()+"/progress", []byte("not json"), []byte("k"))
	rec := serve(h, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestJobsHandler_ProgressInvalidJobID(t *testing.T) {
	// Path id that isn't a uuid is rejected before any DB lookup.
	s := newFakeStore()
	h := NewJobsHandler(s, &fakeSender{}, []byte("k"))
	body, _ := json.Marshal(progressRequest{Content: "x", Channel: "c", ThreadID: "t"})
	req := signedRequest(t, "/v1/agents/jobs/not-a-uuid/progress", body, []byte("k"))
	rec := serve(h, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad uuid, got %d", rec.Code)
	}
}

func TestJobsHandler_CompleteSuccess(t *testing.T) {
	s := newFakeStore()
	sender := &fakeSender{}
	id := runningRow(s, "discord-eng", "1217")
	h := NewJobsHandler(s, sender, []byte("k"))

	body, _ := json.Marshal(completeRequest{
		ExitCode: 0,
		Result:   "Plan complete. PR: https://github.com/x/y/pull/1",
		Channel:  "discord-eng",
		ThreadID: "1217",
		Source:   "stream-task",
	})
	req := signedRequest(t, "/v1/agents/jobs/"+id.String()+"/complete", body, []byte("k"))
	rec := serve(h, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(s.statuses) != 1 || s.statuses[0].status != "done" {
		t.Errorf("expected 1 'done' status, got %+v", s.statuses)
	}
	if len(sender.calls) != 1 {
		t.Errorf("expected 1 sender call, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].content, "PR:") {
		t.Errorf("sender content lost result body: %q", sender.calls[0].content)
	}
}

func TestJobsHandler_CompleteFailureMarksFailed(t *testing.T) {
	s := newFakeStore()
	sender := &fakeSender{}
	id := runningRow(s, "discord-eng", "1217")
	h := NewJobsHandler(s, sender, []byte("k"))

	body, _ := json.Marshal(completeRequest{
		ExitCode: 137, // OOMKilled
		Result:   "out of memory",
		Channel:  "discord-eng",
		ThreadID: "1217",
		Source:   "informer",
	})
	req := signedRequest(t, "/v1/agents/jobs/"+id.String()+"/complete", body, []byte("k"))
	rec := serve(h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	if s.statuses[0].status != "failed" {
		t.Errorf("non-zero exit must mark 'failed', got %s", s.statuses[0].status)
	}
	if !strings.Contains(sender.calls[0].content, "❌") {
		t.Errorf("failure message should be marked: %q", sender.calls[0].content)
	}
}

func TestJobsHandler_CompleteIdempotent(t *testing.T) {
	// Race between stream-task's direct callback and the informer's
	// synthetic callback for the same Job. The second call must
	// 200 no-op rather than double-flip the row's state.
	s := newFakeStore()
	id := runningRow(s, "discord-eng", "1217")
	sender := &fakeSender{}
	h := NewJobsHandler(s, sender, []byte("k"))

	body, _ := json.Marshal(completeRequest{
		ExitCode: 0,
		Result:   "first call",
		Channel:  "discord-eng",
		ThreadID: "1217",
	})
	// First call lands.
	rec := serve(h, signedRequest(t, "/v1/agents/jobs/"+id.String()+"/complete", body, []byte("k")))
	if rec.Code != http.StatusOK {
		t.Fatalf("first call got %d", rec.Code)
	}

	// Second call with different body — must be no-op.
	body2, _ := json.Marshal(completeRequest{
		ExitCode: 137,
		Result:   "second call (would-be racing informer)",
		Channel:  "discord-eng",
		ThreadID: "1217",
	})
	rec2 := serve(h, signedRequest(t, "/v1/agents/jobs/"+id.String()+"/complete", body2, []byte("k")))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second call got %d", rec2.Code)
	}

	// Only one status update happened.
	if len(s.statuses) != 1 {
		t.Errorf("expected 1 status update, got %d (idempotency broken)", len(s.statuses))
	}
	if len(sender.calls) != 1 {
		t.Errorf("expected 1 sender call, got %d", len(sender.calls))
	}
}

func TestJobsHandler_CompleteRepairsInterruptedSpawnJob(t *testing.T) {
	s := newFakeStore()
	sender := &fakeSender{}
	id := runningRow(s, "discord-eng", "1217")
	row, _ := s.Get(context.Background(), id)
	row.Status = "interrupted"
	row.Metadata = map[string]any{
		"runner":        "spawn_job",
		"kind":          "autoplan",
		"command":       "/app/agent/bin/run-discord-plan",
		"worktree_path": "/data/workspace-eng/worktrees/task",
		"sinks":         []any{map[string]any{"type": "discord"}},
	}
	h := NewJobsHandler(s, sender, []byte("k"))

	body, _ := json.Marshal(completeRequest{
		ExitCode: 0,
		Result:   "Recovered plan completion",
		Channel:  "discord-eng",
		ThreadID: "1217",
		Source:   "stream-job",
	})
	rec := serve(h, signedRequest(t, "/v1/agents/jobs/"+id.String()+"/complete", body, []byte("k")))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(s.statuses) != 1 || s.statuses[0].status != "done" {
		t.Fatalf("expected interrupted spawn_job to transition to done, got %+v", s.statuses)
	}
	if len(sender.calls) != 1 || !strings.Contains(sender.calls[0].content, "Recovered plan completion") {
		t.Fatalf("expected completion post, got %+v", sender.calls)
	}
}

func TestJobsHandler_CompleteKeepsInterruptedInProcessSubagentTerminal(t *testing.T) {
	s := newFakeStore()
	sender := &fakeSender{}
	id := runningRow(s, "discord-eng", "1217")
	row, _ := s.Get(context.Background(), id)
	row.Status = "interrupted"
	h := NewJobsHandler(s, sender, []byte("k"))

	body, _ := json.Marshal(completeRequest{
		ExitCode: 0,
		Result:   "late in-process completion",
		Channel:  "discord-eng",
		ThreadID: "1217",
	})
	rec := serve(h, signedRequest(t, "/v1/agents/jobs/"+id.String()+"/complete", body, []byte("k")))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(s.statuses) != 0 {
		t.Fatalf("interrupted in-process subagent should stay terminal, got %+v", s.statuses)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("interrupted in-process subagent should not post completion, got %+v", sender.calls)
	}
}

func TestJobsHandler_VerifyHMACEdgeCases(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"empty", "", false},
		{"no_prefix", "abc", false},
		{"bad_hex", "sha256=zzz", false},
		{"wrong_sig", "sha256=" + hex.EncodeToString([]byte("wrong")), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := verifyHMAC(c.header, []byte("body"), []byte("k"))
			if got != c.want {
				t.Errorf("verifyHMAC(%q): got %v want %v", c.header, got, c.want)
			}
		})
	}
}
