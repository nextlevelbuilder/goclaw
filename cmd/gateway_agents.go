package cmd

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
)

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
