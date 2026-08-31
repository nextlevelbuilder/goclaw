package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// RecoveredTaskInfo contains minimal info for leader notification after batch recovery/stale.
type RecoveredTaskInfo struct {
	ID         uuid.UUID `db:"-"`
	TeamID     uuid.UUID `db:"-"`
	TenantID   uuid.UUID `db:"-"`
	TaskNumber int       `db:"-"`
	Subject    string    `db:"-"`
	Channel    string    `db:"-"` // task's origin channel for notification routing
	ChatID     string    `db:"-"` // task scope for notification routing
}

// ErrTaskNotFound is returned when a task does not exist.
var ErrTaskNotFound = errors.New("task not found")

// Team status constants.
const (
	TeamStatusActive   = "active"
	TeamStatusArchived = "archived"
)

// Team member role constants.
const (
	TeamRoleLead     = "lead"
	TeamRoleMember   = "member"
	TeamRoleReviewer = "reviewer"
)

// Team task status constants.
const (
	TeamTaskStatusPending     = "pending"
	TeamTaskStatusDispatching = "dispatching"
	TeamTaskStatusInProgress  = "in_progress"
	TeamTaskStatusCompleted   = "completed"
	TeamTaskStatusBlocked     = "blocked"
	TeamTaskStatusFailed      = "failed"
	TeamTaskStatusInReview    = "in_review"
	TeamTaskStatusCancelled   = "cancelled"
	TeamTaskStatusStale       = "stale"
)

const (
	TeamWorkflowStatusPendingExpansion = "pending_expansion"
	TeamWorkflowStatusRunning          = "running"
	TeamWorkflowStatusNeedsRevision    = "needs_revision"
	TeamWorkflowStatusFailing          = "failing"
	TeamWorkflowStatusCancelling       = "cancelling"
	TeamWorkflowStatusCompleted        = "completed"
	TeamWorkflowStatusFailed           = "failed"
	TeamWorkflowStatusCancelled        = "cancelled"
)

const (
	TeamWorkflowDeliveryPending   = "pending"
	TeamWorkflowDeliveryEnqueuing = "enqueuing"
	TeamWorkflowDeliveryDelivered = "delivered"
	TeamWorkflowDeliveryDead      = "dead"
)

// Workflow task coordinator-escalation lifecycle. A blocker on a workflow task
// creates a durable pending escalation to the coordinator; the ticker retries
// enqueue with bounded attempts and marks it dead when the budget is exhausted.
const (
	TeamTaskEscalationPending   = "pending"
	TeamTaskEscalationEnqueuing = "enqueuing"
	TeamTaskEscalationDelivered = "delivered"
	TeamTaskEscalationDead      = "dead"
)

const (
	TeamWorkflowTaskKindAudit = "audit"
	TeamWorkflowTaskKindWork  = "work"
)

// Recovery-loop budgets. Every automatic retry path (expansion, external
// delivery) is bounded so a transient failure can no longer retry forever the
// way the July-14 incident did: once the budget is exhausted the workflow moves
// to a terminal/dead state whose durable summary is user-visible. The ticker
// spaces retries with capped exponential backoff computed from the attempt
// count via WorkflowRetryBackoff.
const (
	MaxWorkflowExpansionAttempts = 5
	MaxWorkflowDeliveryAttempts  = 5
	// MaxWorkflowEscalationAttempts bounds the coordinator-recovery enqueue loop.
	// A blocked workflow task arms an escalation whose delivery to the canonical
	// coordinator is retried with the same capped backoff as expansion/delivery;
	// once exhausted the escalation moves to `dead` and the workflow fails with a
	// user-visible summary rather than silently dropping the recovery request the
	// way the July-14 incident did.
	MaxWorkflowEscalationAttempts = 5
	workflowRetryBaseBackoff      = 15 * time.Second
	workflowRetryMaxBackoff       = 10 * time.Minute
)

// WorkflowFailureSettleDelay is the grace window between a workflow entering
// `failing` and the finalizer being allowed to commit failing→failed. It gives
// still-running independent branches a bounded chance to settle before the
// terminal summary is written. Coordinator-confirmed FailWorkflow and the
// legacy SettleWorkflowTask failure path both arm the same window.
const WorkflowFailureSettleDelay = 2 * time.Minute

// WorkflowRetryBackoff returns the capped exponential backoff for the given
// (1-based) attempt number. Shared by the expansion and delivery retry paths so
// both loops space their retries identically.
func WorkflowRetryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := workflowRetryBaseBackoff
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= workflowRetryMaxBackoff {
			return workflowRetryMaxBackoff
		}
	}
	if backoff > workflowRetryMaxBackoff {
		backoff = workflowRetryMaxBackoff
	}
	return backoff
}

// Team task list filter constants (for ListTasks statusFilter parameter).
const (
	TeamTaskFilterActive    = "active"    // pending + in_progress + blocked
	TeamTaskFilterInReview  = "in_review" // only in_review tasks
	TeamTaskFilterCompleted = "completed" // only completed tasks
	TeamTaskFilterAll       = "all"       // all statuses (default when "" passed)
)

// TeamData represents an agent team.
type TeamData struct {
	BaseModel
	Name        string          `json:"name" db:"name"`
	LeadAgentID uuid.UUID       `json:"lead_agent_id" db:"lead_agent_id"`
	Description string          `json:"description,omitempty" db:"description"`
	Status      string          `json:"status" db:"status"`
	Settings    json.RawMessage `json:"settings,omitempty" db:"settings"`
	CreatedBy   string          `json:"created_by" db:"created_by"`

	// Joined fields (populated by queries that JOIN agents table)
	LeadAgentKey    string `json:"lead_agent_key,omitempty" db:"lead_agent_key"`
	LeadDisplayName string `json:"lead_display_name,omitempty" db:"lead_display_name"`

	// Enriched fields (populated by ListTeams)
	MemberCount int              `json:"member_count" db:"member_count"`
	Members     []TeamMemberData `json:"members,omitempty" db:"-"`
}

