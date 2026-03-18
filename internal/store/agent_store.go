package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// Agent type constants.
const (
	AgentTypeOpen       = "open"       // per-user context files, seeded on first chat
	AgentTypePredefined = "predefined" // shared agent-level context files
)

// Agent status constants.
const (
	AgentStatusActive      = "active"
	AgentStatusInactive    = "inactive"
	AgentStatusSummoning   = "summoning"
	AgentStatusSummonFailed = "summon_failed"
)

// AgentData represents an agent in the database.
type AgentData struct {
	BaseModel
	AgentKey            string `json:"agent_key"`
	DisplayName         string `json:"display_name,omitempty"`
	Frontmatter         string `json:"frontmatter,omitempty"` // short expertise summary (NOT other_config.description which is the summoning prompt)
	OwnerID             string `json:"owner_id"`
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	ContextWindow       int    `json:"context_window"`
	MaxToolIterations   int    `json:"max_tool_iterations"`
	Workspace           string `json:"workspace"`
	RestrictToWorkspace bool   `json:"restrict_to_workspace"`
	AgentType           string `json:"agent_type"` // "open" or "predefined"
	IsDefault           bool   `json:"is_default"`
	Status              string `json:"status"`

	// Budget: optional monthly spending limit in cents (nil = unlimited)
	BudgetMonthlyCents *int `json:"budget_monthly_cents,omitempty"`

	// Per-agent JSONB config (nullable — nil means "use global defaults")
	ToolsConfig      json.RawMessage `json:"tools_config,omitempty"`
	SandboxConfig    json.RawMessage `json:"sandbox_config,omitempty"`
	SubagentsConfig  json.RawMessage `json:"subagents_config,omitempty"`
	MemoryConfig     json.RawMessage `json:"memory_config,omitempty"`
	CompactionConfig json.RawMessage `json:"compaction_config,omitempty"`
	ContextPruning   json.RawMessage `json:"context_pruning,omitempty"`
	OtherConfig      json.RawMessage `json:"other_config,omitempty"`
}

// ParseToolsConfig returns per-agent tool policy, or nil if not configured.
func (a *AgentData) ParseToolsConfig() *config.ToolPolicySpec {
	if len(a.ToolsConfig) == 0 {
		return nil
	}
	var c config.ToolPolicySpec
	if json.Unmarshal(a.ToolsConfig, &c) != nil {
		return nil
	}
	return &c
}

// ParseSubagentsConfig returns per-agent subagent config, or nil if not configured.
func (a *AgentData) ParseSubagentsConfig() *config.SubagentsConfig {
	if len(a.SubagentsConfig) == 0 {
		return nil
	}
	var c config.SubagentsConfig
	if json.Unmarshal(a.SubagentsConfig, &c) != nil {
		return nil
	}
	return &c
}

// ParseCompactionConfig returns per-agent compaction config, or nil if not configured.
func (a *AgentData) ParseCompactionConfig() *config.CompactionConfig {
	if len(a.CompactionConfig) == 0 {
		return nil
	}
	var c config.CompactionConfig
	if json.Unmarshal(a.CompactionConfig, &c) != nil {
		return nil
	}
	return &c
}

// ParseContextPruning returns per-agent context pruning config, or nil if not configured.
func (a *AgentData) ParseContextPruning() *config.ContextPruningConfig {
	if len(a.ContextPruning) == 0 {
		return nil
	}
	var c config.ContextPruningConfig
	if json.Unmarshal(a.ContextPruning, &c) != nil {
		return nil
	}
	return &c
}

// ParseSandboxConfig returns per-agent sandbox config, or nil if not configured.
func (a *AgentData) ParseSandboxConfig() *config.SandboxConfig {
	if len(a.SandboxConfig) == 0 {
		return nil
	}
	var c config.SandboxConfig
	if json.Unmarshal(a.SandboxConfig, &c) != nil {
		return nil
	}
	return &c
}

// ParseMemoryConfig returns per-agent memory config, or nil if not configured.
func (a *AgentData) ParseMemoryConfig() *config.MemoryConfig {
	if len(a.MemoryConfig) == 0 {
		return nil
	}
	var c config.MemoryConfig
	if json.Unmarshal(a.MemoryConfig, &c) != nil {
		return nil
	}
	return &c
}

