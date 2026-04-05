package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (t *TeamTasksTool) executeClaim(ctx context.Context, args map[string]any) *Result {
	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	if err := t.manager.Store().ClaimTask(ctx, taskID, agentID, team.ID); err != nil {
		return ErrorResult("failed to claim task: " + err.Error())
	}
	// Record action flag after successful store operation.
	recordTaskAction(ctx, func(f *TaskActionFlags) { f.Claimed = true })

	ownerKey := t.manager.AgentKeyFromID(ctx, agentID)
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskClaimed, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		store.TeamTaskStatusInProgress,
		"agent", ownerKey,
		WithOwner(ownerKey, t.manager.AgentDisplayName(ctx, ownerKey)),
		WithContextInfo(ctx),
	))

	return NewResult(fmt.Sprintf("Task %s claimed successfully. It is now in progress.", taskID))
}

func (t *TeamTasksTool) executeComplete(ctx context.Context, args map[string]any) *Result {
	// Note: reviewer role exists — when task has a reviewer, executeComplete
	// routes to in_review status instead of completed. Leader still approves.
	// Member agents always call this; leader bypasses via direct status update.

	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	result, _ := args["result"].(string)
	if result == "" {
		return ErrorResult("result is required for complete action")
	}

	// Auto-claim if the task is still pending (saves an extra tool call).
	// ClaimTask is atomic — only one agent can succeed, others get an error.
	// Ignore claim error: task may already be in_progress (claimed by us or someone else).
	_ = t.manager.Store().ClaimTask(ctx, taskID, agentID, team.ID)

	if err := t.manager.Store().CompleteTask(ctx, taskID, team.ID, result); err != nil {
		// Fast path: check in-memory turn flags before hitting DB again.
		if flags := TaskActionFlagsFromCtx(ctx); flags != nil && flags.Completed {
			return NewResult(fmt.Sprintf("Task %s already completed.", taskID))
		}
		// Slow path: re-query to determine actual status.
		if current, getErr := t.manager.Store().GetTask(ctx, taskID); getErr == nil && current != nil {
			switch current.Status {
			case store.TeamTaskStatusCompleted:
				recordTaskAction(ctx, func(f *TaskActionFlags) { f.Completed = true })
				return NewResult(fmt.Sprintf("Task %s already completed.", taskID))
			case store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled:
				return NewResult(fmt.Sprintf("Task %s is %s — cannot complete.", taskID, current.Status))
			case store.TeamTaskStatusPending:
				// Task was reset by stale recovery — re-assign and retry once.
				if t.manager.Store().AssignTask(ctx, taskID, agentID, team.ID) == nil {
					if t.manager.Store().CompleteTask(ctx, taskID, team.ID, result) == nil {
						slog.Info("executeComplete: re-assigned stale-recovered task", "task_id", taskID)
						err = nil
					}
				}
			}
		}
		if err != nil {
			return ErrorResult("failed to complete task: " + err.Error())
		}
	}
	// Record action flag after successful store operation.
	recordTaskAction(ctx, func(f *TaskActionFlags) { f.Completed = true })

	ownerKey := t.manager.AgentKeyFromID(ctx, agentID)
	// Fetch task for TaskNumber/Subject needed by notification subscriber.
	completedTask, _ := t.manager.Store().GetTask(ctx, taskID)
	var taskNumber int
	var taskSubject string
	if completedTask != nil {
		taskNumber = completedTask.TaskNumber
		taskSubject = completedTask.Subject
	}
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskCompleted, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		store.TeamTaskStatusCompleted,
		"agent", ownerKey,
		WithTaskInfo(taskNumber, taskSubject),
		WithOwner(ownerKey, t.manager.AgentDisplayName(ctx, ownerKey)),
		WithContextInfo(ctx),
	))

	// Dependent tasks are dispatched by the consumer after this agent's turn ends
	// (post-turn), not mid-turn. This prevents dependent tasks from completing and
	// announcing to the leader before this agent's own run finishes.

	return NewResult(fmt.Sprintf("Task %s completed. Dependent tasks will be dispatched after this turn ends.", taskID))
}

func (t *TeamTasksTool) executeCancel(ctx context.Context, args map[string]any) *Result {
	// Note: reviewer role not yet active. Cancellation goes through leader only.

	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.manager.RequireLead(ctx, team, agentID); err != nil {
		return ErrorResult(err.Error())
	}

	reason, _ := args["text"].(string)
	if reason == "" {
		reason = "Cancelled by agent"
	}

	// CancelTask: guards against completed tasks, unblocks dependents, transitions blocked→pending.
	if err := t.manager.Store().CancelTask(ctx, taskID, team.ID, reason); err != nil {
		return ErrorResult("failed to cancel task: " + err.Error())
	}

	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskCancelled, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		store.TeamTaskStatusCancelled,
		"agent", t.manager.AgentKeyFromID(ctx, agentID),
		WithReason(reason),
		WithContextInfo(ctx),
	))

	// Dependent tasks are dispatched by the consumer after this agent's turn ends (post-turn).

	return NewResult(fmt.Sprintf("Task %s cancelled. Dependent tasks will be unblocked after this turn ends.", taskID))
}

func (t *TeamTasksTool) executeReview(ctx context.Context, args map[string]any) *Result {
	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Verify the agent owns this task.
	task, err := t.manager.Store().GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}
	if task.OwnerAgentID == nil || *task.OwnerAgentID != agentID {
		return ErrorResult("only the task owner can submit for review")
	}

	if err := t.manager.Store().ReviewTask(ctx, taskID, team.ID); err != nil {
		return ErrorResult("failed to submit for review: " + err.Error())
	}
	// Record action flag after successful store operation.
	recordTaskAction(ctx, func(f *TaskActionFlags) { f.Reviewed = true })

	ownerKey := t.manager.AgentKeyFromID(ctx, agentID)
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskReviewed, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		store.TeamTaskStatusInReview,
		"agent", ownerKey,
		WithOwner(ownerKey, t.manager.AgentDisplayName(ctx, ownerKey)),
		WithContextInfo(ctx),
	))

	return NewResult(fmt.Sprintf("Task %s submitted for review.", taskID))
}