// TeamMemberData represents a team member.
type TeamMemberData struct {
	TeamID   uuid.UUID `json:"team_id" db:"team_id"`
	AgentID  uuid.UUID `json:"agent_id" db:"agent_id"`
	Role     string    `json:"role" db:"role"`
	JoinedAt time.Time `json:"joined_at" db:"joined_at"`

	// Joined fields (from agents table via JOIN)
	AgentKey         string `json:"agent_key,omitempty" db:"agent_key"`
	DisplayName      string `json:"display_name,omitempty" db:"display_name"`
	Frontmatter      string `json:"frontmatter,omitempty" db:"frontmatter"`
	AgentDescription string `json:"agent_description,omitempty" db:"agent_description"`
	Emoji            string `json:"emoji,omitempty" db:"emoji"`
}

// TeamTaskData represents a task in the team's shared task list.
type TeamTaskData struct {
	BaseModel
	TeamID       uuid.UUID      `json:"team_id" db:"team_id"`
	TenantID     uuid.UUID      `json:"tenant_id" db:"tenant_id"`
	Subject      string         `json:"subject" db:"subject"`
	Description  string         `json:"description,omitempty" db:"description"`
	Status       string         `json:"status" db:"status"`
	OwnerAgentID *uuid.UUID     `json:"owner_agent_id,omitempty" db:"owner_agent_id"`
	BlockedBy    []uuid.UUID    `json:"blocked_by,omitempty" db:"blocked_by"`
	Priority     int            `json:"priority" db:"priority"`
	Result       *string        `json:"result,omitempty" db:"result"`
	Metadata     map[string]any `json:"metadata,omitempty" db:"metadata"`
	UserID       string         `json:"user_id,omitempty" db:"user_id"`
	Channel      string         `json:"channel,omitempty" db:"channel"`

	// V2 fields
	TaskType         string     `json:"task_type" db:"task_type"`
	TaskNumber       int        `json:"task_number,omitempty" db:"task_number"`
	Identifier       string     `json:"identifier,omitempty" db:"identifier"`
	CreatedByAgentID *uuid.UUID `json:"created_by_agent_id,omitempty" db:"created_by_agent_id"`
	AssigneeUserID   string     `json:"assignee_user_id,omitempty" db:"assignee_user_id"`
	ParentID         *uuid.UUID `json:"parent_id,omitempty" db:"parent_id"`
	ChatID           string     `json:"chat_id,omitempty" db:"chat_id"`
	LockedAt         *time.Time `json:"locked_at,omitempty" db:"locked_at"`
	LockExpiresAt    *time.Time `json:"lock_expires_at,omitempty" db:"lock_expires_at"`
	ProgressPercent  int        `json:"progress_percent,omitempty" db:"progress_percent"`
	ProgressStep     string     `json:"progress_step,omitempty" db:"progress_step"`

	// Durable workflow linkage. Metadata is deliberately not authoritative for
	// workflow scheduling, approval, recovery, or finalization.
	WorkflowID         *uuid.UUID `json:"workflow_id,omitempty" db:"workflow_id"`
	WorkflowStepID     string     `json:"workflow_step_id,omitempty" db:"workflow_step_id"`
	WorkflowKind       string     `json:"workflow_kind,omitempty" db:"workflow_kind"`
	WorkflowTerminal   bool       `json:"workflow_terminal,omitempty" db:"workflow_terminal"`
	DispatchToken      *uuid.UUID `json:"-" db:"dispatch_token"`
	DispatchLeaseUntil *time.Time `json:"dispatch_lease_until,omitempty" db:"dispatch_lease_until"`

	// Enforcement state (migration 000098). PlanRevision scopes a workflow task
	// to the plan generation that created it; DispatchCount is the durable
	// dispatch-attempt counter promoted out of the metadata JSON blob. The
	// blocker/recovery and escalation-retry fields drive coordinator recovery
	// (a blocked terminal task must not silently fail the whole workflow).
	PlanRevision           int        `json:"plan_revision" db:"plan_revision"`
	DispatchCount          int        `json:"dispatch_count" db:"dispatch_count"`
	BlockerReason          string     `json:"blocker_reason,omitempty" db:"blocker_reason"`
	RecoveryCount          int        `json:"recovery_count" db:"recovery_count"`
	EscalationStatus       string     `json:"escalation_status,omitempty" db:"escalation_status"`
	EscalationAttemptCount int        `json:"escalation_attempt_count,omitempty" db:"escalation_attempt_count"`
	EscalationNextAt       *time.Time `json:"escalation_next_at,omitempty" db:"escalation_next_at"`
	EscalationLastError    string     `json:"escalation_last_error,omitempty" db:"escalation_last_error"`

	// Follow-up reminder fields
	FollowupAt      *time.Time `json:"followup_at,omitempty" db:"followup_at"`
	FollowupCount   int        `json:"followup_count,omitempty" db:"followup_count"`
	FollowupMax     int        `json:"followup_max,omitempty" db:"followup_max"`
	FollowupMessage string     `json:"followup_message,omitempty" db:"followup_message"`
	FollowupChannel string     `json:"followup_channel,omitempty" db:"followup_channel"`
	FollowupChatID  string     `json:"followup_chat_id,omitempty" db:"followup_chat_id"`

	// Denormalized counts for dashboard performance
	CommentCount    int `json:"comment_count" db:"comment_count"`
	AttachmentCount int `json:"attachment_count" db:"attachment_count"`

	// Joined fields
	OwnerAgentKey     string `json:"owner_agent_key,omitempty" db:"owner_agent_key"`
	CreatedByAgentKey string `json:"created_by_agent_key,omitempty" db:"created_by_agent_key"`
}

// SeedDispatchCountFromMetadata promotes a legacy metadata "dispatch_count"
// value into the durable DispatchCount column when the caller expressed the
// counter the old way (before migration 000098 moved it out of the JSON blob).
// The durable column is authoritative; if it is already set, metadata does not
// override it. Called from every workflow-task insert path so a seed supplied
// by an older caller (or a characterization test) lands in the column that the
// atomic dispatch claim increments.
func (d *TeamTaskData) SeedDispatchCountFromMetadata() {
	if d.DispatchCount != 0 || d.Metadata == nil {
		return
	}
	switch v := d.Metadata["dispatch_count"].(type) {
	case float64:
		d.DispatchCount = int(v)
	case int:
		d.DispatchCount = v
	case int64:
		d.DispatchCount = int(v)
	}
}

