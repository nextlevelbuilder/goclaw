package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AgentHeartbeat represents the heartbeat configuration for an agent.
type AgentHeartbeat struct {
	ID               uuid.UUID       `json:"id" db:"id"`
	AgentID          uuid.UUID       `json:"agentId" db:"agent_id"`
	Enabled          bool            `json:"enabled" db:"enabled"`
	IntervalSec      int             `json:"intervalSec" db:"interval_sec"`
	Prompt           *string         `json:"prompt,omitempty" db:"prompt"`
	ProviderID       *uuid.UUID      `json:"providerId,omitempty" db:"provider_id"`
	Model            *string         `json:"model,omitempty" db:"model"`
	IsolatedSession  bool            `json:"isolatedSession" db:"isolated_session"`
	LightContext     bool            `json:"lightContext" db:"light_context"`
	AckMaxChars      int             `json:"ackMaxChars" db:"ack_max_chars"`
	MaxRetries       int             `json:"maxRetries" db:"max_retries"`
	ActiveHoursStart *string         `json:"activeHoursStart,omitempty" db:"active_hours_start"`
	ActiveHoursEnd   *string         `json:"activeHoursEnd,omitempty" db:"active_hours_end"`
	Timezone         *string         `json:"timezone,omitempty" db:"timezone"`
	Channel          *string         `json:"channel,omitempty" db:"channel"`
	ChatID           *string         `json:"chatId,omitempty" db:"chat_id"`
	NextRunAt        *time.Time      `json:"nextRunAt,omitempty" db:"next_run_at"`
	LastRunAt        *time.Time      `json:"lastRunAt,omitempty" db:"last_run_at"`
	LastStatus       *string         `json:"lastStatus,omitempty" db:"last_status"`
	LastError        *string         `json:"lastError,omitempty" db:"last_error"`
	RunCount         int             `json:"runCount" db:"run_count"`
	SuppressCount    int             `json:"suppressCount" db:"suppress_count"`
	Metadata         json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt        time.Time       `json:"createdAt" db:"created_at"`
	UpdatedAt        time.Time       `json:"updatedAt" db:"updated_at"`
}

// HeartbeatState holds runtime state updates for a heartbeat run.
type HeartbeatState struct {
	NextRunAt     *time.Time `db:"-"`
	LastRunAt     *time.Time `db:"-"`
	LastStatus    string     `db:"-"`
	LastError     string     `db:"-"`
	RunCount      int        `db:"-"`
	SuppressCount int        `db:"-"`
}

// HeartbeatRunLog records a single heartbeat execution.
type HeartbeatRunLog struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	HeartbeatID  uuid.UUID       `json:"heartbeatId" db:"heartbeat_id"`
	AgentID      uuid.UUID       `json:"agentId" db:"agent_id"`
	Status       string          `json:"status" db:"status"`
	Summary      *string         `json:"summary,omitempty" db:"summary"`
	Error        *string         `json:"error,omitempty" db:"error"`
	DurationMS   *int            `json:"durationMs,omitempty" db:"duration_ms"`
	InputTokens  int             `json:"inputTokens" db:"input_tokens"`
	OutputTokens int             `json:"outputTokens" db:"output_tokens"`
	SkipReason   *string         `json:"skipReason,omitempty" db:"skip_reason"`
	Metadata     json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	RanAt        time.Time       `json:"ranAt" db:"ran_at"`
	CreatedAt    time.Time       `json:"createdAt" db:"created_at"`
}

// StaggerOffset returns a deterministic offset for spreading heartbeats evenly.
// Uses FNV-1a hash of agent ID to produce a value in [0, 10% of intervalSec).
// Capped at 10% to avoid user-visible delay while still preventing thundering herd.
func StaggerOffset(agentID uuid.UUID, intervalSec int) time.Duration {
	if intervalSec <= 0 {
		return 0
	}
	h := uint32(2166136261) // FNV offset basis
	for _, b := range agentID {
		h ^= uint32(b)
		h *= 16777619 // FNV prime
	}
	maxOffset := max(
		// 10% of interval
		intervalSec/10, 1)
	offset := int(h) % maxOffset
	if offset < 0 {
		offset = -offset
	}
	return time.Duration(offset) * time.Second
}

// HeartbeatEvent represents a heartbeat lifecycle event sent to subscribers.
type HeartbeatEvent struct {
	Action   string `json:"action" db:"-"` // "running", "completed", "suppressed", "error", "skipped"
	AgentID  string `json:"agentId" db:"-"`
	AgentKey string `json:"agentKey,omitempty" db:"-"`
	Status   string `json:"status,omitempty" db:"-"`
	Error    string `json:"error,omitempty" db:"-"`
	Reason   string `json:"reason,omitempty" db:"-"` // skip reason
}

// DeliveryTarget represents a known channel+chatID pair from session history.
type DeliveryTarget struct {
	Channel string `json:"channel" db:"-"`
	ChatID  string `json:"chatId" db:"-"`
	Title   string `json:"title,omitempty" db:"-"` // chat/group title from session metadata
	Kind    string `json:"kind" db:"-"`            // "dm" or "group"
}

// HeartbeatStore manages agent heartbeat configurations and run logs.
type HeartbeatStore interface {
	Get(ctx context.Context, agentID uuid.UUID) (*AgentHeartbeat, error)
	Upsert(ctx context.Context, hb *AgentHeartbeat) error
	ListDue(ctx context.Context, now time.Time) ([]AgentHeartbeat, error)
	UpdateState(ctx context.Context, id uuid.UUID, state HeartbeatState) error
	Delete(ctx context.Context, agentID uuid.UUID) error

	// Logs
	InsertLog(ctx context.Context, log *HeartbeatRunLog) error
	ListLogs(ctx context.Context, agentID uuid.UUID, limit, offset int) ([]HeartbeatRunLog, int, error)

	// Delivery targets — known (channel, chatID) pairs from channel_contacts for a tenant.
	ListDeliveryTargets(ctx context.Context, tenantID uuid.UUID) ([]DeliveryTarget, error)

	// Events
	SetOnEvent(fn func(HeartbeatEvent))
}
