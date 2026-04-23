package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	geminiaudio "github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
	minimaxaudio "github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
)

// resolveEmbeddingProvider selects an embedding provider from DB only.
// Resolution order:
//  1. system_configs "embedding.provider" + "embedding.model" → find provider by name in DB
//  2. Auto-detect: first DB provider with settings.embedding.enabled = true
//
// Config file (agents.defaults.memory) is NOT used — all embedding config lives in DB.
func resolveEmbeddingProvider(
	providerStore store.ProviderStore,
	providerReg *providers.Registry,
	sysConfigs store.SystemConfigStore,
) memory.EmbeddingProvider {
	masterCtx := store.WithTenantID(context.Background(), store.MasterTenantID) // for system_configs (tenant-scoped)

	// 1. System config: embedding.provider (set via UI / API)
	if sysConfigs != nil {
		if name, err := sysConfigs.Get(masterCtx, "embedding.provider"); err == nil && name != "" {
			sysModel := ""
			if m, mErr := sysConfigs.Get(masterCtx, "embedding.model"); mErr == nil {
				sysModel = m
			}
			var mcfg *config.MemoryConfig
			if sysModel != "" {
				mcfg = &config.MemoryConfig{EmbeddingModel: sysModel}
			}
			p := resolveEmbeddingFromDB(masterCtx, providerStore, name, mcfg, providerReg)
			if p != nil {
				slog.Info("embedding provider from system_configs", "name", name, "model", p.Model())
				return p
			}
			slog.Warn("system_configs embedding.provider not found in DB", "name", name)
		}
	}

	// 2. Auto-detect: scan DB providers for first with settings.embedding.enabled
	allProviders, err := providerStore.ListAllProviders(context.Background())
	if err != nil {
		slog.Warn("failed to list providers for embedding auto-detect", "error", err)
		return nil
	}

	for _, dbp := range allProviders {
		if !dbp.Enabled || store.NoEmbeddingTypes[dbp.ProviderType] {
			continue
		}
		es := store.ParseEmbeddingSettings(dbp.Settings)
		if es == nil || !es.Enabled {
			continue
		}

		ep := buildEmbeddingProvider(&dbp, es, nil, providerReg)
		if ep != nil {
			slog.Info("embedding provider auto-detected", "name", dbp.Name, "model", ep.Model())
			return ep
		}
	}

	slog.Warn("no embedding provider configured — enable embedding in a provider's settings")
	return nil
}

// resolveEmbeddingFromDB finds a specific provider by name and builds an embedding provider from it.
func resolveEmbeddingFromDB(
	ctx context.Context,
	providerStore store.ProviderStore,
	name string,
	memCfg *config.MemoryConfig,
	providerReg *providers.Registry,
) memory.EmbeddingProvider {
	dbp, err := providerStore.GetProviderByName(ctx, name)
	if err != nil {
		return nil
	}
	if !dbp.Enabled {
		slog.Warn("embedding provider disabled", "name", name)
		return nil
	}
	if store.NoEmbeddingTypes[dbp.ProviderType] {
		slog.Warn("embedding provider type does not support embeddings", "name", name, "type", dbp.ProviderType)
		return nil
	}
	es := store.ParseEmbeddingSettings(dbp.Settings)
	return buildEmbeddingProvider(dbp, es, memCfg, providerReg)
}