// PlanRevisionOrDefault normalizes a zero plan_revision to 1, so an insert from
// a caller that never set the field (every pre-000098 path) lands on the first
// revision instead of overriding the column default with 0.
func PlanRevisionOrDefault(revision int) int {
	if revision <= 0 {
		return 1
	}
	return revision
}

// MirrorDispatchCountToMetadata reflects the durable DispatchCount column back
// into Metadata["dispatch_count"] on read, so backward-compatible consumers
// that still inspect the metadata blob observe the authoritative durable value.
// Applies only to workflow work tasks, whose counter lives in the column.
func (d *TeamTaskData) MirrorDispatchCountToMetadata() {
	if d.WorkflowID == nil || d.WorkflowKind != TeamWorkflowTaskKindWork {
		return
	}
	if d.Metadata == nil {
		d.Metadata = make(map[string]any)
	}
	d.Metadata["dispatch_count"] = float64(d.DispatchCount)
}

// TeamWorkflowData is the canonical persisted multi-agent workflow. The plan
// and origin routing are stored here so expansion, recovery, and finalization
// never depend on the classifier context that created the workflow.
type TeamWorkflowData struct {
	BaseModel
	TeamID                uuid.UUID       `json:"team_id" db:"team_id"`
	TenantID              uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	Status                string          `json:"status" db:"status"`
	CanonicalPlan         json.RawMessage `json:"canonical_plan" db:"canonical_plan"`
	SchemaVersion         int             `json:"schema_version" db:"schema_version"`
	PlanHash              string          `json:"plan_hash" db:"plan_hash"`
	CoordinatorAgentID    uuid.UUID       `json:"coordinator_agent_id" db:"coordinator_agent_id"`
	CoordinatorAgentKey   string          `json:"coordinator_agent_key" db:"coordinator_agent_key"`
	OriginAgentID         uuid.UUID       `json:"origin_agent_id" db:"origin_agent_id"`
	OriginAgentKey        string          `json:"origin_agent_key" db:"origin_agent_key"`
	OriginRunID           string          `json:"origin_run_id" db:"origin_run_id"`
	OriginSessionKey      string          `json:"origin_session_key" db:"origin_session_key"`
	OriginChannel         string          `json:"origin_channel" db:"origin_channel"`
	OriginChatID          string          `json:"origin_chat_id" db:"origin_chat_id"`
	OriginPeerKind        string          `json:"origin_peer_kind" db:"origin_peer_kind"`
	OriginLocalKey        string          `json:"origin_local_key" db:"origin_local_key"`
	OriginUserID          string          `json:"origin_user_id" db:"origin_user_id"`
	OriginSenderID        string          `json:"origin_sender_id" db:"origin_sender_id"`
	OriginRole            string          `json:"origin_role" db:"origin_role"`
	OriginRouting         json.RawMessage `json:"origin_routing" db:"origin_routing"`
	AutoExpand            bool            `json:"auto_expand" db:"auto_expand"`
	AuditTaskID           *uuid.UUID      `json:"audit_task_id,omitempty" db:"audit_task_id"`
	TerminalTaskID        *uuid.UUID      `json:"terminal_task_id,omitempty" db:"terminal_task_id"`
	ExpansionToken        *uuid.UUID      `json:"expansion_token,omitempty" db:"expansion_token"`
	ExpansionLeaseUntil   *time.Time      `json:"expansion_lease_until,omitempty" db:"expansion_lease_until"`
	FinalizeToken         *uuid.UUID      `json:"finalize_token,omitempty" db:"finalize_token"`
	FinalizeLeaseUntil    *time.Time      `json:"finalize_lease_until,omitempty" db:"finalize_lease_until"`
	FinalizeClaimedAt     *time.Time      `json:"finalize_claimed_at,omitempty" db:"finalize_claimed_at"`
	FinalizedAt           *time.Time      `json:"finalized_at,omitempty" db:"finalized_at"`
	FailureSettleDeadline *time.Time      `json:"failure_settle_deadline,omitempty" db:"failure_settle_deadline"`
	FailureSummary        string          `json:"failure_summary,omitempty" db:"failure_summary"`
	ResultSummary         string          `json:"result_summary,omitempty" db:"result_summary"`
	DeliveryStatus        string          `json:"delivery_status" db:"delivery_status"`
	DeliveryToken         *uuid.UUID      `json:"-" db:"delivery_token"`
	DeliveryLeaseUntil    *time.Time      `json:"delivery_lease_until,omitempty" db:"delivery_lease_until"`
	DeliveredAt           *time.Time      `json:"delivered_at,omitempty" db:"delivered_at"`

	// Enforcement state (migration 000098). PlanRevision is bumped on every
	// committed replan; expansion/delivery counters bound the recovery loops so
	// a transient failure can no longer retry forever (the finalizer produces a
	// user-visible summary once a budget is exhausted). ClassificationAuditID
	// links the workflow to the append-only audit row that justified it.
	PlanRevision          int        `json:"plan_revision" db:"plan_revision"`
	ExpansionAttemptCount int        `json:"expansion_attempt_count,omitempty" db:"expansion_attempt_count"`
	NextExpansionAt       *time.Time `json:"next_expansion_at,omitempty" db:"next_expansion_at"`
	LastExpansionError    string     `json:"last_expansion_error,omitempty" db:"last_expansion_error"`
	DeliveryAttemptCount  int        `json:"delivery_attempt_count,omitempty" db:"delivery_attempt_count"`
	NextDeliveryAt        *time.Time `json:"next_delivery_at,omitempty" db:"next_delivery_at"`
	LastDeliveryError     string     `json:"last_delivery_error,omitempty" db:"last_delivery_error"`
	CancelReason          string     `json:"cancel_reason,omitempty" db:"cancel_reason"`
	CancelledAt           *time.Time `json:"cancelled_at,omitempty" db:"cancelled_at"`
	ClassificationAuditID *uuid.UUID `json:"classification_audit_id,omitempty" db:"classification_audit_id"`
}

