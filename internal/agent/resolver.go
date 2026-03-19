package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// ResolverDeps holds shared dependencies for the agent resolver.
type ResolverDeps struct {
	AgentStore     store.AgentStore
	ProviderReg    *providers.Registry
	Bus            bus.EventPublisher
	Sessions       store.SessionStore
	Tools          *tools.Registry
	ToolPolicy     *tools.PolicyEngine
	Skills         *skills.Loader
	HasMemory      bool
	OnEvent        func(AgentEvent)
	TraceCollector *tracing.Collector

	// Per-user file seeding + dynamic context loading
	EnsureUserFiles   EnsureUserFilesFunc
	ContextFileLoader ContextFileLoaderFunc
	BootstrapCleanup  BootstrapCleanupFunc

	// Security
	InjectionAction string // "log", "warn", "block", "off"
	MaxMessageChars int

	// Global defaults (from config.json) — per-agent DB overrides take priority
	CompactionCfg          *config.CompactionConfig
	ContextPruningCfg      *config.ContextPruningConfig
	SandboxEnabled         bool
	SandboxContainerDir    string
	SandboxWorkspaceAccess string

	// Dynamic custom tools
	DynamicLoader *tools.DynamicToolLoader

	// Inter-agent delegation
	AgentLinkStore store.AgentLinkStore

	// Agent teams
	TeamStore store.TeamStore
	DataDir   string // global workspace root for team workspace resolution

	// Secure CLI credential store for credentialed exec
	SecureCLIStore store.SecureCLIStore

	// Builtin tool settings
	BuiltinToolStore store.BuiltinToolStore

	// MCP server store — for per-agent MCP tool loading
	MCPStore store.MCPServerStore

	// Shared MCP connection pool — eliminates duplicate connections across agents
	MCPPool *mcpbridge.Pool

	// Skill access store — for per-agent skill visibility filtering
	SkillAccessStore store.SkillAccessStore

	// Config permission store for group file writer checks
	ConfigPermStore store.ConfigPermissionStore

	// Persistent media storage for cross-turn image/document access
	MediaStore *media.Store

	// Model pricing for cost tracking
	ModelPricing map[string]*config.ModelPricing

	// Tracing store for budget enforcement queries
	TracingStore store.TracingStore
}

