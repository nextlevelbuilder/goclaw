package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ListenRawMessage represents a captured WhatsApp message in listen-only mode.
type ListenRawMessage struct {
	ID                  uuid.UUID     `json:"id" db:"id"`
	ChannelName         string        `json:"channel_name" db:"channel_name"`
	ChatID              string        `json:"chat_id" db:"chat_id"`
	ChatName            string        `json:"chat_name" db:"chat_name"`
	GraphID             string        `json:"graph_id" db:"graph_id"`
	Sender              string        `json:"sender" db:"sender"`
	SenderID            string        `json:"sender_id" db:"sender_id"`
	Body                string        `json:"body" db:"body"`
	MsgTimestamp        time.Time     `json:"msg_timestamp" db:"msg_timestamp"`
	AgentID             string        `json:"agent_id" db:"agent_id"`
	AgentName           string        `json:"agent_name,omitempty" db:"agent_name"`
	TenantID            uuid.UUID     `json:"tenant_id" db:"tenant_id"`
	CreatedAt           time.Time     `json:"created_at" db:"created_at"`
	ProcessedAt         *time.Time    `json:"processed_at,omitempty" db:"processed_at"`
	MediaRefs           []RawMediaRef `json:"media_refs" db:"media_refs"`
	ExtractionStatus    string        `json:"extraction_status,omitempty" db:"extraction_status"`
	ExtractionError     string        `json:"extraction_error,omitempty" db:"extraction_error"`
	ExtractionAttempts  int           `json:"extraction_attempts,omitempty" db:"extraction_attempts"`
	LastAttemptedAt     *time.Time    `json:"last_attempted_at,omitempty" db:"last_attempted_at"`
}

const (
	ExtractionStatusPending   = "pending"
	ExtractionStatusExtracted = "extracted"
	ExtractionStatusFailed    = "failed"
	MaxExtractionAttempts     = 3
)

// RawMediaRef stores a reference to a persisted media file attached to a raw message.
type RawMediaRef struct {
	MediaID     string `json:"media_id"`
	FilePath    string `json:"file_path"`
	MediaType   string `json:"media_type"`    // "image", "video", "audio", "document"
	ContentType string `json:"content_type"`  // "image/jpeg", "audio/ogg", etc.
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
}

// ListenRawMessageGroup represents a distinct (agent_id, graph_id) pair
// with unprocessed messages, used by the extraction worker for polling.
type ListenRawMessageGroup struct {
	AgentID string `json:"agent_id" db:"agent_id"`
	GraphID string `json:"graph_id" db:"graph_id"`
}

// ListenRawMessageListOpts controls filtering and pagination for listing raw messages.
type ListenRawMessageListOpts struct {
	Limit            int
	Offset           int
	ChannelName      string
	ChatID           string
	AgentID          string
	GraphID          string
	Processed        *bool  // nil=all, true=processed only, false=pending only (legacy)
	ExtractionStatus string // ""=all, "pending", "extracted", "failed"
}

// ListenRawMessageStore persists raw messages captured by WhatsApp listen-only mode.
type ListenRawMessageStore interface {
	// AppendBatch inserts multiple raw messages in a single query.
	AppendBatch(ctx context.Context, msgs []ListenRawMessage) error

	// ListPending returns unprocessed messages for a given (agentID, graphID),
	// ordered by msg_timestamp DESC (newest first), limited to maxRows.
	// Only returns messages with extraction_status IN ('pending', 'failed')
	// where failed messages have attempts < MaxExtractionAttempts.
	ListPending(ctx context.Context, agentID, graphID string, maxRows int) ([]ListenRawMessage, error)

	// MarkProcessed sets processed_at = NOW() and extraction_status = 'extracted'
	// for the given message IDs.
	MarkProcessed(ctx context.Context, ids []uuid.UUID) error

	// MarkFailed sets extraction_status = 'failed', increments extraction_attempts,
	// sets extraction_error and last_attempted_at for the given message IDs.
	MarkFailed(ctx context.Context, ids []uuid.UUID, extractErr string) error

	// ListPendingGroups returns distinct (agent_id, graph_id) pairs that have
	// messages with extraction_status IN ('pending', 'failed') where failed
	// messages have attempts < MaxExtractionAttempts.
	ListPendingGroups(ctx context.Context) ([]ListenRawMessageGroup, error)

	// List returns raw messages matching the given opts, with total count for pagination.
	// Results ordered by created_at DESC (newest first).
	List(ctx context.Context, opts ListenRawMessageListOpts) ([]ListenRawMessage, int, error)

	// ResetProcessed sets processed_at = NULL and extraction_status = 'pending'
	// for messages matching the given filters, so the extraction worker will re-process them.
	// Returns the number of rows affected.
	ResetProcessed(ctx context.Context, agentID, graphID string) (int64, error)

	// ResetProcessedByIDs sets processed_at = NULL and extraction_status = 'pending'
	// for messages with the given IDs. Returns the number of rows affected.
	ResetProcessedByIDs(ctx context.Context, ids []uuid.UUID) (int64, error)

	// ListPendingEmbeddings returns messages where embedded_at IS NULL for a given
	// (agentID, graphID), ordered by msg_timestamp ASC (oldest first for sequential
	// chunking), limited to maxRows.
	ListPendingEmbeddings(ctx context.Context, agentID, graphID string, maxRows int) ([]ListenRawMessage, error)

	// ListPendingEmbeddingGroups returns distinct (agent_id, graph_id) pairs that
	// have messages where embedded_at IS NULL.
	ListPendingEmbeddingGroups(ctx context.Context) ([]ListenRawMessageGroup, error)

	// MarkEmbedded sets embedded_at = NOW() for the given message IDs.
	MarkEmbedded(ctx context.Context, ids []uuid.UUID) error

	// ExtractionStats returns counts of messages grouped by extraction_status.
	ExtractionStats(ctx context.Context) (map[string]int, error)

	// EmbeddingStats returns (pending, embedded) counts for embedding status.
	EmbeddingStats(ctx context.Context) (pending int, embedded int, err error)
}