type TeamWorkflowSettlement struct {
	WorkflowID      uuid.UUID
	WorkflowStatus  string
	ReadyToFinalize bool
}

type WorkflowApprovalActor struct {
	AgentID *uuid.UUID
	UserID  string
	Role    string
}

type TeamWorkflowDispatchScope struct {
	TenantID   uuid.UUID
	TeamID     uuid.UUID
	WorkflowID uuid.UUID
}

// EscalationClaim is the result of claiming a due coordinator-escalation for a
// blocked workflow work task. A blocker arms escalation_status='pending'; the
// recovery ticker claims it (pending|enqueuing → enqueuing) and enqueues a
// coordinator recovery run, spacing re-claims with the shared capped backoff so
// an unacknowledged escalation is retried rather than silently dropped the way
// the July-14 incident did.
//
//   - Claimed:   the escalation moved to 'enqueuing'; the caller MUST enqueue a
//     coordinator recovery run. Attempt carries the (1-based) attempt number.
//   - Exhausted: the retry budget ran out. The escalation is now 'dead' and the
//     workflow has moved to 'failing' (finalizer will emit a user-visible failure
//     summary). The caller MUST NOT enqueue a run.
//   - both false: the CAS lost a race — the task left 'blocked' or was not yet
//     due (the coordinator already resolved it). Skip it.
type EscalationClaim struct {
	Claimed    bool
	Exhausted  bool
	WorkflowID uuid.UUID
	TeamID     uuid.UUID
	TaskID     uuid.UUID
	Attempt    int
}

// WorkflowTaskAttempt is the immutable identity of a single workflow work-task
// run. It is minted by ClaimWorkflowTaskDispatch (the dispatch token is the
// generation marker) and threaded unchanged through accept → run → heartbeat →
// progress → blocker → complete/fail → post-turn settlement. Every mutation a
// run performs is CAS-guarded on this full tuple, so a stale attempt (whose
// token was superseded by recovery, replan, or requeue) can observe but never
// mutate the task, publish an event, or fail the workflow.
type WorkflowTaskAttempt struct {
	TenantID      uuid.UUID
	TeamID        uuid.UUID
	WorkflowID    uuid.UUID
	TaskID        uuid.UUID
	DispatchToken uuid.UUID
	PlanRevision  int
	WorkflowStep  string
}

// WorkflowRecoveryContext is the backend-derived identity of the blocked step a
// coordinator recovery run was scheduled to resolve. Like WorkflowTaskAttempt it
// is threaded RunRequest → loop → tool context and is NEVER populated from
// tool/model args: the recovery run runs AS the coordinator with a bounded set
// of resolution actions (retry_blocked / cancel_workflow / fail_workflow), and
// the recovery prompt deliberately hides task IDs and tokens from the model. The
// executors resolve the workflow and blocked task from this context so the
// coordinator never has to (and cannot) supply a UUID.
type WorkflowRecoveryContext struct {
	TenantID      uuid.UUID
	TeamID        uuid.UUID
	WorkflowID    uuid.UUID
	BlockedTaskID uuid.UUID
}

// WorkflowMutationOutcome is the typed result of an attempt-scoped mutation.
// Callers branch on it instead of parsing error strings, so the "did my CAS
// actually change a row" question has one authoritative answer per backend.
type WorkflowMutationOutcome int

const (
	// WorkflowMutationApplied means the CAS matched and the row was mutated.
	WorkflowMutationApplied WorkflowMutationOutcome = iota
	// WorkflowMutationAlreadyApplied means this exact attempt already performed
	// the transition (idempotent replay, e.g. a tool settled then post-turn
	// settlement ran). No duplicate event should be published.
	WorkflowMutationAlreadyApplied
	// WorkflowMutationStale means the attempt's token was superseded. The caller
	// must not mutate, publish, fail the workflow, or cancel the newer attempt.
	WorkflowMutationStale
	// WorkflowMutationOwnerBusy means an owner-exclusion precondition rejected
	// the claim (the owner already holds an active workflow work task). The
	// dispatch count must not be incremented on this outcome.
	WorkflowMutationOwnerBusy
)

func (o WorkflowMutationOutcome) String() string {
	switch o {
	case WorkflowMutationApplied:
		return "applied"
	case WorkflowMutationAlreadyApplied:
		return "already_applied"
	case WorkflowMutationStale:
		return "stale"
	case WorkflowMutationOwnerBusy:
		return "owner_busy"
	default:
		return "unknown"
	}
}

// ErrWorkflowAttemptStale is returned (wrapped) by attempt-scoped store methods
// whose CAS matched zero rows because the attempt token was superseded. It lets
// runtime callers distinguish a benign stale race from a real storage error.
var ErrWorkflowAttemptStale = errors.New("workflow task attempt is stale")

// ErrWorkflowOwnerBusy is returned when ClaimWorkflowTaskDispatch is refused
// because the owner already holds an active (dispatching|in_progress) workflow
// work task. The owner partial-unique index is the authority; this typed error
// lets the dispatcher back off without treating it as a hard failure.
var ErrWorkflowOwnerBusy = errors.New("workflow owner already has an active work task")

// WorkflowTaskTransition is the authoritative result of an attempt-scoped task
// mutation. Outcome tells the caller whether the CAS changed a row (Applied),
// was a benign idempotent replay of the same attempt (AlreadyApplied), or was
// refused because the attempt token was superseded (Stale). On Applied the
// remaining fields carry the post-transition task/workflow state so the caller
// can publish a lifecycle hint without re-reading, and decide whether the
// workflow is now ready to finalize. On Stale/AlreadyApplied the caller must
// not publish a duplicate event, fail the workflow, or cancel a newer attempt.
type WorkflowTaskTransition struct {
	Outcome         WorkflowMutationOutcome
	WorkflowID      uuid.UUID
	TaskStatus      string
	WorkflowStatus  string
	PlanRevision    int
	ReadyToFinalize bool
}

// Applied reports whether the transition actually mutated the row.
func (t WorkflowTaskTransition) Applied() bool { return t.Outcome == WorkflowMutationApplied }

// Stale reports whether the attempt token was superseded (no mutation, no event).
func (t WorkflowTaskTransition) Stale() bool { return t.Outcome == WorkflowMutationStale }

