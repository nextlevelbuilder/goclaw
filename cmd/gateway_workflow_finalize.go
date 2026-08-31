package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// resolveWorkflowTaskOutcome settles a workflow-work task after its agent run
// ends, fenced on the accepted attempt. The attempt (built at accept time and
// threaded through the run goroutine) is the single authority: a superseded
// attempt reaching here gets a typed Stale outcome and settles nothing, and a
// task a tool already settled during the turn gets AlreadyApplied — neither
// re-publishes events, fails the workflow, or double-finalizes. attempt must be
// non-nil for workflow-work runs; the caller gates this on MetaWorkflowID, which
// is set on exactly the same envelopes that carry an accepted attempt.
func resolveWorkflowTaskOutcome(ctx context.Context, deps *ConsumerDeps, outcome scheduler.RunOutcome, flags *tools.TaskActionFlags, meta map[string]string, attempt *store.WorkflowTaskAttempt) {
	workflowStore, ok := deps.TeamStore.(store.TeamWorkflowStore)
	if !ok {
		slog.Error("workflow settle: store unavailable")
		return
	}
	if attempt == nil {
		slog.Warn("workflow settle: missing accepted attempt", "task_id", meta[tools.MetaTeamTaskID])
		return
	}
	taskID := attempt.TaskID
	teamID := attempt.TeamID
	settleCtx, cancelSettle := context.WithTimeout(workflowBackgroundContext(ctx), 30*time.Second)
	defer cancelSettle()
	current, _ := deps.TeamStore.GetTask(settleCtx, taskID)
	failed := outcome.Err != nil || (outcome.Result != nil && outcome.Result.LoopKilled)
	result := ""
	if outcome.Err != nil {
		result = outcome.Err.Error()
	} else if outcome.Result != nil {
		result = outcome.Result.Content
		if len(outcome.Result.Deliverables) > 0 {
			result = strings.Join(outcome.Result.Deliverables, "\n\n---\n\n")
		}
	}
	toolSettled := false
	if current != nil {
		if current.Status == store.TeamTaskStatusFailed || current.Status == store.TeamTaskStatusCancelled {
			failed = true
		}
		if current.Result != nil && strings.TrimSpace(*current.Result) != "" {
			result = *current.Result
		}
		toolSettled = current.Status == store.TeamTaskStatusCompleted ||
			current.Status == store.TeamTaskStatusFailed ||
			current.Status == store.TeamTaskStatusCancelled
	}
	// Decided from the REAL sources, before the placeholder fallback below turns
	// every empty result into prose that would look usable.
	hasUsableResult := workflowStepResultIsUsable(result)
	if result == "" {
		if flags != nil && flags.Completed {
			result = workflowStepPlaceholderCompleted
		} else {
			result = workflowStepPlaceholderNoResult
		}
	}

	// A TRANSIENT provider failure (router timeout, 429/503/529, dropped
	// connection) is not a verdict on the work: the step simply never got to run.
	// Failing it is terminal — the workflow flips to `failing` and there is no path
	// back — so one router timeout on the last step used to discard every step that
	// had already completed. Requeue instead and let the ordinary dispatcher try
	// again, bounded by the SAME maxTaskDispatches budget as any other dispatch, so
	// a persistently broken provider still surfaces as a real failure.
	//
	// Only the run's own error qualifies. A task the tool path already marked
	// failed/cancelled, or a loop the runtime killed, is a real outcome and must
	// stay terminal.
	requeue := false
	if failed && outcome.Err != nil && agent.IsTransientRunFailure(outcome.Err) &&
		settleCtx.Err() == nil &&
		(outcome.Result == nil || !outcome.Result.LoopKilled) &&
		(current == nil || (current.Status != store.TeamTaskStatusFailed && current.Status != store.TeamTaskStatusCancelled)) {
		requeue = tools.WorkflowStepHasDispatchBudget(current)
		if !requeue {
			slog.Warn("workflow settle: transient failure but dispatch budget exhausted",
				"task_id", taskID, "error", outcome.Err)
		}
	}

	// A step that ended without producing anything usable is NOT done. An agent
	// whose turn trails off into "..." or NO_REPLY, never calling
	// team_tasks(action="complete"), used to be settled as COMPLETED carrying that
	// text as its deliverable — so the critic reviewed "..." and the terminal step
	// integrated it. Requeue instead, under the same dispatch budget, and only
	// give up (fail, which is honest) when the budget is gone. A task the tool
	// path already settled is a real verdict and is left alone.
	if !requeue && !failed && !toolSettled && !hasUsableResult && settleCtx.Err() == nil {
		requeue = tools.WorkflowStepHasDispatchBudget(current)
		slog.Warn("workflow settle: step produced no usable result",
			"task_id", taskID, "workflow_id", attempt.WorkflowID,
			"requeue", requeue, "completed_flag", flags != nil && flags.Completed)
		if !requeue {
			failed = true
			result = "Step ended without producing a usable result after repeated attempts"
		}
	}

	var settlement store.WorkflowTaskTransition
	var err error
	switch {
	case requeue:
		slog.Warn("workflow settle: transient run failure, requeueing step",
			"task_id", taskID, "workflow_id", attempt.WorkflowID, "error", outcome.Err)
		settlement, err = workflowStore.RequeueWorkflowTaskAttempt(settleCtx, *attempt, result)
	case failed:
		settlement, err = workflowStore.FailWorkflowTaskAttempt(settleCtx, *attempt, result, time.Now().Add(2*time.Minute))
	default:
		settlement, err = workflowStore.CompleteWorkflowTaskAttempt(settleCtx, *attempt, result)
	}
	if err != nil {
		slog.Warn("workflow settle failed", "task_id", taskID, "error", err)
		return
	}
	switch settlement.Outcome {
	case store.WorkflowMutationStale:
		// Superseded attempt (recovery/replan minted a newer one). Do not mutate,
		// publish, or finalize — the current attempt owns settlement.
		slog.Info("workflow settle: attempt superseded, no-op",
			"task_id", taskID, "dispatch_token", attempt.DispatchToken)
		return
	case store.WorkflowMutationAlreadyApplied:
		// A tool already settled this exact attempt during the turn. The tool path
		// published the lifecycle hint and dispatched dependents; avoid duplicates.
		// Finalization is claim-guarded and idempotent, so honoring ReadyToFinalize
		// here only helps if the tool path didn't already trigger it.
		slog.Debug("workflow settle: already applied by tool", "task_id", taskID)
	}
	if settlement.WorkflowStatus == store.TeamWorkflowStatusRunning && deps.PostTurn != nil {
		deps.PostTurn.DispatchUnblockedTasks(settleCtx, teamID)
	}
	if !settlement.ReadyToFinalize {
		return
	}
	go finalizeWorkflow(workflowBackgroundContext(ctx), deps, workflowStore, settlement.WorkflowID)
}

