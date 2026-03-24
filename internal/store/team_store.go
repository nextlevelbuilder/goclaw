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
	ID         uuid.UUID
	TeamID     uuid.UUID
	TenantID   uuid.UUID
	TaskNumber int
	Subject    string
	Channel    string // task's origin channel for notification routing
	ChatID     string // task scope for notification routing
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
	TeamTaskStatusPending    = "pending"
	TeamTaskStatusInProgress = "in_progress"
	TeamTaskStatusCompleted  = "completed"
	TeamTaskStatusBlocked    = "blocked"
	TeamTaskStatusFailed     = "failed"
	TeamTaskStatusInReview   = "in_review"
	TeamTaskStatusCancelled  = "cancelled"
	TeamTaskStatusStale      = "stale"
)

// Team task list filter constants (for ListTasks statusFilter parameter).
const (
	TeamTaskFilterActive    = "active" // pending + in_progress + blocked
	TeamTaskFilterInReview  = "in_review" // only in_review tasks
	TeamTaskFilterCompleted = "completed" // only completed tasks
	TeamTaskFilterAll       = "all"       // all statuses (default when "" passed)
)


// TeamData represents an agent team.
type TeamData struct {
	BaseModel
	Name        string          `json:"name"`
	LeadAgentID uuid.UUID       `json:"lead_agent_id"`
	Description string          `json:"description,omitempty"`
	Status      string          `json:"status"`
	Settings    json.RawMessage `json:"settings,omitempty"`
	CreatedBy   string          `json:"created_by"`

	// Joined fields (populated by queries that JOIN agents table)
	LeadAgentKey    string `json:"lead_agent_key,omitempty"`
	LeadDisplayName string `json:"lead_display_name,omitempty"`

	// Enriched fields (populated by ListTeams)
	MemberCount int              `json:"member_count"`
	Members     []TeamMemberData `json:"members,omitempty"`
}

// TeamMemberData represents a team member.
type TeamMemberData struct {
	TeamID   uuid.UUID `json:"team_id"`
	AgentID  uuid.UUID `json:"agent_id"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`

	// Joined fields
	AgentKey    string `json:"agent_key,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Frontmatter string `json:"frontmatter,omitempty"`
	Emoji       string `json:"emoji,omitempty"`
}

// TeamTaskData represents a task in the team's shared task list.
type TeamTaskData struct {
	BaseModel
	TeamID       uuid.UUID      `json:"team_id"`
	Subject      string         `json:"subject"`
	Description  string         `json:"description,omitempty"`
	Status       string         `json:"status"`
	OwnerAgentID *uuid.UUID     `json:"owner_agent_id,omitempty"`
	BlockedBy    []uuid.UUID    `json:"blocked_by,omitempty"`
	Priority     int            `json:"priority"`
	Result       *string        `json:"result,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	UserID       string         `json:"user_id,omitempty"`
	Channel      string         `json:"channel,omitempty"`

	// V2 fields
	TaskType        string     `json:"task_type"`
	TaskNumber      int        `json:"task_number,omitempty"`
	Identifier      string     `json:"identifier,omitempty"`
	CreatedByAgentID *uuid.UUID `json:"created_by_agent_id,omitempty"`
	AssigneeUserID  string     `json:"assignee_user_id,omitempty"`
	ParentID        *uuid.UUID `json:"parent_id,omitempty"`
	ChatID          string     `json:"chat_id,omitempty"`
	LockedAt        *time.Time `json:"locked_at,omitempty"`
	LockExpiresAt   *time.Time `json:"lock_expires_at,omitempty"`
	ProgressPercent int        `json:"progress_percent,omitempty"`
	ProgressStep    string     `json:"progress_step,omitempty"`

	// Follow-up reminder fields
	FollowupAt      *time.Time `json:"followup_at,omitempty"`
	FollowupCount   int        `json:"followup_count,omitempty"`
	FollowupMax     int        `json:"followup_max,omitempty"`
	FollowupMessage string     `json:"followup_message,omitempty"`
	FollowupChannel string     `json:"followup_channel,omitempty"`
	FollowupChatID  string     `json:"followup_chat_id,omitempty"`

	// Denormalized counts for dashboard performance
	CommentCount    int `json:"comment_count"`
	AttachmentCount int `json:"attachment_count"`

	// Joined fields
	OwnerAgentKey     string `json:"owner_agent_key,omitempty"`
	CreatedByAgentKey string `json:"created_by_agent_key,omitempty"`
}

