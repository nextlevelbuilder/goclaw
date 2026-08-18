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

	// RunTimelineItemTypeChunk persists streamed content chunks so a reconnecting
	// client can replay the raw stream. Unlike legacy types, chunk/thinking/
	// tool.started items keep their full content (no preview-only strip).
	RunTimelineItemTypeChunk        = "chunk"
	RunTimelineItemTypeThinking     = "thinking"
	RunTimelineItemTypeToolStarted  = "tool.started"
)

// RunTimelineItemContentPersisted reports whether a timeline item type carries
// full content in the content column instead of being a display-only preview
// archive. Stream replay types (chunk/thinking) and tool-start detail persist
// their content intact; legacy types strip content to keep the timeline slim.
func RunTimelineItemContentPersisted(itemType string) bool {
	switch itemType {
	case RunTimelineItemTypeChunk, RunTimelineItemTypeThinking, RunTimelineItemTypeToolStarted:
		return true
	}
	return false
}

const (
	RunTimelineStatusStarted   = "started"
	RunTimelineStatusRunning   = "running"
	RunTimelineStatusCompleted = "completed"
	RunTimelineStatusFailed    = "failed"
	RunTimelineStatusCancelled = "cancelled"

	// RunTimelineStatusThinking/WaitingTool/Verifying are per-phase statuses a
	// running agent transitions through (persisted on run.status timeline items
	// and/or agent_runs.status when a run is mid-pipeline).
	RunTimelineStatusThinking    = "thinking"
	RunTimelineStatusWaitingTool = "waiting_tool"
	RunTimelineStatusVerifying   = "verifying"
	// RunTimelineStatusPaused marks a run interrupted with a valid checkpoint:
	// not terminal-failed, but resumable on the next gateway start or resume call.
	RunTimelineStatusPaused = "paused"
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
// AfterSeq excludes items with seq <= AfterSeq (cursor replay) and is only
// meaningful when RunID is set.
type RunTimelineListOpts struct {
	RunID      string
	SessionKey string
	AfterSeq   int
	Limit      int
	Offset     int
}

// AgentRunStatus enumerates the durable run-state machine statuses.
const (
	AgentRunStatusPending    = "pending"
	AgentRunStatusRunning    = "running"
	AgentRunStatusCompacting = "compacting"
	AgentRunStatusCompleted  = "completed"
	AgentRunStatusFailed     = "failed"
	AgentRunStatusCancelled  = "cancelled"
)

// AgentRunTerminal reports whether s is a terminal status.
func AgentRunTerminal(s string) bool {
	switch s {
	case AgentRunStatusCompleted, AgentRunStatusFailed, AgentRunStatusCancelled:
		return true
	}
	return false
}

// ValidAgentRunStatus reports whether s is a known run-state status.
func ValidAgentRunStatus(s string) bool {
	switch s {
	case AgentRunStatusPending, AgentRunStatusRunning, AgentRunStatusCompacting,
		AgentRunStatusCompleted, AgentRunStatusFailed, AgentRunStatusCancelled,
		RunTimelineStatusPaused:
		return true
	}
	return false
}

// AgentRun is the durable record for one agent run (1 row per run).
// Backs the run-state machine and heartbeat/stale-recovery.
type AgentRun struct {
	ID          uuid.UUID       `json:"id" db:"id"`                // UUIDv7 (PG) / TEXT uud (SQLite)
	TenantID    uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	RunID       string          `json:"run_id" db:"run_id"`        // UUID string or "heartbeat:<agentKey>"
	SessionKey  string          `json:"session_key" db:"session_key"`
	AgentID     *uuid.UUID      `json:"agent_id,omitempty" db:"agent_id"`
	UserID      string          `json:"user_id,omitempty" db:"user_id"`
	Channel     string          `json:"channel,omitempty" db:"channel"`
	ChatID      string          `json:"chat_id,omitempty" db:"chat_id"`
	Status      string          `json:"status" db:"status"`
	Attempt     int             `json:"attempt" db:"attempt"`
	Checkpoint  json.RawMessage `json:"checkpoint,omitempty" db:"checkpoint"` // durable pipeline checkpoint, written each N iterations (enable=resume from here)
	HeartbeatAt time.Time       `json:"heartbeat_at" db:"heartbeat_at"`
	StartedAt   time.Time       `json:"started_at" db:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty" db:"completed_at"`
	Error       string          `json:"error,omitempty" db:"error"`
	Metadata    json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
}

// RunListOpts scopes a run-record list. At least one of RunID or TenantID
// context is required; SessionKey narrows to one session's runs.
type RunListOpts struct {
	RunID      string
	SessionKey string
	Status     string
	Limit      int
	Offset     int
}

// RunsStore persists durable agent-run records for the run-state machine,
// heartbeat, and stale-run recovery. Conceptually distinct from
// RunTimelineStore (append-only event journal); this is the authoritative
// 1-row-per-run record.
type RunsStore interface {
	// CreateRun inserts a new run record (idempotent on (tenant_id, run_id)).
	CreateRun(ctx context.Context, run *AgentRun) error
	// UpdateRunStatus transitions a run to a non-terminal state (running/compacting).
	// Where clause scopes to tenant_id + run_id. Updates updated_at automatically.
	UpdateRunStatus(ctx context.Context, runID string, status string) error
	// UpdateRunCheckpoint writes a durable pipeline checkpoint and transitions the
	// run's status in the same statement (status is typically "running", or
	// "compacting"/"paused" when the checkpoint is written just before a retry or
	// resume is expected). Where clause scopes to tenant_id + run_id. Non-fatal in
	// callers: a checkpoint that fails to persist merely loses resume capability.
	UpdateRunCheckpoint(ctx context.Context, runID string, status string, checkpoint json.RawMessage) error
	// UpdateRunTerminal transitions a run to a terminal state (completed/failed/cancelled),
	// stamping completed_at. Where clause scopes to tenant_id + run_id.
	UpdateRunTerminal(ctx context.Context, runID string, status, errMsg string, completedAt time.Time) error
	// TouchHeartbeat updates heartbeat_at for a live run (coalesced writes).
	TouchHeartbeat(ctx context.Context, runID string) error
	// GetRun returns one run record. Where clause scopes to tenant_id + run_id.
	GetRun(ctx context.Context, runID string) (*AgentRun, error)
	// ListRuns returns run records filtered by opts, scoped to tenant_id.
	ListRuns(ctx context.Context, opts RunListOpts) ([]AgentRun, error)
	// RecoverStaleRuns marks runs whose heartbeat / started_at is older than
	// staleAfter as failed, returning the count. Cross-tenant (startup + periodic).
	RecoverStaleRuns(ctx context.Context, staleAfter time.Duration) (int64, error)
}

// RunTimelineStore appends and lists archived agent run timeline entries.
type RunTimelineStore interface {
	AppendRunTimelineItem(ctx context.Context, item *RunTimelineItem) error
	ListRunTimelineItems(ctx context.Context, opts RunTimelineListOpts) ([]RunTimelineItem, error)
	// RecoverInterruptedRuns reconciles runs left mid-execution when the gateway
	// stopped: a run with a "started" run.status item but no terminal
	// (completed/failed/cancelled) run.status sibling. For each, it appends a
	// terminal failed run.status item so the run is no longer reported as
	// perpetually running. Intended to run once on startup (cross-tenant),
	// mirroring the cron scheduler's stale-'running' reset. Returns the number
	// of runs recovered.
	RecoverInterruptedRuns(ctx context.Context) (int64, error)
}
