package cmd

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// startTaskLockRenewal extends the task lock every 5 min while the agent works.
// Returns a stop func — call it when the run finishes. Returns nil if taskID is
// invalid or teamStore is nil.
func startTaskLockRenewal(ctx context.Context, teamStore store.TeamStore, taskID, teamID uuid.UUID) (stop func()) {
	if taskID == uuid.Nil || teamStore == nil {
		return nil
	}
	ch := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := teamStore.RenewTaskLock(ctx, taskID, teamID); err != nil {
					slog.Warn("teammate lock renewal failed", "task_id", taskID, "error", err)
					return
				}
				slog.Debug("teammate lock renewed", "task_id", taskID)
			case <-ch:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(ch) }
}

// startWorkflowLockRenewal renews a workflow-work task lease every 5 min while
// the agent works, fenced on the accepted attempt. Unlike startTaskLockRenewal,
// the heartbeat CAS carries the full attempt tuple, so a superseded attempt's
// stale ticker cannot extend the lease on the replacement run: HeartbeatWorkflow-
// TaskAttempt returns a non-Applied outcome and this stops renewing. Returns a
// stop func — call it when the run finishes. Returns nil if the store lacks the
// workflow contract or the attempt is zero.
func startWorkflowLockRenewal(ctx context.Context, teamStore store.TeamStore, attempt store.WorkflowTaskAttempt) (stop func()) {
	workflowStore, ok := teamStore.(store.TeamWorkflowStore)
	if !ok || attempt.DispatchToken == uuid.Nil || attempt.TaskID == uuid.Nil {
		return nil
	}
	ch := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				transition, err := workflowStore.HeartbeatWorkflowTaskAttempt(ctx, attempt, time.Now().Add(60*time.Minute))
				if err != nil {
					slog.Warn("workflow lock renewal failed", "task_id", attempt.TaskID, "error", err)
					return
				}
				if transition.Outcome != store.WorkflowMutationApplied {
					// Attempt superseded (stale) or already terminal — stop renewing so
					// this ticker never keeps a lease alive for a task the replacement
					// attempt now owns.
					slog.Info("workflow lock renewal stopping: attempt no longer current",
						"task_id", attempt.TaskID, "outcome", transition.Outcome)
					return
				}
				slog.Debug("workflow lock renewed", "task_id", attempt.TaskID)
			case <-ch:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(ch) }
}

// teammateTaskMeta holds resolved task context for post-turn processing.
type teammateTaskMeta struct {
	TaskID     uuid.UUID
	TeamID     uuid.UUID
	ToAgent    string
	Channel    string
	ChatID     string
	PeerKind   string // "group" or "direct" — for correct notification routing (#266)
	Subject    string
	TaskNumber int
}

