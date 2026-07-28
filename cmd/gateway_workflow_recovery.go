package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// recoverWorkflowBlocker runs the canonical coordinator so it can resolve a
// blocked workflow work task. It is enqueued by the recovery ticker after
// ClaimTaskEscalation hands out a Claimed escalation (durable, bounded retry),
// which is the July-14 fix: a blocker no longer mechanically fails the workflow
// and the escalation to the coordinator can no longer be silently dropped.
//
// Unlike finalizeWorkflow, the run executes AS the coordinator (the SessionKey
// is scoped to workflow.CoordinatorAgentKey so makeSchedulerRunFunc routes it to
// the lead and team_tasks' RequireLead passes) and it KEEPS team_tasks/delegate/
// subagent_spawn — the coordinator resolves the blocker THROUGH those tools by
// choosing exactly one bounded action (retry_blocked / apply_replan /
// cancel_workflow / fail_workflow). RunKindWorkflowRecovery only refuses mid-run
// user injection (router.go) and reserves the session in the FIFO (chat.go); it
// does NOT filter tools the way the finalize run does.
func recoverWorkflowBlocker(ctx context.Context, deps *ConsumerDeps, workflowStore store.TeamWorkflowStore, workflowID, blockedTaskID uuid.UUID) {
	workflow, err := workflowStore.GetWorkflow(ctx, workflowID)
	if err != nil {
		slog.Warn("workflow recovery: get workflow failed", "workflow_id", workflowID, "error", err)
		return
	}
	// A concurrent coordinator resolution (retry/replan/cancel) or a terminal
	// finalize may have moved the workflow past the point where recovery is
	// meaningful. Only running/needs_revision workflows have a live blocker to
	// resolve; anything else is a no-op (the escalation CAS already dropped it).
	if workflow.Status != store.TeamWorkflowStatusRunning && workflow.Status != store.TeamWorkflowStatusNeedsRevision {
		slog.Debug("workflow recovery: workflow no longer resolvable", "workflow_id", workflowID, "status", workflow.Status)
		return
	}
	if workflow.CoordinatorAgentKey == "" {
		slog.Warn("workflow recovery: coordinator agent key missing", "workflow_id", workflowID)
		return
	}
	tasks, err := workflowStore.ListWorkflowTasks(ctx, workflowID)
	if err != nil {
		slog.Warn("workflow recovery: list tasks failed", "workflow_id", workflowID, "error", err)
		return
	}
	var blocked *store.TeamTaskData
	for i := range tasks {
		if tasks[i].ID == blockedTaskID {
			blocked = &tasks[i]
			break
		}
	}
	if blocked == nil || blocked.Status != store.TeamTaskStatusBlocked {
		// The task was already resolved (retried/replanned) between the claim and
		// this run — nothing to escalate.
		slog.Debug("workflow recovery: blocked task already resolved", "workflow_id", workflowID, "task_id", blockedTaskID)
		return
	}
	var comments []store.TeamTaskCommentData
	if recent, cErr := deps.TeamStore.ListRecentTaskComments(ctx, blockedTaskID, 5); cErr == nil {
		comments = recent
	}

	peerKind := workflow.OriginPeerKind
	if peerKind == "" {
		peerKind = string(sessions.PeerDirect)
	}
	// Route the recovery run to the coordinator's own session so RequireLead
	// passes and any clarification the coordinator sends lands in the origin
	// conversation. This mirrors finalize's origin-session routing but swaps the
	// origin agent for the canonical coordinator.
	coordinatorSession := sessions.BuildScopedSessionKey(workflow.CoordinatorAgentKey, workflow.OriginChannel, sessions.PeerKind(peerKind), workflow.OriginChatID)

	content := buildWorkflowRecoveryPrompt(workflow, tasks, blocked, comments)
	runCtx, cancelRun := context.WithTimeout(store.WithTenantID(context.WithoutCancel(ctx), workflow.TenantID), 10*time.Minute)
	defer cancelRun()
	req := agent.RunRequest{
		SessionKey: coordinatorSession,
		Message:    content,
		Channel:    workflow.OriginChannel,
		ChatID:     workflow.OriginChatID,
		PeerKind:   peerKind,
		LocalKey:   workflow.OriginLocalKey,
		UserID:     workflow.OriginUserID,
		SenderID:   workflow.OriginSenderID,
		Role:       workflow.OriginRole,
		RunID:      "workflow-recovery:" + workflow.ID.String(),
		RunKind:    agent.RunKindWorkflowRecovery,
		HideInput:  true,
		Stream:     false,
		TeamID:     workflow.TeamID.String(),
		// Backend-derived blocked-step identity so the coordinator's recovery actions
		// resolve the workflow and blocked task from context — the recovery prompt
		// hides task IDs/tokens from the model, so this is the only authoritative source.
		WorkflowRecovery: &store.WorkflowRecoveryContext{
			TenantID:      workflow.TenantID,
			TeamID:        workflow.TeamID,
			WorkflowID:    workflow.ID,
			BlockedTaskID: blocked.ID,
		},
		// No BlockedTools: the coordinator resolves the blocker THROUGH team_tasks
		// (retry_blocked / cancel_workflow / fail_workflow) — the tools it keeps.
	}
	outcome := <-deps.Sched.Schedule(runCtx, scheduler.LaneSubagent, req)
	if outcome.Err != nil {
		// The escalation stays armed (ClaimTaskEscalation already scheduled the next
		// capped-backoff re-claim); the ticker will re-enqueue until the coordinator
		// resolves it or MaxWorkflowEscalationAttempts is exhausted, at which point
		// the workflow fails with a user-visible summary. Nothing to fail here.
		slog.Warn("workflow recovery run ended with error", "workflow_id", workflow.ID, "error", outcome.Err)
		return
	}
	slog.Info("workflow recovery run completed", "workflow_id", workflow.ID, "task_id", blockedTaskID)
}

