package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ListenRawMessage represents a captured WhatsApp message in listen-only mode.
type ListenRawMessage struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	ChannelName  string     `json:"channel_name" db:"channel_name"`
	ChatID       string     `json:"chat_id" db:"chat_id"`
	ChatName     string     `json:"chat_name" db:"chat_name"`
	GraphID      string     `json:"graph_id" db:"graph_id"`
	Sender       string     `json:"sender" db:"sender"`
	SenderID     string     `json:"sender_id" db:"sender_id"`
	Body         string     `json:"body" db:"body"`
	MsgTimestamp time.Time  `json:"msg_timestamp" db:"msg_timestamp"`
	AgentID      string     `json:"agent_id" db:"agent_id"`
	AgentName    string     `json:"agent_name,omitempty" db:"agent_name"`
	TenantID     uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	ProcessedAt  *time.Time `json:"processed_at,omitempty" db:"processed_at"`
}

// ListenRawMessageGroup represents a distinct (agent_id, graph_id) pair
// with unprocessed messages, used by the extraction worker for polling.
type ListenRawMessageGroup struct {
	AgentID string `json:"agent_id" db:"agent_id"`
	GraphID string `json:"graph_id" db:"graph_id"`
}

// ListenRawMessageListOpts controls filtering and pagination for listing raw messages.
type ListenRawMessageListOpts struct {
	Limit       int
	Offset      int
	ChannelName string
	ChatID      string
	AgentID     string
	GraphID     string
	Processed   *bool // nil=all, true=processed only, false=pending only
}

// ListenRawMessageStore persists raw messages captured by WhatsApp listen-only mode.
type ListenRawMessageStore interface {
	// AppendBatch inserts multiple raw messages in a single query.
	AppendBatch(ctx context.Context, msgs []ListenRawMessage) error

	// ListPending returns unprocessed messages for a given (agentID, graphID),
	// ordered by msg_timestamp DESC (newest first), limited to maxRows.
	ListPending(ctx context.Context, agentID, graphID string, maxRows int) ([]ListenRawMessage, error)

	// MarkProcessed sets processed_at = NOW() for the given message IDs.
	MarkProcessed(ctx context.Context, ids []uuid.UUID) error

	// ListPendingGroups returns distinct (agent_id, graph_id) pairs that have
	// unprocessed messages, for the worker to know what to poll.
	ListPendingGroups(ctx context.Context) ([]ListenRawMessageGroup, error)

	// List returns raw messages matching the given opts, with total count for pagination.
	// Results ordered by created_at DESC (newest first).
	List(ctx context.Context, opts ListenRawMessageListOpts) ([]ListenRawMessage, int, error)

	// ResetProcessed sets processed_at = NULL for messages matching the given filters,
	// so the extraction worker will re-process them. Returns the number of rows affected.
	ResetProcessed(ctx context.Context, agentID, graphID string) (int64, error)
}
