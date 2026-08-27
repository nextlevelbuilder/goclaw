package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// A lead reached through `delegate` runs on the internal "delegate" delivery
// channel, while the real origin is preserved separately. A task must record
// the origin: notifications addressed to the delivery channel have no
// registered handler and are dropped by the outbound dispatcher, so the caller
// never hears about completions, failures or blocker escalations.
func TestCreateRecordsDelegationOriginNotDeliveryChannel(t *testing.T) {
	mb, tool, _, _, ctx := newTestTeamSetup()

	// Shape the context the way a delegated run is built (buildAgentLinkRunRequest):
	// delivery channel is "delegate", origin preserved as the workspace channel/chat.
	ctx = WithToolChannel(ctx, "delegate")
	ctx = WithToolChatID(ctx, "system")
	ctx = WithWorkspaceChannel(ctx, "telegram")
	ctx = WithWorkspaceChatID(ctx, "313683273")

	ptd := NewPendingTeamDispatch()
	ptd.MarkListed()
	ctx = WithPendingTeamDispatch(ctx, ptd)

	result := tool.Execute(ctx, map[string]any{
		"action":      "create",
		"subject":     "Origin routing",
		"description": "Task created by a delegated lead",
		"assignee":    "member-agent",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Task created") {
		t.Fatalf("expected 'Task created', got: %s", result.ForLLM)
	}

	mb.taskStore.mu.Lock()
	var task *store.TeamTaskData
	for _, v := range mb.taskStore.tasks {
		task = v
	}
	mb.taskStore.mu.Unlock()
	if task == nil {
		t.Fatal("no task was created")
	}
	if task.Channel != "telegram" {
		t.Errorf("task.Channel = %q, want %q — notifications on the delivery channel are dropped", task.Channel, "telegram")
	}
	if task.ChatID != "313683273" {
		t.Errorf("task.ChatID = %q, want %q", task.ChatID, "313683273")
	}
}

// Without a delegation origin the resolution is the identity: a task keeps the
// channel and chat of the run that created it.
func TestCreateKeepsOwnChannelWithoutDelegationOrigin(t *testing.T) {
	mb, tool, _, _, ctx := newTestTeamSetup()

	ptd := NewPendingTeamDispatch()
	ptd.MarkListed()
	ctx = WithPendingTeamDispatch(ctx, ptd)

	result := tool.Execute(ctx, map[string]any{
		"action":      "create",
		"subject":     "Direct routing",
		"description": "Task created by a lead talking to the user directly",
		"assignee":    "member-agent",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	mb.taskStore.mu.Lock()
	var task *store.TeamTaskData
	for _, v := range mb.taskStore.tasks {
		task = v
	}
	mb.taskStore.mu.Unlock()
	if task == nil {
		t.Fatal("no task was created")
	}
	if task.Channel != ChannelDashboard {
		t.Errorf("task.Channel = %q, want %q", task.Channel, ChannelDashboard)
	}
	if task.ChatID != testTeamID.String() {
		t.Errorf("task.ChatID = %q, want %q", task.ChatID, testTeamID.String())
	}
}

// delegatedCtx shapes a context the way buildAgentLinkRunRequest does: the run is
// delivered on the internal "delegate" channel while the caller's origin is
// preserved as the workspace channel and chat.
func delegatedCtx(ctx context.Context, originChannel, originChat string) context.Context {
	ctx = WithToolChannel(ctx, "delegate")
	ctx = WithToolChatID(ctx, "system")
	ctx = WithWorkspaceChannel(ctx, originChannel)
	ctx = WithWorkspaceChatID(ctx, originChat)
	ptd := NewPendingTeamDispatch()
	ptd.MarkListed()
	return WithPendingTeamDispatch(ctx, ptd)
}

func createTaskIn(t *testing.T, tool *TeamTasksTool, ctx context.Context, subject string) string {
	t.Helper()
	res := tool.Execute(ctx, map[string]any{
		"action":      "create",
		"subject":     subject,
		"description": "work",
		"assignee":    "member-agent",
	})
	if res.IsError {
		t.Fatalf("create %q: %s", subject, res.ForLLM)
	}
	id := res.ForLLM
	start := strings.Index(id, "id=")
	if start < 0 {
		t.Fatalf("no task id in %q", res.ForLLM)
	}
	id = id[start+3:]
	if end := strings.IndexAny(id, ","); end >= 0 {
		id = id[:end]
	}
	return id
}

func completionEvent(t *testing.T, mb *mockBackend, taskID string) protocol.TeamTaskEventPayload {
	t.Helper()
	mb.mu.Lock()
	defer mb.mu.Unlock()
	for _, ev := range mb.events {
		p, ok := ev.Payload.(protocol.TeamTaskEventPayload)
		if ok && ev.Name == protocol.EventTeamTaskCompleted && p.TaskID == taskID {
			return p
		}
	}
	t.Fatalf("no completion event for task %s", taskID)
	return protocol.TeamTaskEventPayload{}
}

// The notification a delegated lead emits on completion must be addressed to the
// caller's origin. Addressed to the internal "delegate" delivery channel it is
// dropped by the outbound dispatcher and the user never learns the work finished.
func TestDelegatedLeadCompletionNotifiesOrigin(t *testing.T) {
	mb, tool, _, _, base := newTestTeamSetup()
	ctx := delegatedCtx(base, "telegram", "313683273")

	taskID := createTaskIn(t, tool, ctx, "Deliverable")
	res := tool.Execute(ctx, map[string]any{
		"action":  "complete",
		"task_id": taskID,
		"result":  "done",
	})
	if res.IsError {
		t.Fatalf("complete: %s", res.ForLLM)
	}

	ev := completionEvent(t, mb, taskID)
	if ev.Channel != "telegram" || ev.ChatID != "313683273" {
		t.Errorf("completion event addressed to %s/%s, want telegram/313683273 — "+
			"the delivery channel has no registered handler and the notification is dropped",
			ev.Channel, ev.ChatID)
	}
}

// Origins must not bleed into each other: completing a task raised from one chat
// must not produce a notification addressed to an unrelated origin sharing the
// same board.
func TestCompletionNotificationStaysWithinItsOwnOrigin(t *testing.T) {
	mb, tool, _, _, base := newTestTeamSetup()
	ctxA := delegatedCtx(base, "telegram", "chat-A")
	ctxB := delegatedCtx(base, "discord", "chat-B")

	taskA := createTaskIn(t, tool, ctxA, "Task A")
	createTaskIn(t, tool, ctxB, "Task B")

	res := tool.Execute(ctxA, map[string]any{"action": "complete", "task_id": taskA, "result": "done"})
	if res.IsError {
		t.Fatalf("complete: %s", res.ForLLM)
	}

	if ev := completionEvent(t, mb, taskA); ev.Channel != "telegram" || ev.ChatID != "chat-A" {
		t.Errorf("task A announced to %s/%s, want telegram/chat-A", ev.Channel, ev.ChatID)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	for _, ev := range mb.events {
		p, ok := ev.Payload.(protocol.TeamTaskEventPayload)
		if !ok || ev.Name != protocol.EventTeamTaskCompleted {
			continue
		}
		if p.Channel == "discord" || p.ChatID == "chat-B" {
			t.Errorf("completing task A produced a notification addressed to the unrelated origin %s/%s",
				p.Channel, p.ChatID)
		}
	}
}