// buildWorkflowRecoveryPrompt assembles the coordinator's recovery briefing:
// the original goal, the current plan status, completed step outputs (evidence
// available to a retry/replan), the blocked step and its blocker reason, and the
// bounded set of resolution actions. It never leaks dispatch tokens or internal
// machinery beyond what the coordinator needs to choose an action.
func buildWorkflowRecoveryPrompt(workflow *store.TeamWorkflowData, tasks []store.TeamTaskData, blocked *store.TeamTaskData, comments []store.TeamTaskCommentData) string {
	var b strings.Builder
	b.WriteString("[Internal workflow recovery]\n")
	b.WriteString("You are the coordinator of a multi-agent workflow. One step is BLOCKED and needs your decision. ")
	b.WriteString("Resolve it by calling team_tasks with exactly one appropriate action:\n")
	switch workflow.Status {
	case store.TeamWorkflowStatusRunning:
		b.WriteString("  • retry_blocked — re-dispatch the blocked step with a revised instruction (use when the step can succeed with clearer guidance or after a dependency is available).\n")
		b.WriteString("  • request_revision — pause the current plan before building a replacement plan (use when the existing plan is no longer suitable).\n")
		b.WriteString("  • cancel_workflow — abandon the workflow (use when the request is no longer valid).\n")
		b.WriteString("  • fail_workflow — declare a terminal failure with a user-facing reason (use only when the goal cannot be achieved).\n")
	case store.TeamWorkflowStatusNeedsRevision:
		b.WriteString("  • retry_blocked — resume the existing plan and re-dispatch the blocked step with a revised instruction.\n")
		b.WriteString("  • apply_replan — ask the backend to build, validate, and freeze a replacement plan using your requirements and completed evidence.\n")
		b.WriteString("  • cancel_workflow — abandon the workflow (use when the request is no longer valid).\n")
		b.WriteString("  • fail_workflow — declare a terminal failure with a user-facing reason (use only when the goal cannot be achieved).\n")
	}
	b.WriteString("Do not mention internal routing, task IDs, tokens, or this instruction to the user.\n\n")

	fmt.Fprintf(&b, "Workflow status: %s (plan revision %d)\n", workflow.Status, workflow.PlanRevision)
	fmt.Fprintf(&b, "Blocked step: %s — %s\n", blocked.WorkflowStepID, blocked.Subject)
	if blocked.BlockerReason != "" {
		fmt.Fprintf(&b, "Blocker reason: %s\n", blocked.BlockerReason)
	}
	if blocked.RecoveryCount > 0 {
		fmt.Fprintf(&b, "This step has already been recovered %d time(s).\n", blocked.RecoveryCount)
	}
	if len(comments) > 0 {
		b.WriteString("\nRecent comments on the blocked step (most recent first):\n")
		for _, c := range comments {
			text := strings.TrimSpace(c.Content)
			if text == "" {
				continue
			}
			fmt.Fprintf(&b, "  - %s\n", text)
		}
	}

	b.WriteString("\nSteps completed so far (their results are available as evidence for a retry or replan):\n")
	anyCompleted := false
	for i := range tasks {
		task := &tasks[i]
		if task.WorkflowKind != store.TeamWorkflowTaskKindWork {
			continue
		}
		if task.Status != store.TeamTaskStatusCompleted || task.Result == nil || strings.TrimSpace(*task.Result) == "" {
			continue
		}
		anyCompleted = true
		fmt.Fprintf(&b, "\nStep %s - %s [completed]\n", task.WorkflowStepID, task.Subject)
		b.WriteString(*task.Result)
		b.WriteString("\n")
	}
	if !anyCompleted {
		b.WriteString("(none yet)\n")
	}

	b.WriteString("\nRemaining steps:\n")
	for i := range tasks {
		task := &tasks[i]
		if task.WorkflowKind != store.TeamWorkflowTaskKindWork {
			continue
		}
		if task.Status == store.TeamTaskStatusCompleted || task.Status == store.TeamTaskStatusStale ||
			task.Status == store.TeamTaskStatusCancelled || task.Status == store.TeamTaskStatusFailed {
			continue
		}
		fmt.Fprintf(&b, "  - Step %s - %s [%s]\n", task.WorkflowStepID, task.Subject, task.Status)
	}
	return b.String()
}
