package agent

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
)

// TeamWorkGateDecision is the run-level outcome of applying a classifier Result
// to a single turn. It is produced by BuildTeamWorkGateDecision so the WebSocket
// gate and the inbound gate share one fail-safe policy and cannot drift.
//
// Exactly one of two shapes is produced:
//   - a team routing directive with orchestration enabled (Directive!=nil,
//     DisableTeamWork=false), only for a fully validated team decision whose
//     multi-role plan (if any) was frozen, or
//   - a fail-closed self run (Directive=nil, DisableTeamWork=true, canonical
//     orchestration tools blocked) for every other outcome: an accepted self
//     assessment, a degraded fail-safe, or a team decision whose validated plan
//     could not be frozen.
//
// Professional (non-orchestration) tools remain available in both shapes; only
// team_tasks/delegate/spawn are blocked in the fail-closed shape.
type TeamWorkGateDecision struct {
	Directive       *TeamWorkDirective
	DisableTeamWork bool
	BlockedTools    []string
	// PlanFreezeError is set only when a team decision's validated multi-role
	// plan could not be frozen. The decision has already failed closed; the
	// caller should log this with its own session/agent context.
	PlanFreezeError error
}

// teamWorkGateSelfFallback is the single fail-closed decision shared by accepted
// self, degraded self, and a team decision whose validated plan cannot be frozen.
// It disables team work for the run and blocks the canonical orchestration tools;
// all professional tools remain available.
func teamWorkGateSelfFallback(planFreezeErr error) TeamWorkGateDecision {
	return TeamWorkGateDecision{
		DisableTeamWork: true,
		BlockedTools:    teamWorkOrchestrationTools(),
		PlanFreezeError: planFreezeErr,
	}
}

// BuildAuditedTeamWorkGateDecision builds the gate decision, writes the durable
// classification audit BEFORE the run is scheduled, and returns the audit ID so
// a workflow created during the run can link back to the decision that
// authorized it (via tools.WithTeamWorkClassificationAuditID). It is the single
// audit authority for both gate paths (WS + inbound) so they cannot drift.
//
// Every classification attempt is persisted tenant-scoped — self, degraded and
// orchestration decisions alike — so selection and degradation rates can be
// measured. The write is fail-safe: if it fails (or no audit store is wired), an
// orchestration decision is forced closed to a self run so no orchestration is
// ever scheduled without a durable audit record. A self/degraded decision was
// already self, so a write failure there costs only the audit row, never a
// professional answer.
//
// meta carries the caller-known request metadata (ingress, session, agent, run,
// original message, classifier provider/model). The frozen canonical plan hash
// is filled from the decision here so the audit records the exact plan.
func BuildAuditedTeamWorkGateDecision(ctx context.Context, auditStore store.TeamWorkClassificationAuditStore, result teamworkclassify.Result, input teamworkclassify.Input, meta teamworkclassify.ClassificationAuditInput) (TeamWorkGateDecision, uuid.UUID) {
	decision := BuildTeamWorkGateDecision(result, input, meta.OriginalMessage)
	if decision.Directive != nil && decision.Directive.PlanConstraint != nil {
		meta.PlanHash = decision.Directive.PlanConstraint.PlanHash
	}

	failClosedIfOrchestrating := func() (TeamWorkGateDecision, uuid.UUID) {
		if decision.Directive != nil {
			return teamWorkGateSelfFallback(nil), uuid.Nil
		}
		return decision, uuid.Nil
	}

	if auditStore == nil {
		slog.Warn("team_work_classify: no audit store wired; failing closed to self if orchestrating",
			"ingress", meta.Ingress, "session", meta.SessionKey, "run", meta.RunID)
		return failClosedIfOrchestrating()
	}
	audit := teamworkclassify.BuildClassificationAudit(meta, result)
	if err := auditStore.RecordTeamWorkClassificationAudit(ctx, audit); err != nil {
		slog.Warn("team_work_classify: audit write failed; failing closed to self if orchestrating",
			"ingress", meta.Ingress, "session", meta.SessionKey, "run", meta.RunID, "error", err)
		return failClosedIfOrchestrating()
	}
	// A nil error is NOT sufficient proof the audit is durable: the store
	// contract populates audit.ID on success, so a nil error with an unset ID
	// means the row is unlinkable. Never let orchestration proceed without a
	// linkable audit — fail closed just as on a write error.
	if audit.ID == uuid.Nil {
		slog.Warn("team_work_classify: audit write returned success without an ID; failing closed to self if orchestrating",
			"ingress", meta.Ingress, "session", meta.SessionKey, "run", meta.RunID)
		return failClosedIfOrchestrating()
	}
	return decision, audit.ID
}

// BuildTeamWorkGateDecision maps a classifier Result to the run-level gate
// decision. It is the single fail-safe authority for both gate paths.
//
// Any non-team decision — an accepted self assessment or a degraded/failed
// classification — fails closed: team work disabled, orchestration tools blocked,
// no directive. Only an explicit team decision receives a directive, and a
// multi-role team decision additionally requires its validated plan to freeze;
// a freeze failure falls back to the same fail-closed self run rather than
// leaving orchestration open with no plan constraint.
func BuildTeamWorkGateDecision(result teamworkclassify.Result, input teamworkclassify.Input, originalMessage string) TeamWorkGateDecision {
	if result.Decision != teamworkclassify.DecisionTeam {
		return teamWorkGateSelfFallback(nil)
	}
	directive := &TeamWorkDirective{
		Mode:                       string(result.Mode),
		Source:                     "llm",
		Reason:                     result.Reason,
		OriginalMessage:            originalMessage,
		StandaloneRequest:          result.StandaloneRequest,
		RequiredTool:               result.RequiredTool,
		WorkflowHint:               result.WorkflowHint,
		TaskType:                   result.TaskType,
		BestTeamOwner:              result.BestTeamOwner,
		BestTeamOwnerID:            result.BestTeamOwnerID,
		BestTeamOwnerRole:          result.BestTeamOwnerRole,
		OwnerSelectionReason:       result.OwnerSelectionReason,
		SpecialistMatchFound:       result.SpecialistMatchFound,
		LeadSelectedAsFallback:     result.LeadSelectedAsFallback,
		RoutingPriorityUsed:        result.RoutingPriorityUsed,
		ValidatorReason:            result.ValidatorReason,
		TeamRole:                   input.TeamRole,
		CanAssignTeamTasks:         input.CanAssignTeamTasks,
		MemberRequestsEnabled:      input.MemberRequestsEnabled,
		MemberRequestsAutoDispatch: input.MemberRequestsAutoDispatch,
		WorkflowMode:               string(result.WorkflowMode),
		CoordinatorAgentID:         input.CoordinatorAgentID,
		CoordinatorAgentKey:        input.CoordinatorAgentKey,
		// The tenant's Team Work LLM budget also governs enforcement calls in the
		// agent loop. Without this, raising the budget to rescue a slow agent model
		// fixes only the classifier stages and the run still fails closed on the
		// loop's built-in enforcement deadline — discarding a plan the classifier
		// just validated and froze.
		EnforcementTimeout: input.Timeout,
	}
	if result.WorkflowMode == teamworkclassify.WorkflowModeMultiRole {
		constraint, err := teamworkclassify.BuildPlanConstraint(result.Plan)
		if err != nil {
			return teamWorkGateSelfFallback(err)
		}
		directive.PlanConstraint = constraint
	}
	return TeamWorkGateDecision{Directive: directive}
}
