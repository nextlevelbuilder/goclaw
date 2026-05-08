package tools

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeCronStoreCapture captures the AddJob arguments so tests can assert that
// handleAdd correctly forwards creator attribution from ctx into the store.
type fakeCronStoreCapture struct {
	store.CronStore // pin nil — embed for compile satisfaction; only AddJob is used here
	gotCreatorSender string
	gotCreatorRole   string
	gotUserID        string
}

func (f *fakeCronStoreCapture) AddJob(_ context.Context, name string, schedule store.CronSchedule,
	message string, deliver bool, channel, to, agentID, userID, creatorSenderID, creatorRole string,
) (*store.CronJob, error) {
	f.gotCreatorSender = creatorSenderID
	f.gotCreatorRole = creatorRole
	f.gotUserID = userID
	return &store.CronJob{
		ID:      "test-job",
		Name:    name,
		AgentID: agentID,
		UserID:  userID,
		Payload: store.CronPayload{
			Kind:            "agent_turn",
			Message:         message,
			CreatorSenderID: creatorSenderID,
			CreatorRole:     creatorRole,
		},
		Enabled: true,
	}, nil
}

// TestCronTool_handleAdd_PropagatesRealSender verifies a Telegram-rooted
// human dispatch (ctx with a numeric senderID + role) is stamped onto the
// new cron job's Payload — without that, scheduled runs in group chats
// hit "system context cannot ... in group chats" the moment they call
// write_file or another cron mutation.
func TestCronTool_handleAdd_PropagatesRealSender(t *testing.T) {
	fake := &fakeCronStoreCapture{}
	tool := &CronTool{cronStore: fake}

	ctx := context.Background()
	ctx = store.WithSenderID(ctx, "5218954741") // real Telegram numeric id
	ctx = store.WithRole(ctx, "admin")
	ctx = store.WithAgentID(ctx, uuid.New())

	args := map[string]any{
		"job": map[string]any{
			"name":     "neo-daily",
			"schedule": map[string]any{"kind": "cron", "expr": "0 8 * * *", "tz": "Asia/Ho_Chi_Minh"},
			"message":  "Run the daily content cycle",
		},
	}
	res := tool.handleAdd(ctx, args, "agent-x", "group:telegram:-1003812294018")

	if res.IsError {
		t.Fatalf("handleAdd returned error: %s", res.ForLLM)
	}
	if got, want := fake.gotCreatorSender, "5218954741"; got != want {
		t.Errorf("creatorSenderID = %q, want %q (real Telegram sender lost during cron creation)", got, want)
	}
	if got, want := fake.gotCreatorRole, "admin"; got != want {
		t.Errorf("creatorRole = %q, want %q", got, want)
	}
}

// TestCronTool_handleAdd_DropsSyntheticSender ensures we don't accidentally
// stamp a synthetic sender (subagent:..., teammate:..., ticker:...) onto a
// scheduled job — that would attribute every future fire to a system
// component, defeating the deny-on-empty guard at fire time.
func TestCronTool_handleAdd_DropsSyntheticSender(t *testing.T) {
	syntheticSenders := []string{
		"subagent:abc123",
		"teammate:dashboard",
		"ticker:heartbeat",
		"system:cron-installer",
		"notification:progress",
		"session_send_tool",
	}

	for _, syn := range syntheticSenders {
		t.Run(syn, func(t *testing.T) {
			fake := &fakeCronStoreCapture{}
			tool := &CronTool{cronStore: fake}

			ctx := context.Background()
			ctx = store.WithSenderID(ctx, syn)
			ctx = store.WithAgentID(ctx, uuid.New())

			args := map[string]any{
				"job": map[string]any{
					"name":     "test",
					"schedule": map[string]any{"kind": "every", "everyMs": float64(60_000)},
					"message":  "tick",
				},
			}
			res := tool.handleAdd(ctx, args, "agent-x", "user-x")
			if res.IsError {
				t.Fatalf("handleAdd error: %s", res.ForLLM)
			}
			if fake.gotCreatorSender != "" {
				t.Errorf("synthetic sender %q leaked into stored creator: got %q, want empty",
					syn, fake.gotCreatorSender)
			}
		})
	}
}