// NewManagedResolver creates a ResolverFunc that builds Loops from DB agent data.
// Agents are defined in Postgres, not config.json.
func NewManagedResolver(deps ResolverDeps) ResolverFunc {
	return func(agentKey string) (Agent, error) {
		ctx := context.Background()

		// Support lookup by UUID (e.g. from cron jobs that store agent_id as UUID)
		var ag *store.AgentData
		var err error
		if id, parseErr := uuid.Parse(agentKey); parseErr == nil {
			ag, err = deps.AgentStore.GetByID(ctx, id)
		} else {
			ag, err = deps.AgentStore.GetByKey(ctx, agentKey)
		}
		if err != nil {
			return nil, fmt.Errorf("agent not found: %s", agentKey)
		}

		if ag.Status != store.AgentStatusActive {
			return nil, fmt.Errorf("agent %s is inactive", agentKey)
		}

		// Resolve provider
		provider, err := deps.ProviderReg.Get(ag.Provider)
		if err != nil {
			// Fallback to any available provider
			names := deps.ProviderReg.List()
			if len(names) == 0 {
				return nil, fmt.Errorf("no providers configured for agent %s", agentKey)
			}
			provider, _ = deps.ProviderReg.Get(names[0])
			slog.Warn("agent provider not found, using fallback",
				"agent", agentKey, "wanted", ag.Provider, "using", names[0])
			if tl := ag.ParseThinkingLevel(); tl != "" && tl != "off" {
				slog.Warn("agent thinking may not be supported by fallback provider",
					"agent", agentKey, "thinking_level", tl,
					"wanted_provider", ag.Provider, "fallback_provider", names[0])
			}
		}

		if provider == nil {
			return nil, fmt.Errorf("no provider available for agent %s", agentKey)
		}

		// Load bootstrap files from DB
		contextFiles := bootstrap.LoadFromStore(ctx, deps.AgentStore, ag.ID)

		// Inject TEAM.md for all team members (lead + members) so every agent
		// knows the team workflow: create/claim/complete tasks via team_tasks tool.
		hasTeam := false
		isTeamLead := false
		if deps.TeamStore != nil {
			hasTeamMD := false
			for _, cf := range contextFiles {
				if cf.Path == bootstrap.TeamFile {
					hasTeamMD = true
					break
				}
			}
			if !hasTeamMD {
				if team, err := deps.TeamStore.GetTeamForAgent(ctx, ag.ID); err == nil && team != nil {
					if members, err := deps.TeamStore.ListMembers(ctx, team.ID); err == nil {
						hasTeam = true
						contextFiles = append(contextFiles, bootstrap.ContextFile{
							Path:    bootstrap.TeamFile,
							Content: buildTeamMD(team, members, ag.ID, tools.IsTeamV2(team)),
						})
						// Detect lead role for tool policy
						for _, m := range members {
							if m.AgentID == ag.ID && m.Role == store.TeamRoleLead {
								isTeamLead = true
								break
							}
						}
					}
				}
			} else {
				hasTeam = true
			}
		}

		// Inject negative context so the model doesn't waste iterations probing
		// unavailable capabilities (team_tasks, etc.).
		if !hasTeam {
			contextFiles = append(contextFiles, bootstrap.ContextFile{
				Path:    bootstrap.AvailabilityFile,
				Content: "You are NOT part of any team. Do not use team_tasks or team_message tools.",
			})
		}

		contextWindow := ag.ContextWindow
		if contextWindow <= 0 {
			contextWindow = config.DefaultContextWindow
		}
		maxIter := ag.MaxToolIterations
		if maxIter <= 0 {
			maxIter = config.DefaultMaxIterations
		}

		// Per-agent config overrides (fallback to global defaults from config.json)
		compactionCfg := deps.CompactionCfg
		if c := ag.ParseCompactionConfig(); c != nil {
			compactionCfg = c
		}
		contextPruningCfg := deps.ContextPruningCfg
		if c := ag.ParseContextPruning(); c != nil {
			contextPruningCfg = c
		}
		sandboxEnabled := deps.SandboxEnabled
		sandboxContainerDir := deps.SandboxContainerDir
		sandboxWorkspaceAccess := deps.SandboxWorkspaceAccess
		var sandboxCfgOverride *sandbox.Config
		if c := ag.ParseSandboxConfig(); c != nil {
			resolved := c.ToSandboxConfig()
			sandboxContainerDir = resolved.ContainerWorkdir()
			sandboxWorkspaceAccess = string(resolved.WorkspaceAccess)
			sandboxCfgOverride = &resolved
		}

		// Expand ~ in workspace path and ensure directory exists
		workspace := ag.Workspace
		if workspace != "" {
			workspace = config.ExpandHome(workspace)
			if !filepath.IsAbs(workspace) {
				workspace, _ = filepath.Abs(workspace)
			}
			if err := os.MkdirAll(workspace, 0755); err != nil {
				slog.Warn("failed to create agent workspace directory", "workspace", workspace, "agent", agentKey, "error", err)
			}
		}

		// Per-agent custom tools (clone registry if agent has custom tools)
		toolsReg := deps.Tools
		if deps.DynamicLoader != nil {
			if agentReg, err := deps.DynamicLoader.LoadForAgent(ctx, deps.Tools, ag.ID); err != nil {
				slog.Warn("failed to load custom tools", "agent", agentKey, "error", err)
			} else if agentReg != nil {
				toolsReg = agentReg
			}
		}

		// Per-agent MCP servers: connect to granted MCP servers and register their tools.
		// Uses a per-agent MCP Manager that queries the MCPServerStore for accessible servers.
		//
		// IMPORTANT: Always clone the registry before MCP registration to prevent
		// cross-agent tool leaks. Without cloning, MCP BridgeTools registered for
		// one agent pollute the shared deps. Tools and become visible to ALL agents
		// (even those without MCP grants), because FilterTools reads from registry.List().
		hasMCPTools := false
		if deps.MCPStore != nil {
			if toolsReg == deps.Tools {
				toolsReg = deps.Tools.Clone()
			}
			var mcpOpts []mcpbridge.ManagerOption
		mcpOpts = append(mcpOpts, mcpbridge.WithStore(deps.MCPStore))
		if deps.MCPPool != nil {
			mcpOpts = append(mcpOpts, mcpbridge.WithPool(deps.MCPPool))
		}
		mcpMgr := mcpbridge.NewManager(toolsReg, mcpOpts...)
			if err := mcpMgr.LoadForAgent(ctx, ag.ID, ""); err != nil {
				slog.Warn("failed to load MCP servers for agent", "agent", agentKey, "error", err)
			} else if mcpMgr.IsSearchMode() {
				// Search mode: too many tools — register mcp_tool_search meta-tool.
				// Also wire lazy activator so deferred tools can be called by name directly.
				toolsReg.SetDeferredActivator(mcpMgr.ActivateToolIfDeferred)
				searchTool := mcpbridge.NewMCPToolSearchTool(mcpMgr)
				toolsReg.Register(searchTool)
				hasMCPTools = true
				slog.Info("mcp.agent.search_mode", "agent", agentKey,
					"deferred_tools", len(mcpMgr.DeferredToolInfos()))
			} else {
				toolNames := mcpMgr.ToolNames()
				if len(toolNames) > 0 {
					hasMCPTools = true
					slog.Info("mcp.agent.tools_loaded", "agent", agentKey, "tools", len(toolNames))
				}
			}
		}

		// Per-agent memory: enabled if global memory manager exists AND
		// per-agent config doesn't explicitly disable it.
		hasMemory := deps.HasMemory
		if mc := ag.ParseMemoryConfig(); mc != nil && mc.Enabled != nil {
			if !*mc.Enabled {
				hasMemory = false
			}
		}

		// Load global builtin tool settings from DB (for settings cascade)
		var builtinSettings tools.BuiltinToolSettings
		if deps.BuiltinToolStore != nil {
			if allTools, err := deps.BuiltinToolStore.List(ctx); err == nil {
				builtinSettings = make(tools.BuiltinToolSettings, len(allTools))
				for _, t := range allTools {
					if len(t.Settings) > 0 && string(t.Settings) != "{}" {
						builtinSettings[t.Name] = []byte(t.Settings)
					}
				}
			}
		}

		// Filter skills by visibility + agent grants.
		// Only public skills and explicitly granted internal skills appear in the system prompt.
		var skillAllowList []string
		if deps.SkillAccessStore != nil {
			if accessible, err := deps.SkillAccessStore.ListAccessible(ctx, ag.ID, ""); err == nil {
				skillAllowList = make([]string, 0, len(accessible))
				for _, sk := range accessible {
					skillAllowList = append(skillAllowList, sk.Slug)
				}
				slog.Debug("skill visibility filter", "agent", agentKey, "accessible", len(skillAllowList))
			} else {
				slog.Warn("failed to load accessible skills, falling back to all", "agent", agentKey, "error", err)
				// nil = fallback to all (better than blocking all skills)
			}
		}

		restrictVal := true // always restrict agents to their workspace
		loop := NewLoop(LoopConfig{
			ID:                     ag.AgentKey,
			AgentUUID:              ag.ID,
			AgentType:              ag.AgentType,
			Provider:               provider,
			Model:                  ag.Model,
			ContextWindow:          contextWindow,
			MaxTokens:              ag.ParseMaxTokens(),
			MaxIterations:          maxIter,
			Workspace:              workspace,
			DataDir:                deps.DataDir,
			RestrictToWs:           &restrictVal,
			SubagentsCfg:           ag.ParseSubagentsConfig(),
			MemoryCfg:              ag.ParseMemoryConfig(),
			SandboxCfg:             sandboxCfgOverride,
			Bus:                    deps.Bus,
			Sessions:               deps.Sessions,
			Tools:                  toolsReg,
			ToolPolicy:             deps.ToolPolicy,
			AgentToolPolicy:        agentToolPolicyForTeam(agentToolPolicyWithWorkspace(agentToolPolicyWithMCP(ag.ParseToolsConfig(), hasMCPTools), hasTeam), isTeamLead),
			SkillsLoader:           deps.Skills,
			SkillAllowList:         skillAllowList,
			HasMemory:              hasMemory,
			ContextFiles:           contextFiles,
			EnsureUserFiles:        deps.EnsureUserFiles,
			ContextFileLoader:      deps.ContextFileLoader,
			BootstrapCleanup:       deps.BootstrapCleanup,
			OnEvent:                deps.OnEvent,
			TraceCollector:         deps.TraceCollector,
			InjectionAction:        deps.InjectionAction,
			MaxMessageChars:        deps.MaxMessageChars,
			CompactionCfg:          compactionCfg,
			ContextPruningCfg:      contextPruningCfg,
			SandboxEnabled:         sandboxEnabled,
			SandboxContainerDir:    sandboxContainerDir,
			SandboxWorkspaceAccess: sandboxWorkspaceAccess,
			BuiltinToolSettings:    builtinSettings,
			ThinkingLevel:          ag.ParseThinkingLevel(),
			SelfEvolve:             ag.ParseSelfEvolve(),
			SkillEvolve:            ag.AgentType == store.AgentTypePredefined && ag.ParseSkillEvolve(),
			SkillNudgeInterval:     ag.ParseSkillNudgeInterval(),
			WorkspaceSharing:       ag.ParseWorkspaceSharing(),
			ShellDenyGroups:        ag.ParseShellDenyGroups(),
			ConfigPermStore:        deps.ConfigPermStore,
			TeamStore:              deps.TeamStore,
			SecureCLIStore:         deps.SecureCLIStore,
			MediaStore:             deps.MediaStore,
			ModelPricing:           deps.ModelPricing,
			BudgetMonthlyCents:     derefInt(ag.BudgetMonthlyCents),
			TracingStore:           deps.TracingStore,
		})

		slog.Info("resolved agent from DB", "agent", agentKey, "model", ag.Model, "provider", ag.Provider)
		return loop, nil
	}
}

// InvalidateAgent removes an agent from the router cache, forcing re-resolution.
// Used when agent config is updated via API.
func (r *Router) InvalidateAgent(agentKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, agentKey)
	slog.Debug("invalidated agent cache", "agent", agentKey)
}

// InvalidateAll clears the entire agent cache, forcing all agents to re-resolve.
// Used when global tools change (custom tools reload).
func (r *Router) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = make(map[string]*agentEntry)
	slog.Debug("invalidated all agent caches")
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

