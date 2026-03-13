package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

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
	TeamTaskStatusPending         = "pending"
	TeamTaskStatusPendingApproval = "pending_approval"
	TeamTaskStatusInProgress      = "in_progress"
	TeamTaskStatusCompleted       = "completed"
	TeamTaskStatusBlocked         = "blocked"
	TeamTaskStatusFailed          = "failed"
)

// Team task list filter constants (for ListTasks statusFilter parameter).
const (
	TeamTaskFilterActive    = ""          // default: pending + pending_approval + in_progress + blocked
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
	LeadAgentKey string `json:"lead_agent_key,omitempty"`
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

	// Joined fields
	OwnerAgentKey string `json:"owner_agent_key,omitempty"`
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

	// GetTeamForAgent returns the team that the given agent belongs to.
	// Returns nil, nil if the agent is not in any team.
	GetTeamForAgent(ctx context.Context, agentID uuid.UUID) (*TeamData, error)

	// KnownUserIDs returns distinct user IDs from sessions of team member agents.
	// Used by team settings UI to populate user select boxes.
	KnownUserIDs(ctx context.Context, teamID uuid.UUID, limit int) ([]string, error)

	// Tasks (shared task list)
	CreateTask(ctx context.Context, task *TeamTaskData) error
	UpdateTask(ctx context.Context, taskID uuid.UUID, updates map[string]any) error
	// ListTasks returns tasks for a team. orderBy: "priority" or "newest".
	// statusFilter: "" = non-completed (default), "completed", "all".
	// userID: if non-empty, filter to tasks created by this user.
	ListTasks(ctx context.Context, teamID uuid.UUID, orderBy string, statusFilter string, userID string) ([]TeamTaskData, error)
	// GetTask returns a single task by ID with joined agent info.
	GetTask(ctx context.Context, taskID uuid.UUID) (*TeamTaskData, error)
	// SearchTasks performs FTS search over task subject+description.
	// userID: if non-empty, filter to tasks created by this user.
	SearchTasks(ctx context.Context, teamID uuid.UUID, query string, limit int, userID string) ([]TeamTaskData, error)

	// ClaimTask atomically transitions a task from pending to in_progress.
	// Only one agent can claim a given task (row-level lock, race-safe).
	// teamID is validated in the WHERE clause to prevent cross-team task claiming.
	ClaimTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error

	// CompleteTask marks a task as completed and unblocks dependent tasks.
	// teamID is validated in the WHERE clause to prevent cross-team task completion.
	CompleteTask(ctx context.Context, taskID, teamID uuid.UUID, result string) error

	// ApproveTask atomically transitions a task from pending_approval to pending.
	// Returns error if the task is not in pending_approval status or not found.
	ApproveTask(ctx context.Context, taskID, teamID uuid.UUID) error

	// CancelTask marks a non-completed task as cancelled (status=completed, result="CANCELLED: ..."),
	// unblocks dependent tasks, and transitions blocked→pending when all blockers are resolved.
	// Returns error if the task is already completed or not found.
	CancelTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error

	// FailTask marks an in_progress task as failed and stores the error message.
	// Unblocks dependent tasks so they aren't stuck.
	FailTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error

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
}
