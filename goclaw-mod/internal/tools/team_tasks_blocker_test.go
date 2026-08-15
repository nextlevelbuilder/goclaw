package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestBlockerComment(t *testing.T) {
	t.Run("InProgressAutoFail", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    "database is down, cannot proceed",
			"type":    "blocker",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.ForLLM)
		}

		// Task should be failed
		mb.taskStore.mu.Lock()
		task := mb.taskStore.tasks[taskID]
		mb.taskStore.mu.Unlock()
		if task.Status != store.TeamTaskStatusFailed {
			t.Errorf("expected task status=failed, got %s", task.Status)
		}

		// EventTeamTaskFailed must be broadcast
		mb.mu.Lock()
		found := false
		for _, e := range mb.events {
			if e.Name == protocol.EventTeamTaskFailed {
				found = true
			}
		}
		mb.mu.Unlock()
		if !found {
			t.Error("expected EventTeamTaskFailed broadcast")
		}
	})

	t.Run("NotInProgress_NoEscalation", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusCompleted)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    "still a concern",
			"type":    "blocker",
		})

		if result.IsError {
			t.Fatalf("expected success, got: %s", result.ForLLM)
		}

		// Task should NOT be failed (it was already completed)
		mb.taskStore.mu.Lock()
		task := mb.taskStore.tasks[taskID]
		mb.taskStore.mu.Unlock()
		if task.Status != store.TeamTaskStatusCompleted {
			t.Errorf("expected task status unchanged (completed), got %s", task.Status)
		}

		// No EventTeamTaskFailed
		mb.mu.Lock()
		for _, e := range mb.events {
			if e.Name == protocol.EventTeamTaskFailed {
				mb.mu.Unlock()
				t.Error("EventTeamTaskFailed should NOT be broadcast for non-in-progress task")
				return
			}
		}
		mb.mu.Unlock()
	})

	t.Run("ReasonTruncated500", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		longText := strings.Repeat("x", 600)
		result := tool.Execute(memberCtx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    longText,
			"type":    "blocker",
		})

		if result.IsError {
			t.Fatalf("expected success, got: %s", result.ForLLM)
		}

		mb.taskStore.mu.Lock()
		task := mb.taskStore.tasks[taskID]
		mb.taskStore.mu.Unlock()

		// Result stored as "FAILED: Blocked: <reason>"
		// The reason field in FailTask is "Blocked: " + text, truncated to 500 runes total
		if task.Result == nil {
			t.Fatal("expected task result to be set")
		}
		if len([]rune(*task.Result)) > 510 { // allow a bit of slack for "FAILED: " prefix
			t.Errorf("expected reason to be truncated, got %d runes", len([]rune(*task.Result)))
		}
	})

	t.Run("EscalationEnabled", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()

		// Enable blocker escalation in team settings
		mb.team.Settings = json.RawMessage(`{"blocker_escalation":{"enabled":true}}`)
		mb.taskStore.mu.Lock()
		mb.taskStore.team = mb.team
		mb.taskStore.mu.Unlock()

		taskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       testTeamID,
			Subject:      "Escalation task",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &memberID,
			Channel:      ChannelDashboard,
			ChatID:       testTeamID.String(),
		}
		mb.taskStore.mu.Unlock()

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    "completely stuck",
			"type":    "blocker",
		})

		if result.IsError {
			t.Fatalf("expected success, got: %s", result.ForLLM)
		}

		mb.mu.Lock()
		inboundCount := len(mb.inbound)
		mb.mu.Unlock()
		if inboundCount == 0 {
			t.Error("expected inbound escalation message to be published")
		}
	})

	t.Run("EscalationDisabled", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		// Explicitly disable escalation in settings
		mb.team.Settings = json.RawMessage(`{"blocker_escalation":{"enabled":false}}`)
		mb.taskStore.mu.Lock()
		mb.taskStore.team = mb.team
		mb.taskStore.mu.Unlock()

		taskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       testTeamID,
			Subject:      "No escalation task",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &memberID,
			Channel:      ChannelDashboard,
			ChatID:       testTeamID.String(),
		}
		mb.taskStore.mu.Unlock()

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    "stuck here",
			"type":    "blocker",
		})

		if result.IsError {
			t.Fatalf("expected success, got: %s", result.ForLLM)
		}

		mb.mu.Lock()
		inboundCount := len(mb.inbound)
		mb.mu.Unlock()
		if inboundCount != 0 {
			t.Errorf("expected no inbound escalation, got %d", inboundCount)
		}
	})
}