// TeamWorkClassificationAudit is an append-only record of one Team Work
// classification decision, written before the resulting run is scheduled. It
// captures the requested vs effective staffing mode, the verified work shape,
// per-stage statuses and any degradation, so over-selection and fail-safe rates
// can be measured without persisting raw prompts, secrets, or provider payloads.
type TeamWorkClassificationAudit struct {
	BaseModel
	TenantID             uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	Ingress              string          `json:"ingress" db:"ingress"`
	RunID                string          `json:"run_id,omitempty" db:"run_id"`
	SessionKey           string          `json:"session_key,omitempty" db:"session_key"`
	AgentID              *uuid.UUID      `json:"agent_id,omitempty" db:"agent_id"`
	OriginalHash         string          `json:"original_hash,omitempty" db:"original_hash"`
	ResolvedHash         string          `json:"resolved_hash,omitempty" db:"resolved_hash"`
	VerifiedShape        string          `json:"verified_shape,omitempty" db:"verified_shape"`
	Traits               json.RawMessage `json:"traits,omitempty" db:"traits"`
	RequestedMode        string          `json:"requested_mode,omitempty" db:"requested_mode"`
	EffectiveMode        string          `json:"effective_mode,omitempty" db:"effective_mode"`
	IndependentReview    bool            `json:"independent_review" db:"independent_review"`
	SelectedOwnerAgentID *uuid.UUID      `json:"selected_owner_agent_id,omitempty" db:"selected_owner_agent_id"`
	CoordinatorAgentID   *uuid.UUID      `json:"coordinator_agent_id,omitempty" db:"coordinator_agent_id"`
	PlanHash             string          `json:"plan_hash,omitempty" db:"plan_hash"`
	StageStatuses        json.RawMessage `json:"stage_statuses,omitempty" db:"stage_statuses"`
	DegradedStage        string          `json:"degraded_stage,omitempty" db:"degraded_stage"`
	DegradedReason       string          `json:"degraded_reason,omitempty" db:"degraded_reason"`
	ClassifierProvider   string          `json:"classifier_provider,omitempty" db:"classifier_provider"`
	ClassifierModel      string          `json:"classifier_model,omitempty" db:"classifier_model"`
	AuditSchemaVersion   int             `json:"schema_version" db:"schema_version"`
}

// Team Work classification ingress + mode constants (mirror the audit CHECKs).
const (
	TeamWorkIngressInbound = "inbound"
	TeamWorkIngressWS      = "ws"
	TeamWorkIngressSystem  = "system"

	TeamWorkModeSelf        = "self"
	TeamWorkModeSingleOwner = "single_owner"
	TeamWorkModeMultiRole   = "multi_role"
)

// TeamWorkClassificationAuditSchemaVersion is the current audit record schema
// version, stamped on every write so audit consumers can evolve the shape.
const TeamWorkClassificationAuditSchemaVersion = 1

// TeamWorkClassificationAuditStore persists append-only Team Work classification
// audit records. The write happens on the gate path before the resulting run is
// scheduled, so it is a small, tenant-scoped insert with no update/delete surface.
type TeamWorkClassificationAuditStore interface {
	// RecordTeamWorkClassificationAudit inserts one audit row. The tenant is
	// resolved from context; audit.ID and audit.CreatedAt are populated on the
	// passed record so the caller can link a resulting workflow to it via
	// TeamWorkflowData.ClassificationAuditID.
	RecordTeamWorkClassificationAudit(ctx context.Context, audit *TeamWorkClassificationAudit) error
}

// TeamTaskCommentData represents a comment on a team task.
type TeamTaskCommentData struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	TaskID      uuid.UUID  `json:"task_id" db:"task_id"`
	AgentID     *uuid.UUID `json:"agent_id,omitempty" db:"agent_id"`
	UserID      string     `json:"user_id,omitempty" db:"user_id"`
	Content     string     `json:"content" db:"content"`
	CommentType string     `json:"comment_type,omitempty" db:"comment_type"` // "note" (default) or "blocker"
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`

	// Joined
	AgentKey string `json:"agent_key,omitempty" db:"agent_key"`
}

// TeamTaskEventData represents an audit event on a team task.
type TeamTaskEventData struct {
	ID        uuid.UUID       `json:"id" db:"id"`
	TaskID    uuid.UUID       `json:"task_id" db:"task_id"`
	EventType string          `json:"event_type" db:"event_type"`
	ActorType string          `json:"actor_type" db:"actor_type"`
	ActorID   string          `json:"actor_id" db:"actor_id"`
	Data      json.RawMessage `json:"data,omitempty" db:"data"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
}

type TaskEventClaimResult string

const (
	TaskEventClaimed   TaskEventClaimResult = "claimed"
	TaskEventDuplicate TaskEventClaimResult = "duplicate"
	TaskEventConflict  TaskEventClaimResult = "conflict"
)

// TaskSibling represents a vault document attached to the same team task as
// another file sharing the same basename. Returned by BatchGetTaskSiblingsByBasenames
// for Phase 04 task-based auto-linking.
type TaskSibling struct {
	TaskID         uuid.UUID `db:"task_id"`
	DocID          uuid.UUID `db:"doc_id"`
	BaseName       string    `db:"base_name"`
	AttachmentTime time.Time `db:"created_at"`
}

// TeamTaskAttachmentData represents a file attached to a team task (path-based, no FK to workspace).
type TeamTaskAttachmentData struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	TaskID            uuid.UUID       `json:"task_id" db:"task_id"`
	TeamID            uuid.UUID       `json:"team_id" db:"team_id"`
	ChatID            string          `json:"chat_id,omitempty" db:"chat_id"`
	Path              string          `json:"path" db:"path"`
	BaseName          string          `json:"base_name,omitempty" db:"base_name"` // PG: GENERATED; SQLite: app-populated
	FileSize          int64           `json:"file_size" db:"file_size"`
	MimeType          string          `json:"mime_type,omitempty" db:"mime_type"`
	CreatedByAgentID  *uuid.UUID      `json:"created_by_agent_id,omitempty" db:"created_by_agent_id"`
	CreatedBySenderID string          `json:"created_by_sender_id,omitempty" db:"created_by_sender_id"`
	Metadata          json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	DownloadURL       string          `json:"download_url,omitempty" db:"-"` // signed URL, populated at delivery time
}

