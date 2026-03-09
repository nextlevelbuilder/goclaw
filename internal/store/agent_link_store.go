package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Link direction constants.
const (
	LinkDirectionOutbound      = "outbound"
	LinkDirectionInbound       = "inbound"
	LinkDirectionBidirectional = "bidirectional"
)

// Link status constants.
const (
	LinkStatusActive   = "active"
	LinkStatusDisabled = "disabled"
)

// AgentLinkData represents a directional link between two agents for delegation.
type AgentLinkData struct {
	BaseModel
	SourceAgentID uuid.UUID       `json:"source_agent_id"`
	TargetAgentID uuid.UUID       `json:"target_agent_id"`
	Direction     string          `json:"direction"`         // "outbound", "inbound", "bidirectional"
	TeamID        *uuid.UUID      `json:"team_id,omitempty"` // non-nil = auto-created by team
	Description   string          `json:"description,omitempty"`
	MaxConcurrent int             `json:"max_concurrent"`
	Settings      json.RawMessage `json:"settings,omitempty"`
	Status        string          `json:"status"` // "active", "disabled"
	CreatedBy     string          `json:"created_by"`

	// Joined fields (populated by queries that JOIN agents table)
	SourceAgentKey    string `json:"source_agent_key,omitempty"`
	TargetAgentKey    string `json:"target_agent_key,omitempty"`
	TargetDisplayName string `json:"target_display_name,omitempty"`
	TargetDescription string `json:"target_description,omitempty"`
	TeamName          string `json:"team_name,omitempty"`           // from LEFT JOIN agent_teams (link's own team)
	TargetIsTeamLead  bool   `json:"target_is_team_lead,omitempty"` // true if target is lead of any active team
	TargetTeamName    string `json:"target_team_name,omitempty"`    // name of team the target leads
}

// AgentLinkStore manages inter-agent delegation links.
type AgentLinkStore interface {
	CreateLink(ctx context.Context, link *AgentLinkData) error
	DeleteLink(ctx context.Context, id uuid.UUID) error
	UpdateLink(ctx context.Context, id uuid.UUID, updates map[string]any) error
	GetLink(ctx context.Context, id uuid.UUID) (*AgentLinkData, error)

	// ListLinksFrom returns all links where agentID is the source.
	ListLinksFrom(ctx context.Context, agentID uuid.UUID) ([]AgentLinkData, error)

	// ListLinksTo returns all links where agentID is the target.
	ListLinksTo(ctx context.Context, agentID uuid.UUID) ([]AgentLinkData, error)

	// CanDelegate checks if fromAgent can delegate to toAgent considering direction.
	CanDelegate(ctx context.Context, fromAgentID, toAgentID uuid.UUID) (bool, error)

	// GetLinkBetween returns the active link allowing fromAgent to delegate to toAgent.
	// Returns full link data including Settings for per-user permission checks.
	// Returns nil, nil if no matching link exists.
	GetLinkBetween(ctx context.Context, fromAgentID, toAgentID uuid.UUID) (*AgentLinkData, error)

	// DelegateTargets returns all agents that fromAgent can delegate to,
	// with joined agent_key and display_name for AGENTS.md generation.
	DelegateTargets(ctx context.Context, fromAgentID uuid.UUID) ([]AgentLinkData, error)

	// SearchDelegateTargets performs FTS search over delegation targets.
	SearchDelegateTargets(ctx context.Context, fromAgentID uuid.UUID, query string, limit int) ([]AgentLinkData, error)

	// SearchDelegateTargetsByEmbedding performs vector similarity search over delegation targets.
	SearchDelegateTargetsByEmbedding(ctx context.Context, fromAgentID uuid.UUID, embedding []float32, limit int) ([]AgentLinkData, error)

	// DeleteTeamLinksForAgent removes all team-specific links involving an agent.
	DeleteTeamLinksForAgent(ctx context.Context, teamID, agentID uuid.UUID) error
}