func (t *TeamTasksTool) executeApprove(ctx context.Context, args map[string]any) *Result {
	// Note: reviewer role not yet active. All approvals flow through leader or dashboard.

	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Only lead can approve tasks via tool (non-lead agents should not approve).
	// System/dashboard channels bypass this check (human UI approval).
	ch := ToolChannelFromCtx(ctx)
	if ch != ChannelSystem && ch != ChannelDashboard {
		if err := t.manager.RequireLead(ctx, team, agentID); err != nil {
			return ErrorResult(err.Error())
		}
	}

	// Fetch task for subject (used in lead message) and team ownership check
	task, err := t.manager.Store().GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	// Atomic transition: in_review -> completed
	if err := t.manager.Store().ApproveTask(ctx, taskID, team.ID, ""); err != nil {
		return ErrorResult("failed to approve task: " + err.Error())
	}

	// Re-fetch to get the actual post-approval status (pending or blocked)
	approved, _ := t.manager.Store().GetTask(ctx, taskID)
	newStatus := store.TeamTaskStatusPending
	if approved != nil {
		newStatus = approved.Status
	}

	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskApproved, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		newStatus,
		"agent", t.manager.AgentKeyFromID(ctx, agentID),
		WithSubject(task.Subject),
		WithContextInfo(ctx),
	))

	// Record approval as a task comment for audit trail.
	approveMsg := fmt.Sprintf("Task approved (status: %s).", newStatus)
	_ = t.manager.Store().AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  taskID,
		AgentID: &agentID,
		Content: approveMsg,
	})

	return NewResult(fmt.Sprintf("Task %s approved (status: %s).", taskID, newStatus))
}

func (t *TeamTasksTool) executeReject(ctx context.Context, args map[string]any) *Result {
	// Note: reviewer role not yet active. Rejections flow through leader or dashboard.

	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Only lead can reject tasks via tool.
	ch := ToolChannelFromCtx(ctx)
	if ch != ChannelSystem && ch != ChannelDashboard {
		if err := t.manager.RequireLead(ctx, team, agentID); err != nil {
			return ErrorResult(err.Error())
		}
	}

	reason, _ := args["text"].(string)
	if reason == "" {
		reason = "Rejected by user"
	}

	// Fetch task to get subject for the lead message
	task, err := t.manager.Store().GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	// Record rejection as a task comment for audit trail (before status change).
	rejectMsg := fmt.Sprintf("Task rejected. Reason: %s", reason)
	_ = t.manager.Store().AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  taskID,
		AgentID: &agentID,
		Content: rejectMsg,
	})

	// Auto re-dispatch if task has an owner: skip RejectTask (which unblocks dependents)
	// and instead reset in_review → pending → in_progress → dispatch.
	// Dependents stay blocked until this task actually completes or circuit breaker fails it.
	if task.OwnerAgentID != nil {
		// Reset in_review → pending (ResetTaskStatus accepts in_review via store guard).
		if err := t.manager.Store().ResetTaskStatus(ctx, taskID, team.ID); err != nil {
			// Fallback: use RejectTask (cancels + unblocks) if reset fails.
			slog.Warn("reject: reset failed, falling back to RejectTask", "task_id", taskID, "error", err)
			if err := t.manager.Store().RejectTask(ctx, taskID, team.ID, reason); err != nil {
				return ErrorResult("failed to reject task: " + err.Error())
			}
			t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskRejected, BuildTaskEventPayload(
				team.ID.String(), taskID.String(),
				store.TeamTaskStatusCancelled,
				"agent", t.manager.AgentKeyFromID(ctx, agentID),
				WithSubject(task.Subject),
				WithReason(reason),
				WithContextInfo(ctx),
			))
			return NewResult(fmt.Sprintf("Task %s rejected (cancelled). Use retry to re-dispatch manually.", taskID))
		}
		if err := t.manager.Store().AssignTask(ctx, taskID, *task.OwnerAgentID, team.ID); err != nil {
			slog.Warn("reject: assign task failed", "task_id", taskID, "error", err)
			return NewResult(fmt.Sprintf("Task %s rejected but could not assign. Use retry to re-dispatch manually.", taskID))
		}
		t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskRejected, BuildTaskEventPayload(
			team.ID.String(), taskID.String(),
			store.TeamTaskStatusInProgress,
			"agent", t.manager.AgentKeyFromID(ctx, agentID),
			WithSubject(task.Subject),
			WithReason(reason),
			WithContextInfo(ctx),
		))
		t.manager.DispatchTaskToAgent(ctx, task, team, *task.OwnerAgentID)
		return NewResult(fmt.Sprintf("Task %s rejected and re-dispatched to %s with feedback.",
			taskID, t.manager.AgentKeyFromID(ctx, *task.OwnerAgentID)))
	}

	// No owner — use RejectTask to cancel + unblock dependents.
	if err := t.manager.Store().RejectTask(ctx, taskID, team.ID, reason); err != nil {
		return ErrorResult("failed to reject task: " + err.Error())
	}
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskRejected, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		store.TeamTaskStatusCancelled,
		"agent", t.manager.AgentKeyFromID(ctx, agentID),
		WithSubject(task.Subject),
		WithReason(reason),
		WithContextInfo(ctx),
	))
	return NewResult(fmt.Sprintf("Task %s rejected.", taskID))
}