// resolveTeamTaskOutcome determines the correct task lifecycle action
// after a teammate run: error→fail, completed/escalated→skip, reviewed→renew,
// loopKilled→fail, default→auto-complete.
// Returns the resolved team (for announce routing) and dispatches unblocked tasks.
func resolveTeamTaskOutcome(
	ctx context.Context,
	deps *ConsumerDeps,
	outcome scheduler.RunOutcome,
	flags *tools.TaskActionFlags,
	meta teammateTaskMeta,
) *store.TeamData {
	if meta.TaskID == uuid.Nil || deps.TeamStore == nil {
		return nil
	}

	cachedTeam, _ := deps.TeamStore.GetTeam(ctx, meta.TeamID)
	if cachedTeam == nil {
		return nil
	}

	// Check current task status — agent may have already updated it via tool.
	currentTask, taskErr := deps.TeamStore.GetTask(ctx, meta.TaskID)
	alreadyTerminal := taskErr == nil && currentTask != nil &&
		(currentTask.Status == store.TeamTaskStatusCompleted ||
			currentTask.Status == store.TeamTaskStatusFailed ||
			currentTask.Status == store.TeamTaskStatusCancelled)

	if alreadyTerminal {
		// Always dispatch unblocked tasks even if already terminal.
		if deps.PostTurn != nil {
			deps.PostTurn.DispatchUnblockedTasks(ctx, meta.TeamID)
		}
		return cachedTeam
	}

	toAgent := meta.ToAgent
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	taskSubject := meta.Subject
	taskNumber := meta.TaskNumber
	taskChannel := meta.Channel
	taskChatID := meta.ChatID
	taskPeerKind := meta.PeerKind
	var taskLocalKey string

	// Enrich with live task data if available.
	if currentTask != nil {
		if currentTask.Subject != "" {
			taskSubject = currentTask.Subject
		}
		if currentTask.TaskNumber != 0 {
			taskNumber = currentTask.TaskNumber
		}
		if currentTask.Channel != "" {
			taskChannel = currentTask.Channel
		}
		if currentTask.ChatID != "" {
			taskChatID = currentTask.ChatID
		}
		if currentTask.Metadata != nil {
			if taskPeerKind == "" {
				if pk, ok := currentTask.Metadata[tools.TaskMetaPeerKind].(string); ok && pk != "" {
					taskPeerKind = pk
				}
			}
			// local_key is required for forum topic routing (e.g. Telegram supergroup threads).
			if lk, ok := currentTask.Metadata[tools.TaskMetaLocalKey].(string); ok {
				taskLocalKey = lk
			}
		}
	}

	// Smart post-turn decision based on action flags.
	// Only error, completed/escalated, and reviewed block auto-complete.
	switch {
	case outcome.Err != nil:
		// Agent errored → auto-fail.
		if err := deps.TeamStore.FailTask(ctx, meta.TaskID, meta.TeamID, outcome.Err.Error()); err != nil {
			slog.Warn("auto-complete: FailTask error", "task_id", meta.TaskID, "error", err)
		} else {
			tools.PublishTaskEventWithResolver(deps.TeamStore, deps.MsgBus, deps.AgentStore, protocol.EventTeamTaskFailed, tools.BuildTaskEventPayload(
				meta.TeamID.String(), meta.TaskID.String(),
				store.TeamTaskStatusFailed,
				"agent", toAgent,
				tools.WithTaskInfo(taskNumber, taskSubject),
				tools.WithReason(outcome.Err.Error()),
				tools.WithChannel(taskChannel),
				tools.WithChatID(taskChatID),
				tools.WithPeerKind(taskPeerKind),
				tools.WithLocalKey(taskLocalKey),
				tools.WithTimestamp(now),
			), uuid.Nil)
		}

	case flags.Completed || flags.Escalated:
		// Tool already completed/failed the task — skip auto-complete.
		slog.Info("post-turn: tool handled task", "task_id", meta.TaskID,
			"completed", flags.Completed, "escalated", flags.Escalated)

	case flags.Reviewed:
		// Task submitted for review — skip auto-complete, renew lock.
		_ = deps.TeamStore.RenewTaskLock(ctx, meta.TaskID, meta.TeamID)
		slog.Info("post-turn: task submitted for review", "task_id", meta.TaskID)

	case outcome.Result != nil && outcome.Result.LoopKilled:
		// Loop detector killed the run → auto-fail instead of auto-complete.
		// The agent was terminated (not stuck in_progress) — mark as failed
		// so the leader sees clear failure signal and can retry directly.
		failMsg := outcome.Result.Content
		if failMsg == "" {
			failMsg = "Agent run terminated by loop detector"
		}
		if err := deps.TeamStore.FailTask(ctx, meta.TaskID, meta.TeamID, failMsg); err != nil {
			slog.Warn("auto-fail: FailTask error (loop kill)", "task_id", meta.TaskID, "error", err)
		} else {
			tools.PublishTaskEventWithResolver(deps.TeamStore, deps.MsgBus, deps.AgentStore, protocol.EventTeamTaskFailed, tools.BuildTaskEventPayload(
				meta.TeamID.String(), meta.TaskID.String(),
				store.TeamTaskStatusFailed,
				"system", "loop_detector",
				tools.WithTaskInfo(taskNumber, taskSubject),
				tools.WithReason("loop_detector_kill"),
				tools.WithChannel(taskChannel),
				tools.WithChatID(taskChatID),
				tools.WithPeerKind(taskPeerKind),
				tools.WithLocalKey(taskLocalKey),
				tools.WithTimestamp(now),
			), uuid.Nil)
		}
		slog.Warn("post-turn: loop detector killed member run",
			"task_id", meta.TaskID, "agent", toAgent)

	default:
		// Agent turn ended without terminal action — auto-complete.
		// Covers: Progressed, Commented, Claimed, or no flags at all.
		result := ""
		if outcome.Result != nil {
			result = outcome.Result.Content
			if len(outcome.Result.Deliverables) > 0 {
				result = strings.Join(outcome.Result.Deliverables, "\n\n---\n\n")
			}
		}
		// A turn that produced nothing usable is not a completed task. The
		// workflow-step path already refuses to settle on "..." / NO_REPLY
		// (workflowStepResultIsUsable); member request tasks settle here
		// instead and were being marked completed with a 3-character
		// placeholder as their deliverable, so the requester got a "done"
		// task with no work in it. Requeue for another dispatch instead —
		// an empty turn is usually a transient provider failure.
		if !workflowStepResultIsUsable(result) {
			requeueMemberTaskWithoutResult(ctx, deps, meta, taskNumber, taskSubject, toAgent,
				taskChannel, taskChatID, taskPeerKind, taskLocalKey, now)
			break
		}
		if len(result) > 100_000 {
			result = result[:100_000] + "\n[truncated]"
		}
		if err := deps.TeamStore.CompleteTask(ctx, meta.TaskID, meta.TeamID, result); err != nil {
			slog.Warn("auto-complete: CompleteTask error", "task_id", meta.TaskID, "error", err)
		} else {
			tools.PublishTaskEventWithResolver(deps.TeamStore, deps.MsgBus, deps.AgentStore, protocol.EventTeamTaskCompleted, tools.BuildTaskEventPayload(
				meta.TeamID.String(), meta.TaskID.String(),
				store.TeamTaskStatusCompleted,
				"agent", toAgent,
				tools.WithTaskInfo(taskNumber, taskSubject),
				tools.WithOwnerAgentKey(toAgent),
				tools.WithChannel(taskChannel),
				tools.WithChatID(taskChatID),
				tools.WithPeerKind(taskPeerKind),
				tools.WithLocalKey(taskLocalKey),
				tools.WithTimestamp(now),
			), uuid.Nil)
		}
	}

	// Always dispatch unblocked tasks after member turn ends,
	// regardless of whether the task was already completed by the tool.
	// This ensures dependent tasks start only after the member's run finishes.
	if deps.PostTurn != nil {
		deps.PostTurn.DispatchUnblockedTasks(ctx, meta.TeamID)
	}

	return cachedTeam
}