// Placeholders this settle path substitutes when a run produced no text at all.
// They are named constants so workflowStepResultIsUsable can reject them by
// identity: they are long enough to clear any length floor while carrying zero
// deliverable content.
const (
	workflowStepPlaceholderCompleted = "Step completed"
	workflowStepPlaceholderNoResult  = "Agent run ended without explicit result"
)

// minUsableStepResultRunes is the floor below which a workflow step result
// cannot plausibly be a deliverable. Real step results observed live run from
// several hundred to several thousand characters; the failures this guards
// against are "...", "OK", "done", and a bare NO_REPLY.
const minUsableStepResultRunes = 24

// workflowStepResultIsUsable reports whether a settled step actually produced
// something a downstream critic or integrator can work with. It is deliberately
// conservative: only silent replies, empty text, and content too short to be any
// deliverable are rejected, because a false negative costs one extra dispatch
// while a false positive poisons every downstream step.
func workflowStepResultIsUsable(result string) bool {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" || agent.IsSilentReply(trimmed) {
		return false
	}
	// Our own placeholders never count as work, however long they read.
	if trimmed == workflowStepPlaceholderCompleted || trimmed == workflowStepPlaceholderNoResult {
		return false
	}
	// Strip the decorative-only case ("...", "---", "**"): if nothing but
	// punctuation is left there is no content, however long the string was.
	stripped := strings.TrimSpace(strings.Trim(trimmed, "_ \t\n\r.,:;!?\"'`*~#>-()[]{}/\\|+="))
	if stripped == "" {
		return false
	}
	return len([]rune(stripped)) >= minUsableStepResultRunes
}

