package methods

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type stubRunTimelineStore struct {
	opts  []store.RunTimelineListOpts
	items []store.RunTimelineItem
	err   error
}

func (s *stubRunTimelineStore) AppendRunTimelineItem(context.Context, *store.RunTimelineItem) error {
	return nil
}

func (s *stubRunTimelineStore) ListRunTimelineItems(_ context.Context, opts store.RunTimelineListOpts) ([]store.RunTimelineItem, error) {
	s.opts = append(s.opts, opts)
	if s.err != nil {
		return nil, s.err
	}
	return s.items, nil
}

func (s *stubRunTimelineStore) RecoverInterruptedRuns(context.Context) (int64, error) {
	return 0, nil
}

func TestRunTimelineGetScopesViewerByUser(t *testing.T) {
	timeline := &stubRunTimelineStore{
		items: []store.RunTimelineItem{
			{RunID: "run-1", UserID: "caller", Seq: 1, Preview: "visible"},
			{RunID: "run-1", UserID: "other", Seq: 2, Preview: "hidden"},
		},
	}
	m := NewRunTimelineMethods(timeline, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodRunTimelineGet, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	rawItems, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T", data["items"])
	}
	if len(rawItems) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(rawItems))
	}
	if timeline.opts[0].RunID != "run-1" {
		t.Fatalf("RunID = %q", timeline.opts[0].RunID)
	}
}

