package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrFileLocked is returned when a workspace file is being written by another agent.
var ErrFileLocked = errors.New("file is being written by another agent, try again shortly")

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
	TeamTaskFilterActive    = ""          // default: pending + in_progress + blocked
	TeamTaskFilterInReview  = "in_review" // only in_review tasks
	TeamTaskFilterCompleted = "completed" // only completed tasks
	TeamTaskFilterAll       = "all"       // all statuses
)

// Team message type constants.
const (
	TeamMessageTypeChat      = "chat"
	TeamMessageTypeBroadcast = "broadcast"
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
	LeadAgentKey     string `json:"lead_agent_key,omitempty"`
	LeadDisplayName  string `json:"lead_display_name,omitempty"`
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

	// Joined fields
	OwnerAgentKey     string `json:"owner_agent_key,omitempty"`
	CreatedByAgentKey string `json:"created_by_agent_key,omitempty"`
}

// TeamTaskCommentData represents a comment on a team task.
type TeamTaskCommentData struct {
	ID        uuid.UUID  `json:"id"`
	TaskID    uuid.UUID  `json:"task_id"`
	AgentID   *uuid.UUID `json:"agent_id,omitempty"`
	UserID    string     `json:"user_id,omitempty"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`

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

// TeamTaskAttachmentData represents a workspace file attached to a team task.
type TeamTaskAttachmentData struct {
	ID        uuid.UUID  `json:"id"`
	TaskID    uuid.UUID  `json:"task_id"`
	FileID    uuid.UUID  `json:"file_id"`
	AddedBy   *uuid.UUID `json:"added_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`

	// Joined
	FileName string `json:"file_name,omitempty"`
}

// DelegationHistoryData represents a persisted delegation record.
type DelegationHistoryData struct {
	BaseModel
	SourceAgentID uuid.UUID      `json:"source_agent_id"`
	TargetAgentID uuid.UUID      `json:"target_agent_id"`
	TeamID        *uuid.UUID     `json:"team_id,omitempty"`
	TeamTaskID    *uuid.UUID     `json:"team_task_id,omitempty"`
	UserID        string         `json:"user_id,omitempty"`
	Task          string         `json:"task"`
	Mode          string         `json:"mode"`
	Status        string         `json:"status"`
	Result        *string        `json:"result,omitempty"`
	Error         *string        `json:"error,omitempty"`
	Iterations    int            `json:"iterations"`
	TraceID       *uuid.UUID     `json:"trace_id,omitempty"`
	DurationMS    int            `json:"duration_ms"`
	CompletedAt   *time.Time     `json:"completed_at,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`

	// Joined fields
	SourceAgentKey string `json:"source_agent_key,omitempty"`
	TargetAgentKey string `json:"target_agent_key,omitempty"`
}

// DelegationHistoryListOpts configures delegation history queries.
type DelegationHistoryListOpts struct {
	SourceAgentID *uuid.UUID
	TargetAgentID *uuid.UUID
	TeamID        *uuid.UUID
	UserID        string
	Status        string // "completed", "failed", "" = all
	Limit         int
	Offset        int
}

// HandoffRouteData represents an active routing override for agent handoff.
type HandoffRouteData struct {
	ID           uuid.UUID      `json:"id"`
	TeamID       uuid.UUID      `json:"team_id,omitempty"`
	Channel      string         `json:"channel"`
	ChatID       string         `json:"chat_id"`
	FromAgentKey string         `json:"from_agent_key"`
	ToAgentKey   string         `json:"to_agent_key"`
	Reason       string         `json:"reason,omitempty"`
	CreatedBy    string         `json:"created_by"`
	CreatedAt    time.Time      `json:"created_at"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// TeamMessageData represents a message in the team mailbox.
type TeamMessageData struct {
	ID          uuid.UUID      `json:"id"`
	TeamID      uuid.UUID      `json:"team_id"`
	FromAgentID uuid.UUID      `json:"from_agent_id"`
	ToAgentID   *uuid.UUID     `json:"to_agent_id,omitempty"`
	Content     string         `json:"content"`
	MessageType string         `json:"message_type"`
	Read        bool           `json:"read"`
	TaskID      *uuid.UUID     `json:"task_id,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`

	// Joined fields
	FromAgentKey string `json:"from_agent_key,omitempty"`
	ToAgentKey   string `json:"to_agent_key,omitempty"`
}

// TeamWorkspaceFileData represents a file in the team's shared workspace.
type TeamWorkspaceFileData struct {
	ID         uuid.UUID       `json:"id"`
	TeamID     uuid.UUID       `json:"team_id"`
	Channel    string          `json:"channel"`
	ChatID     string          `json:"chat_id"`
	FileName   string          `json:"file_name"`
	MimeType   string          `json:"mime_type,omitempty"`
	FilePath   string          `json:"-"`
	SizeBytes  int64           `json:"size_bytes"`
	UploadedBy uuid.UUID       `json:"uploaded_by"`
	TaskID     *uuid.UUID      `json:"task_id,omitempty"`
	Pinned     bool            `json:"pinned"`
	Tags       []string        `json:"tags,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	ArchivedAt *time.Time      `json:"archived_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`

	// Joined
	UploadedByKey string `json:"uploaded_by_key,omitempty"`
}

// TeamWorkspaceFileVersionData represents a version of a workspace file.
type TeamWorkspaceFileVersionData struct {
	ID         uuid.UUID `json:"id"`
	FileID     uuid.UUID `json:"file_id"`
	Version    int       `json:"version"`
	FilePath   string    `json:"-"`
	SizeBytes  int64     `json:"size_bytes"`
	UploadedBy uuid.UUID `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`

	// Joined
	UploadedByKey string `json:"uploaded_by_key,omitempty"`
}

// TeamWorkspaceCommentData represents a comment on a workspace file.
type TeamWorkspaceCommentData struct {
	ID        uuid.UUID `json:"id"`
	FileID    uuid.UUID `json:"file_id"`
	AgentID   uuid.UUID `json:"agent_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`

	// Joined
	AgentKey string `json:"agent_key,omitempty"`
}

// ScopeEntry represents a unique channel+chatID scope across tasks and workspace.
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
	ListTasks(ctx context.Context, teamID uuid.UUID, orderBy string, statusFilter string, userID string, channel string, chatID string) ([]TeamTaskData, error)
	// GetTask returns a single task by ID with joined agent info.
	GetTask(ctx context.Context, taskID uuid.UUID) (*TeamTaskData, error)
	// SearchTasks performs FTS search over task subject+description.
	// userID: if non-empty, filter to tasks created by this user.
	SearchTasks(ctx context.Context, teamID uuid.UUID, query string, limit int, userID string) ([]TeamTaskData, error)

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

	// Audit events
	RecordTaskEvent(ctx context.Context, event *TeamTaskEventData) error
	ListTaskEvents(ctx context.Context, taskID uuid.UUID) ([]TeamTaskEventData, error)
	// ListTeamEvents returns recent events across all tasks in a team.
	ListTeamEvents(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]TeamTaskEventData, error)

	// Attachments
	AttachFileToTask(ctx context.Context, att *TeamTaskAttachmentData) error
	ListTaskAttachments(ctx context.Context, taskID uuid.UUID) ([]TeamTaskAttachmentData, error)
	DetachFileFromTask(ctx context.Context, taskID, fileID uuid.UUID) error

	// Follow-up reminders
	SetTaskFollowup(ctx context.Context, taskID, teamID uuid.UUID, followupAt time.Time, max int, message, channel, chatID string) error
	ClearTaskFollowup(ctx context.Context, taskID uuid.UUID) error
	ListFollowupDueTasks(ctx context.Context, teamID uuid.UUID) ([]TeamTaskData, error)
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

	// Stale recovery
	RecoverStaleTasks(ctx context.Context, teamID uuid.UUID) (int, error)
	// ForceRecoverAllTasks resets ALL in_progress tasks back to pending (ignoring lock expiry).
	// Used on startup when no agents are running.
	ForceRecoverAllTasks(ctx context.Context, teamID uuid.UUID) (int, error)
	// ListRecoverableTasks returns all pending tasks (including stale in_progress with expired locks).
	ListRecoverableTasks(ctx context.Context, teamID uuid.UUID) ([]TeamTaskData, error)
	// MarkStaleTasks sets pending tasks older than olderThan to stale status.
	MarkStaleTasks(ctx context.Context, teamID uuid.UUID, olderThan time.Time) (int, error)
	// ResetTaskStatus resets a stale or failed task back to pending for retry.
	ResetTaskStatus(ctx context.Context, taskID, teamID uuid.UUID) error

	// Delegation history
	SaveDelegationHistory(ctx context.Context, record *DelegationHistoryData) error
	ListDelegationHistory(ctx context.Context, opts DelegationHistoryListOpts) ([]DelegationHistoryData, int, error)
	GetDelegationHistory(ctx context.Context, id uuid.UUID) (*DelegationHistoryData, error)

	// Handoff routing
	SetHandoffRoute(ctx context.Context, route *HandoffRouteData) error
	GetHandoffRoute(ctx context.Context, channel, chatID string) (*HandoffRouteData, error)
	ClearHandoffRoute(ctx context.Context, channel, chatID string) error

	// Messages (mailbox)
	SendMessage(ctx context.Context, msg *TeamMessageData) error
	GetUnread(ctx context.Context, teamID, agentID uuid.UUID) ([]TeamMessageData, error)
	MarkRead(ctx context.Context, messageID uuid.UUID) error
	// ListMessages returns paginated team messages ordered by created_at DESC.
	ListMessages(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]TeamMessageData, int, error)

	// Workspace files
	// UpsertWorkspaceFile acquires an advisory lock, calls diskWriteFn (if non-nil)
	// to perform disk I/O under that lock, then upserts DB metadata — all within one tx.
	// diskWriteFn receives isNew (true if this is a new file, false if update).
	UpsertWorkspaceFile(ctx context.Context, file *TeamWorkspaceFileData, diskWriteFn func(isNew bool) error) (isNew bool, err error)
	GetWorkspaceFile(ctx context.Context, teamID uuid.UUID, channel, chatID, fileName string) (*TeamWorkspaceFileData, error)
	// ListWorkspaceFiles returns non-archived files for a team.
	// When both channel and chatID are empty, all files for the team are returned
	// regardless of scope (used by the Workspace UI tab).
	// When either is non-empty, results are filtered to that exact channel+chatID scope.
	ListWorkspaceFiles(ctx context.Context, teamID uuid.UUID, channel, chatID string) ([]TeamWorkspaceFileData, error)
	DeleteWorkspaceFile(ctx context.Context, teamID uuid.UUID, channel, chatID, fileName string) (filePath string, err error)
	CountWorkspaceFiles(ctx context.Context, teamID uuid.UUID, channel, chatID string) (int, error)
	PinWorkspaceFile(ctx context.Context, teamID uuid.UUID, channel, chatID, fileName string, pinned bool) error
	TagWorkspaceFile(ctx context.Context, teamID uuid.UUID, channel, chatID, fileName string, tags []string) error
	ListDeliverableFiles(ctx context.Context, teamID uuid.UUID, channel, chatID string) ([]TeamWorkspaceFileData, error)
	ArchiveWorkspaceFilesByTask(ctx context.Context, taskID uuid.UUID) error
	ListOrphanWorkspaceFiles(ctx context.Context, teamID uuid.UUID, olderThan time.Time) ([]TeamWorkspaceFileData, error)
	CopyFilesToTeam(ctx context.Context, fileIDs []uuid.UUID, targetTeamID uuid.UUID, targetChannel, targetChatID, dataDir string) error

	// Workspace versioning
	CreateFileVersion(ctx context.Context, version *TeamWorkspaceFileVersionData) error
	ListFileVersions(ctx context.Context, fileID uuid.UUID) ([]TeamWorkspaceFileVersionData, error)
	GetFileVersion(ctx context.Context, fileID uuid.UUID, version int) (*TeamWorkspaceFileVersionData, error)
	PruneOldVersions(ctx context.Context, fileID uuid.UUID, keepN int) ([]string, error) // returns pruned file paths

	// Workspace comments
	AddFileComment(ctx context.Context, comment *TeamWorkspaceCommentData) error
	ListFileComments(ctx context.Context, fileID uuid.UUID) ([]TeamWorkspaceCommentData, error)

	// Workspace quota
	GetWorkspaceTotalSize(ctx context.Context, teamID uuid.UUID) (int64, error)
}