// TeamUserGrant represents a user's access grant to a team.
type TeamUserGrant struct {
	ID        uuid.UUID `json:"id" db:"id"`
	TeamID    uuid.UUID `json:"team_id" db:"team_id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Role      string    `json:"role" db:"role"`
	GrantedBy string    `json:"granted_by,omitempty" db:"granted_by"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ScopeEntry represents a unique channel+chatID scope across tasks.
type ScopeEntry struct {
	Channel string `json:"channel" db:"-"`
	ChatID  string `json:"chat_id" db:"-"`
}

// TeamCRUDStore manages core team and member operations.
type TeamCRUDStore interface {
	CreateTeam(ctx context.Context, team *TeamData) error
	GetTeam(ctx context.Context, teamID uuid.UUID) (*TeamData, error)
	GetTeamUnscoped(ctx context.Context, id uuid.UUID) (*TeamData, error)
	UpdateTeam(ctx context.Context, teamID uuid.UUID, updates map[string]any) error
	DeleteTeam(ctx context.Context, teamID uuid.UUID) error
	ListTeams(ctx context.Context) ([]TeamData, error)
	AddMember(ctx context.Context, teamID, agentID uuid.UUID, role string) error
	RemoveMember(ctx context.Context, teamID, agentID uuid.UUID) error
	ListMembers(ctx context.Context, teamID uuid.UUID) ([]TeamMemberData, error)
	ListIdleMembers(ctx context.Context, teamID uuid.UUID) ([]TeamMemberData, error)
	GetTeamForAgent(ctx context.Context, agentID uuid.UUID) (*TeamData, error)
	KnownUserIDs(ctx context.Context, teamID uuid.UUID, limit int) ([]string, error)
	ListTaskScopes(ctx context.Context, teamID uuid.UUID) ([]ScopeEntry, error)
}

// TaskStore manages task CRUD, lifecycle transitions, and progress.
type TaskStore interface {
	CreateTask(ctx context.Context, task *TeamTaskData) error
	UpdateTask(ctx context.Context, taskID uuid.UUID, updates map[string]any) error
	ListTasks(ctx context.Context, teamID uuid.UUID, orderBy string, statusFilter string, userID string, channel string, chatID string, limit int, offset int) ([]TeamTaskData, error)
	GetTask(ctx context.Context, taskID uuid.UUID) (*TeamTaskData, error)
	GetTasksByIDs(ctx context.Context, ids []uuid.UUID) ([]TeamTaskData, error)
	SearchTasks(ctx context.Context, teamID uuid.UUID, query string, limit int, userID string) ([]TeamTaskData, error)
	DeleteTask(ctx context.Context, taskID, teamID uuid.UUID) error
	DeleteTasks(ctx context.Context, taskIDs []uuid.UUID, teamID uuid.UUID) ([]uuid.UUID, error)
	ClaimTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error
	AssignTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error
	CompleteTask(ctx context.Context, taskID, teamID uuid.UUID, result string) error
	CancelTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error
	FailTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error
	FailPendingTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error
	ReviewTask(ctx context.Context, taskID, teamID uuid.UUID) error
	ApproveTask(ctx context.Context, taskID, teamID uuid.UUID, comment string) error
	RejectTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error
	UpdateTaskProgress(ctx context.Context, taskID, teamID uuid.UUID, percent int, step string) error
	RenewTaskLock(ctx context.Context, taskID, teamID uuid.UUID) error
	ResetTaskStatus(ctx context.Context, taskID, teamID uuid.UUID) error
	ListActiveTasksByChatID(ctx context.Context, chatID string) ([]TeamTaskData, error)
}

// TaskCommentStore manages task comments, audit events, and attachments.
type TaskCommentStore interface {
	AddTaskComment(ctx context.Context, comment *TeamTaskCommentData) error
	ListTaskComments(ctx context.Context, taskID uuid.UUID) ([]TeamTaskCommentData, error)
	ListRecentTaskComments(ctx context.Context, taskID uuid.UUID, limit int) ([]TeamTaskCommentData, error)
	RecordTaskEvent(ctx context.Context, event *TeamTaskEventData) error
	ClaimTaskEvent(ctx context.Context, event *TeamTaskEventData) (TaskEventClaimResult, error)
	ListTaskEvents(ctx context.Context, taskID uuid.UUID) ([]TeamTaskEventData, error)
	ListTeamEvents(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]TeamTaskEventData, error)
	AttachFileToTask(ctx context.Context, att *TeamTaskAttachmentData) error
	GetAttachment(ctx context.Context, attachmentID uuid.UUID) (*TeamTaskAttachmentData, error)
	ListTaskAttachments(ctx context.Context, taskID uuid.UUID) ([]TeamTaskAttachmentData, error)
	DetachFileFromTask(ctx context.Context, taskID uuid.UUID, path string) error

	// BatchGetTaskSiblingsByBasenames returns, for each basename, the vault
	// documents attached to the SAME team task(s) as that basename. Excludes
	// the source basename's own docs. Capped per (source_basename × task_id)
	// at `limit` (red-team concern #11). Chunks input at 500.
	BatchGetTaskSiblingsByBasenames(
		ctx context.Context,
		tenantID uuid.UUID,
		basenames []string,
		limit int,
	) (map[string][]TaskSibling, error)
}

// TaskRecoveryStore manages stale task detection and recovery.
type TaskRecoveryStore interface {
	RecoverAllStaleTasks(ctx context.Context) ([]RecoveredTaskInfo, error)
	ForceRecoverAllTasks(ctx context.Context) ([]RecoveredTaskInfo, error)
	ListRecoverableTasks(ctx context.Context, teamID uuid.UUID) ([]TeamTaskData, error)
	MarkAllStaleTasks(ctx context.Context, olderThan time.Time) ([]RecoveredTaskInfo, error)
	MarkInReviewStaleTasks(ctx context.Context, olderThan time.Time) ([]RecoveredTaskInfo, error)
	FixOrphanedBlockedTasks(ctx context.Context) ([]RecoveredTaskInfo, error)
}

