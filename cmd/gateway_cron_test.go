package cmd

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestCronJobHandlerInjectsPayloadCredentialUserID(t *testing.T) {
	wantCredentialUserID := "tenant-user-123"
	var gotCredentialUserID string

	sched := scheduler.NewScheduler(
		scheduler.DefaultLanes(),
		scheduler.QueueConfig{
			Mode:          scheduler.QueueModeQueue,
			Cap:           1,
			Drop:          scheduler.DropOld,
			DebounceMs:    0,
			MaxConcurrent: 1,
		},
		func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
			gotCredentialUserID = store.CredentialUserIDFromContext(ctx)
			return &agent.RunResult{Content: "ok"}, nil
		},
	)
	defer sched.Stop()

	handler := makeCronJobHandler(
		sched,
		nil,
		&config.Config{},
		nil,
		nil,
		nil,
	)

	result, err := handler(&store.CronJob{
		ID:        uuid.NewString(),
		TenantID:  uuid.New(),
		Name:      "credentialed-report",
		AgentID:   "reporter",
		UserID:    "group:telegram:-100123",
		Stateless: true,
		Payload: store.CronPayload{
			Kind:             "agent_turn",
			Message:          "run gh issue list",
			CredentialUserID: wantCredentialUserID,
		},
	})
	if err != nil {
		t.Fatalf("cron handler returned error: %v", err)
	}
	if result == nil || result.Content != "ok" {
		t.Fatalf("cron result = %#v, want content ok", result)
	}
	if gotCredentialUserID != wantCredentialUserID {
		t.Fatalf("credential user ID in scheduled context = %q, want %q", gotCredentialUserID, wantCredentialUserID)
	}
}
