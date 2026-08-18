package methods

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestHibernateMethodsPauseUnavailableWithoutSuspendFn(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handlePause(ctx, client, sessionReqFrame(t, protocol.MethodRunsPause, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestHibernateMethodsWakeUnavailableWithoutResumer(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleWake(ctx, client, sessionReqFrame(t, protocol.MethodRunsWake, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestHibernateMethodsPauseMissingRunID(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	m.SetSuspendFn(func(context.Context, string) error { return nil })
	m.SetRunsStore(&stubRunsStore{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handlePause(ctx, client, sessionReqFrame(t, protocol.MethodRunsPause, map[string]any{}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestHibernateMethodsWakeMissingRunID(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	m.SetResumer(func(context.Context, string) (*agent.RunResult, error) { return &agent.RunResult{}, nil })
	m.SetRunsStore(&stubRunsStore{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleWake(ctx, client, sessionReqFrame(t, protocol.MethodRunsWake, map[string]any{}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestHibernateMethodsPauseSuccess(t *testing.T) {
	called := 0
	m := NewHibernateMethods(&config.Config{})
	m.SetRunsStore(&stubRunsStore{run: &store.AgentRun{RunID: "run-1", UserID: "caller"}})
	m.SetSuspendFn(func(_ context.Context, runID string) error {
		called++
		if runID != "run-1" {
			t.Fatalf("runID = %q, want run-1", runID)
		}
		return nil
	})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handlePause(ctx, client, sessionReqFrame(t, protocol.MethodRunsPause, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	if data["status"] != store.RunTimelineStatusPaused {
		t.Fatalf("status = %v, want %q", data["status"], store.RunTimelineStatusPaused)
	}
	if called != 1 {
		t.Fatalf("suspend called %d times, want 1", called)
	}
}

func TestHibernateMethodsWakeSuccess(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	m.SetRunsStore(&stubRunsStore{run: &store.AgentRun{RunID: "run-1", UserID: "caller"}})
	m.SetResumer(func(_ context.Context, runID string) (*agent.RunResult, error) {
		if runID != "run-1" {
			t.Fatalf("runID = %q, want run-1", runID)
		}
		return &agent.RunResult{}, nil
	})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleWake(ctx, client, sessionReqFrame(t, protocol.MethodRunsWake, map[string]any{"runId": "run-1"}))

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
}

func TestHibernateMethodsViewerScopedToOwnRun(t *testing.T) {
	// Viewer calling suspend on another user's run is denied as not-found.
	m := NewHibernateMethods(&config.Config{})
	m.SetRunsStore(&stubRunsStore{run: &store.AgentRun{RunID: "run-1", UserID: "owner"}})
	m.SetSuspendFn(func(context.Context, string) error { return nil })
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handlePause(ctx, client, sessionReqFrame(t, protocol.MethodRunsPause, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND", resp.Error)
	}
}

func TestHibernateMethodsPauseNotFoundMapping(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	m.SetSuspendFn(func(context.Context, string) error { return agent.ErrRunSuspendNotFound })
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handlePause(ctx, client, sessionReqFrame(t, protocol.MethodRunsPause, map[string]any{"runId": "run-missing"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND", resp.Error)
	}
}

func TestHibernateMethodsPauseUnavailableMapping(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	m.SetSuspendFn(func(context.Context, string) error { return agent.ErrRunSuspendUnavailable })
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handlePause(ctx, client, sessionReqFrame(t, protocol.MethodRunsPause, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestHibernateMethodsWakeNotFoundMapping(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	m.SetResumer(func(context.Context, string) (*agent.RunResult, error) {
		return nil, errors.New("boom")
	})
	m.SetRunsStore(&stubRunsStore{run: &store.AgentRun{RunID: "run-1", UserID: "caller"}})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleWake(ctx, client, sessionReqFrame(t, protocol.MethodRunsWake, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInternal {
		t.Fatalf("error = %+v, want INTERNAL_ERROR", resp.Error)
	}
}

func TestHibernateMethodsWakeErrorsNotFound(t *testing.T) {
	m := NewHibernateMethods(&config.Config{})
	m.SetResumer(func(context.Context, string) (*agent.RunResult, error) {
		return nil, agent.ErrRunResumeNotFound
	})
	m.SetRunsStore(&stubRunsStore{run: &store.AgentRun{RunID: "run-1", UserID: "caller"}})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleWake(ctx, client, sessionReqFrame(t, protocol.MethodRunsWake, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND", resp.Error)
	}
}