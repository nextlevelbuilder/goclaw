package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSubagentRootAgentIDRequired = errors.New("subagent root agent ID required")
	ErrSubagentTaskNotFound        = errors.New("subagent task not found in owner scope")
)

const (
	InterruptedSubagentTaskResult = "interrupted: gateway stopped before terminal completion was durably recorded"
	// SubagentTaskStatusMaxLength matches the durable PG and SQLite schema contract.
	SubagentTaskStatusMaxLength = 20
)

// IsTerminalSubagentTaskStatus reports whether a status ends the task
// lifecycle and therefore receives a completed_at timestamp.
func IsTerminalSubagentTaskStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// SubagentTaskData represents a persisted subagent task for audit trail and cost attribution.
type SubagentTaskData struct {
	BaseModel
	TenantID       uuid.UUID      `json:"tenant_id" db:"tenant_id"`
	RootAgentID    uuid.UUID      `json:"root_agent_id" db:"root_agent_id"`
	ParentAgentKey string         `json:"parent_agent_key" db:"parent_agent_key"`
	SessionKey     *string        `json:"session_key,omitempty" db:"session_key"`
	Subject        string         `json:"subject" db:"subject"`
	Description    string         `json:"description" db:"description"`
	Status         string         `json:"status" db:"status"`
	Result         *string        `json:"result,omitempty" db:"result"`
	Depth          int            `json:"depth" db:"depth"`
	Model          *string        `json:"model,omitempty" db:"model"`
	Provider       *string        `json:"provider,omitempty" db:"provider"`
	Iterations     int            `json:"iterations" db:"iterations"`
	InputTokens    int64          `json:"input_tokens" db:"input_tokens"`
	OutputTokens   int64          `json:"output_tokens" db:"output_tokens"`
	OriginChannel  *string        `json:"origin_channel,omitempty" db:"origin_channel"`
	OriginChatID   *string        `json:"origin_chat_id,omitempty" db:"origin_chat_id"`
	OriginPeerKind *string        `json:"origin_peer_kind,omitempty" db:"origin_peer_kind"`
	OriginUserID   *string        `json:"origin_user_id,omitempty" db:"origin_user_id"`
	SpawnedBy      *uuid.UUID     `json:"spawned_by,omitempty" db:"spawned_by"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty" db:"completed_at"`
	ArchivedAt     *time.Time     `json:"archived_at,omitempty" db:"archived_at"`
	Metadata       map[string]any `json:"metadata,omitempty" db:"metadata"`
}

// SubagentTaskStore persists subagent task lifecycle for audit trail and cost attribution.
// In-memory SubagentManager remains the source of truth for active operations.
type SubagentTaskStore interface {
	// Create persists a new subagent task at spawn time.
	Create(ctx context.Context, task *SubagentTaskData) error

	// Get retrieves a task owned by the tenant and immutable root-agent UUID.
	Get(ctx context.Context, rootAgentID, id uuid.UUID) (*SubagentTaskData, error)

	// UpdateStatus updates status, result, iterations, and token counts on completion/failure.
	UpdateStatus(ctx context.Context, rootAgentID, id uuid.UUID, status string, result *string, iterations int, inputTokens, outputTokens int64) error

	// ListByParent returns tasks owned by a root-agent UUID, optionally filtered by status.
	// Empty statusFilter returns all statuses. Ordered by created_at DESC.
	ListByParent(ctx context.Context, rootAgentID uuid.UUID, statusFilter string) ([]SubagentTaskData, error)

	// ListBySession returns tasks for a session owned by the tenant and immutable root-agent UUID.
	ListBySession(ctx context.Context, rootAgentID uuid.UUID, sessionKey string) ([]SubagentTaskData, error)

	// Archive marks at most limit old terminal tasks owned by the tenant and
	// immutable root-agent UUID as archived. Returns the number of rows affected.
	Archive(ctx context.Context, rootAgentID uuid.UUID, olderThan time.Duration, limit int) (int64, error)

	// UpdateMetadata merges metadata on an existing task.
	UpdateMetadata(ctx context.Context, rootAgentID, id uuid.UUID, metadata map[string]any) error
}

// SubagentTaskRecoveryStore exposes the startup-only, cross-tenant maintenance
// operation separately from tenant/root-scoped task CRUD.
type SubagentTaskRecoveryStore interface {
	// RecoverInterrupted marks every non-terminal task from the previous
	// process as failed. It is a cross-tenant startup operation and must run
	// before the gateway accepts new child runs. The current gateway
	// architecture is single-process; multi-replica deployments would need
	// coordinated ownership before invoking this recovery.
	RecoverInterrupted(ctx context.Context) (int64, error)
}