func workflowBackgroundContext(ctx context.Context) context.Context {
	return store.WithTenantID(context.Background(), store.TenantIDFromContext(ctx))
}

func finalizeWorkflow(ctx context.Context, deps *ConsumerDeps, workflowStore store.TeamWorkflowStore, workflowID uuid.UUID) {
	if existing, err := workflowStore.GetWorkflow(ctx, workflowID); err == nil && existing.FinalizedAt != nil {
		deliverWorkflowFinalOutput(ctx, deps, workflowStore, workflowID)
		return
	}
	workflow, token, err := workflowStore.ClaimWorkflowFinalization(ctx, workflowID, time.Now().Add(10*time.Minute))
	if err != nil {
		slog.Debug("workflow finalize claim skipped", "workflow_id", workflowID, "error", err)
		return
	}
	tasks, err := workflowStore.ListWorkflowTasks(ctx, workflowID)
	if err != nil {
		slog.Warn("workflow finalize: list tasks failed", "workflow_id", workflowID, "error", err)
		return
	}
	finalStatus := store.TeamWorkflowStatusCompleted
	switch workflow.Status {
	case store.TeamWorkflowStatusFailing:
		finalStatus = store.TeamWorkflowStatusFailed
	case store.TeamWorkflowStatusCancelling:
		finalStatus = store.TeamWorkflowStatusCancelled
	}
	// No LLM finalizer (Correction E). A completed workflow delivers the terminal
	// integration task's own settled result VERBATIM — that task is the single
	// user-deliverable output of the DAG, and synthesizing it through a model only
	// risked rewriting or losing it. Failed/cancelled workflows, and any completed
	// workflow whose terminal result is empty, fall back to the deterministic
	// summary, which never surfaces raw internal error text. The terminal task is
	// resolved from durable task state, never the canonical plan (Correction B).
	finalContent := workflowTerminalResult(tasks)
	if finalStatus != store.TeamWorkflowStatusCompleted || strings.TrimSpace(finalContent) == "" {
		finalContent = deterministicWorkflowSummary(workflow, tasks, workflowLocale(workflow))
	}
	commitCtx, cancelCommit := context.WithTimeout(store.WithTenantID(context.Background(), workflow.TenantID), 30*time.Second)
	defer cancelCommit()
	if err := workflowStore.CompleteWorkflowFinalization(commitCtx, workflow.ID, token, finalStatus, finalContent); err != nil {
		slog.Warn("workflow finalize commit failed", "workflow_id", workflow.ID, "error", err)
		return
	}
	deliverWorkflowFinalOutput(commitCtx, deps, workflowStore, workflow.ID)
}

