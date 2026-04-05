package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SubagentTaskData represents a persisted subagent task for audit trail and cost attribution.
type SubagentTaskData struct {
	BaseModel
	TenantID       uuid.UUID      `json:"tenant_id"`
	ParentAgentKey string         `json:"parent_agent_key"`
	SessionKey     *string        `json:"session_key,omitempty"`
	Subject        string         `json:"subject"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Result         *string        `json:"result,omitempty"`
	Depth          int            `json:"depth"`
	Model          *string        `json:"model,omitempty"`
	Provider       *string        `json:"provider,omitempty"`
	Iterations     int            `json:"iterations"`
	InputTokens    int64          `json:"input_tokens"`
	OutputTokens   int64          `json:"output_tokens"`
	OriginChannel  *string        `json:"origin_channel,omitempty"`
	OriginChatID   *string        `json:"origin_chat_id,omitempty"`
	OriginPeerKind *string        `json:"origin_peer_kind,omitempty"`
	OriginUserID   *string        `json:"origin_user_id,omitempty"`
	SpawnedBy      *uuid.UUID     `json:"spawned_by,omitempty"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
	ArchivedAt     *time.Time     `json:"archived_at,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// SubagentTaskStore persists subagent task lifecycle for audit trail and cost attribution.
// In-memory SubagentManager remains the source of truth for active operations;
// DB writes are fire-and-forget (non-blocking).
type SubagentTaskStore interface {
	// Create persists a new subagent task at spawn time.
	Create(ctx context.Context, task *SubagentTaskData) error

	// Get retrieves a single task by ID (tenant-scoped).
	Get(ctx context.Context, id uuid.UUID) (*SubagentTaskData, error)

	// UpdateStatus updates status, result, iterations, and token counts on completion/failure.
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, result *string, iterations int, inputTokens, outputTokens int64) error

	// ListByParent returns tasks for a parent agent key, optionally filtered by status.
	// Empty statusFilter returns all statuses. Ordered by created_at DESC.
	ListByParent(ctx context.Context, parentAgentKey string, statusFilter string) ([]SubagentTaskData, error)

	// ListBySession returns tasks for a specific session key (tenant-scoped).
	ListBySession(ctx context.Context, sessionKey string) ([]SubagentTaskData, error)

	// Archive marks old completed/failed/cancelled tasks as archived.
	// Returns the number of rows affected.
	Archive(ctx context.Context, olderThan time.Duration) (int64, error)

	// UpdateMetadata merges metadata on an existing task.
	UpdateMetadata(ctx context.Context, id uuid.UUID, metadata map[string]any) error
}
