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
// Exactly one of three shapes is produced:
//   - a native single-owner team routing directive with orchestration enabled,
//   - a coordinator directive for an executable coordinated (multi_role) route,
//     with orchestration enabled and no single-owner constraint, or
//   - a fail-closed run (Directive=nil, DisableTeamWork=true, canonical
//     orchestration tools blocked) for every other outcome. A non-executable
//     coordinated team route fails closed with NonExecutable=true and a stable
//     ConfigErrorCode so the caller returns a user-facing configuration error
//     instead of silently running the work as self.
//
// Professional (non-orchestration) tools remain available in the fail-closed
// shape; only team_tasks/delegate/spawn are blocked.
type TeamWorkGateDecision struct {
	Directive       *TeamWorkDirective
	DisableTeamWork bool
	BlockedTools    []string
	NonExecutable   bool
	ConfigErrorCode string
}

// teamWorkGateSelfFallback is the single fail-closed decision for every result
// that is not a valid native single-owner team route. It disables team work for
// the run and blocks canonical orchestration tools; all professional tools remain
// available.
func teamWorkGateSelfFallback() TeamWorkGateDecision {
	return TeamWorkGateDecision{
		DisableTeamWork: true,
		BlockedTools:    teamWorkOrchestrationTools(),
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

	failClosedIfOrchestrating := func() (TeamWorkGateDecision, uuid.UUID) {
		if decision.Directive != nil {
			return teamWorkGateSelfFallback(), uuid.Nil
		}
		return decision, uuid.Nil
	}

	if auditStore == nil {
		slog.Warn("team_work_classify: no audit store wired; failing closed to self if orchestrating",
			"ingress", meta.Ingress, "session", meta.SessionKey, "run", meta.RunID)
		return failClosedIfOrchestrating()
	}
	audit := teamworkclassify.BuildClassificationAudit(meta, result, input)
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
// Exactly one of three shapes is produced:
//   - a native single-owner directive for an executable single_owner team route,
//   - a coordinator directive (Mode=coordinator) for an executable coordinated
//     (multi_role) team route, carrying the canonical lead as owner with
//     orchestration enabled and no single-owner constraint, or
//   - a fail-closed run for every other outcome. A non-executable coordinated
//     team route fails closed with NonExecutable=true and a stable
//     ConfigErrorCode (from DegradedReasonCode) so the caller returns a
//     user-facing configuration error instead of silently running the work as
//     self; any other non-team or degraded result fails closed to a plain self
//     run.
func BuildTeamWorkGateDecision(result teamworkclassify.Result, input teamworkclassify.Input, originalMessage string) TeamWorkGateDecision {
	if result.Decision != teamworkclassify.DecisionTeam {
		return teamWorkGateSelfFallback()
	}
	switch result.WorkflowMode {
	case teamworkclassify.WorkflowModeSingleOwner:
		if result.BestTeamOwnerID == uuid.Nil {
			return teamWorkGateSelfFallback()
		}
		return TeamWorkGateDecision{Directive: &TeamWorkDirective{
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
			EnforcementTimeout:         input.Timeout,
		}}
	case teamworkclassify.WorkflowModeMultiRole:
		// A coordinated request that cannot execute fails closed with a stable
		// configuration error code rather than degrading to a silent self run.
		if result.NonExecutable {
			return TeamWorkGateDecision{
				DisableTeamWork: true,
				BlockedTools:    teamWorkOrchestrationTools(),
				NonExecutable:   true,
				ConfigErrorCode: result.DegradedReasonCode,
			}
		}
		if result.BestTeamOwnerID == uuid.Nil {
			return teamWorkGateSelfFallback()
		}
		return TeamWorkGateDecision{Directive: &TeamWorkDirective{
			Mode:                       TeamWorkDirectiveModeCoordinator,
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
			EnforcementTimeout:         input.Timeout,
			ReviewRequired:             result.EffectiveReviewRequired,
		}}
	default:
		return teamWorkGateSelfFallback()
	}
}
