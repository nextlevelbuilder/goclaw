package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestAskUser(t *testing.T) {
	t.Run("OwnerSuccess", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())
		// Set an external channel so followup can resolve
		memberCtx = WithToolChannel(memberCtx, "telegram")

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "ask_user",
			"task_id": taskID.String(),
			"text":    "Do you want me to proceed with option A or B?",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.ForLLM)
		}

		mb.taskStore.mu.Lock()
		task := mb.taskStore.tasks[taskID]
		mb.taskStore.mu.Unlock()

		if task.FollowupAt == nil {
			t.Error("expected FollowupAt to be set")
		}
		if task.FollowupMessage == "" {
			t.Error("expected FollowupMessage to be set")
		}
	})

	t.Run("NonOwnerBlocked", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "ask_user",
			"task_id": taskID.String(),
			"text":    "Can you clarify the deadline?",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "only the task owner") {
			t.Errorf("expected owner-only error, got: %s (isError=%v)", result.ForLLM, result.IsError)
		}
	})

	t.Run("EmptyText", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "ask_user",
			"task_id": taskID.String(),
			"text":    "",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "text is required") {
			t.Errorf("expected text-required error, got: %s", result.ForLLM)
		}
	})

	t.Run("InternalChannelBlocked", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()

		taskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       testTeamID,
			Subject:      "Internal channel task",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &memberID,
			Channel:      ChannelTeammate,
			ChatID:       "some-chat",
		}
		mb.taskStore.mu.Unlock()

		// Context channel is also internal
		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithToolChannel(memberCtx, ChannelTeammate)
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "ask_user",
			"task_id": taskID.String(),
			"text":    "What should I do next?",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "no valid channel") {
			t.Errorf("expected no-valid-channel error, got: %s (isError=%v)", result.ForLLM, result.IsError)
		}
	})
}

func TestClearAskUser(t *testing.T) {
	// helper: set a followup on a task so there's something to clear
	setFollowup := func(mb *mockBackend, taskID uuid.UUID) {
		mb.taskStore.mu.Lock()
		task := mb.taskStore.tasks[taskID]
		now := time.Now().Add(30 * time.Minute)
		task.FollowupAt = &now
		task.FollowupMessage = "ping"
		task.FollowupChannel = "telegram"
		task.FollowupMax = 3
		mb.taskStore.mu.Unlock()
	}

	t.Run("OwnerSuccess", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)
		setFollowup(mb, taskID)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "clear_ask_user",
			"task_id": taskID.String(),
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.ForLLM)
		}

		mb.taskStore.mu.Lock()
		task := mb.taskStore.tasks[taskID]
		mb.taskStore.mu.Unlock()

		if task.FollowupAt != nil || task.FollowupMessage != "" {
			t.Error("expected followup to be cleared")
		}
	})

	t.Run("LeadSuccess", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)
		setFollowup(mb, taskID)

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "clear_ask_user",
			"task_id": taskID.String(),
		})

		if result.IsError {
			t.Fatalf("lead should be able to clear any task followup, got error: %s", result.ForLLM)
		}
	})

	t.Run("NonOwnerNonLeadBlocked", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)
		setFollowup(mb, taskID)

		// member2 tries to clear member's task
		member2Ctx := store.WithAgentID(ctx, testMember2ID)
		member2Ctx = WithTaskActionFlags(member2Ctx, &TaskActionFlags{})

		result := tool.Execute(member2Ctx, map[string]any{
			"action":  "clear_ask_user",
			"task_id": taskID.String(),
		})

		if !result.IsError {
			t.Errorf("expected error for non-owner non-lead, got success: %s", result.ForLLM)
		}
	})
}

func TestRetry(t *testing.T) {
	t.Run("LeadSuccess", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusFailed)

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "retry",
			"task_id": taskID.String(),
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.ForLLM)
		}

		// Task should be in_progress after reset + assign
		mb.taskStore.mu.Lock()
		task := mb.taskStore.tasks[taskID]
		mb.taskStore.mu.Unlock()

		if task.Status != store.TeamTaskStatusInProgress {
			t.Errorf("expected task status=in_progress after retry, got %s", task.Status)
		}

		// Dispatch should have been called
		mb.mu.Lock()
		dispatched := len(mb.dispatches) > 0
		mb.mu.Unlock()
		if !dispatched {
			t.Error("expected DispatchTaskToAgent to be called on retry")
		}
	})

	t.Run("MemberBlocked", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusFailed)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "retry",
			"task_id": taskID.String(),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "only the team lead") {
			t.Errorf("expected lead-only error, got: %s", result.ForLLM)
		}
	})

	t.Run("InvalidStatus", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusPending)

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "retry",
			"task_id": taskID.String(),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "retry only works on") {
			t.Errorf("expected invalid-status error, got: %s", result.ForLLM)
		}
	})

	t.Run("NoAssignee", func(t *testing.T) {
		mb, tool, leadID, _, ctx := newTestTeamSetup()
		taskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       testTeamID,
			Subject:      "Unassigned failed task",
			Status:       store.TeamTaskStatusFailed,
			OwnerAgentID: nil,
		}
		mb.taskStore.mu.Unlock()

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "retry",
			"task_id": taskID.String(),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "task has no assignee") {
			t.Errorf("expected no-assignee error, got: %s", result.ForLLM)
		}
	})

	t.Run("SelfDispatchBlocked", func(t *testing.T) {
		mb, tool, leadID, _, ctx := newTestTeamSetup()
		taskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       testTeamID,
			Subject:      "Lead-assigned failed task",
			Status:       store.TeamTaskStatusFailed,
			OwnerAgentID: &leadID,
		}
		mb.taskStore.mu.Unlock()

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "retry",
			"task_id": taskID.String(),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "cannot retry task assigned to the team lead") {
			t.Errorf("expected self-dispatch error, got: %s", result.ForLLM)
		}
	})

	t.Run("CrossTeamBlocked", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		otherTeamID := uuid.New()
		taskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       otherTeamID,
			Subject:      "Foreign failed task",
			Status:       store.TeamTaskStatusFailed,
			OwnerAgentID: &memberID,
		}
		mb.taskStore.mu.Unlock()

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "retry",
			"task_id": taskID.String(),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "does not belong") {
			t.Errorf("expected cross-team error, got: %s", result.ForLLM)
		}
	})
}
