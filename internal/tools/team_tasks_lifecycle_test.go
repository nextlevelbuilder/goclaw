package tools

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// preloadTask stores a fully-specified task directly into the mock backend.
func preloadTask(mb *mockBackend, task *store.TeamTaskData) {
	mb.taskStore.mu.Lock()
	if task.ID == uuid.Nil {
		task.ID = uuid.New()
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
		task.UpdatedAt = time.Now()
	}
	mb.taskStore.tasks[task.ID] = task
	mb.taskStore.mu.Unlock()
}

func readTask(mb *mockBackend, id uuid.UUID) *store.TeamTaskData {
	mb.taskStore.mu.Lock()
	defer mb.taskStore.mu.Unlock()
	return mb.taskStore.tasks[id]
}

func TestClaim(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    testTeamID,
			Subject:   "pending task",
			Status:    store.TeamTaskStatusPending,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "claim",
			"task_id": taskID.String(),
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.ForLLM)
		}
		task := readTask(mb, taskID)
		if task.Status != store.TeamTaskStatusInProgress {
			t.Errorf("expected in_progress, got %q", task.Status)
		}
		mb.mu.Lock()
		evCount := len(mb.events)
		mb.mu.Unlock()
		if evCount == 0 {
			t.Fatal("expected broadcast event")
		}
		if mb.events[0].Name != protocol.EventTeamTaskClaimed {
			t.Errorf("expected %q, got %q", protocol.EventTeamTaskClaimed, mb.events[0].Name)
		}
	})

	t.Run("AlreadyInProgress", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    testTeamID,
			Subject:   "in-progress task",
			Status:    store.TeamTaskStatusInProgress,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "claim",
			"task_id": taskID.String(),
		})

		if !result.IsError {
			t.Error("expected error for already in_progress task")
		}
	})

	t.Run("TeamResolutionFail", func(t *testing.T) {
		// Backend with no team — ResolveTeam returns error.
		noTeamMB := &mockBackend{}
		noTeamTool := NewTeamTasksTool(noTeamMB, FullTeamPolicy{})

		emptyCtx := WithTaskActionFlags(
			WithToolChannel(
				store.WithAgentID(store.WithTenantID(t.Context(), uuid.Nil), uuid.Nil),
				ChannelDashboard,
			),
			&TaskActionFlags{},
		)
		result := noTeamTool.Execute(emptyCtx, map[string]any{
			"action":  "claim",
			"task_id": uuid.New().String(),
		})
		if !result.IsError {
			t.Error("expected error when team resolution fails")
		}
	})
}

func TestComplete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "work task",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &memberID,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "complete",
			"task_id": taskID.String(),
			"result":  "done",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.ForLLM)
		}
		task := readTask(mb, taskID)
		if task.Status != store.TeamTaskStatusCompleted {
			t.Errorf("expected completed, got %q", task.Status)
		}
		mb.mu.Lock()
		evCount := len(mb.events)
		mb.mu.Unlock()
		if evCount == 0 {
			t.Fatal("expected broadcast event")
		}
	})

	t.Run("MissingResult", func(t *testing.T) {
		_, tool, _, memberID, ctx := newTestTeamSetup()
		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "complete",
			"task_id": uuid.New().String(),
		})
		if !result.IsError {
			t.Error("expected error for missing result")
		}
	})

	t.Run("AutoClaim", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    testTeamID,
			Subject:   "unclaimed task",
			Status:    store.TeamTaskStatusPending,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "complete",
			"task_id": taskID.String(),
			"result":  "auto-completed",
		})

		if result.IsError {
			t.Fatalf("expected success with auto-claim, got error: %v", result.ForLLM)
		}
		task := readTask(mb, taskID)
		if task.Status != store.TeamTaskStatusCompleted {
			t.Errorf("expected completed, got %q", task.Status)
		}
	})

	t.Run("AlreadyCompleted", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "already done",
			Status:       store.TeamTaskStatusCompleted,
			OwnerAgentID: &memberID,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "complete",
			"task_id": taskID.String(),
			"result":  "done again",
		})

		if result.IsError {
			t.Errorf("expected idempotent success, got error: %v", result.ForLLM)
		}
	})

	t.Run("TerminalStatus", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "failed task",
			Status:       store.TeamTaskStatusFailed,
			OwnerAgentID: &memberID,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "complete",
			"task_id": taskID.String(),
			"result":  "trying to complete failed task",
		})

		// Should return info message (not error) about terminal status.
		if result.IsError {
			t.Errorf("expected info message for terminal status, got hard error: %v", result.ForLLM)
		}
	})
}

