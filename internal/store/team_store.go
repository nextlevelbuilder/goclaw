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
	AgentKey    string `json:"agent_key,omitempty" db:"agent_key"`
	DisplayName string `json:"display_name,omitempty" db:"display_name"`
	Frontmatter string `json:"frontmatter,omitempty" db:"frontmatter"`
	Emoji       string `json:"emoji,omitempty" db:"emoji"`
}

// TeamTaskData represents a task in the team's shared task list.
type TeamTaskData struct {
	BaseModel
	TeamID       uuid.UUID      `json:"team_id" db:"team_id"`
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
}