// TeamTaskCommentData represents a comment on a team task.
type TeamTaskCommentData struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      uuid.UUID  `json:"task_id"`
	AgentID     *uuid.UUID `json:"agent_id,omitempty"`
	UserID      string     `json:"user_id,omitempty"`
	Content     string     `json:"content"`
	CommentType string     `json:"comment_type,omitempty"` // "note" (default) or "blocker"
	CreatedAt   time.Time  `json:"created_at"`

	// Joined
	AgentKey string `json:"agent_key,omitempty"`
}

// TeamTaskEventData represents an audit event on a team task.
type TeamTaskEventData struct {
	ID        uuid.UUID       `json:"id"`
	TaskID    uuid.UUID       `json:"task_id"`
	EventType string          `json:"event_type"`
	ActorType string          `json:"actor_type"` // "agent" | "human"
	ActorID   string          `json:"actor_id"`
	Data      json.RawMessage `json:"data,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// TeamTaskAttachmentData represents a file attached to a team task (path-based, no FK to workspace).
type TeamTaskAttachmentData struct {
	ID                uuid.UUID       `json:"id"`
	TaskID            uuid.UUID       `json:"task_id"`
	TeamID            uuid.UUID       `json:"team_id"`
	ChatID            string          `json:"chat_id,omitempty"`
	Path              string          `json:"path"`
	FileSize          int64           `json:"file_size"`
	MimeType          string          `json:"mime_type,omitempty"`
	CreatedByAgentID  *uuid.UUID      `json:"created_by_agent_id,omitempty"`
	CreatedBySenderID string          `json:"created_by_sender_id,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	DownloadURL       string          `json:"download_url,omitempty"` // signed URL, populated at delivery time
}

