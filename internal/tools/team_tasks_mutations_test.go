package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// makeTask inserts a minimal task into the mock store and returns its ID.
func makeTask(mb *mockBackend, teamID, ownerID uuid.UUID, status string) uuid.UUID {
	taskID := uuid.New()
	ownerCopy := ownerID
	now := time.Now()
	mb.taskStore.mu.Lock()
	mb.taskStore.tasks[taskID] = &store.TeamTaskData{
		BaseModel:    store.BaseModel{ID: taskID, CreatedAt: now, UpdatedAt: now},
		TeamID:       teamID,
		Subject:      "Test task",
		Status:       status,
		OwnerAgentID: &ownerCopy,
	}
	mb.taskStore.mu.Unlock()
	return taskID
}

func TestComment(t *testing.T) {
	t.Run("NoteSuccess", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    "making progress",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.ForLLM)
		}
		mb.mu.Lock()
		found := false
		for _, e := range mb.events {
			if e.Name == protocol.EventTeamTaskCommented {
				found = true
			}
		}
		mb.mu.Unlock()
		if !found {
			t.Error("expected EventTeamTaskCommented broadcast")
		}
		flags := TaskActionFlagsFromCtx(memberCtx)
		if flags == nil || !flags.Commented {
			t.Error("expected Commented flag set")
		}
	})

	t.Run("CrossTeamBlocked", func(t *testing.T) {
		mb, tool, _, _, ctx := newTestTeamSetup()
		otherTeamID := uuid.New()
		taskID := uuid.New()
		ownerID := testMemberID
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       otherTeamID,
			Subject:      "Other team task",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &ownerID,
		}
		mb.taskStore.mu.Unlock()

		result := tool.Execute(ctx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    "hello",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "task does not belong") {
			t.Errorf("expected cross-team error, got: %s (isError=%v)", result.ForLLM, result.IsError)
		}
	})

	t.Run("EmptyText", func(t *testing.T) {
		mb, tool, _, _, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, testLeadID, store.TeamTaskStatusInProgress)

		result := tool.Execute(ctx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    "",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "text is required") {
			t.Errorf("expected text-required error, got: %s", result.ForLLM)
		}
	})

	t.Run("MaxLengthExceeded", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "comment",
			"task_id": taskID.String(),
			"text":    strings.Repeat("x", 10001),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "comment text too long") {
			t.Errorf("expected too-long error, got: %s", result.ForLLM)
		}
	})
}

func TestProgress(t *testing.T) {
	t.Run("OwnerSuccess", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "progress",
			"task_id": taskID.String(),
			"percent": float64(60),
			"text":    "halfway",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.ForLLM)
		}
		mb.mu.Lock()
		found := false
		for _, e := range mb.events {
			if e.Name == protocol.EventTeamTaskProgress {
				found = true
			}
		}
		mb.mu.Unlock()
		if !found {
			t.Error("expected EventTeamTaskProgress broadcast")
		}
	})

	t.Run("NonOwnerBlocked", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "progress",
			"task_id": taskID.String(),
			"percent": float64(50),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "only the assigned task owner") {
			t.Errorf("expected owner-only error, got: %s", result.ForLLM)
		}
	})

	t.Run("RegressionPrevented", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID].ProgressPercent = 50
		mb.taskStore.mu.Unlock()

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "progress",
			"task_id": taskID.String(),
			"percent": float64(30),
		})

		if result.IsError {
			t.Fatalf("expected success (regression clamped), got: %s", result.ForLLM)
		}
		mb.taskStore.mu.Lock()
		task := mb.taskStore.tasks[taskID]
		mb.taskStore.mu.Unlock()
		if task.ProgressPercent < 50 {
			t.Errorf("progress should not regress below 50, got %d", task.ProgressPercent)
		}
	})

	t.Run("TerminalSkip", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusCompleted)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "progress",
			"task_id": taskID.String(),
			"percent": float64(50),
		})

		if result.IsError {
			t.Fatalf("expected silent result, got error: %s", result.ForLLM)
		}
		if !strings.Contains(result.ForLLM, "already completed") {
			t.Errorf("expected 'already completed', got: %s", result.ForLLM)
		}
	})

	t.Run("InvalidPercent", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "progress",
			"task_id": taskID.String(),
			"percent": float64(150),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "percent must be 0-100") {
			t.Errorf("expected percent-range error, got: %s", result.ForLLM)
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("LeadSuccess", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusPending)

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":      "update",
			"task_id":     taskID.String(),
			"description": "updated description",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.ForLLM)
		}
	})

	t.Run("MemberBlocked", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":      "update",
			"task_id":     taskID.String(),
			"description": "sneaky update",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "only the team lead") {
			t.Errorf("expected lead-only error, got: %s", result.ForLLM)
		}
	})

	t.Run("CrossTeamBlocked", func(t *testing.T) {
		mb, tool, leadID, _, ctx := newTestTeamSetup()
		otherTeamID := uuid.New()
		ownerID := testMemberID
		taskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       otherTeamID,
			Subject:      "foreign task",
			Status:       store.TeamTaskStatusPending,
			OwnerAgentID: &ownerID,
		}
		mb.taskStore.mu.Unlock()

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":      "update",
			"task_id":     taskID.String(),
			"description": "cross-team",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "does not belong") {
			t.Errorf("expected cross-team error, got: %s", result.ForLLM)
		}
	})

	t.Run("NoUpdates", func(t *testing.T) {
		mb, tool, leadID, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusPending)

		leadCtx := store.WithAgentID(ctx, leadID)
		leadCtx = WithTaskActionFlags(leadCtx, &TaskActionFlags{})

		result := tool.Execute(leadCtx, map[string]any{
			"action":  "update",
			"task_id": taskID.String(),
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "no updates provided") {
			t.Errorf("expected no-updates error, got: %s", result.ForLLM)
		}
	})
}

func TestAttach(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})
		memberCtx = WithTeamTaskID(memberCtx, taskID.String())

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "attach",
			"task_id": taskID.String(),
			"path":    "/tmp/test/output.txt",
		})

		if result.IsError {
			t.Fatalf("expected success, got error: %s", result.ForLLM)
		}
		mb.mu.Lock()
		found := false
		for _, e := range mb.events {
			if e.Name == protocol.EventTeamTaskAttachmentAdded {
				found = true
			}
		}
		mb.mu.Unlock()
		if !found {
			t.Error("expected EventTeamTaskAttachmentAdded broadcast")
		}
	})

	t.Run("CrossTeamBlocked", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		otherTeamID := uuid.New()
		taskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[taskID] = &store.TeamTaskData{
			BaseModel:    store.BaseModel{ID: taskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:       otherTeamID,
			Subject:      "foreign task",
			Status:       store.TeamTaskStatusInProgress,
			OwnerAgentID: &memberID,
		}
		mb.taskStore.mu.Unlock()

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "attach",
			"task_id": taskID.String(),
			"path":    "/tmp/test/file.txt",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "does not belong") {
			t.Errorf("expected cross-team error, got: %s", result.ForLLM)
		}
	})

	t.Run("EmptyPath", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		taskID := makeTask(mb, testTeamID, memberID, store.TeamTaskStatusInProgress)

		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":  "attach",
			"task_id": taskID.String(),
			"path":    "",
		})

		if !result.IsError || !strings.Contains(result.ForLLM, "path is required") {
			t.Errorf("expected path-required error, got: %s", result.ForLLM)
		}
	})
}