// buildEmbeddingProvider creates a memory.EmbeddingProvider from a DB provider record.
func buildEmbeddingProvider(
	dbp *store.LLMProviderData,
	es *store.EmbeddingSettings,
	memCfg *config.MemoryConfig,
	providerReg *providers.Registry,
) memory.EmbeddingProvider {
	// Resolve model: embedding settings → memCfg override → default
	model := "text-embedding-3-small"
	if es != nil && es.Model != "" {
		model = es.Model
	}
	if memCfg != nil && memCfg.EmbeddingModel != "" {
		model = memCfg.EmbeddingModel
	}

	// Resolve API base: embedding settings → provider api_base → registry
	apiBase := dbp.APIBase
	if es != nil && es.APIBase != "" {
		apiBase = es.APIBase
	}
	if memCfg != nil && memCfg.EmbeddingAPIBase != "" {
		apiBase = memCfg.EmbeddingAPIBase
	}

	// Dimension truncation: default to RequiredMemoryEmbeddingDimensions to match pgvector schema.
	// Models that natively output 1536 ignore the parameter; models with larger native dims get truncated.
	dims := store.RequiredMemoryEmbeddingDimensions
	if es != nil && es.Dimensions > 0 && es.Dimensions != store.RequiredMemoryEmbeddingDimensions {
		slog.Warn("ignoring incompatible provider embedding dimensions for memory schema",
			"provider", dbp.Name, "requested", es.Dimensions, "required", store.RequiredMemoryEmbeddingDimensions)
	}

	// Try registry first for the actual API key / base (handles runtime-registered providers)
	if providerReg != nil {
		if regProv, regErr := providerReg.Get(context.Background(), dbp.Name); regErr == nil {
			if op, ok := regProv.(*providers.OpenAIProvider); ok {
				if apiBase == "" {
					apiBase = op.APIBase()
				}
				ep := memory.NewOpenAIEmbeddingProvider(dbp.Name, op.APIKey(), apiBase, model)
				ep.WithDimensions(dims)
				return ep
			}
			slog.Debug("embedding provider in registry is not OpenAI-compatible, using DB record", "name", dbp.Name)
		}
	}

	// Fallback: build directly from DB record
	if dbp.APIKey != "" {
		ep := memory.NewOpenAIEmbeddingProvider(dbp.Name, dbp.APIKey, apiBase, model)
		ep.WithDimensions(dims)
		return ep
	}

	return nil
}

func setupSubagents(providerReg *providers.Registry, cfg *config.Config, msgBus *bus.MessageBus, toolsReg *tools.Registry, workspace string, sandboxMgr sandbox.Manager, secureCLIStore store.SecureCLIStore) *tools.SubagentManager {
	names := providerReg.List(context.Background())
	if len(names) == 0 {
		return nil
	}

	agentCfg := cfg.ResolveAgent("default")
	provider, err := providerReg.Get(context.Background(), agentCfg.Provider)
	if err != nil {
		provider, _ = providerReg.Get(context.Background(), names[0])
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
		if sc.MaxRetries > 0 {
			subCfg.MaxRetries = sc.MaxRetries
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
		reg, _ := buildSubagentToolsRegistry(toolsReg, workspace, agentCfg.RestrictToWorkspace, sandboxMgr, secureCLIStore)
		return reg
	}

	return tools.NewSubagentManager(provider, providerReg, agentCfg.Model, msgBus, toolsFactory, subCfg)
}

// buildSubagentToolsRegistry produces a cloned tool registry for a subagent
// with workspace-scoped file/exec tools registered. The returned ExecTool is
// also returned for test assertion (Red Team F3): callers verify that the
// secureCLIStore is wired so the subagent's exec path enforces the gate.
func buildSubagentToolsRegistry(
	parentReg *tools.Registry,
	workspace string,
	restrict bool,
	sandboxMgr sandbox.Manager,
	secureCLIStore store.SecureCLIStore,
) (*tools.Registry, *tools.ExecTool) {
	reg := parentReg.Clone()
	var execTool *tools.ExecTool
	if sandboxMgr != nil {
		reg.Register(tools.NewSandboxedReadFileTool(workspace, restrict, sandboxMgr))
		reg.Register(tools.NewSandboxedWriteFileTool(workspace, restrict, sandboxMgr))
		reg.Register(tools.NewSandboxedListFilesTool(workspace, restrict, sandboxMgr))
		execTool = tools.NewSandboxedExecTool(workspace, restrict, sandboxMgr)
		reg.Register(execTool)
	} else {
		reg.Register(tools.NewReadFileTool(workspace, restrict))
		reg.Register(tools.NewWriteFileTool(workspace, restrict))
		reg.Register(tools.NewListFilesTool(workspace, restrict))
		execTool = tools.NewExecTool(workspace, restrict)
		reg.Register(execTool)
	}
	// Red Team F3: subagent ExecTool must enforce the secure-CLI gate
	// (and env scrub on fall-through) — without this, a parent agent
	// can spawn a subagent to bypass the gate via host-inherited env.
	if secureCLIStore != nil {
		execTool.SetSecureCLIStore(secureCLIStore)
	}
	return reg, execTool
}

// setupTTS creates the TTS manager from config and registers providers.
// Edge TTS is always registered (free, no API key required).
// Always returns a non-nil manager with at least one provider.
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

	// Edge TTS is free (no API key) — always register so it's available as primary or fallback.
	mgr.RegisterProvider(tts.NewEdgeProvider(tts.EdgeConfig{
		Voice:     ttsCfg.Edge.Voice,
		Rate:      ttsCfg.Edge.Rate,
		TimeoutMs: ttsCfg.TimeoutMs,
	}))

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

	if key := ttsCfg.Gemini.APIKey; key != "" {
		mgr.RegisterProvider(geminiaudio.NewProvider(geminiaudio.Config{
			APIKey:    key,
			APIBase:   ttsCfg.Gemini.APIBase,
			Voice:     ttsCfg.Gemini.Voice,
			Model:     ttsCfg.Gemini.Model,
			TimeoutMs: ttsCfg.TimeoutMs,
		}))
	}

	if !mgr.HasProviders() {
		return nil
	}

	return mgr
}