func deliverWorkflowFinalOutput(ctx context.Context, deps *ConsumerDeps, workflowStore store.TeamWorkflowStore, workflowID uuid.UUID) {
	workflow, token, err := workflowStore.ClaimWorkflowDelivery(ctx, workflowID, time.Now().Add(2*time.Minute))
	if err != nil {
		slog.Debug("workflow delivery claim skipped", "workflow_id", workflowID, "error", err)
		return
	}
	if workflow.OriginChannel == "ws" {
		if err := persistWorkflowWSResult(ctx, deps.SessStore, workflow); err != nil {
			if _, failErr := workflowStore.FailWorkflowDeliveryAttempt(ctx, workflow.ID, token, "ws persist: "+err.Error()); failErr != nil {
				slog.Warn("workflow WS delivery retry release failed", "workflow_id", workflow.ID, "error", failErr)
			}
			slog.Warn("workflow WS delivery deferred", "workflow_id", workflow.ID, "error", err)
			return
		}
		if err := workflowStore.CompleteWorkflowDelivery(ctx, workflow.ID, token); err != nil {
			slog.Warn("workflow WS delivery acknowledgement failed", "workflow_id", workflow.ID, "error", err)
		}
		return
	}
	if deps.MsgBus == nil {
		if _, failErr := workflowStore.FailWorkflowDeliveryAttempt(ctx, workflow.ID, token, "outbound bus unavailable"); failErr != nil {
			slog.Warn("workflow delivery retry release failed", "workflow_id", workflow.ID, "error", failErr)
		}
		slog.Warn("workflow delivery deferred: outbound bus unavailable", "workflow_id", workflowID)
		return
	}
	metadata := make(map[string]string)
	_ = json.Unmarshal(workflow.OriginRouting, &metadata)
	for key, value := range buildAnnounceOutMeta(workflow.OriginLocalKey) {
		metadata[key] = value
	}
	metadata["workflow_delivery_id"] = workflow.ID.String()
	media := workflowDeliveryMedia(ctx, deps, workflowStore, workflow.ID)
	if !deps.MsgBus.TryPublishOutbound(bus.OutboundMessage{
		Channel:  workflow.OriginChannel,
		ChatID:   workflow.OriginChatID,
		Content:  workflow.ResultSummary,
		Media:    media,
		Metadata: metadata,
		TenantID: workflow.TenantID,
		AgentID:  workflow.OriginAgentID,
		DeliveryAck: func(deliveryErr error) {
			ackCtx, cancelAck := context.WithTimeout(store.WithTenantID(context.Background(), workflow.TenantID), 30*time.Second)
			defer cancelAck()
			if deliveryErr != nil {
				if _, failErr := workflowStore.FailWorkflowDeliveryAttempt(ackCtx, workflow.ID, token, "outbound send: "+deliveryErr.Error()); failErr != nil {
					slog.Warn("workflow delivery retry release failed", "workflow_id", workflow.ID, "error", failErr)
				}
				return
			}
			if completeErr := workflowStore.CompleteWorkflowDelivery(ackCtx, workflow.ID, token); completeErr != nil {
				slog.Warn("workflow delivery acknowledgement failed", "workflow_id", workflow.ID, "error", completeErr)
			}
		},
	}) {
		if _, failErr := workflowStore.FailWorkflowDeliveryAttempt(ctx, workflow.ID, token, "outbound queue full"); failErr != nil {
			slog.Warn("workflow delivery retry release failed", "workflow_id", workflow.ID, "error", failErr)
		}
		slog.Warn("workflow delivery deferred: outbound queue full", "workflow_id", workflow.ID)
	}
}

// workflowDeliveryMedia collects file attachments recorded on the workflow's
// terminal task and converts them to outbound media entries so channel senders
// (Zalo/Telegram) attach the actual files instead of dropping them.
func workflowDeliveryMedia(ctx context.Context, deps *ConsumerDeps, workflowStore store.TeamWorkflowStore, workflowID uuid.UUID) []bus.MediaAttachment {
	if deps == nil || deps.TeamStore == nil {
		return nil
	}
	tasks, err := workflowStore.ListWorkflowTasks(ctx, workflowID)
	if err != nil {
		return nil
	}
	var terminalID uuid.UUID
	for i := range tasks {
		if tasks[i].WorkflowKind == store.TeamWorkflowTaskKindWork && tasks[i].WorkflowTerminal {
			terminalID = tasks[i].ID
			break
		}
	}
	if terminalID == uuid.Nil {
		return nil
	}
	atts, err := deps.TeamStore.ListTaskAttachments(ctx, terminalID)
	if err != nil || len(atts) == 0 {
		return nil
	}
	media := make([]bus.MediaAttachment, 0, len(atts))
	for _, att := range atts {
		if att.Path == "" {
			continue
		}
		media = append(media, bus.MediaAttachment{URL: att.Path, ContentType: att.MimeType})
	}
	return media
}

