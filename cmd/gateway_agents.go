package cmd

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/heartbeat"
	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// createAgentLoop creates and registers an agent Loop for the given agent ID.
// Works for "default" and any agent in agents.list.
func createAgentLoop(agentID string, cfg *config.Config, router *agent.Router, providerReg *providers.Registry, msgBus *bus.MessageBus, sess store.SessionStore, toolsReg *tools.Registry, toolPE *tools.PolicyEngine, contextFiles []bootstrap.ContextFile, skillsLoader *skills.Loader, hasMemory bool, sandboxMgr sandbox.Manager, agentStore store.AgentStore, ensureUserFiles agent.EnsureUserFilesFunc, contextFileLoader agent.ContextFileLoaderFunc) error {
	agentCfg := cfg.ResolveAgent(agentID)

	provider, err := providerReg.Get(agentCfg.Provider)
	if err != nil {
		// Fallback: try any available provider
		names := providerReg.List()
		if len(names) == 0 {
			slog.Warn("no providers configured, agent will fail on first LLM call", "agent", agentID)
			return nil
		}
		provider, _ = providerReg.Get(names[0])
		slog.Warn("configured provider not found, using fallback", "agent", agentID, "wanted", agentCfg.Provider, "using", names[0])
	}

	if provider == nil {
		slog.Warn("no provider available for agent", "agent", agentID)
		return nil
	}

	workspace := config.ExpandHome(agentCfg.Workspace)
	if !filepath.IsAbs(workspace) {
		workspace, _ = filepath.Abs(workspace)
	}

	// Resolve sandbox info for system prompt
	sandboxEnabled := sandboxMgr != nil
	sandboxContainerDir := ""
	sandboxWorkspaceAccess := ""
	if sandboxEnabled {
		sbCfg := agentCfg.Sandbox
		if sbCfg == nil {
			sbCfg = cfg.Agents.Defaults.Sandbox
		}
		if sbCfg != nil {
			resolved := sbCfg.ToSandboxConfig()
			sandboxContainerDir = resolved.ContainerWorkdir()
			sandboxWorkspaceAccess = string(resolved.WorkspaceAccess)
		}
	}

	// Per-agent skill allowlist.
	// AgentSpec.Skills: nil = all skills, [] = none, ["x","y"] = only those.
	var skillAllowList []string
	var agentToolPolicy *config.ToolPolicySpec
	if spec, ok := cfg.Agents.List[agentID]; ok {
		skillAllowList = spec.Skills
		agentToolPolicy = spec.Tools
	}

	// Resolve AgentUUID and AgentType from store (standalone mode with FileAgentStore)
	var agentUUID uuid.UUID
	var agentType string
	if agentStore != nil {
		if ad, err := agentStore.GetByKey(context.Background(), agentID); err == nil {
			agentUUID = ad.ID
			agentType = ad.AgentType
		}
	}

	loop := agent.NewLoop(agent.LoopConfig{
		ID:             agentID,
		AgentUUID:      agentUUID,
		AgentType:      agentType,
		Provider:       provider,
		Model:          agentCfg.Model,
		ContextWindow:  agentCfg.ContextWindow,
		MaxIterations:  agentCfg.MaxToolIterations,
		Workspace:      workspace,
		Bus:            msgBus,
		Sessions:       sess,
		Tools:          toolsReg,
		ToolPolicy:      toolPE,
		AgentToolPolicy: agentToolPolicy,
		OwnerIDs:       cfg.Gateway.OwnerIDs,
		SkillsLoader:   skillsLoader,
		SkillAllowList: skillAllowList,
		HasMemory:      hasMemory,
		ContextFiles:      contextFiles,
		EnsureUserFiles:   ensureUserFiles,
		ContextFileLoader: contextFileLoader,
		BootstrapCleanup:  buildBootstrapCleanup(agentStore),
		CompactionCfg:      cfg.Agents.Defaults.Compaction,
		ContextPruningCfg:  cfg.Agents.Defaults.ContextPruning,
		SandboxEnabled:         sandboxEnabled,
		SandboxContainerDir:    sandboxContainerDir,
		SandboxWorkspaceAccess: sandboxWorkspaceAccess,
		InjectionAction:        cfg.Gateway.InjectionAction,
		MaxMessageChars:        cfg.Gateway.MaxMessageChars,
		OnEvent: func(event agent.AgentEvent) {
			msgBus.Broadcast(bus.Event{
				Name:    protocol.EventAgent,
				Payload: event,
			})
		},
	})

	router.Register(loop)
	slog.Info("created agent", "agent", agentID, "model", agentCfg.Model, "provider", agentCfg.Provider)
	return nil
}

