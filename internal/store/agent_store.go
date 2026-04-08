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
	TenantID            uuid.UUID `json:"tenant_id" db:"tenant_id"`
	AgentKey            string    `json:"agent_key" db:"agent_key"`
	DisplayName         string    `json:"display_name,omitempty" db:"display_name"`
	Frontmatter         string    `json:"frontmatter,omitempty" db:"frontmatter"` // short expertise summary (NOT other_config.description which is the summoning prompt)
	OwnerID             string    `json:"owner_id" db:"owner_id"`
	Provider            string    `json:"provider" db:"provider"`
	Model               string    `json:"model" db:"model"`
	ContextWindow       int       `json:"context_window" db:"context_window"`
	MaxToolIterations   int       `json:"max_tool_iterations" db:"max_tool_iterations"`
	Workspace           string    `json:"workspace" db:"workspace"`
	RestrictToWorkspace bool      `json:"restrict_to_workspace" db:"restrict_to_workspace"`
	AgentType           string    `json:"agent_type" db:"agent_type"` // "open" or "predefined"
	IsDefault           bool      `json:"is_default" db:"is_default"`
	Status              string    `json:"status" db:"status"`

	// Budget: optional monthly spending limit in cents (nil = unlimited)
	BudgetMonthlyCents *int `json:"budget_monthly_cents,omitempty" db:"budget_monthly_cents"`

	// Per-agent JSONB config (nullable — nil means "use global defaults")
	ToolsConfig      json.RawMessage `json:"tools_config,omitempty" db:"tools_config"`
	SandboxConfig    json.RawMessage `json:"sandbox_config,omitempty" db:"sandbox_config"`
	SubagentsConfig  json.RawMessage `json:"subagents_config,omitempty" db:"subagents_config"`
	MemoryConfig     json.RawMessage `json:"memory_config,omitempty" db:"memory_config"`
	CompactionConfig json.RawMessage `json:"compaction_config,omitempty" db:"compaction_config"`
	ContextPruning   json.RawMessage `json:"context_pruning,omitempty" db:"context_pruning"`
	OtherConfig      json.RawMessage `json:"other_config,omitempty" db:"other_config"` // extensibility bag for future fields

	// Promoted from other_config (migration 000037 v3)
	Emoji               string          `json:"emoji" db:"emoji"`
	AgentDescription    string          `json:"agent_description" db:"agent_description"`
	ThinkingLevel       string          `json:"thinking_level" db:"thinking_level"`
	MaxTokens           int             `json:"max_tokens" db:"max_tokens"`
	SelfEvolve          bool            `json:"self_evolve" db:"self_evolve"`
	SkillEvolve         bool            `json:"skill_evolve" db:"skill_evolve"`
	SkillNudgeInterval  int             `json:"skill_nudge_interval" db:"skill_nudge_interval"`
	ReasoningConfig     json.RawMessage `json:"reasoning_config,omitempty" db:"reasoning_config"`
	WorkspaceSharing    json.RawMessage `json:"workspace_sharing,omitempty" db:"workspace_sharing"`
	ChatGPTOAuthRouting json.RawMessage `json:"chatgpt_oauth_routing,omitempty" db:"chatgpt_oauth_routing"`
	ShellDenyGroups     json.RawMessage `json:"shell_deny_groups,omitempty" db:"shell_deny_groups"`
	KGDedupConfig       json.RawMessage `json:"kg_dedup_config,omitempty" db:"kg_dedup_config"`
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

