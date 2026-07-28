package store

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// This file defines the shared typed contract for operator/coordinator workflow
// recovery actions (Phase 8B). Both ingresses — the coordinator recovery tool
// (backend-injected WorkflowRecoveryContext) and the admin dashboard RPC
// (RoleAdmin) — build a WorkflowActionGuard and receive a WorkflowActionResult.
// The store transitions are the single authority: each runs a transaction/CAS,
// is tenant/team scoped, locks and re-checks the row, compares the expected
// status + plan revision, mutates exactly once, and only writes a comment/event
// on an Applied outcome. PG uses FOR UPDATE; SQLite uses a conditional UPDATE
// with the identical predicate and outcome.

// WorkflowAction is one of exactly seven authorized recovery transitions. The
// enum is closed: any other string is rejected before a transaction opens.
type WorkflowAction string

const (
	// WorkflowActionRetryBlocked re-issues a blocked current-revision work step
	// (blocked→pending, clears blocker/escalation/tokens/leases, bumps
	// recovery_count). If the workflow was needs_revision it returns to running
	// and paused old-plan tasks resume.
	WorkflowActionRetryBlocked WorkflowAction = "retry_blocked"
	// WorkflowActionRequestRevision pauses the current plan: running →
	// needs_revision, keeps the selected blocker + its durable escalation, and
	// returns current-revision dispatching|in_progress tasks to pending (tokens
	// cleared). The coordinator then either resumes (retry) or replans.
	WorkflowActionRequestRevision WorkflowAction = "request_revision"
	// WorkflowActionApplyReplan commits a backend-built, frozen replacement plan
	// (revision N→N+1) while the workflow is needs_revision. Never accepts a
	// client/model plan, hash, task list, tool list, or token.
	WorkflowActionApplyReplan WorkflowAction = "apply_replan"
	// WorkflowActionCancelWorkflow moves pending_expansion|running|needs_revision
	// → cancelling and cancels nonterminal work (completed evidence preserved).
	WorkflowActionCancelWorkflow WorkflowAction = "cancel_workflow"
	// WorkflowActionFailWorkflow moves running|needs_revision → failing with a
	// first-failure summary and settle deadline, cancels nonterminal work.
	WorkflowActionFailWorkflow WorkflowAction = "fail_workflow"
	// WorkflowActionRetryExpansion re-arms a stuck pending_expansion (clears an
	// expired claim, next_expansion_at=now) while preserving the bounded budget.
	WorkflowActionRetryExpansion WorkflowAction = "retry_expansion"
	// WorkflowActionRetryDelivery starts a fresh bounded manual delivery cycle
	// for a terminal workflow whose delivery went dead.
	WorkflowActionRetryDelivery WorkflowAction = "retry_delivery"
)

// AllWorkflowActions is the canonical, ordered set of the seven actions.
var AllWorkflowActions = []WorkflowAction{
	WorkflowActionRetryBlocked,
	WorkflowActionRequestRevision,
	WorkflowActionApplyReplan,
	WorkflowActionCancelWorkflow,
	WorkflowActionFailWorkflow,
	WorkflowActionRetryExpansion,
	WorkflowActionRetryDelivery,
}

// Valid reports whether a is one of the seven authorized actions.
func (a WorkflowAction) Valid() bool {
	switch a {
	case WorkflowActionRetryBlocked, WorkflowActionRequestRevision, WorkflowActionApplyReplan,
		WorkflowActionCancelWorkflow, WorkflowActionFailWorkflow, WorkflowActionRetryExpansion,
		WorkflowActionRetryDelivery:
		return true
	}
	return false
}

// StepScoped reports whether the action targets a specific blocked work step
// (and therefore requires a TaskID guard) rather than the whole workflow.
func (a WorkflowAction) StepScoped() bool {
	switch a {
	case WorkflowActionRetryBlocked, WorkflowActionRequestRevision, WorkflowActionApplyReplan:
		return true
	}
	return false
}

// WorkflowActionActorKind distinguishes the two authorized ingresses so the
// store records the comment/audit author correctly (AgentID for a coordinator
// tool run, UserID for an admin RPC). It is NEVER used to grant authority — the
// ingress performs its own authorization before building the guard.
type WorkflowActionActorKind string

const (
	WorkflowActorCoordinator WorkflowActionActorKind = "coordinator"
	WorkflowActorAdmin       WorkflowActionActorKind = "admin"
)

// Valid reports whether k is one of the two authorized actor kinds.
func (k WorkflowActionActorKind) Valid() bool {
	return k == WorkflowActorCoordinator || k == WorkflowActorAdmin
}

// MaxWorkflowActionReasonRunes bounds the operator-supplied reason/instruction
// so a single action cannot write an unbounded comment. Matches the coordinator
// recovery tool's existing text cap.
const MaxWorkflowActionReasonRunes = 10000

// ErrWorkflowActionInvalid is returned when the guard fails validation before a
// transaction opens (bad action, missing IDs, out-of-range reason, bad actor).
var ErrWorkflowActionInvalid = errors.New("invalid workflow action request")