// requeueMemberTaskWithoutResult returns a member request task to pending after a
// turn that produced no usable deliverable, so the next dispatch can try again.
//
// The empty turn is almost always a transient provider failure — a stream that
// died before its first chunk reads as a successful empty answer — so failing the
// task outright would discard work the requester still wants. But an unbounded
// requeue would loop forever against a provider that keeps returning nothing, so
// the retry shares the SAME dispatch budget as every other dispatch. Once the
// budget is spent the task stays failed with a diagnosable reason.
//
// There is no single-step "back to pending" transition from in_progress, so this
// goes through the two supported store calls (FailTask → ResetTaskStatus) rather
// than writing status SQL of its own. If the reset fails the task is left failed,
// which is the safe end state: visible to the requester, not silently "done".
func requeueMemberTaskWithoutResult(
	ctx context.Context,
	deps *ConsumerDeps,
	meta teammateTaskMeta,
	taskNumber int,
	taskSubject string,
	toAgent string,
	taskChannel string,
	taskChatID string,
	taskPeerKind string,
	taskLocalKey string,
	now string,
) {
	const reason = "Agent run ended without a usable result"

	dispatches := 0
	if task, err := deps.TeamStore.GetTask(ctx, meta.TaskID); err == nil && task != nil {
		dispatches = tools.TaskDispatchCount(task)
	}

	if err := deps.TeamStore.FailTask(ctx, meta.TaskID, meta.TeamID, reason); err != nil {
		slog.Warn("post-turn: FailTask error before requeue", "task_id", meta.TaskID, "error", err)
		return
	}

	if dispatches >= tools.MaxTaskDispatches {
		slog.Warn("post-turn: no usable result and dispatch budget exhausted",
			"task_id", meta.TaskID, "agent", toAgent, "dispatch_count", dispatches)
		tools.PublishTaskEventWithResolver(deps.TeamStore, deps.MsgBus, deps.AgentStore, protocol.EventTeamTaskFailed, tools.BuildTaskEventPayload(
			meta.TeamID.String(), meta.TaskID.String(),
			store.TeamTaskStatusFailed,
			"system", "no_usable_result",
			tools.WithTaskInfo(taskNumber, taskSubject),
			tools.WithReason("no_usable_result"),
			tools.WithChannel(taskChannel),
			tools.WithChatID(taskChatID),
			tools.WithPeerKind(taskPeerKind),
			tools.WithLocalKey(taskLocalKey),
			tools.WithTimestamp(now),
		), uuid.Nil)
		return
	}

	if err := deps.TeamStore.ResetTaskStatus(ctx, meta.TaskID, meta.TeamID); err != nil {
		slog.Warn("post-turn: requeue after empty run failed; task left failed",
			"task_id", meta.TaskID, "error", err)
		return
	}
	slog.Warn("post-turn: no usable result, requeueing member task",
		"task_id", meta.TaskID, "agent", toAgent, "dispatch_count", dispatches)
}