// TeamWorkflowStore owns durable workflow creation, expansion, dispatch, and
// finalization state. Implementations must preserve tenant isolation and use
// database transactions/CAS for every state transition.
type TeamWorkflowStore interface {
	CreateWorkflowWithTasks(ctx context.Context, workflow *TeamWorkflowData, tasks []TeamTaskData) error
	CreatePendingWorkflowRequest(ctx context.Context, workflow *TeamWorkflowData, auditTask *TeamTaskData) error
	GetWorkflow(ctx context.Context, workflowID uuid.UUID) (*TeamWorkflowData, error)
	FindWorkflowByCreationKey(ctx context.Context, teamID uuid.UUID, originRunID, planHash string) (*TeamWorkflowData, error)
	ListWorkflowTasks(ctx context.Context, workflowID uuid.UUID) ([]TeamTaskData, error)
	ClaimPendingWorkflowExpansion(ctx context.Context, workflowID, coordinatorID uuid.UUID, leaseUntil time.Time) (uuid.UUID, error)
	ExpandPendingWorkflow(ctx context.Context, workflowID, coordinatorID, expansionToken uuid.UUID, tasks []TeamTaskData) error
	ApprovePendingWorkflowRequest(ctx context.Context, workflowID, auditTaskID uuid.UUID, actor WorkflowApprovalActor, tasks []TeamTaskData) error
	ClaimWorkflowTaskDispatch(ctx context.Context, taskID, teamID uuid.UUID, leaseUntil time.Time) (uuid.UUID, error)
	AcceptWorkflowTaskDispatch(ctx context.Context, taskID, teamID, dispatchToken uuid.UUID, lockExpiresAt time.Time) error
	// Attempt-fenced workflow-task transitions. Every mutation is CAS-guarded on
	// the full WorkflowTaskAttempt tuple (tenant/team/workflow/task/dispatch
	// token/plan_revision) plus the expected current status, so a superseded
	// attempt can never mutate the task, publish an event, or fail the workflow.
	// A zero-row CAS returns typed AlreadyApplied (idempotent replay) vs Stale.
	AcceptWorkflowTaskAttempt(ctx context.Context, attempt WorkflowTaskAttempt, lockExpiresAt time.Time) (WorkflowTaskTransition, error)
	HeartbeatWorkflowTaskAttempt(ctx context.Context, attempt WorkflowTaskAttempt, lockExpiresAt time.Time) (WorkflowTaskTransition, error)
	UpdateWorkflowTaskProgress(ctx context.Context, attempt WorkflowTaskAttempt, percent int, step string) (WorkflowTaskTransition, error)
	CompleteWorkflowTaskAttempt(ctx context.Context, attempt WorkflowTaskAttempt, result string) (WorkflowTaskTransition, error)
	BlockWorkflowTaskAttempt(ctx context.Context, attempt WorkflowTaskAttempt, reason string) (WorkflowTaskTransition, error)
	FailWorkflowTaskAttempt(ctx context.Context, attempt WorkflowTaskAttempt, reason string, failureSettleDeadline time.Time) (WorkflowTaskTransition, error)
	// RequeueWorkflowTaskAttempt returns a running attempt in_progress→pending
	// because the run died for a TRANSIENT reason (provider timeout, 429/503/529,
	// connection reset) rather than a defect in the work.
	//
	// FailWorkflowTaskAttempt is terminal: it flips the whole workflow to `failing`
	// and there is no path back, so a single router timeout on the last step
	// discards every completed step before it. Requeueing keeps the workflow
	// `running` and lets the ordinary dispatcher try the step again. dispatch_count
	// is deliberately NOT reset, so maxTaskDispatches still bounds the retries and
	// a persistently broken provider still surfaces as a real failure.
	RequeueWorkflowTaskAttempt(ctx context.Context, attempt WorkflowTaskAttempt, reason string) (WorkflowTaskTransition, error)
	RequeueExpiredWorkflowDispatches(ctx context.Context, now time.Time) ([]TeamTaskData, error)
	RecoverWorkflowRuns(ctx context.Context, force bool, now time.Time) ([]TeamTaskData, error)
	SettleWorkflowTask(ctx context.Context, taskID, teamID uuid.UUID, result string, failed bool, failureSettleDeadline time.Time) (*TeamWorkflowSettlement, error)
	ClaimWorkflowFinalization(ctx context.Context, workflowID uuid.UUID, leaseUntil time.Time) (*TeamWorkflowData, uuid.UUID, error)
	CompleteWorkflowFinalization(ctx context.Context, workflowID, finalizeToken uuid.UUID, status, resultSummary string) error
	ClaimWorkflowDelivery(ctx context.Context, workflowID uuid.UUID, leaseUntil time.Time) (*TeamWorkflowData, uuid.UUID, error)
	CompleteWorkflowDelivery(ctx context.Context, workflowID, deliveryToken uuid.UUID) error
	// Coordinator-driven recovery (Phase 4 store contract). A blocker no longer
	// mechanically fails the workflow — the coordinator resolves it with exactly
	// one of these bounded, authorized transitions.
	//
	// RetryBlockedWorkflowTask moves a blocked task blocked→pending, bumps its
	// recovery_count, clears the blocker/escalation state, and carries the
	// coordinator's revised instruction into the next dispatch as a comment. The
	// task keeps its owner and plan_revision; the next ClaimWorkflowTaskDispatch
	// mints a fresh attempt token so the old (invalidated) attempt stays stale.
	RetryBlockedWorkflowTask(ctx context.Context, taskID, teamID uuid.UUID, instruction string) (WorkflowTaskTransition, error)
	// CancelWorkflow performs an authorized workflow-level cancellation: it moves
	// the workflow to cancelling, records the reason, and cancels every
	// non-terminal work task (completed results are preserved). The finalizer then
	// commits cancelling→cancelled with a durable summary.
	CancelWorkflow(ctx context.Context, workflowID, teamID uuid.UUID, reason string) (*TeamWorkflowData, error)
	// FailWorkflow performs an authorized coordinator-confirmed terminal failure:
	// it moves a non-terminal workflow (running|needs_revision) → failing, records
	// the user-facing reason as the failure summary, arms the settle deadline, and
	// cancels every non-terminal work task (completed results are preserved). The
	// finalizer then commits failing→failed with the durable summary. This is the
	// authorized fail path the recovery coordinator chooses when a blocker cannot be
	// resolved — distinct from the retired mechanical SettleWorkflowTask fail.
	FailWorkflow(ctx context.Context, workflowID, teamID uuid.UUID, reason string) (*TeamWorkflowData, error)
	// FailWorkflowExpansion consumes one bounded expansion attempt. transient=true
	// schedules a capped-backoff retry (next_expansion_at); once the attempt budget
	// is exhausted, or transient=false (deterministic invalidation), the workflow
	// moves to failing so the finalizer emits a user-visible failure summary.
	FailWorkflowExpansion(ctx context.Context, workflowID, coordinatorID, expansionToken uuid.UUID, reason string, transient bool) (*TeamWorkflowData, error)
	// FailWorkflowDeliveryAttempt consumes one bounded delivery attempt. It records
	// the error, schedules a capped-backoff retry (next_delivery_at) while attempts
	// remain, and marks delivery dead once the budget is exhausted so an operator
	// can see the last error and the summary stays readable via the API/UI.
	FailWorkflowDeliveryAttempt(ctx context.Context, workflowID, deliveryToken uuid.UUID, reason string) (*TeamWorkflowData, error)
	// ApplyWorkflowAction is the single authoritative entry point for the five
	// operator/coordinator recovery transitions (retry_blocked, cancel_workflow,
	// fail_workflow, retry_expansion, retry_delivery). The transition opens a
	// transaction, locks and re-reads the authoritative workflow (PG FOR
	// UPDATE; SQLite conditional predicate), enforces the ExpectedStatus /
	// ExpectedPlanRevision / ExpectedTaskStatus optimistic guards (returning a typed
	// Conflict on mismatch, never a false AlreadyApplied), mutates exactly once,
	// writes the actor-attributed comment only on Applied, reloads the workflow +
	// tasks, and commits. A replay that finds the workflow already in the exact
	// post-state returns AlreadyApplied with no duplicate comment.
	ApplyWorkflowAction(ctx context.Context, guard WorkflowActionGuard) (WorkflowActionResult, error)
	SearchWorkflows(ctx context.Context, teamID uuid.UUID, query string, limit int) ([]TeamWorkflowData, error)
	ListPendingAutoExpandWorkflows(ctx context.Context, now time.Time) ([]TeamWorkflowData, error)
	ListWorkflowDispatchScopes(ctx context.Context) ([]TeamWorkflowDispatchScope, error)
	ListWorkflowsReadyToFinalize(ctx context.Context, now time.Time) ([]TeamWorkflowDispatchScope, error)
	// ListEscalationDueTasks returns blocked workflow work tasks whose
	// coordinator-escalation is due (escalation_status IN ('pending','enqueuing')
	// AND escalation_next_at <= now). It is the cross-tenant sweep the recovery
	// ticker runs to find blockers that still need a coordinator recovery run
	// enqueued; the per-task ClaimTaskEscalation CAS then guards the enqueue.
	ListEscalationDueTasks(ctx context.Context, now time.Time) ([]TeamTaskData, error)
	// ClaimTaskEscalation atomically claims one due escalation for enqueue. It
	// bumps escalation_attempt_count, and while the budget remains moves the
	// escalation pending|enqueuing → enqueuing and schedules the next capped-
	// backoff re-claim (escalation_next_at), returning Claimed=true so the caller
	// enqueues a coordinator recovery run. Once MaxWorkflowEscalationAttempts is
	// reached it instead moves the escalation → dead and the workflow → failing
	// (returning Exhausted=true), so an unacknowledged blocker fails with a
	// user-visible summary rather than being silently dropped. A lost race (task
	// no longer blocked/due) returns neither flag set.
	ClaimTaskEscalation(ctx context.Context, taskID, teamID uuid.UUID, now time.Time) (EscalationClaim, error)
}

