package methods

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// stubCheckpointSnapshots implements store.CheckpointSnapshotStore for handler
// tests.
type stubCheckpointSnapshots struct {
	items []store.CheckpointSnapshot
	err   error
}

func (s *stubCheckpointSnapshots) AppendCheckpointSnapshot(context.Context, *store.CheckpointSnapshot) error {
	return nil
}
func (s *stubCheckpointSnapshots) ListCheckpointSnapshots(context.Context, store.CheckpointSnapshotListOpts) ([]store.CheckpointSnapshot, error) {
	return s.items, s.err
}
func (s *stubCheckpointSnapshots) GetCheckpointSnapshot(context.Context, string, int) (*store.CheckpointSnapshot, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.items) > 0 {
		return &s.items[0], nil
	}
	return nil, nil
}

// stubReplayer is a minimal agent.ReplayRun-compatible function for handler
// tests.
func stubReplayer(result *agent.RunResult, err error) func(context.Context, string, int) (*agent.RunResult, error) {
	return func(context.Context, string, int) (*agent.RunResult, error) {
		return result, err
	}
}

func TestRunsCheckpointsListUnavailableWithoutStore(t *testing.T) {
	m := NewTimeTravelMethods(nil, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleCheckpointsList(ctx, client, sessionReqFrame(t, protocol.MethodRunsCheckpointsList, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestRunsCheckpointsListSuccess(t *testing.T) {
	snaps := &stubCheckpointSnapshots{items: []store.CheckpointSnapshot{
		{RunID: "run-1", Seq: 2, Status: store.CheckpointSnapshotPaused},
		{RunID: "run-1", Seq: 1, Status: store.CheckpointSnapshotRunning},
	}}
	m := NewTimeTravelMethods(snaps, &config.Config{})

	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleCheckpointsList(ctx, client, sessionReqFrame(t, protocol.MethodRunsCheckpointsList, map[string]any{"runId": "run-1", "limit": 10}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	if data["runId"] != "run-1" {
		t.Fatalf("runId = %v, want run-1", data["runId"])
	}
	items, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T", data["items"])
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
}

func TestRunsCheckpointsListMissingRunID(t *testing.T) {
	m := NewTimeTravelMethods(&stubCheckpointSnapshots{}, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleCheckpointsList(ctx, client, sessionReqFrame(t, protocol.MethodRunsCheckpointsList, map[string]any{}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestRunsCheckpointsListRejectsNegativeOffset(t *testing.T) {
	snaps := &stubCheckpointSnapshots{}
	m := NewTimeTravelMethods(snaps, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleCheckpointsList(ctx, client, sessionReqFrame(t, protocol.MethodRunsCheckpointsList, map[string]any{
		"runId":  "run-1",
		"offset": -1,
	}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestRunsReplayUnavailableWithoutReplayer(t *testing.T) {
	m := NewTimeTravelMethods(&stubCheckpointSnapshots{}, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleReplay(ctx, client, sessionReqFrame(t, protocol.MethodRunsReplay, map[string]any{"runId": "run-1", "seq": 2}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestRunsReplaySuccess(t *testing.T) {
	m := NewTimeTravelMethods(&stubCheckpointSnapshots{}, &config.Config{})
	m.SetReplay(stubReplayer(&agent.RunResult{Content: "replayed answer"}, nil))

	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleReplay(ctx, client, sessionReqFrame(t, protocol.MethodRunsReplay, map[string]any{"runId": "run-1", "seq": 2}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	if data["runId"] != "run-1" || data["seq"] != float64(2) {
		t.Fatalf("data = %+v, want runId run-1 seq 2", data)
	}
	result, ok := data["result"].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", data["result"])
	}
	if result["content"] != "replayed answer" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunsReplayMissingParams(t *testing.T) {
	m := NewTimeTravelMethods(&stubCheckpointSnapshots{}, &config.Config{})
	m.SetReplay(stubReplayer(&agent.RunResult{}, nil))
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	// Missing runId.
	m.handleReplay(ctx, client, sessionReqFrame(t, protocol.MethodRunsReplay, map[string]any{"seq": 1}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("missing runId error = %+v, want INVALID_REQUEST", resp.Error)
	}

	// Missing seq.
	client2, responses2 := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	m.handleReplay(ctx, client2, sessionReqFrame(t, protocol.MethodRunsReplay, map[string]any{"runId": "run-1"}))
	resp2 := readTimelineResponse(t, responses2)
	if resp2.Error == nil || resp2.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("missing seq error = %+v, want INVALID_REQUEST", resp2.Error)
	}
}

func TestRunsReplayNotFoundError(t *testing.T) {
	m := NewTimeTravelMethods(&stubCheckpointSnapshots{}, &config.Config{})
	m.SetReplay(stubReplayer(nil, agent.ErrRunReplayNotFound))
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleReplay(ctx, client, sessionReqFrame(t, protocol.MethodRunsReplay, map[string]any{"runId": "nope", "seq": 9}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND", resp.Error)
	}
}

func TestRunsReplayViewerScopedToOwnRun(t *testing.T) {
	runs := &stubRunsStore{run: &store.AgentRun{RunID: "run-1", SessionKey: "s", UserID: "other", Status: store.AgentRunStatusCompacting}}
	m := NewTimeTravelMethods(&stubCheckpointSnapshots{}, &config.Config{})
	m.SetRunsStore(runs)
	m.SetReplay(stubReplayer(&agent.RunResult{Content: "x"}, nil))

	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleReplay(ctx, client, sessionReqFrame(t, protocol.MethodRunsReplay, map[string]any{"runId": "run-1", "seq": 1}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND for another user's run", resp.Error)
	}
}

