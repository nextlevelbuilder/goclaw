package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// SessionData holds conversation state for one session.
type SessionData struct {
	Key      string              `json:"key"`
	Messages []providers.Message `json:"messages"`
	Summary  string              `json:"summary,omitempty"`
	Created  time.Time           `json:"created"`
	Updated  time.Time           `json:"updated"`

	AgentUUID uuid.UUID  `json:"agentUUID,omitempty"` // DB agent UUID
	UserID    string     `json:"userID,omitempty"`    // External user ID (e.g. Telegram user ID)
	TeamID    *uuid.UUID `json:"teamID,omitempty"`    // Team UUID (set for team sessions)

	Model                      string `json:"model,omitempty"`
	Provider                   string `json:"provider,omitempty"`
	Channel                    string `json:"channel,omitempty"`
	InputTokens                int64  `json:"inputTokens,omitempty"`
	OutputTokens               int64  `json:"outputTokens,omitempty"`
	CompactionCount            int    `json:"compactionCount,omitempty"`
	MemoryFlushCompactionCount int    `json:"memoryFlushCompactionCount,omitempty"`
	MemoryFlushAt              int64  `json:"memoryFlushAt,omitempty"`
	Label                      string `json:"label,omitempty"`
	SpawnedBy                  string            `json:"spawnedBy,omitempty"`
	SpawnDepth                 int               `json:"spawnDepth,omitempty"`
	Metadata                   map[string]string `json:"metadata,omitempty"`

	// Adaptive throttle: cached per-session so scheduler reads without DB lookup.
	ContextWindow    int `json:"contextWindow,omitempty"`    // agent's context window (set on first run)
	LastPromptTokens int `json:"lastPromptTokens,omitempty"` // actual prompt tokens from last LLM response
	LastMessageCount int `json:"lastMessageCount,omitempty"` // message count at time of last LLM call
}

// SessionInfo is lightweight session metadata for listing.
type SessionInfo struct {
	Key          string            `json:"key"`
	MessageCount int               `json:"messageCount"`
	Created      time.Time         `json:"created"`
	Updated      time.Time         `json:"updated"`
	Label        string            `json:"label,omitempty"`
	Channel      string            `json:"channel,omitempty"`
	UserID       string            `json:"userID,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// SessionListOpts holds pagination options for ListPaged.
type SessionListOpts struct {
	AgentID  string
	Channel  string    // optional: filter by channel prefix ("ws", "telegram", etc.)
	UserID   string    // optional: filter by user_id
	TenantID uuid.UUID // optional: filter by tenant (uuid.Nil = no filter)
	Limit    int
	Offset   int
}

// SessionListResult is the paginated result of ListPaged.
type SessionListResult struct {
	Sessions []SessionInfo `json:"sessions"`
	Total    int           `json:"total"`
}

// SessionInfoRich is an enriched session info for API responses (includes model, tokens, agent name).
type SessionInfoRich struct {
	SessionInfo
	Model           string `json:"model,omitempty"`
	Provider        string `json:"provider,omitempty"`
	InputTokens     int64  `json:"inputTokens,omitempty"`
	OutputTokens    int64  `json:"outputTokens,omitempty"`
	AgentName       string `json:"agentName,omitempty"`
	EstimatedTokens int    `json:"estimatedTokens,omitempty"` // estimated current context tokens (messages bytes/4 + 12k system prompt)
	ContextWindow   int    `json:"contextWindow,omitempty"`   // agent's context window size
	CompactionCount int    `json:"compactionCount,omitempty"` // number of compactions performed
}

// SessionListRichResult is the paginated result of ListPagedRich.
type SessionListRichResult struct {
	Sessions []SessionInfoRich `json:"sessions"`
	Total    int               `json:"total"`
}

// SessionCoreStore manages session lifecycle, messages, and history.
type SessionCoreStore interface {
	GetOrCreate(ctx context.Context, key string) *SessionData
	// Get returns the session if it exists (cache or DB), nil otherwise. Never creates.
	Get(ctx context.Context, key string) *SessionData
	AddMessage(ctx context.Context, key string, msg providers.Message)
	GetHistory(ctx context.Context, key string) []providers.Message
	GetSummary(ctx context.Context, key string) string
	SetSummary(ctx context.Context, key, summary string)
	GetLabel(ctx context.Context, key string) string
	SetLabel(ctx context.Context, key, label string)
	SetAgentInfo(ctx context.Context, key string, agentUUID uuid.UUID, userID string)
	TruncateHistory(ctx context.Context, key string, keepLast int)
	SetHistory(ctx context.Context, key string, msgs []providers.Message)
	Reset(ctx context.Context, key string)
	Delete(ctx context.Context, key string) error
	Save(ctx context.Context, key string) error
}

// SessionMetadataStore manages session metadata, token tracking, and calibration.
type SessionMetadataStore interface {
	UpdateMetadata(ctx context.Context, key, model, provider, channel string)
	AccumulateTokens(ctx context.Context, key string, input, output int64)
	IncrementCompaction(ctx context.Context, key string)
	GetCompactionCount(ctx context.Context, key string) int
	GetMemoryFlushCompactionCount(ctx context.Context, key string) int
	SetMemoryFlushDone(ctx context.Context, key string)
	GetSessionMetadata(ctx context.Context, key string) map[string]string
	SetSessionMetadata(ctx context.Context, key string, metadata map[string]string)
	SetSpawnInfo(ctx context.Context, key, spawnedBy string, depth int)
	SetContextWindow(ctx context.Context, key string, cw int)
	GetContextWindow(ctx context.Context, key string) int
	SetLastPromptTokens(ctx context.Context, key string, tokens, msgCount int)
	GetLastPromptTokens(ctx context.Context, key string) (tokens, msgCount int)
}

// SessionListingStore manages session listing, search, and discovery.
type SessionListingStore interface {
	List(ctx context.Context, agentID string) []SessionInfo
	ListPaged(ctx context.Context, opts SessionListOpts) SessionListResult
	ListPagedRich(ctx context.Context, opts SessionListOpts) SessionListRichResult
	LastUsedChannel(ctx context.Context, agentID string) (channel, chatID string)
}

// SessionStore composes all session sub-interfaces for backward compatibility.
// New code should depend on the specific sub-interface it needs.
type SessionStore interface {
	SessionCoreStore
	SessionMetadataStore
	SessionListingStore
}
