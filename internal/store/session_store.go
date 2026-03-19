package store

import (
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
	AgentID string
	Limit   int
	Offset  int
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

// SessionStore manages conversation sessions.
type SessionStore interface {
	GetOrCreate(key string) *SessionData
	AddMessage(key string, msg providers.Message)
	GetHistory(key string) []providers.Message
	GetSummary(key string) string
	SetSummary(key, summary string)
	SetLabel(key, label string)
	SetAgentInfo(key string, agentUUID uuid.UUID, userID string)
	UpdateMetadata(key, model, provider, channel string)
	AccumulateTokens(key string, input, output int64)
	IncrementCompaction(key string)
	GetCompactionCount(key string) int
	GetMemoryFlushCompactionCount(key string) int
	SetMemoryFlushDone(key string)
	GetSessionMetadata(key string) map[string]string
	SetSessionMetadata(key string, metadata map[string]string)
	SetSpawnInfo(key, spawnedBy string, depth int)
	SetContextWindow(key string, cw int)
	GetContextWindow(key string) int
	SetLastPromptTokens(key string, tokens, msgCount int)
	GetLastPromptTokens(key string) (tokens, msgCount int)
	TruncateHistory(key string, keepLast int)
	SetHistory(key string, msgs []providers.Message)
	Reset(key string)
	Delete(key string) error
	List(agentID string) []SessionInfo
	ListPaged(opts SessionListOpts) SessionListResult
	ListPagedRich(opts SessionListOpts) SessionListRichResult
	Save(key string) error
	LastUsedChannel(agentID string) (channel, chatID string)
}
