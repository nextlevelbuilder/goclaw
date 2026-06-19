package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	RunTimelineItemTypeActivity         = "activity"
	RunTimelineItemTypeAssistantMessage = "assistant.message"
	RunTimelineItemTypeToolCall         = "tool.call"
	RunTimelineItemTypeToolResult       = "tool.result"
	RunTimelineItemTypeRunStatus        = "run.status"
)

const (
	RunTimelineStatusStarted   = "started"
	RunTimelineStatusRunning   = "running"
	RunTimelineStatusCompleted = "completed"
	RunTimelineStatusFailed    = "failed"
	RunTimelineStatusCancelled = "cancelled"
)

// RunTimelineItem is a persisted, display-safe archive entry for one agent run.
type RunTimelineItem struct {
	ID         uuid.UUID       `json:"id" db:"id"`
	TenantID   uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	RunID      string          `json:"run_id" db:"run_id"`
	SessionKey string          `json:"session_key" db:"session_key"`
	AgentID    *uuid.UUID      `json:"agent_id,omitempty" db:"agent_id"`
	UserID     string          `json:"user_id,omitempty" db:"user_id"`
	Channel    string          `json:"channel,omitempty" db:"channel"`
	ChatID     string          `json:"chat_id,omitempty" db:"chat_id"`
	Seq        int             `json:"seq" db:"seq"`
	ItemType   string          `json:"item_type" db:"item_type"`
	Status     string          `json:"status,omitempty" db:"status"`
	Title      string          `json:"title,omitempty" db:"title"`
	Preview    string          `json:"preview,omitempty" db:"preview"`
	Content    string          `json:"content,omitempty" db:"content"`
	ToolName   string          `json:"tool_name,omitempty" db:"tool_name"`
	ToolCallID string          `json:"tool_call_id,omitempty" db:"tool_call_id"`
	TraceID    *uuid.UUID      `json:"trace_id,omitempty" db:"trace_id"`
	SpanID     *uuid.UUID      `json:"span_id,omitempty" db:"span_id"`
	Metadata   json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

// RunTimelineListOpts scopes a timeline read. RunID is preferred; SessionKey is
// a fallback for session archive views that need the latest runs in a session.
type RunTimelineListOpts struct {
	RunID      string
	SessionKey string
	Limit      int
	Offset     int
}

// RunTimelineStore appends and lists archived agent run timeline entries.
type RunTimelineStore interface {
	AppendRunTimelineItem(ctx context.Context, item *RunTimelineItem) error
	ListRunTimelineItems(ctx context.Context, opts RunTimelineListOpts) ([]RunTimelineItem, error)
}