// ParseReasoningConfig reads advanced reasoning settings from the dedicated
// reasoning_config column with ThinkingLevel as legacy fallback.
func (a *AgentData) ParseReasoningConfig() AgentReasoningConfig {
	cfg := AgentReasoningConfig{
		OverrideMode: ReasoningOverrideInherit,
		Effort:       "off",
		Fallback:     ReasoningFallbackDowngrade,
		Source:       ReasoningSourceUnset,
	}

	var reasoning struct {
		OverrideMode string `json:"override_mode"`
		Effort       string `json:"effort"`
		Fallback     string `json:"fallback"`
	}
	explicitInherit := false
	if len(a.ReasoningConfig) > 2 && json.Unmarshal(a.ReasoningConfig, &reasoning) == nil {
		if reasoning.OverrideMode == ReasoningOverrideInherit {
			explicitInherit = true
		} else {
			cfg.OverrideMode = ReasoningOverrideCustom
			cfg.Source = ReasoningSourceAdvanced
			if effort := normalizeReasoningEffort(reasoning.Effort); effort != "" {
				cfg.Effort = effort
			}
			cfg.Fallback = normalizeReasoningFallback(reasoning.Fallback)
		}
	}

	if !explicitInherit && a.ThinkingLevel != "" {
		if effort := normalizeReasoningEffort(a.ThinkingLevel); effort != "" {
			if cfg.Source == ReasoningSourceUnset {
				cfg.OverrideMode = ReasoningOverrideCustom
				cfg.Source = ReasoningSourceLegacy
				cfg.Effort = effort
			} else if cfg.Effort == "off" {
				cfg.Effort = effort
			}
		}
	}

	return cfg
}

// ParseMaxTokens returns per-agent max_tokens. 0 means use provider default.
func (a *AgentData) ParseMaxTokens() int { return a.MaxTokens }

// ParseSelfEvolve returns whether predefined agents can update their SOUL.md through chat.
func (a *AgentData) ParseSelfEvolve() bool { return a.SelfEvolve }

// ParseSkillEvolve returns whether the agent's skill learning loop is enabled.
func (a *AgentData) ParseSkillEvolve() bool { return a.SkillEvolve }

// validPromptModes is the set of allowed prompt_mode values.
var validPromptModes = map[string]bool{
	"full": true, "task": true, "minimal": true, "none": true,
}

// ParsePromptMode returns the configured prompt mode from OtherConfig JSONB.
// Returns "" (defaults to "full") if not set or invalid.
func (a *AgentData) ParsePromptMode() string {
	if len(a.OtherConfig) == 0 {
		return ""
	}
	var bag map[string]json.RawMessage
	if json.Unmarshal(a.OtherConfig, &bag) != nil {
		return ""
	}
	raw, ok := bag["prompt_mode"]
	if !ok {
		return ""
	}
	var mode string
	if json.Unmarshal(raw, &mode) != nil {
		return ""
	}
	if !validPromptModes[mode] {
		return "" // invalid mode → default to full
	}
	return mode
}

