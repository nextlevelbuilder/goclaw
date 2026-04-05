package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// sanitizeToolCallPrefix strips characters not in [a-z0-9_{}] from the prefix.
// This matches the UI-side regex and prevents injection via direct API calls.
func sanitizeToolCallPrefix(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '{' || r == '}' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Agent type constants.
const (
	AgentTypeOpen       = "open"       // per-user context files, seeded on first chat
	AgentTypePredefined = "predefined" // shared agent-level context files
)

// Agent status constants.
const (
	AgentStatusActive       = "active"
	AgentStatusInactive     = "inactive"
	AgentStatusSummoning    = "summoning"
	AgentStatusSummonFailed = "summon_failed"
)

// AgentData represents an agent in the database.
type AgentData struct {
	BaseModel
	TenantID            uuid.UUID `json:"tenant_id"`
	AgentKey            string    `json:"agent_key"`
	DisplayName         string    `json:"display_name,omitempty"`
	Frontmatter         string    `json:"frontmatter,omitempty"` // short expertise summary (NOT other_config.description which is the summoning prompt)
	OwnerID             string    `json:"owner_id"`
	Provider            string    `json:"provider"`
	Model               string    `json:"model"`
	ContextWindow       int       `json:"context_window"`
	MaxToolIterations   int       `json:"max_tool_iterations"`
	Workspace           string    `json:"workspace"`
	RestrictToWorkspace bool      `json:"restrict_to_workspace"`
	AgentType           string    `json:"agent_type"` // "open" or "predefined"
	IsDefault           bool      `json:"is_default"`
	Status              string    `json:"status"`

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
	// Backward compat: migrate old "toolPrefix" key to "toolCallPrefix"
	if c.ToolCallPrefix == "" {
		var raw map[string]json.RawMessage
		if json.Unmarshal(a.ToolsConfig, &raw) == nil {
			if v, ok := raw["toolPrefix"]; ok {
				var s string
				if json.Unmarshal(v, &s) == nil && s != "" {
					c.ToolCallPrefix = s
				}
			}
		}
	}
	// Sanitize: only allow [a-z0-9_{}] to prevent injection via API bypass.
	c.ToolCallPrefix = sanitizeToolCallPrefix(c.ToolCallPrefix)
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

// ParseThinkingLevel extracts the normalized reasoning effort from other_config JSONB.
// Missing config defaults to "off" to match the dashboard and docs.
func (a *AgentData) ParseThinkingLevel() string {
	return a.ParseReasoningConfig().Effort
}

// ParseReasoningConfig extracts additive advanced reasoning settings from other_config.
// Legacy thinking_level remains a backward-compatible fallback source.
func (a *AgentData) ParseReasoningConfig() AgentReasoningConfig {
	cfg := AgentReasoningConfig{
		OverrideMode: ReasoningOverrideInherit,
		Effort:       "off",
		Fallback:     ReasoningFallbackDowngrade,
		Source:       ReasoningSourceUnset,
	}
	if len(a.OtherConfig) == 0 {
		return cfg
	}

	var raw map[string]json.RawMessage
	if json.Unmarshal(a.OtherConfig, &raw) != nil {
		return cfg
	}

	var reasoning struct {
		OverrideMode string `json:"override_mode"`
		Effort       string `json:"effort"`
		Fallback     string `json:"fallback"`
	}
	explicitInherit := false
	if data, ok := raw["reasoning"]; ok && len(data) > 0 && json.Unmarshal(data, &reasoning) == nil {
		if reasoning.OverrideMode == ReasoningOverrideInherit {
			explicitInherit = true
			cfg.OverrideMode = ReasoningOverrideInherit
			cfg.Source = ReasoningSourceUnset
			cfg.Effort = "off"
			cfg.Fallback = ReasoningFallbackDowngrade
		} else {
			cfg.OverrideMode = ReasoningOverrideCustom
			cfg.Source = ReasoningSourceAdvanced
			if effort := normalizeReasoningEffort(reasoning.Effort); effort != "" {
				cfg.Effort = effort
			}
			cfg.Fallback = normalizeReasoningFallback(reasoning.Fallback)
		}
	}

	if !explicitInherit {
		if data, ok := raw["thinking_level"]; ok && len(data) > 0 {
			var legacy string
			if json.Unmarshal(data, &legacy) == nil {
				if effort := normalizeReasoningEffort(legacy); effort != "" {
					if cfg.Source == ReasoningSourceUnset {
						cfg.OverrideMode = ReasoningOverrideCustom
						cfg.Source = ReasoningSourceLegacy
						cfg.Effort = effort
					} else if cfg.Effort == "off" {
						cfg.Effort = effort
					}
				}
			}
		}
	}

	return cfg
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

// normalizeReasoningEffort delegates to providers.NormalizeReasoningEffort (DRY).
func normalizeReasoningEffort(value string) string {
	return providers.NormalizeReasoningEffort(value)
}

// normalizeReasoningFallback delegates to providers.NormalizeReasoningFallback (DRY).
func normalizeReasoningFallback(value string) string {
	return providers.NormalizeReasoningFallback(value)
}

// WorkspaceSharingConfig controls per-user workspace isolation.
// When shared_dm/shared_group is true, users share the base workspace directory
// instead of each getting an isolated subfolder.
type WorkspaceSharingConfig struct {
	SharedDM            bool     `json:"shared_dm"`
	SharedGroup         bool     `json:"shared_group"`
	SharedUsers         []string `json:"shared_users,omitempty"`
	ShareMemory         bool     `json:"share_memory"`
	ShareKnowledgeGraph bool     `json:"share_knowledge_graph"`
}

const (
	ReasoningSourceUnset             = "unset"
	ReasoningSourceLegacy            = "thinking_level"
	ReasoningSourceAdvanced          = "reasoning"
	ReasoningSourceProviderDefault   = "provider_default"
	// Reasoning fallback constants — canonical definitions in providers package.
	ReasoningFallbackDowngrade       = providers.ReasoningFallbackDowngrade
	ReasoningFallbackDisable         = providers.ReasoningFallbackDisable
	ReasoningFallbackProviderDefault = providers.ReasoningFallbackProviderDefault
	ReasoningOverrideInherit         = "inherit"
	ReasoningOverrideCustom          = "custom"
)

type AgentReasoningConfig struct {
	OverrideMode string `json:"override_mode,omitempty"`
	Effort       string `json:"effort,omitempty"`
	Fallback     string `json:"fallback,omitempty"`
	Source       string `json:"-"`
}

// ResolveEffectiveReasoningConfig applies provider-owned defaults unless the agent
// has an explicit custom reasoning override.
func ResolveEffectiveReasoningConfig(
	providerDefaults *ProviderReasoningConfig,
	agentConfig AgentReasoningConfig,
) AgentReasoningConfig {
	if agentConfig.OverrideMode == "" {
		agentConfig.OverrideMode = ReasoningOverrideInherit
	}
	if agentConfig.Fallback == "" {
		agentConfig.Fallback = ReasoningFallbackDowngrade
	}
	if agentConfig.Effort == "" {
		agentConfig.Effort = "off"
	}

	if agentConfig.OverrideMode == ReasoningOverrideCustom {
		return agentConfig
	}

	if providerDefaults == nil {
		return AgentReasoningConfig{
			OverrideMode: ReasoningOverrideInherit,
			Effort:       "off",
			Fallback:     ReasoningFallbackDowngrade,
			Source:       ReasoningSourceUnset,
		}
	}

	return AgentReasoningConfig{
		OverrideMode: ReasoningOverrideInherit,
		Effort:       providerDefaults.Effort,
		Fallback:     providerDefaults.Fallback,
		Source:       ReasoningSourceProviderDefault,
	}
}

const (
	ChatGPTOAuthStrategyManual       = "manual" // legacy alias
	ChatGPTOAuthStrategyPrimaryFirst = "primary_first"
	ChatGPTOAuthStrategyRoundRobin   = "round_robin"
	ChatGPTOAuthStrategyPriority     = "priority_order"
)

const (
	ChatGPTOAuthOverrideInherit = "inherit"
	ChatGPTOAuthOverrideCustom  = "custom"
)

// ChatGPTOAuthRoutingConfig controls optional multi-account selection for agents
// whose primary provider is a ChatGPT OAuth-backed provider.
type ChatGPTOAuthRoutingConfig struct {
	OverrideMode       string   `json:"override_mode,omitempty"`
	Strategy           string   `json:"strategy,omitempty"`
	ExtraProviderNames []string `json:"extra_provider_names,omitempty"`
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
	if !cfg.WS.SharedDM && !cfg.WS.SharedGroup && len(cfg.WS.SharedUsers) == 0 && !cfg.WS.ShareMemory && !cfg.WS.ShareKnowledgeGraph {
		return nil
	}
	return cfg.WS
}

// ParseChatGPTOAuthRouting extracts chatgpt_oauth_routing from other_config JSONB.
// Returns nil when no routing is configured.
func (a *AgentData) ParseChatGPTOAuthRouting() *ChatGPTOAuthRoutingConfig {
	if len(a.OtherConfig) == 0 {
		return nil
	}
	var cfg struct {
		Routing *ChatGPTOAuthRoutingConfig `json:"chatgpt_oauth_routing"`
	}
	if json.Unmarshal(a.OtherConfig, &cfg) != nil || cfg.Routing == nil {
		return nil
	}
	explicitOverrideMode := strings.TrimSpace(cfg.Routing.OverrideMode) != ""
	explicitStrategy := strings.TrimSpace(cfg.Routing.Strategy) != ""
	explicitExtras := cfg.Routing.ExtraProviderNames != nil
	routing := normalizeChatGPTOAuthRoutingConfig(cfg.Routing)
	if routing == nil {
		if !explicitOverrideMode && !explicitStrategy && !explicitExtras {
			return nil
		}
		overrideMode := ChatGPTOAuthOverrideCustom
		if explicitOverrideMode {
			overrideMode = normalizeChatGPTOAuthOverrideMode(cfg.Routing.OverrideMode)
		}
		return &ChatGPTOAuthRoutingConfig{
			OverrideMode:       overrideMode,
			Strategy:           normalizeChatGPTOAuthStrategy(cfg.Routing.Strategy),
			ExtraProviderNames: normalizeProviderNames(cfg.Routing.ExtraProviderNames),
		}
	}
	if explicitOverrideMode {
		return routing
	}
	if explicitStrategy || explicitExtras {
		routing.OverrideMode = ChatGPTOAuthOverrideCustom
		return routing
	}
	routing.OverrideMode = ""
	if routing.Strategy == ChatGPTOAuthStrategyPrimaryFirst && len(routing.ExtraProviderNames) == 0 {
		return nil
	}
	return routing
}

func normalizeChatGPTOAuthRoutingConfig(cfg *ChatGPTOAuthRoutingConfig) *ChatGPTOAuthRoutingConfig {
	if cfg == nil {
		return nil
	}
	routing := &ChatGPTOAuthRoutingConfig{
		OverrideMode:       normalizeChatGPTOAuthOverrideMode(cfg.OverrideMode),
		Strategy:           normalizeChatGPTOAuthStrategy(cfg.Strategy),
		ExtraProviderNames: normalizeProviderNames(cfg.ExtraProviderNames),
	}
	if routing.OverrideMode == "" && routing.Strategy == ChatGPTOAuthStrategyPrimaryFirst && len(routing.ExtraProviderNames) == 0 {
		return nil
	}
	return routing
}

func normalizeChatGPTOAuthOverrideMode(value string) string {
	switch value {
	case ChatGPTOAuthOverrideInherit:
		return ChatGPTOAuthOverrideInherit
	case "", ChatGPTOAuthOverrideCustom:
		return ChatGPTOAuthOverrideCustom
	default:
		return ChatGPTOAuthOverrideCustom
	}
}

func normalizeChatGPTOAuthStrategy(value string) string {
	switch value {
	case "", ChatGPTOAuthStrategyManual, ChatGPTOAuthStrategyPrimaryFirst:
		return ChatGPTOAuthStrategyPrimaryFirst
	case ChatGPTOAuthStrategyRoundRobin, ChatGPTOAuthStrategyPriority:
		return value
	default:
		return ChatGPTOAuthStrategyPrimaryFirst
	}
}

func CloneChatGPTOAuthRoutingConfig(cfg *ChatGPTOAuthRoutingConfig) *ChatGPTOAuthRoutingConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.ExtraProviderNames = append([]string(nil), cfg.ExtraProviderNames...)
	return &clone
}

func ResolveEffectiveChatGPTOAuthRouting(defaults, agentRouting *ChatGPTOAuthRoutingConfig) *ChatGPTOAuthRoutingConfig {
	normalizedDefaults := normalizeChatGPTOAuthRoutingConfig(defaults)
	normalizedAgent := normalizeChatGPTOAuthRoutingConfig(agentRouting)
	if normalizedAgent == nil {
		return CloneChatGPTOAuthRoutingConfig(normalizedDefaults)
	}
	if normalizedAgent.OverrideMode == ChatGPTOAuthOverrideInherit {
		return CloneChatGPTOAuthRoutingConfig(normalizedDefaults)
	}
	effective := CloneChatGPTOAuthRoutingConfig(normalizedAgent)
	if effective == nil {
		return nil
	}
	effective.OverrideMode = ""
	if normalizedDefaults != nil && len(normalizedDefaults.ExtraProviderNames) > 0 {
		if effective.Strategy == ChatGPTOAuthStrategyPrimaryFirst &&
			len(normalizedAgent.ExtraProviderNames) == 0 {
			effective.ExtraProviderNames = nil
		} else {
			effective.ExtraProviderNames = append([]string(nil), normalizedDefaults.ExtraProviderNames...)
		}
	}
	if effective.Strategy == ChatGPTOAuthStrategyPrimaryFirst &&
		len(effective.ExtraProviderNames) == 0 &&
		normalizedAgent.OverrideMode != ChatGPTOAuthOverrideCustom {
		return nil
	}
	return effective
}

func normalizeProviderNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

// AgentCRUDStore manages core agent CRUD operations.
type AgentCRUDStore interface {
	Create(ctx context.Context, agent *AgentData) error
	GetByKey(ctx context.Context, agentKey string) (*AgentData, error)
	GetByID(ctx context.Context, id uuid.UUID) (*AgentData, error)
	GetByIDUnscoped(ctx context.Context, id uuid.UUID) (*AgentData, error)
	GetByKeys(ctx context.Context, keys []string) ([]AgentData, error)
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]AgentData, error)
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, ownerID string) ([]AgentData, error)
	GetDefault(ctx context.Context) (*AgentData, error) // agent with is_default=true, or first available
}

