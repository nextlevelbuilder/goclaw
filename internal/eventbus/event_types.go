// Package eventbus provides typed domain events for the v3 consolidation pipeline.
// Separate from internal/bus (retained for channel message routing).
//
// V3 design: Phase 1C — foundation interface.
package eventbus

import "time"

// EventType identifies the event category.
type EventType string

const (
	EventSessionCompleted EventType = "session.completed"
	EventEpisodicCreated  EventType = "episodic.created"
	EventEntityUpserted   EventType = "entity.upserted"
	EventMemoryLint       EventType = "memory.lint"
	EventRunCompleted     EventType = "run.completed"
	EventToolExecuted     EventType = "tool.executed"

	// Delegation events (v3 orchestration)
	EventDelegateSent      EventType = "delegate.sent"
	EventDelegateCompleted EventType = "delegate.completed"
	EventDelegateFailed    EventType = "delegate.failed"

	// Evolution events (v3 self-improvement)
	EventSuggestionCreated EventType = "evolution.suggestion_created"
)

// DomainEvent is a typed event with metadata for the consolidation pipeline.
type DomainEvent struct {
	ID        string    // UUID v7 for ordering
	Type      EventType
	SourceID  string    // dedup key (e.g. session key, run ID)
	TenantID  string
	AgentID   string
	UserID    string
	Timestamp time.Time
	Payload   any       // typed per EventType (see payload structs below)
}

// --- Typed payloads, one per EventType ---

// SessionCompletedPayload is emitted after session end or compaction.
type SessionCompletedPayload struct {
	SessionKey      string
	MessageCount    int
	TokensUsed      int
	Summary         string // compaction summary if available
	CompactionCount int    // tracks how many times compaction ran
}

// EpisodicCreatedPayload is emitted after episodic summary is stored.
type EpisodicCreatedPayload struct {
	EpisodicID  string
	SessionKey  string
	Summary     string
	KeyEntities []string
}

// EntityUpsertedPayload is emitted after KG entity upsert.
type EntityUpsertedPayload struct {
	EntityIDs []string
}

// MemoryLintPayload triggers periodic memory cleanup.
type MemoryLintPayload struct {
	Scope string // "agent" or "user"
}

// RunCompletedPayload is emitted after pipeline run finishes.
type RunCompletedPayload struct {
	RunID      string
	Iterations int
	TokensUsed int
	ToolCalls  int
	LoopKilled bool
}

// ToolExecutedPayload is emitted per tool call for metrics.
type ToolExecutedPayload struct {
	ToolName string
	Duration time.Duration
	Success  bool
	ReadOnly bool
}

// DelegateSentPayload is emitted when a delegation is dispatched.
type DelegateSentPayload struct {
	DelegationID string
	FromAgent    string
	ToAgent      string
	Task         string
	Mode         string // "async" or "sync"
}

// DelegateCompletedPayload is emitted when a delegatee finishes.
type DelegateCompletedPayload struct {
	DelegationID string
	FromAgent    string
	ToAgent      string
	Content      string
}

// DelegateFailedPayload is emitted when a delegation fails.
type DelegateFailedPayload struct {
	DelegationID string
	FromAgent    string
	ToAgent      string
	Error        string
}