// TaskFollowupStore manages follow-up reminder scheduling.
type TaskFollowupStore interface {
	SetTaskFollowup(ctx context.Context, taskID, teamID uuid.UUID, followupAt time.Time, max int, message, channel, chatID string) error
	ClearTaskFollowup(ctx context.Context, taskID uuid.UUID) error
	ListAllFollowupDueTasks(ctx context.Context) ([]TeamTaskData, error)
	IncrementFollowupCount(ctx context.Context, taskID uuid.UUID, nextAt *time.Time) error
	ClearFollowupByScope(ctx context.Context, channel, chatID string) (int, error)
	SetFollowupForActiveTasks(ctx context.Context, teamID uuid.UUID, channel, chatID string, followupAt time.Time, max int, message string) (int, error)
	HasActiveMemberTasks(ctx context.Context, teamID uuid.UUID, excludeAgentID uuid.UUID) (bool, error)
}

// UserTeamIDLister reads the active teams a user may receive events for.
// Callers must pass a tenant-scoped context; implementations fail closed when no
// tenant scope is present.
type UserTeamIDLister interface {
	ListUserTeamIDs(ctx context.Context, userID string) ([]uuid.UUID, error)
}

// TeamAccessStore manages user-level team access grants.
type TeamAccessStore interface {
	GrantTeamAccess(ctx context.Context, teamID uuid.UUID, userID, role, grantedBy string) error
	RevokeTeamAccess(ctx context.Context, teamID uuid.UUID, userID string) error
	ListTeamGrants(ctx context.Context, teamID uuid.UUID) ([]TeamUserGrant, error)
	ListUserTeams(ctx context.Context, userID string) ([]TeamData, error)
	HasTeamAccess(ctx context.Context, teamID uuid.UUID, userID string) (bool, error)
}

// TeamStore composes all team sub-interfaces for backward compatibility.
// New code should depend on the specific sub-interface it needs.
type TeamStore interface {
	TeamCRUDStore
	TaskStore
	TaskCommentStore
	TaskRecoveryStore
	TaskFollowupStore
	TeamAccessStore
	TeamWorkClassificationAuditStore
}