// ParsePinnedSkills returns per-agent pinned skill names from OtherConfig JSONB.
// Max 10 enforced. Returns nil if not set.
func (a *AgentData) ParsePinnedSkills() []string {
	if len(a.OtherConfig) == 0 {
		return nil
	}
	var bag map[string]json.RawMessage
	if json.Unmarshal(a.OtherConfig, &bag) != nil {
		return nil
	}
	raw, ok := bag["pinned_skills"]
	if !ok {
		return nil
	}
	var names []string
	if json.Unmarshal(raw, &names) != nil {
		return nil
	}
	// Filter empty strings
	var result []string
	for _, n := range names {
		if n != "" {
			result = append(result, n)
		}
	}
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

// ParseSkillNudgeInterval returns the tool-call interval for skill creation reminders.
// Returns 15 (default) when column is 0 (unset).
func (a *AgentData) ParseSkillNudgeInterval() int {
	if a.SkillNudgeInterval <= 0 {
		return 15
	}
	return a.SkillNudgeInterval
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
	SharedDM            bool     `json:"shared_dm" db:"-"`
	SharedGroup         bool     `json:"shared_group" db:"-"`
	SharedUsers         []string `json:"shared_users,omitempty" db:"-"`
	ShareMemory         bool     `json:"share_memory" db:"-"`
	ShareKnowledgeGraph bool     `json:"share_knowledge_graph" db:"-"`
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
	OverrideMode string `json:"override_mode,omitempty" db:"-"`
	Effort       string `json:"effort,omitempty" db:"-"`
	Fallback     string `json:"fallback,omitempty" db:"-"`
	Source       string `json:"-" db:"-"`
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
	OverrideMode       string   `json:"override_mode,omitempty" db:"-"`
	Strategy           string   `json:"strategy,omitempty" db:"-"`
	ExtraProviderNames []string `json:"extra_provider_names,omitempty" db:"-"`
}

// ParseWorkspaceSharing reads workspace sharing config from the dedicated column.
// Returns nil if not configured or all fields are default (isolation enabled).
func (a *AgentData) ParseWorkspaceSharing() *WorkspaceSharingConfig {
	if len(a.WorkspaceSharing) <= 2 {
		return nil
	}
	var ws WorkspaceSharingConfig
	if json.Unmarshal(a.WorkspaceSharing, &ws) != nil {
		return nil
	}
	if !ws.SharedDM && !ws.SharedGroup && len(ws.SharedUsers) == 0 && !ws.ShareMemory && !ws.ShareKnowledgeGraph {
		return nil
	}
	return &ws
}

// ParseChatGPTOAuthRouting reads chatgpt_oauth_routing from the dedicated column.
// Returns nil when no routing is configured.
func (a *AgentData) ParseChatGPTOAuthRouting() *ChatGPTOAuthRoutingConfig {
	if len(a.ChatGPTOAuthRouting) <= 2 {
		return nil
	}
	var raw ChatGPTOAuthRoutingConfig
	if json.Unmarshal(a.ChatGPTOAuthRouting, &raw) != nil {
		return nil
	}
	explicitOverrideMode := strings.TrimSpace(raw.OverrideMode) != ""
	explicitStrategy := strings.TrimSpace(raw.Strategy) != ""
	explicitExtras := raw.ExtraProviderNames != nil
	routing := normalizeChatGPTOAuthRoutingConfig(&raw)
	if routing == nil {
		if !explicitOverrideMode && !explicitStrategy && !explicitExtras {
			return nil
		}
		overrideMode := ChatGPTOAuthOverrideCustom
		if explicitOverrideMode {
			overrideMode = normalizeChatGPTOAuthOverrideMode(raw.OverrideMode)
		}
		return &ChatGPTOAuthRoutingConfig{
			OverrideMode:       overrideMode,
			Strategy:           normalizeChatGPTOAuthStrategy(raw.Strategy),
			ExtraProviderNames: normalizeProviderNames(raw.ExtraProviderNames),
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

// ParseShellDenyGroups reads shell deny group toggles from the dedicated column.
// Returns nil if not configured (all defaults apply).
func (a *AgentData) ParseShellDenyGroups() map[string]bool {
	if len(a.ShellDenyGroups) <= 2 {
		return nil
	}
	var groups map[string]bool
	if json.Unmarshal(a.ShellDenyGroups, &groups) != nil || len(groups) == 0 {
		return nil
	}
	return groups
}

// AgentShareData represents an agent share grant.
type AgentShareData struct {
	BaseModel
	AgentID   uuid.UUID `json:"agent_id" db:"agent_id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Role      string    `json:"role" db:"role"`
	GrantedBy string    `json:"granted_by" db:"granted_by"`
}

// AgentContextFileData represents an agent-level context file (SOUL.md, IDENTITY.md, etc).
type AgentContextFileData struct {
	AgentID  uuid.UUID `json:"agent_id" db:"agent_id"`
	FileName string    `json:"file_name" db:"file_name"`
	Content  string    `json:"content" db:"content"`
}

// UserContextFileData represents a per-user context file.
type UserContextFileData struct {
	AgentID  uuid.UUID `json:"agent_id" db:"agent_id"`
	UserID   string    `json:"user_id" db:"user_id"`
	FileName string    `json:"file_name" db:"file_name"`
	Content  string    `json:"content" db:"content"`
}

// UserAgentOverrideData represents per-user agent overrides.
type UserAgentOverrideData struct {
	AgentID  uuid.UUID `json:"agent_id" db:"agent_id"`
	UserID   string    `json:"user_id" db:"user_id"`
	Provider string    `json:"provider,omitempty" db:"provider"`
	Model    string    `json:"model,omitempty" db:"model"`
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
	UserID      string            `json:"user_id" db:"user_id"`
	FirstSeenAt *string           `json:"first_seen_at,omitempty" db:"first_seen_at"`
	LastSeenAt  *string           `json:"last_seen_at,omitempty" db:"last_seen_at"`
	FileCount   int               `json:"file_count" db:"file_count"`
	Metadata    map[string]string `json:"metadata,omitempty" db:"-"`
}
