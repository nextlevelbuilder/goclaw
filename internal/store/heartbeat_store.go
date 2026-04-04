package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AgentHeartbeat represents the heartbeat configuration for an agent.
type AgentHeartbeat struct {
	ID               uuid.UUID       `json:"id"`
	AgentID          uuid.UUID       `json:"agentId"`
	Enabled          bool            `json:"enabled"`
	IntervalSec      int             `json:"intervalSec"`
	Prompt           *string         `json:"prompt,omitempty"`
	ProviderID       *uuid.UUID      `json:"providerId,omitempty"`
	Model            *string         `json:"model,omitempty"`
	IsolatedSession  bool            `json:"isolatedSession"`
	LightContext     bool            `json:"lightContext"`
	AckMaxChars      int             `json:"ackMaxChars"`
	MaxRetries       int             `json:"maxRetries"`
	ActiveHoursStart *string         `json:"activeHoursStart,omitempty"`
	ActiveHoursEnd   *string         `json:"activeHoursEnd,omitempty"`
	Timezone         *string         `json:"timezone,omitempty"`
	Channel          *string         `json:"channel,omitempty"`
	ChatID           *string         `json:"chatId,omitempty"`
	NextRunAt        *time.Time      `json:"nextRunAt,omitempty"`
	LastRunAt        *time.Time      `json:"lastRunAt,omitempty"`
	LastStatus       *string         `json:"lastStatus,omitempty"`
	LastError        *string         `json:"lastError,omitempty"`
	RunCount         int             `json:"runCount"`
	SuppressCount    int             `json:"suppressCount"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

// HeartbeatState holds runtime state updates for a heartbeat run.
type HeartbeatState struct {
	NextRunAt     *time.Time
	LastRunAt     *time.Time
	LastStatus    string
	LastError     string
	RunCount      int
	SuppressCount int
}

// HeartbeatRunLog records a single heartbeat execution.
type HeartbeatRunLog struct {
	ID           uuid.UUID       `json:"id"`
	HeartbeatID  uuid.UUID       `json:"heartbeatId"`
	AgentID      uuid.UUID       `json:"agentId"`
	Status       string          `json:"status"`
	Summary      *string         `json:"summary,omitempty"`
	Error        *string         `json:"error,omitempty"`
	DurationMS   *int            `json:"durationMs,omitempty"`
	InputTokens  int             `json:"inputTokens"`
	OutputTokens int             `json:"outputTokens"`
	SkipReason   *string         `json:"skipReason,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	RanAt        time.Time       `json:"ranAt"`
	CreatedAt    time.Time       `json:"createdAt"`
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
	Action   string `json:"action"` // "running", "completed", "suppressed", "error", "skipped"
	AgentID  string `json:"agentId"`
	AgentKey string `json:"agentKey,omitempty"`
	Status   string `json:"status,omitempty"`
	Error    string `json:"error,omitempty"`
	Reason   string `json:"reason,omitempty"` // skip reason
}

// DeliveryTarget represents a known channel+chatID pair from session history.
type DeliveryTarget struct {
	Channel string `json:"channel"`
	ChatID  string `json:"chatId"`
	Title   string `json:"title,omitempty"` // chat/group title from session metadata
	Kind    string `json:"kind"`            // "dm" or "group"
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
