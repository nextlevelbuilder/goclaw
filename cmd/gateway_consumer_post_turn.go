package cmd

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
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
		if taskPeerKind == "" && currentTask.Metadata != nil {
			if pk, ok := currentTask.Metadata[tools.TaskMetaPeerKind].(string); ok && pk != "" {
				taskPeerKind = pk
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
			bus.BroadcastForTenant(deps.MsgBus, protocol.EventTeamTaskFailed, store.TenantIDFromContext(ctx), tools.BuildTaskEventPayload(
				meta.TeamID.String(), meta.TaskID.String(),
				store.TeamTaskStatusFailed,
				"agent", toAgent,
				tools.WithTaskInfo(taskNumber, taskSubject),
				tools.WithReason(outcome.Err.Error()),
				tools.WithChannel(taskChannel),
				tools.WithChatID(taskChatID),
				tools.WithPeerKind(taskPeerKind),
				tools.WithTimestamp(now),
			))
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
			bus.BroadcastForTenant(deps.MsgBus, protocol.EventTeamTaskFailed, store.TenantIDFromContext(ctx), tools.BuildTaskEventPayload(
				meta.TeamID.String(), meta.TaskID.String(),
				store.TeamTaskStatusFailed,
				"system", "loop_detector",
				tools.WithTaskInfo(taskNumber, taskSubject),
				tools.WithReason("loop_detector_kill"),
				tools.WithChannel(taskChannel),
				tools.WithChatID(taskChatID),
				tools.WithPeerKind(taskPeerKind),
				tools.WithTimestamp(now),
			))
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
		if result == "" {
			result = "Agent run ended without explicit result"
		}
		if len(result) > 100_000 {
			result = result[:100_000] + "\n[truncated]"
		}
		if err := deps.TeamStore.CompleteTask(ctx, meta.TaskID, meta.TeamID, result); err != nil {
			slog.Warn("auto-complete: CompleteTask error", "task_id", meta.TaskID, "error", err)
		} else {
			bus.BroadcastForTenant(deps.MsgBus, protocol.EventTeamTaskCompleted, store.TenantIDFromContext(ctx), tools.BuildTaskEventPayload(
				meta.TeamID.String(), meta.TaskID.String(),
				store.TeamTaskStatusCompleted,
				"agent", toAgent,
				tools.WithTaskInfo(taskNumber, taskSubject),
				tools.WithOwnerAgentKey(toAgent),
				tools.WithChannel(taskChannel),
				tools.WithChatID(taskChatID),
				tools.WithPeerKind(taskPeerKind),
				tools.WithTimestamp(now),
			))
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
