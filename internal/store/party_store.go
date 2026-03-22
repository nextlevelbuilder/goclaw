package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Party session statuses.
const (
	PartyStatusAssembling  = "assembling"
	PartyStatusDiscussing  = "discussing"
	PartyStatusSummarizing = "summarizing"
	PartyStatusClosed      = "closed"
)

// Party discussion modes.
const (
	PartyModeStandard  = "standard"
	PartyModeDeep      = "deep"
	PartyModeTokenRing = "token_ring"
)

// PartySessionData represents a party mode session.
type PartySessionData struct {
	ID         uuid.UUID       `json:"id"`
	Topic      string          `json:"topic"`
	TeamPreset string          `json:"team_preset,omitempty"`
	Status     string          `json:"status"`
	Mode       string          `json:"mode"`
	Round      int             `json:"round"`
	MaxRounds  int             `json:"max_rounds"`
	UserID     string          `json:"user_id"`
	Channel    string          `json:"channel,omitempty"`
	ChatID     string          `json:"chat_id,omitempty"`
	Personas   json.RawMessage `json:"personas"`
	Context    json.RawMessage `json:"context"`
	History    json.RawMessage `json:"history"`
	Summary    json.RawMessage `json:"summary,omitempty"`
	Artifacts  json.RawMessage `json:"artifacts"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// PartyStore manages party mode sessions.
type PartyStore interface {
	CreateSession(ctx context.Context, session *PartySessionData) error
	GetSession(ctx context.Context, id uuid.UUID) (*PartySessionData, error)
	UpdateSession(ctx context.Context, id uuid.UUID, updates map[string]any) error
	ListSessions(ctx context.Context, userID string, status string, limit int) ([]*PartySessionData, error)
	// GetActiveSession returns the active (assembling/discussing) session for a user+channel+chat.
	GetActiveSession(ctx context.Context, userID, channel, chatID string) (*PartySessionData, error)
	DeleteSession(ctx context.Context, id uuid.UUID) error
}