func setupMemory(workspace string, appCfg *config.Config) *memory.Manager {
	memCfg := appCfg.Agents.Defaults.Memory

	// Check if explicitly disabled
	if memCfg != nil && memCfg.Enabled != nil && !*memCfg.Enabled {
		slog.Info("memory system disabled by config")
		return nil
	}

	mgrCfg := memory.DefaultManagerConfig(workspace)

	// Apply config overrides
	if memCfg != nil {
		if memCfg.MaxResults > 0 {
			mgrCfg.MaxResults = memCfg.MaxResults
		}
		if memCfg.MaxChunkLen > 0 {
			mgrCfg.MaxChunkLen = memCfg.MaxChunkLen
		}
		if memCfg.VectorWeight > 0 {
			mgrCfg.VectorWeight = memCfg.VectorWeight
		}
		if memCfg.TextWeight > 0 {
			mgrCfg.TextWeight = memCfg.TextWeight
		}
	}

	mgr, err := memory.NewManager(mgrCfg)
	if err != nil {
		slog.Warn("memory system unavailable", "error", err)
		return nil
	}

	// Auto-wire embedding provider (matching TS priority: openai → openrouter → gemini)
	provider := resolveEmbeddingProvider(appCfg, memCfg)
	if provider != nil {
		mgr.SetEmbeddingProvider(provider)
		slog.Info("memory embeddings enabled", "provider", provider.Name(), "model", provider.Model())
	} else {
		slog.Info("memory embeddings disabled (no API key), FTS-only mode")
	}

	// Index existing memory files on startup
	ctx := context.Background()
	if err := mgr.IndexAll(ctx); err != nil {
		slog.Warn("memory initial indexing failed", "error", err)
	}

	// Start file watcher for auto re-indexing on changes
	// (matching TS chokidar watcher with 1500ms debounce)
	if err := mgr.StartWatcher(ctx); err != nil {
		slog.Warn("memory file watcher unavailable", "error", err)
	}

	return mgr
}

// resolveEmbeddingProvider auto-selects an embedding provider based on config and available API keys.
// Matching TS embedding provider auto-selection order.
func resolveEmbeddingProvider(cfg *config.Config, memCfg *config.MemoryConfig) memory.EmbeddingProvider {
	// Explicit provider in config
	if memCfg != nil && memCfg.EmbeddingProvider != "" {
		return createEmbeddingProvider(memCfg.EmbeddingProvider, cfg, memCfg)
	}

	// Auto-select: openai → openrouter → gemini
	for _, name := range []string{"openai", "openrouter", "gemini"} {
		if p := createEmbeddingProvider(name, cfg, memCfg); p != nil {
			return p
		}
	}
	return nil
}

func createEmbeddingProvider(name string, cfg *config.Config, memCfg *config.MemoryConfig) memory.EmbeddingProvider {
	model := "text-embedding-3-small"
	apiBase := ""
	if memCfg != nil {
		if memCfg.EmbeddingModel != "" {
			model = memCfg.EmbeddingModel
		}
		if memCfg.EmbeddingAPIBase != "" {
			apiBase = memCfg.EmbeddingAPIBase
		}
	}

	switch name {
	case "openai":
		if cfg.Providers.OpenAI.APIKey == "" {
			return nil
		}
		if apiBase == "" {
			apiBase = "https://api.openai.com/v1"
		}
		return memory.NewOpenAIEmbeddingProvider("openai", cfg.Providers.OpenAI.APIKey, apiBase, model)
	case "openrouter":
		if cfg.Providers.OpenRouter.APIKey == "" {
			return nil
		}
		// OpenRouter requires provider prefix: "openai/text-embedding-3-small"
		orModel := model
		if !strings.Contains(orModel, "/") {
			orModel = "openai/" + orModel
		}
		return memory.NewOpenAIEmbeddingProvider("openrouter", cfg.Providers.OpenRouter.APIKey, "https://openrouter.ai/api/v1", orModel)
	case "gemini":
		if cfg.Providers.Gemini.APIKey == "" {
			return nil
		}
		geminiModel := "gemini-embedding-001"
		if memCfg != nil && memCfg.EmbeddingModel != "" {
			geminiModel = memCfg.EmbeddingModel
		}
		return memory.NewOpenAIEmbeddingProvider("gemini", cfg.Providers.Gemini.APIKey, "https://generativelanguage.googleapis.com/v1beta/openai", geminiModel).
			WithDimensions(1536)
	}
	return nil
}