// ParseThinkingLevel extracts thinking_level from other_config JSONB.
// Returns "" if not configured (meaning "off").
func (a *AgentData) ParseThinkingLevel() string {
	if len(a.OtherConfig) == 0 {
		return ""
	}
	var cfg struct {
		ThinkingLevel string `json:"thinking_level"`
	}
	if json.Unmarshal(a.OtherConfig, &cfg) != nil {
		return ""
	}
	return cfg.ThinkingLevel
}

// ParseMaxTokens extracts max_tokens from other_config JSONB.
// Returns 0 if not configured (caller should apply default).
func (a *AgentData) ParseMaxTokens() int {
	if len(a.OtherConfig) == 0 {
		return 0
	}
	var cfg struct {
		MaxTokens int `json:"max_tokens"`
	}
	if json.Unmarshal(a.OtherConfig, &cfg) != nil {
		return 0
	}
	return cfg.MaxTokens
}

// ParseSelfEvolve extracts self_evolve from other_config JSONB.
// When true, predefined agents can update their SOUL.md (style/tone) through chat.
func (a *AgentData) ParseSelfEvolve() bool {
	if len(a.OtherConfig) == 0 {
		return false
	}
	var cfg struct {
		SelfEvolve bool `json:"self_evolve"`
	}
	if json.Unmarshal(a.OtherConfig, &cfg) != nil {
		return false
	}
	return cfg.SelfEvolve
}

// ParseSkillEvolve extracts skill_evolve from other_config JSONB.
// When true, the agent's learning loop is enabled: system prompt includes skill
// creation guidance, and the loop injects nudges at tool count milestones.
func (a *AgentData) ParseSkillEvolve() bool {
	if len(a.OtherConfig) == 0 {
		return false
	}
	var cfg struct {
		SkillEvolve bool `json:"skill_evolve"`
	}
	if json.Unmarshal(a.OtherConfig, &cfg) != nil {
		return false
	}
	return cfg.SkillEvolve
}

// ParseSkillNudgeInterval extracts skill_nudge_interval from other_config JSONB.
// Returns the interval (in tool calls) at which the loop injects a skill creation reminder.
// Default 15 when not set. Explicitly 0 disables mid-loop nudges (system prompt guidance still shown).
func (a *AgentData) ParseSkillNudgeInterval() int {
	if len(a.OtherConfig) == 0 {
		return 15
	}
	var cfg struct {
		SkillNudgeInterval *int `json:"skill_nudge_interval"`
	}
	if json.Unmarshal(a.OtherConfig, &cfg) != nil {
		return 15
	}
	if cfg.SkillNudgeInterval == nil {
		return 15
	}
	return *cfg.SkillNudgeInterval
}

// WorkspaceSharingConfig controls per-user workspace isolation.
// When shared_dm/shared_group is true, users share the base workspace directory
// instead of each getting an isolated subfolder.
type WorkspaceSharingConfig struct {
	SharedDM    bool     `json:"shared_dm"`
	SharedGroup bool     `json:"shared_group"`
	SharedUsers []string `json:"shared_users,omitempty"`
	ShareMemory bool     `json:"share_memory"`
}

// ParseWorkspaceSharing extracts workspace_sharing from other_config JSONB.
// Returns nil if not configured or all fields are default (isolation enabled).
func (a *AgentData) ParseWorkspaceSharing() *WorkspaceSharingConfig {
	if len(a.OtherConfig) == 0 {
		return nil
	}
	var cfg struct {
		WS *WorkspaceSharingConfig `json:"workspace_sharing"`
	}
	if json.Unmarshal(a.OtherConfig, &cfg) != nil || cfg.WS == nil {
		return nil
	}
	if !cfg.WS.SharedDM && !cfg.WS.SharedGroup && len(cfg.WS.SharedUsers) == 0 && !cfg.WS.ShareMemory {
		return nil
	}
	return cfg.WS
}

// ParseShellDenyGroups extracts shell_deny_groups from other_config JSONB.
// Returns nil if not configured (all defaults apply).
func (a *AgentData) ParseShellDenyGroups() map[string]bool {
	if len(a.OtherConfig) == 0 {
		return nil
	}
	var cfg struct {
		ShellDenyGroups map[string]bool `json:"shell_deny_groups"`
	}
	if json.Unmarshal(a.OtherConfig, &cfg) != nil || len(cfg.ShellDenyGroups) == 0 {
		return nil
	}
	return cfg.ShellDenyGroups
}