// AgentAccessStore manages agent sharing and access control.
type AgentAccessStore interface {
	ShareAgent(ctx context.Context, agentID uuid.UUID, userID, role, grantedBy string) error
	RevokeShare(ctx context.Context, agentID uuid.UUID, userID string) error
	ListShares(ctx context.Context, agentID uuid.UUID) ([]AgentShareData, error)
	CanAccess(ctx context.Context, agentID uuid.UUID, userID string) (bool, string, error) // (allowed, role, err)
	ListAccessible(ctx context.Context, userID string) ([]AgentData, error)
}

// AgentContextStore manages agent-level and per-user context files and overrides.
type AgentContextStore interface {
	GetAgentContextFiles(ctx context.Context, agentID uuid.UUID) ([]AgentContextFileData, error)
	SetAgentContextFile(ctx context.Context, agentID uuid.UUID, fileName, content string) error
	PropagateContextFile(ctx context.Context, agentID uuid.UUID, fileName string) (int, error)
	GetUserContextFiles(ctx context.Context, agentID uuid.UUID, userID string) ([]UserContextFileData, error)
	// ListUserContextFilesByName returns all per-user copies of fileName across all users of agentID.
	// Used for bulk targeted updates (e.g. updating Name: in IDENTITY.md on agent rename).
	ListUserContextFilesByName(ctx context.Context, agentID uuid.UUID, fileName string) ([]UserContextFileData, error)
	SetUserContextFile(ctx context.Context, agentID uuid.UUID, userID, fileName, content string) error
	DeleteUserContextFile(ctx context.Context, agentID uuid.UUID, userID, fileName string) error
	// MigrateUserDataOnMerge moves per-user data from oldUserIDs to newUserID when contacts are merged.
	// Covers: user_context_files, user_agent_overrides, user_agent_profiles, memory_documents/chunks.
	// On conflict, keeps the newest by updated_at. Best-effort per table.
	MigrateUserDataOnMerge(ctx context.Context, oldUserIDs []string, newUserID string) error
	GetUserOverride(ctx context.Context, agentID uuid.UUID, userID string) (*UserAgentOverrideData, error)
	SetUserOverride(ctx context.Context, override *UserAgentOverrideData) error
}

// AgentProfileStore manages user-agent profiles and instances.
type AgentProfileStore interface {
	GetOrCreateUserProfile(ctx context.Context, agentID uuid.UUID, userID, workspace, channel string) (isNew bool, effectiveWorkspace string, err error)
	EnsureUserProfile(ctx context.Context, agentID uuid.UUID, userID string) error
	ListUserInstances(ctx context.Context, agentID uuid.UUID) ([]UserInstanceData, error)
	UpdateUserProfileMetadata(ctx context.Context, agentID uuid.UUID, userID string, metadata map[string]string) error
}

// AgentStore composes all agent sub-interfaces for backward compatibility.
// New code should depend on the specific sub-interface it needs.
type AgentStore interface {
	AgentCRUDStore
	AgentAccessStore
	AgentContextStore
	AgentProfileStore
}

// UserInstanceData represents a user instance for a predefined agent.
type UserInstanceData struct {
	UserID      string            `json:"user_id"`
	FirstSeenAt *string           `json:"first_seen_at,omitempty"`
	LastSeenAt  *string           `json:"last_seen_at,omitempty"`
	FileCount   int               `json:"file_count"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}