type workflowSessionWriter interface {
	GetHistory(context.Context, string) []providers.Message
	AddMessage(context.Context, string, providers.Message)
	Save(context.Context, string) error
}

func persistWorkflowWSResult(ctx context.Context, sessions workflowSessionWriter, workflow *store.TeamWorkflowData) error {
	if sessions == nil || workflow.OriginSessionKey == "" {
		return fmt.Errorf("origin session store is unavailable")
	}
	found := false
	for _, message := range sessions.GetHistory(ctx, workflow.OriginSessionKey) {
		if message.Role == "assistant" && message.Content == workflow.ResultSummary {
			found = true
			break
		}
	}
	if !found {
		sessions.AddMessage(ctx, workflow.OriginSessionKey, providers.Message{Role: "assistant", Content: workflow.ResultSummary})
	}
	return sessions.Save(ctx, workflow.OriginSessionKey)
}

// workflowTerminalResult returns the settled result of the workflow's terminal
// integration task — the single output the requester is allowed to see. The
// terminal task is identified from durable task state (the one task flagged
// WorkflowTerminal of kind work), never from the canonical plan or any metadata
// the coordinator could have shaped (Correction B). It returns "" when there is
// no terminal task, its Result pointer is nil, or the result is whitespace-only;
// the caller treats all three as "fall back to the deterministic summary."
func workflowTerminalResult(tasks []store.TeamTaskData) string {
	for i := range tasks {
		task := &tasks[i]
		if task.WorkflowKind != store.TeamWorkflowTaskKindWork || !task.WorkflowTerminal {
			continue
		}
		if task.Result == nil {
			return ""
		}
		return *task.Result
	}
	return ""
}

// workflowStepFailedResultPrefix is the marker the stores prepend when a task is
// failed (see teams_tasks_lifecycle.go / team_workflows.go). Everything after it
// is an internal diagnostic string, never user-facing copy.
const workflowStepFailedResultPrefix = "FAILED: "

// userFacingStepFailure converts a failed step's stored result into a line the
// requester can act on, and reports whether the result was a failure at all.
// Non-failure results are returned untouched by the caller.
func userFacingStepFailure(result, subject string, isVI bool) (string, bool) {
	if !strings.HasPrefix(strings.TrimSpace(result), workflowStepFailedResultPrefix) {
		return "", false
	}
	label := strings.TrimSpace(subject)
	if isVI {
		if label != "" {
			return fmt.Sprintf("- %s: không hoàn thành do lỗi tạm thời của hệ thống.", label), true
		}
		return "- Một bước không hoàn thành do lỗi tạm thời của hệ thống.", true
	}
	if label != "" {
		return fmt.Sprintf("- %s: did not complete due to a temporary system error.", label), true
	}
	return "- A step did not complete due to a temporary system error.", true
}

