package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
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

func TestCronOutputContainsNoReplySentinel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "exact", in: "NO_REPLY", want: true},
		{name: "prefix explanation", in: "NO_REPLY - nothing to report", want: true},
		{name: "suffix", in: "No relevant update. NO_REPLY", want: true},
		{name: "mid sentence", in: "No changes found. NO_REPLY for this run.", want: true},
		{name: "lowercase", in: "no_reply", want: true},
		{name: "decorative underscore", in: "NO_REPLY_", want: true},
		{name: "glued suffix", in: "NO_REPLYING", want: false},
		{name: "glued prefix", in: "XNO_REPLY", want: false},
		{name: "empty", in: "", want: false},
		{name: "unrelated", in: "no reply needed", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cronOutputContainsNoReplySentinel(tt.in); got != tt.want {
				t.Fatalf("cronOutputContainsNoReplySentinel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestCronJobHandlerSuppressesNoReplyDelivery(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantPublish bool
	}{
		{name: "normal content", content: "daily report ready", wantPublish: true},
		{name: "exact no reply", content: "NO_REPLY", wantPublish: false},
		{name: "suffix no reply", content: "No relevant update. NO_REPLY", wantPublish: false},
		{name: "prefix no reply", content: "NO_REPLY - nothing to report", wantPublish: false},
		{name: "decorative underscore no reply", content: "NO_REPLY_", wantPublish: false},
		{name: "glued token still delivers", content: "NO_REPLYING is a different word", wantPublish: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb := bus.New()
			defer mb.Close()

			sched := scheduler.NewScheduler(
				scheduler.DefaultLanes(),
				scheduler.QueueConfig{
					Mode:          scheduler.QueueModeQueue,
					Cap:           1,
					Drop:          scheduler.DropOld,
					DebounceMs:    0,
					MaxConcurrent: 1,
				},
				func(context.Context, agent.RunRequest) (*agent.RunResult, error) {
					return &agent.RunResult{Content: tt.content}, nil
				},
			)
			defer sched.Stop()

			handler := makeCronJobHandler(
				sched,
				mb,
				&config.Config{},
				nil,
				nil,
				nil,
			)

			result, err := handler(&store.CronJob{
				ID:             uuid.NewString(),
				TenantID:       uuid.New(),
				Name:           "delivery-report",
				AgentID:        "reporter",
				UserID:         "user-1",
				Stateless:      true,
				Deliver:        true,
				DeliverChannel: "telegram",
				DeliverTo:      "chat-1",
				Payload: store.CronPayload{
					Kind:    "agent_turn",
					Message: "daily report",
				},
			})
			if err != nil {
				t.Fatalf("cron handler returned error: %v", err)
			}
			if result == nil || result.Content != tt.content {
				t.Fatalf("cron result = %#v, want content %q", result, tt.content)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			got, ok := mb.SubscribeOutbound(ctx)
			if !tt.wantPublish {
				if ok {
					t.Fatalf("unexpected outbound message: %#v", got)
				}
				return
			}
			if !ok {
				t.Fatal("expected outbound message")
			}
			if got.Content != tt.content || got.Channel != "telegram" || got.ChatID != "chat-1" {
				t.Fatalf("outbound message = %#v, want channel telegram chat chat-1 content %q", got, tt.content)
			}
		})
	}
}