// setupAudioExtras wires Music and SFX providers into the audio Manager.
// ElevenLabs is registered for both SFX and Music when an API key is present.
// MiniMax music is registered when cfg.Audio.Music is configured with a key.
// Phase 4 will add STT providers here.
func setupAudioExtras(cfg *config.Config, mgr *tts.Manager) {
	ellKey := cfg.Tts.ElevenLabs.APIKey
	ellBase := cfg.Tts.ElevenLabs.BaseURL

	// ElevenLabs SFX — reuse TTS credentials.
	if ellKey != "" {
		mgr.RegisterSFX(elevenlabs.NewSFXProvider(elevenlabs.Config{
			APIKey:  ellKey,
			BaseURL: ellBase,
		}))
		slog.Info("audio.sfx: elevenlabs registered")
	}

	// ElevenLabs Music — same credentials, uses /v1/music endpoint.
	if ellKey != "" {
		mgr.RegisterMusic(elevenlabs.NewMusicProvider(elevenlabs.Config{
			APIKey:  ellKey,
			BaseURL: ellBase,
		}))
		slog.Info("audio.music: elevenlabs registered")
	}

	// MiniMax Music — optional, from cfg.Audio.Music block.
	if cfg.Audio != nil && cfg.Audio.Music != nil {
		mc := cfg.Audio.Music
		if mc.APIKey != "" {
			mgr.RegisterMusic(minimaxaudio.NewMusicProvider(minimaxaudio.MusicConfig{
				APIKey:  mc.APIKey,
				APIBase: mc.BaseURL,
				Model:   mc.Model,
			}))
			slog.Info("audio.music: minimax registered")
		}
	}

	// ElevenLabs STT (Scribe v2) — reuse TTS credentials. Registered as tenant-scope
	// default; per-request tenant override lands via builtin_tools[stt] in Phase 5
	// channel migration. Legacy per-channel STTProxyURL is bridged separately.
	if ellKey != "" {
		mgr.RegisterSTT(elevenlabs.NewSTTProvider(elevenlabs.Config{
			APIKey:  ellKey,
			BaseURL: ellBase,
		}))
		mgr.SetSTTChain([]string{"elevenlabs", "proxy"})
		slog.Info("audio.stt: elevenlabs registered")
	}
}