func TestCancel(t *testing.T) {
	t.Run("LeadSuccess", func(t *testing.T) {
		mb, tool, leadID, _, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    testTeamID,
			Subject:   "to cancel",
			Status:    store.TeamTaskStatusPending,
		})

		lCtx := store.WithAgentID(ctx, leadID)
		lCtx = WithTaskActionFlags(lCtx, &TaskActionFlags{})
		result := tool.Execute(lCtx, map[string]any{
			"action":  "cancel",
			"task_id": taskID.String(),
			"text":    "no longer needed",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.ForLLM)
		}
		task := readTask(mb, taskID)
		if task.Status != store.TeamTaskStatusCancelled {
			t.Errorf("expected cancelled, got %q", task.Status)
		}
		mb.mu.Lock()
		evCount := len(mb.events)
		mb.mu.Unlock()
		if evCount == 0 {
			t.Fatal("expected broadcast event")
		}
	})

	t.Run("MemberBlocked", func(t *testing.T) {
		_, tool, _, memberID, ctx := newTestTeamSetup()
		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithToolChannel(mCtx, ChannelDashboard) // non-bypass channel
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "cancel",
			"task_id": uuid.New().String(),
		})
		if !result.IsError {
			t.Error("expected error: member cannot cancel via dashboard channel")
		}
	})

	t.Run("ChannelBypass", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    testTeamID,
			Subject:   "bypass cancel",
			Status:    store.TeamTaskStatusPending,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithToolChannel(mCtx, ChannelTeammate)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "cancel",
			"task_id": taskID.String(),
		})

		if result.IsError {
			t.Fatalf("expected success via teammate channel bypass, got error: %v", result.ForLLM)
		}
	})

	t.Run("DefaultReason", func(t *testing.T) {
		mb, tool, leadID, _, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    testTeamID,
			Subject:   "default reason test",
			Status:    store.TeamTaskStatusPending,
		})

		lCtx := store.WithAgentID(ctx, leadID)
		lCtx = WithTaskActionFlags(lCtx, &TaskActionFlags{})
		result := tool.Execute(lCtx, map[string]any{
			"action":  "cancel",
			"task_id": taskID.String(),
			// no "text" field — default reason applied
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.ForLLM)
		}
		task := readTask(mb, taskID)
		if task.Result == nil || *task.Result != "Cancelled by agent" {
			t.Errorf("expected default reason %q, got %v", "Cancelled by agent", task.Result)
		}
	})
}

func TestReview(t *testing.T) {
	t.Run("OwnerSuccess", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "review task",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &memberID,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "review",
			"task_id": taskID.String(),
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.ForLLM)
		}
		task := readTask(mb, taskID)
		if task.Status != store.TeamTaskStatusInReview {
			t.Errorf("expected in_review, got %q", task.Status)
		}
		mb.mu.Lock()
		evCount := len(mb.events)
		mb.mu.Unlock()
		if evCount == 0 {
			t.Fatal("expected broadcast event")
		}
	})

	t.Run("NonOwnerBlocked", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "owned by member",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &memberID,
		})

		// member2 tries to submit for review
		m2Ctx := store.WithAgentID(ctx, testMember2ID)
		m2Ctx = WithTaskActionFlags(m2Ctx, &TaskActionFlags{})
		result := tool.Execute(m2Ctx, map[string]any{
			"action":  "review",
			"task_id": taskID.String(),
		})

		if !result.IsError {
			t.Error("expected error: only owner can submit for review")
		}
	})

	t.Run("CrossTeamBlocked", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		otherTeamID := uuid.New()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       otherTeamID,
			Subject:      "cross-team task",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &memberID,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "review",
			"task_id": taskID.String(),
		})

		if !result.IsError {
			t.Error("expected error: task from different team")
		}
	})
}

func TestApprove(t *testing.T) {
	t.Run("LeadSuccess", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "review me",
			Status:       store.TeamTaskStatusInReview,
			OwnerAgentID: &memberID,
		})

		lCtx := store.WithAgentID(ctx, leadID)
		lCtx = WithTaskActionFlags(lCtx, &TaskActionFlags{})
		result := tool.Execute(lCtx, map[string]any{
			"action":  "approve",
			"task_id": taskID.String(),
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.ForLLM)
		}
		task := readTask(mb, taskID)
		if task.Status != store.TeamTaskStatusCompleted {
			t.Errorf("expected completed, got %q", task.Status)
		}
		mb.mu.Lock()
		evCount := len(mb.events)
		mb.mu.Unlock()
		if evCount == 0 {
			t.Fatal("expected broadcast event")
		}
		if mb.events[0].Name != protocol.EventTeamTaskApproved {
			t.Errorf("expected %q, got %q", protocol.EventTeamTaskApproved, mb.events[0].Name)
		}
	})

	t.Run("MemberBlocked", func(t *testing.T) {
		_, tool, _, memberID, ctx := newTestTeamSetup()
		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithToolChannel(mCtx, ChannelTeammate) // not dashboard/system
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "approve",
			"task_id": uuid.New().String(),
		})
		if !result.IsError {
			t.Error("expected error: member cannot approve via teammate channel")
		}
	})

	t.Run("DashboardBypass", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "dashboard approve",
			Status:       store.TeamTaskStatusInReview,
			OwnerAgentID: &memberID,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithToolChannel(mCtx, ChannelDashboard)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "approve",
			"task_id": taskID.String(),
		})

		if result.IsError {
			t.Fatalf("expected success via dashboard bypass, got error: %v", result.ForLLM)
		}
	})

	t.Run("CrossTeamBlocked", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		otherTeamID := uuid.New()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       otherTeamID,
			Subject:      "other team task",
			Status:       store.TeamTaskStatusInReview,
			OwnerAgentID: &memberID,
		})

		lCtx := store.WithAgentID(ctx, leadID)
		lCtx = WithTaskActionFlags(lCtx, &TaskActionFlags{})
		result := tool.Execute(lCtx, map[string]any{
			"action":  "approve",
			"task_id": taskID.String(),
		})

		if !result.IsError {
			t.Error("expected error: task from different team")
		}
	})

	t.Run("AuditComment", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "audit trail test",
			Status:       store.TeamTaskStatusInReview,
			OwnerAgentID: &memberID,
		})

		lCtx := store.WithAgentID(ctx, leadID)
		lCtx = WithTaskActionFlags(lCtx, &TaskActionFlags{})
		tool.Execute(lCtx, map[string]any{
			"action":  "approve",
			"task_id": taskID.String(),
		})

		mb.taskStore.mu.Lock()
		comments := mb.taskStore.comments[taskID]
		mb.taskStore.mu.Unlock()
		if len(comments) == 0 {
			t.Error("expected audit comment added on approval")
		}
	})
}