// WorkflowActionGuard is the fully-validated, tenant-scoped request handed to a
// store transition. The tenant is always taken from the context, never from the
// guard, so a caller cannot act across tenants by forging a field. ExpectedStatus
// and ExpectedPlanRevision are optimistic-concurrency guards: the transition only
// lands if the authoritative row still matches, otherwise it returns a typed
// Conflict so the UI can reconcile against a fresh fetch.
type WorkflowActionGuard struct {
	Action     WorkflowAction
	TeamID     uuid.UUID
	WorkflowID uuid.UUID

	// ExpectedStatus must equal the workflow's current status.
	ExpectedStatus string
	// ExpectedPlanRevision must be positive and equal the workflow's current revision.
	ExpectedPlanRevision int

	// TaskID identifies the blocked work step for step-scoped actions. Required
	// (non-nil) iff Action.StepScoped().
	TaskID *uuid.UUID
	// ExpectedTaskStatus is required iff Action.StepScoped() and must equal the
	// task's current status.
	ExpectedTaskStatus string

	// Reason is the trimmed, bounded operator justification recorded as a comment
	// on Applied. Required and non-empty.
	Reason string

	// Actor records who is performing the action for the comment/audit author.
	// It does not grant authority.
	Actor WorkflowActionActor
}

// WorkflowActionActor carries the authenticated identity of the ingress. Exactly
// one of AgentID (coordinator) / UserID (admin) is meaningful per Kind.
type WorkflowActionActor struct {
	Kind    WorkflowActionActorKind
	AgentID *uuid.UUID
	UserID  string
}

// Validate checks the guard's static invariants before any transaction opens.
// Tenant scoping is enforced separately from context inside each transition.
func (g WorkflowActionGuard) Validate() error {
	if !g.Action.Valid() {
		return ErrWorkflowActionInvalid
	}
	if g.TeamID == uuid.Nil || g.WorkflowID == uuid.Nil ||
		strings.TrimSpace(g.ExpectedStatus) == "" || g.ExpectedPlanRevision <= 0 {
		return ErrWorkflowActionInvalid
	}
	if !g.Actor.Kind.Valid() {
		return ErrWorkflowActionInvalid
	}
	if g.Actor.Kind == WorkflowActorCoordinator && (g.Actor.AgentID == nil || *g.Actor.AgentID == uuid.Nil) {
		return ErrWorkflowActionInvalid
	}
	if g.Actor.Kind == WorkflowActorAdmin && strings.TrimSpace(g.Actor.UserID) == "" {
		return ErrWorkflowActionInvalid
	}
	if g.Action.StepScoped() {
		if g.TaskID == nil || *g.TaskID == uuid.Nil || strings.TrimSpace(g.ExpectedTaskStatus) == "" {
			return ErrWorkflowActionInvalid
		}
	} else if g.TaskID != nil || strings.TrimSpace(g.ExpectedTaskStatus) != "" {
		return ErrWorkflowActionInvalid
	}
	reason := strings.TrimSpace(g.Reason)
	if reason == "" {
		return ErrWorkflowActionInvalid
	}
	if len([]rune(reason)) > MaxWorkflowActionReasonRunes {
		return ErrWorkflowActionInvalid
	}
	return nil
}

// WorkflowActionOutcome is the typed result of an action transition. Unlike
// WorkflowMutationOutcome (attempt-scoped runtime mutations), this is the
// operator-facing outcome the UI reconciles against.
type WorkflowActionOutcome int

const (
	// WorkflowActionUnknown is the reserved zero value. A caller that accidentally
	// ignores an error must never interpret WorkflowActionResult{} as Applied.
	WorkflowActionUnknown WorkflowActionOutcome = iota
	// WorkflowActionApplied means the CAS matched and the transition mutated the
	// authoritative row(s). A comment/event was written.
	WorkflowActionApplied
	// WorkflowActionAlreadyApplied means the workflow was already in the
	// post-transition state for this exact request (idempotent replay). No
	// duplicate comment/event is written.
	WorkflowActionAlreadyApplied
	// WorkflowActionConflict means the expected status/revision/task-status guard
	// did not match the authoritative row (a concurrent action won, or the client
	// held a stale view). No mutation happened; the UI must refetch and retry.
	WorkflowActionConflict
)

func (o WorkflowActionOutcome) String() string {
	switch o {
	case WorkflowActionApplied:
		return "applied"
	case WorkflowActionAlreadyApplied:
		return "already_applied"
	case WorkflowActionConflict:
		return "conflict"
	default:
		return "unknown"
	}
}

// WorkflowActionResult is the authoritative post-state returned by every action
// transition. On Applied/AlreadyApplied, Workflow is the reloaded row and Tasks
// are the workflow's work tasks after the transition, so the caller can respond
// without re-reading. On Conflict, Workflow reflects the current (unchanged)
// authoritative row when it could be read, so the UI reconciles immediately.
type WorkflowActionResult struct {
	Outcome  WorkflowActionOutcome
	Action   WorkflowAction
	Workflow *TeamWorkflowData
	Tasks    []TeamTaskData
}

// Applied reports whether the transition mutated the row.
func (r WorkflowActionResult) Applied() bool { return r.Outcome == WorkflowActionApplied }

// Conflict reports whether the guard did not match (no mutation).
func (r WorkflowActionResult) Conflict() bool { return r.Outcome == WorkflowActionConflict }