// AgentShareData represents an agent share grant.
type AgentShareData struct {
	BaseModel
	AgentID   uuid.UUID `json:"agent_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	GrantedBy string    `json:"granted_by"`
}

// AgentContextFileData represents an agent-level context file (SOUL.md, IDENTITY.md, etc).
type AgentContextFileData struct {
	AgentID  uuid.UUID `json:"agent_id"`
	FileName string    `json:"file_name"`
	Content  string    `json:"content"`
}

// UserContextFileData represents a per-user context file.
type UserContextFileData struct {
	AgentID  uuid.UUID `json:"agent_id"`
	UserID   string    `json:"user_id"`
	FileName string    `json:"file_name"`
	Content  string    `json:"content"`
}

// UserAgentOverrideData represents per-user agent overrides.
type UserAgentOverrideData struct {
	AgentID  uuid.UUID `json:"agent_id"`
	UserID   string    `json:"user_id"`
	Provider string    `json:"provider,omitempty"`
	Model    string    `json:"model,omitempty"`
}

// AgentStore manages agents and access control.
type AgentStore interface {
	Create(ctx context.Context, agent *AgentData) error
	GetByKey(ctx context.Context, agentKey string) (*AgentData, error)
	GetByID(ctx context.Context, id uuid.UUID) (*AgentData, error)
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, ownerID string) ([]AgentData, error)
	GetDefault(ctx context.Context) (*AgentData, error) // agent with is_default=true, or first available

	// Access control
	ShareAgent(ctx context.Context, agentID uuid.UUID, userID, role, grantedBy string) error
	RevokeShare(ctx context.Context, agentID uuid.UUID, userID string) error
	ListShares(ctx context.Context, agentID uuid.UUID) ([]AgentShareData, error)
	CanAccess(ctx context.Context, agentID uuid.UUID, userID string) (bool, string, error) // (allowed, role, err)
	ListAccessible(ctx context.Context, userID string) ([]AgentData, error)

	// Agent-level context files
	GetAgentContextFiles(ctx context.Context, agentID uuid.UUID) ([]AgentContextFileData, error)
	SetAgentContextFile(ctx context.Context, agentID uuid.UUID, fileName, content string) error

	// Per-user context files + overrides
	GetUserContextFiles(ctx context.Context, agentID uuid.UUID, userID string) ([]UserContextFileData, error)
	SetUserContextFile(ctx context.Context, agentID uuid.UUID, userID, fileName, content string) error
	DeleteUserContextFile(ctx context.Context, agentID uuid.UUID, userID, fileName string) error
	GetUserOverride(ctx context.Context, agentID uuid.UUID, userID string) (*UserAgentOverrideData, error)
	SetUserOverride(ctx context.Context, override *UserAgentOverrideData) error

	// User-agent profiles + instances
	GetOrCreateUserProfile(ctx context.Context, agentID uuid.UUID, userID, workspace, channel string) (isNew bool, effectiveWorkspace string, err error)
	EnsureUserProfile(ctx context.Context, agentID uuid.UUID, userID string) error
	ListUserInstances(ctx context.Context, agentID uuid.UUID) ([]UserInstanceData, error)
	UpdateUserProfileMetadata(ctx context.Context, agentID uuid.UUID, userID string, metadata map[string]string) error

	// Group file writers (allowlist for protected file edits in group chats)
	IsGroupFileWriter(ctx context.Context, agentID uuid.UUID, groupID, userID string) (bool, error)
	AddGroupFileWriter(ctx context.Context, agentID uuid.UUID, groupID, userID, displayName, username string) error
	RemoveGroupFileWriter(ctx context.Context, agentID uuid.UUID, groupID, userID string) error
	ListGroupFileWriters(ctx context.Context, agentID uuid.UUID, groupID string) ([]GroupFileWriterData, error)
	ListGroupFileWriterGroups(ctx context.Context, agentID uuid.UUID) ([]GroupWriterGroupInfo, error)
}

// UserInstanceData represents a user instance for a predefined agent.
type UserInstanceData struct {
	UserID      string            `json:"user_id"`
	FirstSeenAt *string           `json:"first_seen_at,omitempty"`
	LastSeenAt  *string           `json:"last_seen_at,omitempty"`
	FileCount   int               `json:"file_count"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// GroupFileWriterData represents a group file writer entry.
type GroupFileWriterData struct {
	UserID      string  `json:"user_id"`
	DisplayName *string `json:"display_name,omitempty"`
	Username    *string `json:"username,omitempty"`
}

// GroupWriterGroupInfo represents a group that has writers configured.
type GroupWriterGroupInfo struct {
	GroupID     string `json:"group_id"`
	WriterCount int    `json:"writer_count"`
}