func TestReject(t *testing.T) {
	t.Run("WithOwner_ReDispatch", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "re-dispatch on reject",
			Status:       store.TeamTaskStatusInReview,
			OwnerAgentID: &memberID,
		})

		lCtx := store.WithAgentID(ctx, leadID)
		lCtx = WithTaskActionFlags(lCtx, &TaskActionFlags{})
		result := tool.Execute(lCtx, map[string]any{
			"action":  "reject",
			"task_id": taskID.String(),
			"text":    "needs rework",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.ForLLM)
		}
		// Task must NOT be cancelled — it is re-dispatched to owner.
		task := readTask(mb, taskID)
		if task.Status == store.TeamTaskStatusCancelled {
			t.Error("task should not be cancelled when owner exists")
		}
		mb.mu.Lock()
		dispCount := len(mb.dispatches)
		mb.mu.Unlock()
		if dispCount == 0 {
			t.Fatal("expected DispatchTaskToAgent call after reject with owner")
		}
		if mb.dispatches[0].AgentID != memberID {
			t.Errorf("expected dispatch to memberID %v, got %v", memberID, mb.dispatches[0].AgentID)
		}
	})

	t.Run("WithoutOwner_Cancel", func(t *testing.T) {
		mb, tool, leadID, _, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    testTeamID,
			Subject:   "no owner reject",
			Status:    store.TeamTaskStatusInReview,
			// no OwnerAgentID
		})

		lCtx := store.WithAgentID(ctx, leadID)
		lCtx = WithTaskActionFlags(lCtx, &TaskActionFlags{})
		result := tool.Execute(lCtx, map[string]any{
			"action":  "reject",
			"task_id": taskID.String(),
			"text":    "rejected no owner",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %v", result.ForLLM)
		}
		task := readTask(mb, taskID)
		if task.Status != store.TeamTaskStatusCancelled {
			t.Errorf("expected cancelled, got %q", task.Status)
		}
	})

	t.Run("DashboardBypass", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: taskID},
			TeamID:    testTeamID,
			Subject:   "dashboard reject",
			Status:    store.TeamTaskStatusInReview,
		})

		mCtx := store.WithAgentID(ctx, memberID)
		mCtx = WithToolChannel(mCtx, ChannelDashboard)
		mCtx = WithTaskActionFlags(mCtx, &TaskActionFlags{})
		result := tool.Execute(mCtx, map[string]any{
			"action":  "reject",
			"task_id": taskID.String(),
			"text":    "dashboard user rejects",
		})

		if result.IsError {
			t.Fatalf("expected success via dashboard bypass, got error: %v", result.ForLLM)
		}
	})

	t.Run("AuditComment", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := uuid.New()
		preloadTask(mb, &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID},
			TeamID:       testTeamID,
			Subject:      "reject audit",
			Status:       store.TeamTaskStatusInReview,
			OwnerAgentID: &memberID,
		})

		lCtx := store.WithAgentID(ctx, leadID)
		lCtx = WithTaskActionFlags(lCtx, &TaskActionFlags{})
		tool.Execute(lCtx, map[string]any{
			"action":  "reject",
			"task_id": taskID.String(),
			"text":    "audit this rejection",
		})

		mb.taskStore.mu.Lock()
		comments := mb.taskStore.comments[taskID]
		mb.taskStore.mu.Unlock()
		if len(comments) == 0 {
			t.Error("expected audit comment added before status change on reject")
		}
	})
}
