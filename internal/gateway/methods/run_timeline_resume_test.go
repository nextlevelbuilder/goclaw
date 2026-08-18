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

// stubResumer is a minimal agent.ResumeRun-compatible function for handler tests.
func stubResumer(result *agent.RunResult, err error) func(context.Context, string) (*agent.RunResult, error) {
	return func(context.Context, string) (*agent.RunResult, error) {
		return result, err
	}
}

func TestRunsResumeUnavailableWithoutResumer(t *testing.T) {
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsResume(ctx, client, sessionReqFrame(t, protocol.MethodRunsResume, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestRunsResumeSuccess(t *testing.T) {
	runs := &stubRunsStore{run: &store.AgentRun{RunID: "run-1", SessionKey: "s", UserID: "caller", Status: store.AgentRunStatusCompleted}}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	m.SetResumer(stubResumer(&agent.RunResult{Content: "resumed answer", Thinking: "reasoned"}, nil))

	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsResume(ctx, client, sessionReqFrame(t, protocol.MethodRunsResume, map[string]any{"runId": "run-1"}))

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
	result, ok := data["result"].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", data["result"])
	}
	if result["content"] != "resumed answer" || result["thinking"] != "reasoned" {
		t.Fatalf("result = %+v", result)
	}
	// Fresh run record is re-fetched after the resume and included.
	run, ok := data["run"].(map[string]any)
	if !ok {
		t.Fatalf("run type = %T", data["run"])
	}
	if run["status"] != store.AgentRunStatusCompleted {
		t.Fatalf("run status = %v", run["status"])
	}
}

func TestRunsResumeMissingRunID(t *testing.T) {
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetResumer(stubResumer(&agent.RunResult{}, nil))
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsResume(ctx, client, sessionReqFrame(t, protocol.MethodRunsResume, map[string]any{}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestRunsResumeNotFoundError(t *testing.T) {
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetResumer(stubResumer(nil, agent.ErrRunResumeNotFound))
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsResume(ctx, client, sessionReqFrame(t, protocol.MethodRunsResume, map[string]any{"runId": "nope"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND", resp.Error)
	}
}

func TestRunsResumeViewerScopedToOwnRun(t *testing.T) {
	runs := &stubRunsStore{run: &store.AgentRun{RunID: "run-1", SessionKey: "s", UserID: "other", Status: store.AgentRunStatusCompacting}}
	m := NewRunTimelineMethods(&stubRunTimelineStore{}, &config.Config{})
	m.SetRunsStore(runs)
	m.SetResumer(stubResumer(&agent.RunResult{Content: "x"}, nil))

	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleRunsResume(ctx, client, sessionReqFrame(t, protocol.MethodRunsResume, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND for another user's run", resp.Error)
	}
}