func TestRunTimelineGetRejectsNegativeOffset(t *testing.T) {
	timeline := &stubRunTimelineStore{}
	m := NewRunTimelineMethods(timeline, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodRunTimelineGet, map[string]any{
		"runId":  "run-1",
		"offset": -1,
	}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
	if len(timeline.opts) != 0 {
		t.Fatalf("store called with opts: %+v", timeline.opts)
	}
}

func TestRunTimelineGetHidesStoreErrorDetail(t *testing.T) {
	timeline := &stubRunTimelineStore{err: errors.New("pq: syntax error at or near \"OFFSET -1\"")}
	m := NewRunTimelineMethods(timeline, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodRunTimelineGet, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInternal {
		t.Fatalf("error = %+v, want INTERNAL_ERROR", resp.Error)
	}
	if strings.Contains(resp.Error.Message, "OFFSET -1") || strings.Contains(resp.Error.Message, "pq:") {
		t.Fatalf("error leaked store detail: %q", resp.Error.Message)
	}
}

func readTimelineResponse(t *testing.T, ch <-chan []byte) protocol.ResponseFrame {
	t.Helper()
	raw := <-ch
	var resp protocol.ResponseFrame
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

// stubRunsStore implements store.RunsStore for handler tests.
type stubRunsStore struct {
	run   *store.AgentRun
	runs  []store.AgentRun
	opts  []store.RunListOpts
	err   error
	calls []string
}

func (s *stubRunsStore) CreateRun(_ context.Context, _ *store.AgentRun) error { return s.err }

func (s *stubRunsStore) UpdateRunStatus(context.Context, string, string) error { return s.err }

func (s *stubRunsStore) UpdateRunTerminal(context.Context, string, string, string, time.Time) error {
	return s.err
}

func (s *stubRunsStore) TouchHeartbeat(context.Context, string) error { return s.err }

func (s *stubRunsStore) GetRun(_ context.Context, runID string) (*store.AgentRun, error) {
	s.calls = append(s.calls, "get:"+runID)
	if s.err != nil {
		return nil, s.err
	}
	if s.run == nil {
		return nil, errors.New("not found")
	}
	return s.run, nil
}

func (s *stubRunsStore) ListRuns(_ context.Context, opts store.RunListOpts) ([]store.AgentRun, error) {
	s.calls = append(s.calls, "list")
	s.opts = append(s.opts, opts)
	if s.err != nil {
		return nil, s.err
	}
	return s.runs, nil
}

func (s *stubRunsStore) RecoverStaleRuns(context.Context, time.Duration) (int64, error) {
	return 0, s.err
}

func TestRunsGetReturnsRun(t *testing.T) {
	runs := &stubRunsStore{run: &store.AgentRun{RunID: "run-1", SessionKey: "s", UserID: "caller", Status: store.AgentRunStatusRunning}}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsGet(ctx, client, sessionReqFrame(t, protocol.MethodRunsGet, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	run, ok := data["run"].(map[string]any)
	if !ok {
		t.Fatalf("run type = %T", data["run"])
	}
	if run["status"] != store.AgentRunStatusRunning {
		t.Fatalf("status = %v, want running", run["status"])
	}
	if len(runs.calls) != 1 || runs.calls[0] != "get:run-1" {
		t.Fatalf("store calls = %v", runs.calls)
	}
}

func TestRunsGetUnavailableWhenStoreNil(t *testing.T) {
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{}) // SetRunsStore never called
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsGet(ctx, client, sessionReqFrame(t, protocol.MethodRunsGet, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestRunsGetMissingRunID(t *testing.T) {
	runs := &stubRunsStore{run: &store.AgentRun{RunID: "run-1"}}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsGet(ctx, client, sessionReqFrame(t, protocol.MethodRunsGet, map[string]any{}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
	if len(runs.calls) != 0 {
		t.Fatalf("store called unexpectedly: %v", runs.calls)
	}
}

func TestRunsGetNotFound(t *testing.T) {
	runs := &stubRunsStore{err: errors.New("sql: no rows in result set")}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsGet(ctx, client, sessionReqFrame(t, protocol.MethodRunsGet, map[string]any{"runId": "nope"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND", resp.Error)
	}
}

func TestRunsListScopesAndDefaults(t *testing.T) {
	runs := &stubRunsStore{
		runs: []store.AgentRun{
			{RunID: "r1", Status: store.AgentRunStatusCompleted},
			{RunID: "r2", Status: store.AgentRunStatusFailed},
		},
	}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsList(ctx, client, sessionReqFrame(t, protocol.MethodRunsList, map[string]any{
		"sessionKey": "s-1",
		"status":     store.AgentRunStatusCompleted,
	}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	rawRuns, ok := data["runs"].([]any)
	if !ok {
		t.Fatalf("runs type = %T", data["runs"])
	}
	if len(rawRuns) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(rawRuns))
	}
	// Default limit applied (0 → 100) and offset rejected when negative.
	if runs.opts[0].Limit != 100 {
		t.Fatalf("limit = %d, want 100", runs.opts[0].Limit)
	}
	if runs.opts[0].SessionKey != "s-1" || runs.opts[0].Status != store.AgentRunStatusCompleted {
		t.Fatalf("opts = %+v", runs.opts[0])
	}
}

func TestRunsGetScopesViewerByUser(t *testing.T) {
	runs := &stubRunsStore{run: &store.AgentRun{RunID: "run-1", SessionKey: "s", UserID: "other", Status: store.AgentRunStatusRunning}}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsGet(ctx, client, sessionReqFrame(t, protocol.MethodRunsGet, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND for another user's run", resp.Error)
	}
	// The store is still consulted (existence is not leaked, but authorization
	// is enforced server-side regardless of store result).
	if len(runs.calls) != 1 || runs.calls[0] != "get:run-1" {
		t.Fatalf("store calls = %v", runs.calls)
	}
}

func TestRunsGetAdminSeesOtherUserRun(t *testing.T) {
	runs := &stubRunsStore{run: &store.AgentRun{RunID: "run-1", SessionKey: "s", UserID: "other", Status: store.AgentRunStatusRunning}}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsGet(ctx, client, sessionReqFrame(t, protocol.MethodRunsGet, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	if _, ok := data["run"]; !ok {
		t.Fatalf("run missing from payload: %+v", data)
	}
}

func TestRunsListScopesViewerByUser(t *testing.T) {
	runs := &stubRunsStore{
		runs: []store.AgentRun{
			{RunID: "r1", UserID: "caller", Status: store.AgentRunStatusCompleted},
			{RunID: "r2", UserID: "other", Status: store.AgentRunStatusFailed},
		},
	}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsList(ctx, client, sessionReqFrame(t, protocol.MethodRunsList, map[string]any{}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	rawRuns, ok := data["runs"].([]any)
	if !ok {
		t.Fatalf("runs type = %T", data["runs"])
	}
	if len(rawRuns) != 1 {
		t.Fatalf("len(runs) = %d, want 1 (only caller's)", len(rawRuns))
	}
}

func TestRunsEventsUsesAfterSeqCursor(t *testing.T) {
	timeline := &stubRunTimelineStore{
		items: []store.RunTimelineItem{
			{RunID: "run-1", UserID: "caller", Seq: 4, ItemType: store.RunTimelineItemTypeActivity, Preview: "event-4"},
			{RunID: "run-1", UserID: "caller", Seq: 5, ItemType: store.RunTimelineItemTypeActivity, Preview: "event-5"},
		},
	}
	m := NewRunTimelineMethods(timeline, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsEvents(ctx, client, sessionReqFrame(t, protocol.MethodRunsEvents, map[string]any{
		"runId":    "run-1",
		"afterSeq": 3,
	}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if timeline.opts[0].RunID != "run-1" {
		t.Fatalf("RunID = %q", timeline.opts[0].RunID)
	}
	if timeline.opts[0].AfterSeq != 3 {
		t.Fatalf("AfterSeq = %d, want 3", timeline.opts[0].AfterSeq)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	// nextAfter = last item seq = 5.
	if got := data["nextAfter"]; got != float64(5) {
		t.Fatalf("nextAfter = %v, want 5", got)
	}
}

func TestRunsEventsRejectsNegativeAfterSeq(t *testing.T) {
	timeline := &stubRunTimelineStore{}
	m := NewRunTimelineMethods(timeline, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsEvents(ctx, client, sessionReqFrame(t, protocol.MethodRunsEvents, map[string]any{
		"runId":    "run-1",
		"afterSeq": -1,
	}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
	if len(timeline.opts) != 0 {
		t.Fatalf("store called with opts: %+v", timeline.opts)
	}
}