func deterministicWorkflowSummary(workflow *store.TeamWorkflowData, tasks []store.TeamTaskData, locale string) string {
	var b strings.Builder
	isVI := strings.HasPrefix(strings.ToLower(locale), "vi")
	switch workflow.Status {
	case store.TeamWorkflowStatusFailing:
		if isVI {
			b.WriteString("Yêu cầu đã được xử lý một phần. Một số bước không hoàn thành:\n")
		} else {
			b.WriteString("The request was partially processed. Some steps did not complete:\n")
		}
	case store.TeamWorkflowStatusCancelling:
		reason := strings.TrimSpace(workflow.CancelReason)
		if isVI {
			if reason != "" {
				fmt.Fprintf(&b, "Yêu cầu đã được huỷ: %s\n", reason)
			} else {
				b.WriteString("Yêu cầu đã được huỷ.\n")
			}
		} else {
			if reason != "" {
				fmt.Fprintf(&b, "The request was cancelled: %s\n", reason)
			} else {
				b.WriteString("The request was cancelled.\n")
			}
		}
	}
	for _, task := range tasks {
		if task.WorkflowKind != store.TeamWorkflowTaskKindWork || task.Result == nil || strings.TrimSpace(*task.Result) == "" {
			continue
		}
		// A failed step's stored result is "FAILED: <raw internal error>" — the
		// operator's diagnostic string, complete with provider name, internal
		// model alias and agent-loop internals ("iter 1 think: llm call: ...").
		// It must not reach the requester: they need to know the step did not
		// finish, not how our gateway is wired. The raw text stays in the task
		// row and in workflow.failure_summary for diagnosis.
		if body, failed := userFacingStepFailure(*task.Result, task.Subject, isVI); failed {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(body)
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(*task.Result)
	}
	if b.Len() == 0 {
		if strings.HasPrefix(strings.ToLower(locale), "vi") {
			return "Không thể hoàn tất yêu cầu ở lượt này."
		}
		return "The request could not be completed in this run."
	}
	return b.String()
}

// vietnameseOnlyRunes are letters that exist in Vietnamese and in none of the
// other locales this gateway serves (en/zh/ko/ru). Tone-marked vowels are
// deliberately excluded: they overlap with other Latin scripts, and these seven
// are enough to identify any sentence of real Vietnamese prose.
const vietnameseOnlyRunes = "ăâđêôơư" + "ĂÂĐÊÔƠƯ"

// looksVietnamese reports whether text is Vietnamese prose. It is a fallback for
// when no explicit locale is available, so it errs toward "no": a single stray
// diacritic in an otherwise English string (a proper noun, a pasted name) must
// not flip the whole summary's language.
func looksVietnamese(text string) bool {
	if strings.ContainsAny(text, vietnameseOnlyRunes) {
		return true
	}
	// Some Vietnamese sentences carry only tone marks (e.g. "quyết định đã có"
	// minus its đ). Fall back to common function words, which are too short to
	// match by themselves but are reliable in combination with word boundaries.
	lower := strings.ToLower(text)
	hits := 0
	for _, w := range []string{" của ", " và ", " các ", " những ", " không ", " được ", " cho ", " với ", " là ", " đã "} {
		if strings.Contains(lower, w) {
			hits++
			if hits >= 2 {
				return true
			}
		}
	}
	return false
}

// workflowLocale resolves the language for this workflow's deterministic
// summary.
//
// The explicit routing key stays authoritative, but it cannot be the only
// source: metadata["locale"] is populated from the inbound message metadata, and
// no channel actually writes it (bitrix24 even documents that it does not). So
// every workflow ever created has origin_routing without a locale, and returning
// a bare "en" meant a Vietnamese requester whose workflow partially failed got
// an English notice with a Vietnamese step title spliced into it. There is no
// LLM finalizer to paper over this any more (Correction E): the deterministic
// summary is the ONLY delivery for failed/cancelled workflows and for a completed
// workflow whose terminal result is empty, so locale detection from the plan
// matters on exactly those paths.
//
// The canonical plan is the signal that is actually there: the planner writes
// goal, titles and instructions in the requester's language, and a workflow
// cannot exist without one. Detect from that rather than from a field with no
// writer.
func workflowLocale(workflow *store.TeamWorkflowData) string {
	var metadata map[string]string
	if json.Unmarshal(workflow.OriginRouting, &metadata) == nil && metadata["locale"] != "" {
		return metadata["locale"]
	}
	if looksVietnamese(workflowPlanText(workflow)) {
		return "vi"
	}
	return "en"
}

// workflowPlanText returns the requester-language prose out of the canonical
// plan: the goal plus each step's title. Instructions are skipped — they are far
// longer and carry enough English technical vocabulary that they add noise
// without adding signal.
func workflowPlanText(workflow *store.TeamWorkflowData) string {
	if workflow == nil || len(workflow.CanonicalPlan) == 0 {
		return ""
	}
	var plan struct {
		Goal  string `json:"goal"`
		Steps []struct {
			Title string `json:"title"`
		} `json:"steps"`
	}
	if json.Unmarshal(workflow.CanonicalPlan, &plan) != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(plan.Goal)
	for _, step := range plan.Steps {
		b.WriteByte(' ')
		b.WriteString(step.Title)
	}
	return b.String()
}