func setupSubagents(providerReg *providers.Registry, cfg *config.Config, msgBus *bus.MessageBus, toolsReg *tools.Registry, workspace string, sandboxMgr sandbox.Manager) *tools.SubagentManager {
	names := providerReg.List()
	if len(names) == 0 {
		return nil
	}

	agentCfg := cfg.ResolveAgent("default")
	provider, err := providerReg.Get(agentCfg.Provider)
	if err != nil {
		provider, _ = providerReg.Get(names[0])
	}
	if provider == nil {
		return nil
	}

	subCfg := tools.DefaultSubagentConfig()

	// Apply config file overrides if present (matching TS agents.defaults.subagents).
	if sc := agentCfg.Subagents; sc != nil {
		if sc.MaxConcurrent > 0 {
			subCfg.MaxConcurrent = sc.MaxConcurrent
		}
		if sc.MaxSpawnDepth > 0 {
			subCfg.MaxSpawnDepth = min(sc.MaxSpawnDepth, 5) // TS: max 5
		}
		if sc.MaxChildrenPerAgent > 0 {
			subCfg.MaxChildrenPerAgent = min(sc.MaxChildrenPerAgent, 20) // TS: max 20
		}
		if sc.ArchiveAfterMinutes > 0 {
			subCfg.ArchiveAfterMinutes = sc.ArchiveAfterMinutes
		}
		if sc.Model != "" {
			subCfg.Model = sc.Model
		}
	}

	// Tool factory: clone parent registry (inherits web_fetch, web_search, browser, MCP tools, etc.)
	// then override file/exec tools with workspace-scoped versions.
	// NOTE: SubagentManager.applyDenyList() handles deny lists after createTools(),
	// so we don't apply deny lists here.
	toolsFactory := func() *tools.Registry {
		reg := toolsReg.Clone()
		if sandboxMgr != nil {
			reg.Register(tools.NewSandboxedReadFileTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
			reg.Register(tools.NewSandboxedWriteFileTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
			reg.Register(tools.NewSandboxedListFilesTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
			reg.Register(tools.NewSandboxedExecTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
		} else {
			reg.Register(tools.NewReadFileTool(workspace, agentCfg.RestrictToWorkspace))
			reg.Register(tools.NewWriteFileTool(workspace, agentCfg.RestrictToWorkspace))
			reg.Register(tools.NewListFilesTool(workspace, agentCfg.RestrictToWorkspace))
			reg.Register(tools.NewExecTool(workspace, agentCfg.RestrictToWorkspace))
		}
		return reg
	}

	return tools.NewSubagentManager(provider, agentCfg.Model, msgBus, toolsFactory, subCfg)
}

// setupTTS creates the TTS manager from config and registers providers.
// Returns nil if no TTS provider has an API key configured.
func setupTTS(cfg *config.Config) *tts.Manager {
	ttsCfg := cfg.Tts

	mgr := tts.NewManager(tts.ManagerConfig{
		Primary:   ttsCfg.Provider,
		Auto:      tts.AutoMode(ttsCfg.Auto),
		Mode:      tts.Mode(ttsCfg.Mode),
		MaxLength: ttsCfg.MaxLength,
		TimeoutMs: ttsCfg.TimeoutMs,
	})

	// Register providers that have API keys configured
	if key := ttsCfg.OpenAI.APIKey; key != "" {
		mgr.RegisterProvider(tts.NewOpenAIProvider(tts.OpenAIConfig{
			APIKey:    key,
			APIBase:   ttsCfg.OpenAI.APIBase,
			Model:     ttsCfg.OpenAI.Model,
			Voice:     ttsCfg.OpenAI.Voice,
			TimeoutMs: ttsCfg.TimeoutMs,
		}))
	}

	if key := ttsCfg.ElevenLabs.APIKey; key != "" {
		mgr.RegisterProvider(tts.NewElevenLabsProvider(tts.ElevenLabsConfig{
			APIKey:    key,
			BaseURL:   ttsCfg.ElevenLabs.BaseURL,
			VoiceID:   ttsCfg.ElevenLabs.VoiceID,
			ModelID:   ttsCfg.ElevenLabs.ModelID,
			TimeoutMs: ttsCfg.TimeoutMs,
		}))
	}

	if ttsCfg.Edge.Enabled {
		mgr.RegisterProvider(tts.NewEdgeProvider(tts.EdgeConfig{
			Voice:     ttsCfg.Edge.Voice,
			Rate:      ttsCfg.Edge.Rate,
			TimeoutMs: ttsCfg.TimeoutMs,
		}))
	}

	if key := ttsCfg.MiniMax.APIKey; key != "" {
		mgr.RegisterProvider(tts.NewMiniMaxProvider(tts.MiniMaxConfig{
			APIKey:    key,
			GroupID:   ttsCfg.MiniMax.GroupID,
			APIBase:   ttsCfg.MiniMax.APIBase,
			Model:     ttsCfg.MiniMax.Model,
			VoiceID:   ttsCfg.MiniMax.VoiceID,
			TimeoutMs: ttsCfg.TimeoutMs,
		}))
	}

	if !mgr.HasProviders() {
		return nil
	}

	return mgr
}

// setupHeartbeat creates and configures the heartbeat service from config.
// Returns nil if heartbeats are disabled (every="0m" or no config).
// Matching TS startHeartbeatRunner().
func setupHeartbeat(cfg *config.Config, router *agent.Router, sess store.SessionStore, msgBus *bus.MessageBus, workspace string, managedStores *store.Stores) *heartbeat.Service {
	hbCfg := cfg.Agents.Defaults.Heartbeat

	// Determine interval
	interval := heartbeat.DefaultInterval()
	if hbCfg != nil && hbCfg.Every != "" {
		d, err := parseDuration(hbCfg.Every)
		if err != nil {
			slog.Warn("heartbeat: invalid 'every' value, using default", "value", hbCfg.Every, "error", err)
		} else {
			interval = d
		}
	}

	// Disabled
	if interval <= 0 {
		slog.Info("heartbeat disabled (every=0)")
		return nil
	}

	agentID := resolveDefaultAgentManaged(cfg, managedStores)

	svcCfg := heartbeat.Config{
		AgentID:   agentID,
		Interval:  interval,
		Workspace: workspace,
	}

	if hbCfg != nil {
		svcCfg.ActiveHours = hbCfg.ActiveHours
		svcCfg.Model = hbCfg.Model
		svcCfg.Target = hbCfg.Target
		svcCfg.To = hbCfg.To
		svcCfg.Prompt = hbCfg.Prompt
		svcCfg.AckMaxChars = hbCfg.AckMaxChars
		if hbCfg.Session != "" {
			svcCfg.SessionKey = hbCfg.Session
		}
	}

	// Build agent runner callback
	runner := func(ctx context.Context, aID, sessionKey, message, runID string) (string, error) {
		loop, err := router.Get(aID)
		if err != nil {
			return "", err
		}
		result, err := loop.Run(ctx, agent.RunRequest{
			SessionKey: sessionKey,
			Message:    message,
			Channel:    "heartbeat",
			RunID:      runID,
			Stream:     false,
		})
		if err != nil {
			return "", err
		}
		return result.Content, nil
	}

	// Build last-used resolver
	lastUsed := func(aID string) (string, string) {
		return sess.LastUsedChannel(aID)
	}

	return heartbeat.NewService(svcCfg, runner, msgBus, lastUsed)
}

// parseDuration parses a duration string like "30m", "1h", "0m".
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// resolveDefaultAgentManaged resolves the default agent ID, falling back to the
// managed-mode DB when config returns the generic "default" (which doesn't exist
// as a real agent in managed mode — agents live in the DB, not config).
func resolveDefaultAgentManaged(cfg *config.Config, managedStores *store.Stores) string {
	agentID := cfg.ResolveDefaultAgentID()

	if managedStores == nil || managedStores.Agents == nil || agentID != config.DefaultAgentID {
		return agentID
	}

	agents, err := managedStores.Agents.List(context.Background(), "")
	if err != nil {
		slog.Warn("resolveDefaultAgentManaged: failed to list agents", "error", err)
		return agentID
	}
	for _, a := range agents {
		if a.IsDefault {
			return a.AgentKey
		}
	}
	// No agent marked as default — use first available
	if len(agents) > 0 {
		return agents[0].AgentKey
	}
	return agentID
}