// TeamUserGrant represents a user's access grant to a team.
type TeamUserGrant struct {
	ID        uuid.UUID `json:"id"`
	TeamID    uuid.UUID `json:"team_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	GrantedBy string    `json:"granted_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ScopeEntry represents a unique channel+chatID scope across tasks.
type ScopeEntry struct {
	Channel string `json:"channel"`
	ChatID  string `json:"chat_id"`
}

// TeamStore manages agent teams, tasks, and messages.
type TeamStore interface {
	// Team CRUD
	CreateTeam(ctx context.Context, team *TeamData) error
	GetTeam(ctx context.Context, teamID uuid.UUID) (*TeamData, error)
	UpdateTeam(ctx context.Context, teamID uuid.UUID, updates map[string]any) error
	DeleteTeam(ctx context.Context, teamID uuid.UUID) error
	ListTeams(ctx context.Context) ([]TeamData, error)

	// Members
	AddMember(ctx context.Context, teamID, agentID uuid.UUID, role string) error
	RemoveMember(ctx context.Context, teamID, agentID uuid.UUID) error
	ListMembers(ctx context.Context, teamID uuid.UUID) ([]TeamMemberData, error)
	// ListIdleMembers returns team members (non-lead) that have no in_progress tasks.
	ListIdleMembers(ctx context.Context, teamID uuid.UUID) ([]TeamMemberData, error)

	// GetTeamForAgent returns the team that the given agent belongs to.
	// Returns nil, nil if the agent is not in any team.
	GetTeamForAgent(ctx context.Context, agentID uuid.UUID) (*TeamData, error)

	// KnownUserIDs returns distinct user IDs from sessions of team member agents.
	// Used by team settings UI to populate user select boxes.
	KnownUserIDs(ctx context.Context, teamID uuid.UUID, limit int) ([]string, error)

	// Scopes (channel+chatID discovery across tasks and workspace)
	ListTaskScopes(ctx context.Context, teamID uuid.UUID) ([]ScopeEntry, error)

	// Tasks (shared task list)
	CreateTask(ctx context.Context, task *TeamTaskData) error
	UpdateTask(ctx context.Context, taskID uuid.UUID, updates map[string]any) error
	// ListTasks returns tasks for a team. orderBy: "priority" or "newest".
	// statusFilter: "" = non-completed (default), "completed", "all".
	// userID: if non-empty, filter to tasks created by this user.
	// channel+chatID: if either is non-empty, filter to that exact scope.
	// limit: max rows to return; 0 uses the default (30).
	ListTasks(ctx context.Context, teamID uuid.UUID, orderBy string, statusFilter string, userID string, channel string, chatID string, limit int, offset int) ([]TeamTaskData, error)
	// GetTask returns a single task by ID with joined agent info.
	GetTask(ctx context.Context, taskID uuid.UUID) (*TeamTaskData, error)
	// GetTasksByIDs returns multiple tasks by IDs in a single query.
	GetTasksByIDs(ctx context.Context, ids []uuid.UUID) ([]TeamTaskData, error)
	// SearchTasks performs FTS search over task subject+description.
	// userID: if non-empty, filter to tasks created by this user.
	SearchTasks(ctx context.Context, teamID uuid.UUID, query string, limit int, userID string) ([]TeamTaskData, error)
	// DeleteTask permanently removes a terminal-status task (completed/failed/cancelled).
	// Returns ErrTaskNotFound if the task does not exist or is not in a terminal status.
	DeleteTask(ctx context.Context, taskID, teamID uuid.UUID) error
	// DeleteTasks permanently removes multiple terminal-status tasks in a single query.
	// Returns the list of IDs that were actually deleted.
	DeleteTasks(ctx context.Context, taskIDs []uuid.UUID, teamID uuid.UUID) ([]uuid.UUID, error)

	// ClaimTask atomically transitions a task from pending to in_progress.
	// Only one agent can claim a given task (row-level lock, race-safe).
	// teamID is validated in the WHERE clause to prevent cross-team task claiming.
	ClaimTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error

	// AssignTask admin-assigns a pending task to a specific agent.
	// teamID is validated in the WHERE clause to prevent cross-team assignment.
	AssignTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error

	// CompleteTask marks a task as completed and unblocks dependent tasks.
	// teamID is validated in the WHERE clause to prevent cross-team task completion.
	CompleteTask(ctx context.Context, taskID, teamID uuid.UUID, result string) error

	// CancelTask marks a non-completed task as cancelled (status=completed, result="CANCELLED: ..."),
	// unblocks dependent tasks, and transitions blocked→pending when all blockers are resolved.
	// Returns error if the task is already completed or not found.
	CancelTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error

	// FailTask marks an in_progress task as failed and stores the error message.
	// Unblocks dependent tasks so they aren't stuck.
	FailTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error
	// FailPendingTask marks a pending or blocked task as failed (post-turn validation).
	// Unlike FailTask, accepts pending/blocked source statuses.
	FailPendingTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error

	// Review workflow
	ReviewTask(ctx context.Context, taskID, teamID uuid.UUID) error
	ApproveTask(ctx context.Context, taskID, teamID uuid.UUID, comment string) error
	RejectTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error

	// Task comments
	AddTaskComment(ctx context.Context, comment *TeamTaskCommentData) error
	ListTaskComments(ctx context.Context, taskID uuid.UUID) ([]TeamTaskCommentData, error)
	ListRecentTaskComments(ctx context.Context, taskID uuid.UUID, limit int) ([]TeamTaskCommentData, error)

	// Audit events
	RecordTaskEvent(ctx context.Context, event *TeamTaskEventData) error
	ListTaskEvents(ctx context.Context, taskID uuid.UUID) ([]TeamTaskEventData, error)
	// ListTeamEvents returns recent events across all tasks in a team.
	ListTeamEvents(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]TeamTaskEventData, error)

	// Attachments (path-based, no FK to workspace files)
	AttachFileToTask(ctx context.Context, att *TeamTaskAttachmentData) error
	GetAttachment(ctx context.Context, attachmentID uuid.UUID) (*TeamTaskAttachmentData, error)
	ListTaskAttachments(ctx context.Context, taskID uuid.UUID) ([]TeamTaskAttachmentData, error)
	DetachFileFromTask(ctx context.Context, taskID uuid.UUID, path string) error

	// Follow-up reminders
	SetTaskFollowup(ctx context.Context, taskID, teamID uuid.UUID, followupAt time.Time, max int, message, channel, chatID string) error
	ClearTaskFollowup(ctx context.Context, taskID uuid.UUID) error
	// ListAllFollowupDueTasks returns due followup tasks across all v2 active teams (batch).
	ListAllFollowupDueTasks(ctx context.Context) ([]TeamTaskData, error)
	IncrementFollowupCount(ctx context.Context, taskID uuid.UUID, nextAt *time.Time) error

	// Auto follow-up guardrails (system-level, no LLM dependency)
	// ClearFollowupByScope clears followup on all in_progress tasks for a given channel+chatID scope.
	ClearFollowupByScope(ctx context.Context, channel, chatID string) (int, error)
	// SetFollowupForActiveTasks sets followup on in_progress tasks that don't already have one.
	// Matches tasks scoped to the given channel+chatID, or unscoped tasks in the same team.
	SetFollowupForActiveTasks(ctx context.Context, teamID uuid.UUID, channel, chatID string, followupAt time.Time, max int, message string) (int, error)
	// HasActiveMemberTasks returns true if there are pending/in_progress/blocked tasks
	// assigned to agents other than the given agent (typically the lead).
	// Used to suppress auto-followup when the lead is waiting for teammates, not the user.
	HasActiveMemberTasks(ctx context.Context, teamID uuid.UUID, excludeAgentID uuid.UUID) (bool, error)

	// Progress
	UpdateTaskProgress(ctx context.Context, taskID, teamID uuid.UUID, percent int, step string) error

	// Lock renewal (heartbeat to prevent stale recovery of long-running tasks)
	RenewTaskLock(ctx context.Context, taskID, teamID uuid.UUID) error

	// Stale recovery (batch — all v2 active teams in single query)
	// RecoverAllStaleTasks resets in_progress tasks with expired locks back to pending.
	RecoverAllStaleTasks(ctx context.Context) ([]RecoveredTaskInfo, error)
	// ForceRecoverAllTasks resets ALL in_progress tasks back to pending (ignoring lock expiry).
	// Used on startup when no agents are running.
	ForceRecoverAllTasks(ctx context.Context) ([]RecoveredTaskInfo, error)
	// ListRecoverableTasks returns all pending tasks (including stale in_progress with expired locks).
	// Per-team: used by DispatchUnblockedTasks after task completion.
	ListRecoverableTasks(ctx context.Context, teamID uuid.UUID) ([]TeamTaskData, error)
	// MarkAllStaleTasks sets pending tasks older than olderThan to stale status across all v2 active teams.
	MarkAllStaleTasks(ctx context.Context, olderThan time.Time) ([]RecoveredTaskInfo, error)
	// MarkInReviewStaleTasks sets in_review tasks older than olderThan to stale across all v2 active teams.
	MarkInReviewStaleTasks(ctx context.Context, olderThan time.Time) ([]RecoveredTaskInfo, error)
	// FixOrphanedBlockedTasks unblocks blocked tasks where all blockers reached terminal status.
	FixOrphanedBlockedTasks(ctx context.Context) ([]RecoveredTaskInfo, error)
	// ResetTaskStatus resets a stale or failed task back to pending for retry.
	ResetTaskStatus(ctx context.Context, taskID, teamID uuid.UUID) error

	// Team user grants
	GrantTeamAccess(ctx context.Context, teamID uuid.UUID, userID, role, grantedBy string) error
	RevokeTeamAccess(ctx context.Context, teamID uuid.UUID, userID string) error
	ListTeamGrants(ctx context.Context, teamID uuid.UUID) ([]TeamUserGrant, error)
	ListUserTeams(ctx context.Context, userID string) ([]TeamData, error)
	HasTeamAccess(ctx context.Context, teamID uuid.UUID, userID string) (bool, error)
}
