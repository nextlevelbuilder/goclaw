package tools

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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
