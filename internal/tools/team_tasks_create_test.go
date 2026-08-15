package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestCreate(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mb, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":      "create",
			"subject":     "Test task",
			"description": "Do something",
			"assignee":    "member-agent",
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", result.ForLLM)
		}
		if !strings.Contains(result.ForLLM, "Task created") {
			t.Errorf("expected 'Task created' in response, got: %s", result.ForLLM)
		}
		mb.taskStore.mu.Lock()
		created := len(mb.taskStore.tasks)
		mb.taskStore.mu.Unlock()
		if created != 1 {
			t.Errorf("expected 1 task, got %d", created)
		}
		// Verify event was broadcast
		mb.mu.Lock()
		evCount := len(mb.events)
		mb.mu.Unlock()
		if evCount == 0 {
			t.Error("expected at least one event broadcast")
		}
		// Check task status is pending
		mb.taskStore.mu.Lock()
		var task *store.TeamTaskData
		for _, v := range mb.taskStore.tasks {
			task = v
		}
		mb.taskStore.mu.Unlock()
		if task.Status != store.TeamTaskStatusPending {
			t.Errorf("expected status pending, got %s", task.Status)
		}
	})

	t.Run("MemberGeneralBlocked", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)
		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":   "create",
			"subject":  "General task",
			"assignee": "member2-agent",
		})
		if !result.IsError {
			t.Fatal("expected error for member creating general task")
		}
		if !strings.Contains(result.ForLLM, "Members cannot create tasks") {
			t.Errorf("expected 'Members cannot create tasks', got: %s", result.ForLLM)
		}
		_ = mb
	})

	t.Run("MemberRequestEnabled", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		mb.team.Settings = json.RawMessage(`{"member_requests":{"enabled":true}}`)
		mb.taskStore.team = mb.team
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)
		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":    "create",
			"subject":   "Need help with this",
			"task_type": "request",
			"assignee":  "lead-agent",
		})
		// Note: lead-agent is the lead, but the self-assign check only applies when lead assigns to itself.
		// Here the member is assigning to the lead which is allowed.
		// Actually lead-agent IS team.LeadAgentID, so the check "assigneeID == team.LeadAgentID" fires.
		// Let's assign to member2 instead.
		_ = result

		result = tool.Execute(memberCtx, map[string]any{
			"action":    "create",
			"subject":   "Need help with this",
			"task_type": "request",
			"assignee":  "member2-agent",
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", result.ForLLM)
		}
	})

	t.Run("MemberRequestDisabled", func(t *testing.T) {
		mb, tool, _, memberID, ctx := newTestTeamSetup()
		// Settings empty = member_request disabled
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)
		memberCtx := store.WithAgentID(ctx, memberID)
		memberCtx = WithTaskActionFlags(memberCtx, &TaskActionFlags{})

		result := tool.Execute(memberCtx, map[string]any{
			"action":    "create",
			"subject":   "Request something",
			"task_type": "request",
			"assignee":  "lead-agent",
		})
		if !result.IsError {
			t.Fatal("expected error for member request when disabled")
		}
		if !strings.Contains(result.ForLLM, "Members cannot create tasks") {
			t.Errorf("expected 'Members cannot create tasks', got: %s", result.ForLLM)
		}
		_ = mb
	})

	t.Run("RequiresSearchGate", func(t *testing.T) {
		_, tool, _, _, ctx := newTestTeamSetup()
		// ptd exists but HasListed=false
		ptd := NewPendingTeamDispatch()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":   "create",
			"subject":  "Test task",
			"assignee": "member-agent",
		})
		if !result.IsError {
			t.Fatal("expected error when list gate not passed")
		}
		if !strings.Contains(result.ForLLM, "check existing tasks") {
			t.Errorf("expected gate message, got: %s", result.ForLLM)
		}
	})

	t.Run("EmptySubject", func(t *testing.T) {
		_, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":   "create",
			"subject":  "",
			"assignee": "member-agent",
		})
		if !result.IsError {
			t.Fatal("expected error for empty subject")
		}
		if !strings.Contains(result.ForLLM, "subject is required") {
			t.Errorf("expected 'subject is required', got: %s", result.ForLLM)
		}
	})

	t.Run("RequiresAssignee", func(t *testing.T) {
		_, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":  "create",
			"subject": "Task without assignee",
		})
		if !result.IsError {
			t.Fatal("expected error for missing assignee")
		}
		if !strings.Contains(result.ForLLM, "assignee is required") {
			t.Errorf("expected 'assignee is required', got: %s", result.ForLLM)
		}
	})

	t.Run("SelfAssignBlocked", func(t *testing.T) {
		_, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":   "create",
			"subject":  "Self assigned task",
			"assignee": "lead-agent",
		})
		if !result.IsError {
			t.Fatal("expected error for self-assign")
		}
		if !strings.Contains(result.ForLLM, "cannot assign tasks to itself") {
			t.Errorf("expected self-assign error, got: %s", result.ForLLM)
		}
	})

	t.Run("NonMemberAssignee", func(t *testing.T) {
		_, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":   "create",
			"subject":  "Task for unknown",
			"assignee": "unknown-agent",
		})
		if !result.IsError {
			t.Fatal("expected error for non-member assignee")
		}
		if !strings.Contains(result.ForLLM, "not found") {
			t.Errorf("expected 'not found', got: %s", result.ForLLM)
		}
	})

	t.Run("BlockedByTerminal", func(t *testing.T) {
		mb, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		blockerID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[blockerID] = &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: blockerID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:    testTeamID,
			Subject:   "Blocker task",
			Status:    store.TeamTaskStatusCompleted,
		}
		mb.taskStore.mu.Unlock()

		result := tool.Execute(ctx, map[string]any{
			"action":     "create",
			"subject":    "Blocked task",
			"assignee":   "member-agent",
			"blocked_by": []any{blockerID.String()},
		})
		if !result.IsError {
			t.Fatal("expected error for blocking on terminal task")
		}
		if !strings.Contains(result.ForLLM, "already") {
			t.Errorf("expected finished task error, got: %s", result.ForLLM)
		}
	})

	t.Run("BlockedByCrossTeam", func(t *testing.T) {
		mb, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		otherTeamID := uuid.New()
		crossTeamTaskID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[crossTeamTaskID] = &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: crossTeamTaskID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:    otherTeamID,
			Subject:   "Cross team task",
			Status:    store.TeamTaskStatusPending,
		}
		mb.taskStore.mu.Unlock()

		result := tool.Execute(ctx, map[string]any{
			"action":     "create",
			"subject":    "Cross-team blocked task",
			"assignee":   "member-agent",
			"blocked_by": []any{crossTeamTaskID.String()},
		})
		if !result.IsError {
			t.Fatal("expected error for cross-team blocked_by")
		}
		if !strings.Contains(result.ForLLM, "different team") {
			t.Errorf("expected 'different team' error, got: %s", result.ForLLM)
		}
	})

	t.Run("BlockedByInvalidUUID", func(t *testing.T) {
		_, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":     "create",
			"subject":    "Task with bad blocker",
			"assignee":   "member-agent",
			"blocked_by": []any{"not-a-uuid"},
		})
		if !result.IsError {
			t.Fatal("expected error for invalid UUID in blocked_by")
		}
		if !strings.Contains(result.ForLLM, "invalid") {
			t.Errorf("expected 'invalid' UUID error, got: %s", result.ForLLM)
		}
	})

	t.Run("RequireApproval", func(t *testing.T) {
		mb, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":           "create",
			"subject":          "Approval task",
			"assignee":         "member-agent",
			"require_approval": true,
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
		if task.Status != store.TeamTaskStatusInReview {
			t.Errorf("expected status in_review, got %s", task.Status)
		}
	})

	t.Run("WithBlockers", func(t *testing.T) {
		mb, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		blockerID := uuid.New()
		mb.taskStore.mu.Lock()
		mb.taskStore.tasks[blockerID] = &store.TeamTaskData{
			BaseModel: store.BaseModel{ID: blockerID, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			TeamID:    testTeamID,
			Subject:   "Pending blocker",
			Status:    store.TeamTaskStatusPending,
		}
		mb.taskStore.mu.Unlock()

		result := tool.Execute(ctx, map[string]any{
			"action":     "create",
			"subject":    "Blocked task",
			"assignee":   "member-agent",
			"blocked_by": []any{blockerID.String()},
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", result.ForLLM)
		}
		mb.taskStore.mu.Lock()
		var created *store.TeamTaskData
		for _, v := range mb.taskStore.tasks {
			if v.Subject == "Blocked task" {
				created = v
			}
		}
		mb.taskStore.mu.Unlock()
		if created == nil {
			t.Fatal("task not found in store")
		}
		if created.Status != store.TeamTaskStatusBlocked {
			t.Errorf("expected status blocked, got %s", created.Status)
		}
	})

	t.Run("PendingDispatchTracked", func(t *testing.T) {
		mb, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":   "create",
			"subject":  "Tracked task",
			"assignee": "member-agent",
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", result.ForLLM)
		}
		drained := ptd.Drain()
		ids := drained[testTeamID]
		if len(ids) != 1 {
			t.Errorf("expected 1 pending dispatch, got %d", len(ids))
		}
		mb.taskStore.mu.Lock()
		var task *store.TeamTaskData
		for _, v := range mb.taskStore.tasks {
			task = v
		}
		mb.taskStore.mu.Unlock()
		if task != nil && len(ids) == 1 && ids[0] != task.ID {
			t.Errorf("tracked task ID %s does not match created task ID %s", ids[0], task.ID)
		}
	})

	t.Run("CompoundSubjectWarning", func(t *testing.T) {
		_, tool, _, _, ctx := newTestTeamSetup()
		ptd := NewPendingTeamDispatch()
		ptd.MarkListed()
		ctx = WithPendingTeamDispatch(ctx, ptd)

		result := tool.Execute(ctx, map[string]any{
			"action":   "create",
			"subject":  "Implement and design the new feature",
			"assignee": "member-agent",
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", result.ForLLM)
		}
		if !strings.Contains(result.ForLLM, "Warning") {
			t.Errorf("expected Warning in response for compound subject, got: %s", result.ForLLM)
		}
	})
}
