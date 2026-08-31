package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// handleBlockerComment resolves a blocker escalation. For a workflow work task
// it transitions the step in_progress→blocked (attempt-fenced) and arms a
// durable coordinator-escalation pending state — it does NOT fail the whole
// workflow. For a non-workflow task it keeps the legacy behavior: auto-fail the
// task, cancel the member session via an EventTeamTaskFailed broadcast, and
// escalate to the leader agent.
func (t *TeamTasksTool) handleBlockerComment(
	ctx context.Context,
	team *store.TeamData,
	task *store.TeamTaskData,
	taskID, agentID uuid.UUID,
	text string,
) *Result {
	// Only escalate if task is still in progress.
	if task.Status != store.TeamTaskStatusInProgress {
		return NewResult(fmt.Sprintf(
			"Blocker comment saved on task #%d \"%s\", but task is already %s — no escalation needed.",
			task.TaskNumber, task.Subject, task.Status))
	}

	// Auto-fail the task. FailTask guards with AND status='in_progress' so
	// concurrent calls are safe — only one succeeds.
	reason := "Blocked: " + text
	if len([]rune(reason)) > 500 {
		reason = string([]rune(reason)[:500])
	}
	// Workflow work tasks do NOT mechanically fail the whole workflow on a blocker
	// (the July-14 incident). The attempt-fenced CAS moves this step
	// in_progress→blocked and arms a durable coordinator-escalation pending state;
	// the recovery ticker then enqueues a coordinator recovery run (retrying the
	// enqueue with capped backoff, so the escalation can never be silently dropped
	// the way the incident's fire-and-forget publish was). Independent DAG branches
	// keep running. We broadcast the authoritative team.task.blocked refetch hint,
	// NOT team.task.failed — the latter fires the cancel subscriber and would
	// mechanically fail the step.
	if task.WorkflowID != nil && task.WorkflowKind == store.TeamWorkflowTaskKindWork {
		workflowStore, ok := t.manager.Store().(store.TeamWorkflowStore)
		if !ok {
			return ErrorResult("team workflow store is unavailable")
		}
		attempt, ok := store.WorkflowTaskAttemptFromContext(ctx)
		if !ok {
			return ErrorResult("workflow attempt identity missing from run context")
		}
		transition, err := workflowStore.BlockWorkflowTaskAttempt(ctx, attempt, reason)
		if err != nil {
			return ErrorResult("failed to record workflow blocker: " + err.Error())
		}
		if transition.Stale() {
			return ErrorResult("this workflow step was superseded by recovery/replan and can no longer be blocked")
		}
		if transition.Outcome == store.WorkflowMutationAlreadyApplied {
			return NewResult(fmt.Sprintf("Workflow step %s is already blocked; the coordinator will resolve it.", task.WorkflowStepID))
		}
		recordTaskAction(ctx, func(f *TaskActionFlags) {
			f.Commented = true
			f.Escalated = true
		})
		blockerPeerKind := ""
		if pk, ok := task.Metadata[TaskMetaPeerKind].(string); ok {
			blockerPeerKind = pk
		}
		blockerLocalKey := ""
		if lk, ok := task.Metadata[TaskMetaLocalKey].(string); ok {
			blockerLocalKey = lk
		}
		ownerKey := t.manager.AgentKeyFromID(ctx, agentID)
		t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskBlocked, BuildTaskEventPayload(
			team.ID.String(), taskID.String(),
			store.TeamTaskStatusBlocked,
			"agent", ownerKey,
			WithTaskInfo(task.TaskNumber, task.Subject),
			WithOwnerAgentKey(ownerKey),
			WithReason(reason),
			WithUserID(store.ActorIDFromContext(ctx)),
			WithChannel(task.Channel),
			WithChatID(task.ChatID),
			WithPeerKind(blockerPeerKind),
			WithLocalKey(blockerLocalKey),
			WithContextInfo(ctx),
		))
		return NewResult(fmt.Sprintf(
			"Workflow step %s marked blocked. The workflow coordinator will resolve it (retry, replan, cancel, or fail) — independent steps keep running.",
			task.WorkflowStepID))
	}
	if err := t.manager.Store().FailTask(ctx, taskID, team.ID, reason); err != nil {
		slog.Warn("blocker: FailTask error", "task_id", taskID, "error", err)
		// Task may have completed concurrently — not a hard error.
		return NewResult("Blocker comment saved. Task may have already completed — check task status.")
	}

	// Broadcast EventTeamTaskFailed — triggers:
	// 1. Cancel subscriber → sched.CancelSession() → member stops
	// 2. Notify subscriber → "❌ Task failed" → chat channel (direct outbound)
	// 3. WS broadcast → web UI dashboard real-time update
	memberKey := t.manager.AgentKeyFromID(ctx, agentID)
	blockerPeerKind := ""
	if pk, ok := task.Metadata[TaskMetaPeerKind].(string); ok {
		blockerPeerKind = pk
	}
	blockerLocalKey := ""
	if lk, ok := task.Metadata[TaskMetaLocalKey].(string); ok {
		blockerLocalKey = lk
	}
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskFailed, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		store.TeamTaskStatusFailed,
		"agent", memberKey,
		WithTaskInfo(task.TaskNumber, task.Subject),
		WithOwnerAgentKey(memberKey),
		WithReason(reason),
		WithUserID(store.ActorIDFromContext(ctx)), // audit actor, not scope (#915)
		WithChannel(task.Channel),
		WithChatID(task.ChatID),
		WithPeerKind(blockerPeerKind),
		WithLocalKey(blockerLocalKey),
	))

	// Escalate to leader if enabled in team settings.
	escalationCfg := ParseBlockerEscalationConfig(team.Settings)
	if escalationCfg.Enabled {
		leadAg, err := t.manager.CachedGetAgentByID(ctx, team.LeadAgentID)
		if err == nil {
			escalationMsg := fmt.Sprintf(
				"[Escalation] Member %q is blocked on task #%d \"%s\"\n\n"+
					"Blocker: %s\n\n"+
					"Use team_tasks(action=\"retry\", task_id=\"%s\") to reopen with updated instructions.",
				memberKey, task.TaskNumber, task.Subject, text, taskID)

			// Escalation metadata: preserve original actor sender through re-ingress
			// so the leader's response turn can attribute to the real user (#915).
			escalationMeta := TaskLocalKeyMetadata(task)
			if escalationMeta == nil {
				escalationMeta = map[string]string{}
			}
			if actorSender := store.SenderIDFromContext(ctx); actorSender != "" {
				escalationMeta[MetaOriginSenderID] = actorSender
			}
			if !t.manager.TryPublishInbound(bus.InboundMessage{
				Channel:  task.Channel,
				SenderID: "system:escalation",
				ChatID:   task.ChatID,
				Metadata: escalationMeta,
				Content:  escalationMsg,
				UserID:   store.ActorIDFromContext(ctx), // audit actor (#915)
				PeerKind: blockerPeerKind,
				TenantID: store.TenantIDFromContext(ctx),
				AgentID:  leadAg.AgentKey,
			}) {
				slog.Warn("blocker: escalation dropped (bus unavailable or buffer full)",
					"task_id", taskID, "leader", leadAg.AgentKey)
			}
		}
	}

	return NewResult(fmt.Sprintf(
		"Task #%d \"%s\" auto-failed due to blocker. Leader has been notified and can retry with updated instructions.",
		task.TaskNumber, task.Subject))
}
