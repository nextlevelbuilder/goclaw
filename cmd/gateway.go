package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/discord"
	"github.com/nextlevelbuilder/goclaw/internal/channels/feishu"
	"github.com/nextlevelbuilder/goclaw/internal/channels/telegram"
	"github.com/nextlevelbuilder/goclaw/internal/channels/whatsapp"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/cron"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	"github.com/nextlevelbuilder/goclaw/internal/pairing"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/file"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/browser"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func runGateway() {
	// Setup structured logging
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

	// Load config
	cfgPath := resolveConfigPath()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Auto-detect: if no provider API key is configured, help the user.
	// Also trigger auto-onboard when config file doesn't exist (first run),
	// even if env vars provide API keys — managed mode needs DB seeding.
	_, cfgStatErr := os.Stat(cfgPath)
	configMissing := os.IsNotExist(cfgStatErr)
	if !cfg.HasAnyProvider() || configMissing {
		// Docker / CI: env vars provide API keys → non-interactive auto-onboard.
		if canAutoOnboard() {
			if runAutoOnboard(cfgPath) {
				cfg, _ = config.Load(cfgPath)
			} else {
				os.Exit(1)
			}
		} else if _, statErr := os.Stat(cfgPath); statErr == nil {
			// Config file exists — user already onboarded but forgot to source .env.local.
			envPath := filepath.Join(filepath.Dir(cfgPath), ".env.local")
			fmt.Println("No AI provider API key found. Did you forget to load your secrets?")
			fmt.Println()
			fmt.Printf("  source %s && ./goclaw\n", envPath)
			fmt.Println()
			fmt.Println("Or re-run the setup wizard:  ./goclaw onboard")
			os.Exit(1)
		} else {
			// No config file at all → first time, redirect to onboard wizard.
			fmt.Println("No configuration found. Starting setup wizard...")
			fmt.Println()
			runOnboard()
			return
		}
	}

	// Create core components
	msgBus := bus.New()

	// Create provider registry
	providerRegistry := providers.NewRegistry()
	registerProviders(providerRegistry, cfg)

	// Resolve workspace (must be absolute for system prompt + file tool path resolution)
	workspace := config.ExpandHome(cfg.Agents.Defaults.Workspace)
	if !filepath.IsAbs(workspace) {
		workspace, _ = filepath.Abs(workspace)
	}
	os.MkdirAll(workspace, 0755)

	// Seed bootstrap templates to disk (standalone mode only).
	// In managed mode, bootstrap files live in Postgres — not on disk.
	if cfg.Database.Mode != "managed" {
		seededFiles, seedErr := bootstrap.EnsureWorkspaceFiles(workspace)
		if seedErr != nil {
			slog.Warn("bootstrap template seeding failed", "error", seedErr)
		} else if len(seededFiles) > 0 {
			slog.Info("seeded workspace templates", "files", seededFiles)
		}
	}

	// Create tool registry with all tools
	toolsReg := tools.NewRegistry()
	agentCfg := cfg.ResolveAgent("default")

	// Sandbox manager (optional — routes tools through Docker containers)
	var sandboxMgr sandbox.Manager
	if sbCfg := cfg.Agents.Defaults.Sandbox; sbCfg != nil && sbCfg.Mode != "" && sbCfg.Mode != "off" {
		if err := sandbox.CheckDockerAvailable(context.Background()); err != nil {
			slog.Warn("sandbox disabled: Docker not available",
				"configured_mode", sbCfg.Mode,
				"error", err,
			)
		} else {
			resolved := sbCfg.ToSandboxConfig()
			sandboxMgr = sandbox.NewDockerManager(resolved)
			slog.Info("sandbox enabled", "mode", string(resolved.Mode), "image", resolved.Image, "scope", string(resolved.Scope))
		}
	}

	// Register file tools + exec tool (with sandbox routing via FsBridge if enabled)
	if sandboxMgr != nil {
		toolsReg.Register(tools.NewSandboxedReadFileTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
		toolsReg.Register(tools.NewSandboxedWriteFileTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
		toolsReg.Register(tools.NewSandboxedListFilesTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
		toolsReg.Register(tools.NewSandboxedEditTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
		toolsReg.Register(tools.NewSandboxedExecTool(workspace, agentCfg.RestrictToWorkspace, sandboxMgr))
	} else {
		toolsReg.Register(tools.NewReadFileTool(workspace, agentCfg.RestrictToWorkspace))
		toolsReg.Register(tools.NewWriteFileTool(workspace, agentCfg.RestrictToWorkspace))
		toolsReg.Register(tools.NewListFilesTool(workspace, agentCfg.RestrictToWorkspace))
		toolsReg.Register(tools.NewEditTool(workspace, agentCfg.RestrictToWorkspace))
		toolsReg.Register(tools.NewExecTool(workspace, agentCfg.RestrictToWorkspace))
	}

	// Memory system
	memMgr := setupMemory(workspace, cfg)
	if memMgr != nil {
		defer memMgr.Close()
		toolsReg.Register(tools.NewMemorySearchTool(memMgr))
		toolsReg.Register(tools.NewMemoryGetTool(memMgr))
		slog.Info("memory system enabled", "tools", []string{"memory_search", "memory_get"})
	}

	// Browser automation tool
	var browserMgr *browser.Manager
	if cfg.Tools.Browser.Enabled {
		browserMgr = browser.New(
			browser.WithHeadless(cfg.Tools.Browser.Headless),
		)
		toolsReg.Register(browser.NewBrowserTool(browserMgr))
		defer browserMgr.Close()
		slog.Info("browser tool enabled", "headless", cfg.Tools.Browser.Headless)
	}

	// Web tools (web_search + web_fetch)
	webSearchTool := tools.NewWebSearchTool(tools.WebSearchConfig{
		BraveEnabled: cfg.Tools.Web.Brave.Enabled,
		BraveAPIKey:  cfg.Tools.Web.Brave.APIKey,
		DDGEnabled:   cfg.Tools.Web.DuckDuckGo.Enabled,
	})
	if webSearchTool != nil {
		toolsReg.Register(webSearchTool)
		slog.Info("web_search tool enabled")
	}
	webFetchTool := tools.NewWebFetchTool(tools.WebFetchConfig{})
	toolsReg.Register(webFetchTool)
	slog.Info("web_fetch tool enabled")

	// Vision fallback tool (for non-vision providers like MiniMax)
	toolsReg.Register(tools.NewReadImageTool(providerRegistry))
	toolsReg.Register(tools.NewCreateImageTool(providerRegistry))

	// TTS (text-to-speech) system
	ttsMgr := setupTTS(cfg)
	if ttsMgr != nil {
		toolsReg.Register(tools.NewTtsTool(ttsMgr))
		slog.Info("tts enabled", "provider", ttsMgr.PrimaryProvider(), "auto", string(ttsMgr.AutoMode()))
	}

	// Tool rate limiting (per session, sliding window)
	if cfg.Tools.RateLimitPerHour > 0 {
		toolsReg.SetRateLimiter(tools.NewToolRateLimiter(cfg.Tools.RateLimitPerHour))
		slog.Info("tool rate limiting enabled", "per_hour", cfg.Tools.RateLimitPerHour)
	}

	// Credential scrubbing (enabled by default, can be disabled via config)
	if cfg.Tools.ScrubCredentials != nil && !*cfg.Tools.ScrubCredentials {
		toolsReg.SetScrubbing(false)
		slog.Info("credential scrubbing disabled")
	}

	// MCP servers (standalone mode: shared across all agents)
	var mcpMgr *mcpbridge.Manager
	if len(cfg.Tools.McpServers) > 0 {
		mcpMgr = mcpbridge.NewManager(toolsReg, mcpbridge.WithConfigs(cfg.Tools.McpServers))
		if err := mcpMgr.Start(context.Background()); err != nil {
			slog.Warn("mcp.startup_errors", "error", err)
		}
		defer mcpMgr.Stop()
		slog.Info("MCP servers initialized", "configured", len(cfg.Tools.McpServers), "tools", len(mcpMgr.ToolNames()))
	}

	// Subagent system
	subagentMgr := setupSubagents(providerRegistry, cfg, msgBus, toolsReg, workspace, sandboxMgr)
	if subagentMgr != nil {
		// Wire announce queue for batched subagent result delivery (matching TS debounce pattern)
		announceQueue := tools.NewAnnounceQueue(1000, 20,
			func(sessionKey string, items []tools.AnnounceQueueItem, meta tools.AnnounceMetadata) {
				remainingActive := subagentMgr.CountRunningForParent(meta.ParentAgent)
				content := tools.FormatBatchedAnnounce(items, remainingActive)
				senderID := fmt.Sprintf("subagent:batch-%d", len(items))
				label := items[0].Label
				if len(items) > 1 {
					label = fmt.Sprintf("%d tasks", len(items))
				}
				msgBus.PublishInbound(bus.InboundMessage{
					Channel:  "system",
					SenderID: senderID,
					ChatID:   meta.OriginChatID,
					Content:  content,
					UserID:   meta.OriginUserID,
					Metadata: map[string]string{
						"origin_channel":      meta.OriginChannel,
						"origin_peer_kind":    meta.OriginPeerKind,
						"parent_agent":        meta.ParentAgent,
						"subagent_label":      label,
						"origin_trace_id":     meta.OriginTraceID,
						"origin_root_span_id": meta.OriginRootSpanID,
					},
				})
			},
			func(parentID string) int {
				return subagentMgr.CountRunningForParent(parentID)
			},
		)
		subagentMgr.SetAnnounceQueue(announceQueue)

		toolsReg.Register(tools.NewSpawnTool(subagentMgr, "default", 0))
		slog.Info("subagent system enabled", "tools", []string{"spawn"})
	}

	// Exec approval system — always active (deny patterns + safe bins + configurable ask mode)
	var execApprovalMgr *tools.ExecApprovalManager
	{
		approvalCfg := tools.DefaultExecApprovalConfig()
		// Override from user config (backward compat: explicit values take precedence)
		if eaCfg := cfg.Tools.ExecApproval; eaCfg.Security != "" {
			approvalCfg.Security = tools.ExecSecurity(eaCfg.Security)
		}
		if eaCfg := cfg.Tools.ExecApproval; eaCfg.Ask != "" {
			approvalCfg.Ask = tools.ExecAskMode(eaCfg.Ask)
		}
		if len(cfg.Tools.ExecApproval.Allowlist) > 0 {
			approvalCfg.Allowlist = cfg.Tools.ExecApproval.Allowlist
		}
		execApprovalMgr = tools.NewExecApprovalManager(approvalCfg)

		// Wire approval to exec tools in the registry
		if execTool, ok := toolsReg.Get("exec"); ok {
			if aa, ok := execTool.(tools.ApprovalAware); ok {
				aa.SetApprovalManager(execApprovalMgr, "default")
			}
		}
		slog.Info("exec approval enabled", "security", string(approvalCfg.Security), "ask", string(approvalCfg.Ask))
	}

	// --- Enforcement: Policy engines ---

	// Permission policy engine (role-based RPC access control)
	permPE := permissions.NewPolicyEngine(cfg.Gateway.OwnerIDs)

	// Tool policy engine (7-step tool filtering pipeline)
	toolPE := tools.NewPolicyEngine(&cfg.Tools)

	// Data directory for Phase 2 services
	dataDir := os.Getenv("GOCLAW_DATA_DIR")
	if dataDir == "" {
		dataDir = config.ExpandHome("~/.goclaw/data")
	}
	os.MkdirAll(dataDir, 0755)

	// --- Mode-based store creation ---
	// Standalone: file-based adapters wrapping sessions/cron/pairing packages.
	// Managed: Postgres stores from pg.NewPGStores.
	var sessStore store.SessionStore
	var cronStore store.CronStore
	var pairingStore store.PairingStore
	var managedStores *store.Stores
	var traceCollector *tracing.Collector

	if cfg.Database.Mode == "managed" && cfg.Database.PostgresDSN != "" {
		// Schema compatibility check: ensure DB schema matches this binary.
		if err := checkSchemaOrAutoUpgrade(cfg.Database.PostgresDSN); err != nil {
			slog.Error("schema compatibility check failed", "error", err)
			os.Exit(1)
		}

		storeCfg := store.StoreConfig{
			PostgresDSN:   cfg.Database.PostgresDSN,
			Mode:          cfg.Database.Mode,
			EncryptionKey: os.Getenv("GOCLAW_ENCRYPTION_KEY"),
		}
		pgStores, pgErr := pg.NewPGStores(storeCfg)
		if pgErr != nil {
			slog.Error("failed to create PG stores", "error", pgErr)
			os.Exit(1)
		}
		managedStores = pgStores
		sessStore = pgStores.Sessions
		cronStore = pgStores.Cron
		pairingStore = pgStores.Pairing
		if pgStores.Tracing != nil {
			traceCollector = tracing.NewCollector(pgStores.Tracing)
			traceCollector.Start()
			slog.Info("LLM tracing enabled")
		}
	} else {
		// Standalone mode: file-based stores
		sessStore = file.NewFileSessionStore(sessions.NewManager(config.ExpandHome(cfg.Sessions.Storage)))
		cronStorePath := filepath.Join(dataDir, "cron", "jobs.json")
		cronStore = file.NewFileCronStore(cron.NewService(cronStorePath, nil))
		pairingStorePath := filepath.Join(dataDir, "pairing.json")
		pairingStore = file.NewFilePairingStore(pairing.NewService(pairingStorePath))
	}
	if traceCollector != nil {
		defer traceCollector.Stop()
		// OTel OTLP export: compiled via build tags. Build with 'go build -tags otel' to enable.
		initOTelExporter(context.Background(), cfg, traceCollector)
	}

	// Wire cron retry config from config.json
	cronRetryCfg := cfg.Cron.ToRetryConfig()
	if svc, ok := cronStore.(interface{ SetRetryConfig(cron.RetryConfig) }); ok {
		svc.SetRetryConfig(cronRetryCfg)
	}

	// Managed mode: load secrets from config_secrets table before env overrides.
	// Precedence: config.json → DB secrets → env vars (highest).
	if managedStores != nil && managedStores.ConfigSecrets != nil {
		if secrets, err := managedStores.ConfigSecrets.GetAll(context.Background()); err == nil && len(secrets) > 0 {
			cfg.ApplyDBSecrets(secrets)
			cfg.ApplyEnvOverrides()
			slog.Info("managed mode: config secrets loaded from DB", "count", len(secrets))
		}
	}

	// Managed mode: register providers from DB (overrides config providers).
	if managedStores != nil && managedStores.Providers != nil {
		registerProvidersFromDB(providerRegistry, managedStores.Providers)
	}

	// Managed mode: wire embedding provider to PGMemoryStore so IndexDocument generates vectors.
	if managedStores != nil && managedStores.Memory != nil {
		memCfg := cfg.Agents.Defaults.Memory
		if embProvider := resolveEmbeddingProvider(cfg, memCfg); embProvider != nil {
			managedStores.Memory.SetEmbeddingProvider(embProvider)
			slog.Info("managed mode: memory embeddings enabled", "provider", embProvider.Name(), "model", embProvider.Model())

			// Backfill embeddings for existing chunks that were stored without vectors.
			type backfiller interface {
				BackfillEmbeddings(ctx context.Context) (int, error)
			}
			if bf, ok := managedStores.Memory.(backfiller); ok {
				go func() {
					bgCtx := context.Background()
					count, err := bf.BackfillEmbeddings(bgCtx)
					if err != nil {
						slog.Warn("memory embeddings backfill failed", "error", err)
					} else if count > 0 {
						slog.Info("memory embeddings backfill complete", "chunks_updated", count)
					}
				}()
			}
		} else {
			slog.Warn("managed mode: memory embeddings disabled (no API key), chunks stored without vectors")
		}
	}

	// Load bootstrap files for default agent's system prompt.
	// Managed mode: load from DB first, seed if empty, fallback to filesystem.
	// Standalone mode: load from workspace filesystem.
	var contextFiles []bootstrap.ContextFile

	if managedStores != nil && managedStores.Agents != nil {
		bgCtx := context.Background()
		defaultAgent, agErr := managedStores.Agents.GetByKey(bgCtx, "default")
		if agErr == nil {
			dbFiles := bootstrap.LoadFromStore(bgCtx, managedStores.Agents, defaultAgent.ID)
			if len(dbFiles) > 0 {
				contextFiles = dbFiles
				slog.Info("bootstrap loaded from store", "count", len(dbFiles))
			} else {
				// DB empty → seed templates, then load
				if _, seedErr := bootstrap.SeedToStore(bgCtx, managedStores.Agents, defaultAgent.ID, defaultAgent.AgentType); seedErr != nil {
					slog.Warn("failed to seed bootstrap to store", "error", seedErr)
				} else {
					contextFiles = bootstrap.LoadFromStore(bgCtx, managedStores.Agents, defaultAgent.ID)
					slog.Info("bootstrap seeded and loaded from store", "count", len(contextFiles))
				}
			}
		}
	}

	if len(contextFiles) == 0 {
		// Standalone mode or DB fallback
		rawFiles := bootstrap.LoadWorkspaceFiles(workspace)
		truncCfg := bootstrap.TruncateConfig{
			MaxCharsPerFile: agentCfg.BootstrapMaxChars,
			TotalMaxChars:   agentCfg.BootstrapTotalMaxChars,
		}
		if truncCfg.MaxCharsPerFile <= 0 {
			truncCfg.MaxCharsPerFile = bootstrap.DefaultMaxCharsPerFile
		}
		if truncCfg.TotalMaxChars <= 0 {
			truncCfg.TotalMaxChars = bootstrap.DefaultTotalMaxChars
		}
		contextFiles = bootstrap.BuildContextFiles(rawFiles, truncCfg)
		slog.Info("bootstrap loaded from filesystem", "count", len(contextFiles))
	}

	// Debug: log bootstrap file loading results
	{
		var loadedNames []string
		for _, cf := range contextFiles {
			loadedNames = append(loadedNames, fmt.Sprintf("%s(%d)", cf.Path, len(cf.Content)))
		}
		slog.Info("bootstrap context files", "count", len(contextFiles), "files", loadedNames)
	}

	// Skills loader + search tool
	// Global skills live under ~/.goclaw/skills/ (user-managed), not data/skills/.
	globalSkillsDir := os.Getenv("GOCLAW_SKILLS_DIR")
	if globalSkillsDir == "" {
		globalSkillsDir = filepath.Join(config.ExpandHome("~/.goclaw"), "skills")
	}
	skillsLoader := skills.NewLoader(workspace, globalSkillsDir, "")
	skillSearchTool := tools.NewSkillSearchTool(skillsLoader)
	toolsReg.Register(skillSearchTool)
	slog.Info("skill_search tool registered", "skills", len(skillsLoader.ListSkills()))

	// Managed mode: wire embedding-based skill search
	if managedStores != nil && managedStores.Skills != nil {
		if pgSkills, ok := managedStores.Skills.(*pg.PGSkillStore); ok {
			memCfg := cfg.Agents.Defaults.Memory
			if embProvider := resolveEmbeddingProvider(cfg, memCfg); embProvider != nil {
				pgSkills.SetEmbeddingProvider(embProvider)
				skillSearchTool.SetEmbeddingSearcher(pgSkills, embProvider)
				slog.Info("managed mode: skill embeddings enabled", "provider", embProvider.Name())

				// Backfill embeddings for existing skills
				go func() {
					count, err := pgSkills.BackfillSkillEmbeddings(context.Background())
					if err != nil {
						slog.Warn("skill embeddings backfill failed", "error", err)
					} else if count > 0 {
						slog.Info("skill embeddings backfill complete", "skills_updated", count)
					}
				}()
			}
		}
	}

	// Cron tool (agent-facing, matching TS cron-tool.ts)
	toolsReg.Register(tools.NewCronTool(cronStore))
	slog.Info("cron tool registered")

	// Session tools (list, status, history, send)
	toolsReg.Register(tools.NewSessionsListTool())
	toolsReg.Register(tools.NewSessionStatusTool())
	toolsReg.Register(tools.NewSessionsHistoryTool())
	toolsReg.Register(tools.NewSessionsSendTool())

	// Message tool (send to channels)
	toolsReg.Register(tools.NewMessageTool())
	slog.Info("session + message tools registered")

	// Allow read_file to access skills directories (outside workspace).
	// Skills can live in ~/.goclaw/skills/, ~/.agents/skills/, etc.
	homeDir, _ := os.UserHomeDir()
	if readTool, ok := toolsReg.Get("read_file"); ok {
		if pa, ok := readTool.(tools.PathAllowable); ok {
			pa.AllowPaths(globalSkillsDir)
			if homeDir != "" {
				pa.AllowPaths(filepath.Join(homeDir, ".agents", "skills"))
			}
		}
	}

	// Memory detection: SQLite (standalone) or PG (managed) — either enables memory.
	hasMemory := memMgr != nil
	if !hasMemory && managedStores != nil && managedStores.Memory != nil {
		hasMemory = true
		// PG memory is available but SQLite failed or wasn't created.
		// Ensure memory tools are registered so wireManagedExtras can wire PG store to them.
		if _, exists := toolsReg.Get("memory_search"); !exists {
			toolsReg.Register(tools.NewMemorySearchTool(nil))
			toolsReg.Register(tools.NewMemoryGetTool(nil))
			slog.Info("memory tools registered for managed mode (PG-backed)")
		}
	}

	// Wire SessionStoreAware + BusAware on tools that need them
	for _, name := range []string{"sessions_list", "session_status", "sessions_history", "sessions_send"} {
		if t, ok := toolsReg.Get(name); ok {
			if sa, ok := t.(tools.SessionStoreAware); ok {
				sa.SetSessionStore(sessStore)
			}
			if ba, ok := t.(tools.BusAware); ok {
				ba.SetMessageBus(msgBus)
			}
		}
	}
	// Wire BusAware on message tool
	if t, ok := toolsReg.Get("message"); ok {
		if ba, ok := t.(tools.BusAware); ok {
			ba.SetMessageBus(msgBus)
		}
	}

	// Standalone mode: wire FileAgentStore + interceptors + callbacks.
	// Must happen after tool registration (wires interceptors to read_file, write_file, edit).
	var fileAgentStore store.AgentStore
	var ensureUserFiles agent.EnsureUserFilesFunc
	var contextFileLoader agent.ContextFileLoaderFunc
	if cfg.Database.Mode != "managed" {
		var standaloneCleanup func()
		fileAgentStore, ensureUserFiles, contextFileLoader, standaloneCleanup =
			wireStandaloneExtras(cfg, toolsReg, dataDir, workspace)
		if standaloneCleanup != nil {
			defer standaloneCleanup()
		}
	}

	// Create all agents
	agentRouter := agent.NewRouter()

	isManaged := managedStores != nil

	// In managed mode, agents are created lazily by the resolver (from DB).
	// In standalone mode, create agents eagerly from config.
	if !isManaged {
		// Always create "default" agent
		if err := createAgentLoop("default", cfg, agentRouter, providerRegistry, msgBus, sessStore, toolsReg, toolPE, contextFiles, skillsLoader, hasMemory, sandboxMgr, fileAgentStore, ensureUserFiles, contextFileLoader); err != nil {
			slog.Error("failed to create default agent", "error", err)
			os.Exit(1)
		}

		// Create additional agents from agents.list
		for agentID := range cfg.Agents.List {
			if agentID == "default" {
				continue
			}
			if err := createAgentLoop(agentID, cfg, agentRouter, providerRegistry, msgBus, sessStore, toolsReg, toolPE, contextFiles, skillsLoader, hasMemory, sandboxMgr, fileAgentStore, ensureUserFiles, contextFileLoader); err != nil {
				slog.Error("failed to create agent", "agent", agentID, "error", err)
			}
		}
	} else {
		slog.Info("managed mode: agents will be resolved lazily from database")
	}

	// Create gateway server and wire enforcement
	server := gateway.NewServer(cfg, msgBus, agentRouter, sessStore, toolsReg)
	server.SetPolicyEngine(permPE)
	server.SetPairingService(pairingStore)

	// Managed mode: set agent store for tools_invoke context injection + wire extras
	if managedStores != nil && managedStores.Agents != nil {
		server.SetAgentStore(managedStores.Agents)
	}
	if managedStores != nil {
		// Dynamic custom tools: load global tools from DB before resolver
		var dynamicLoader *tools.DynamicToolLoader
		if managedStores.CustomTools != nil {
			dynamicLoader = tools.NewDynamicToolLoader(managedStores.CustomTools, workspace)
			if err := dynamicLoader.LoadGlobal(context.Background(), toolsReg); err != nil {
				slog.Warn("failed to load global custom tools", "error", err)
			}
		}

		wireManagedExtras(managedStores, agentRouter, providerRegistry, msgBus, sessStore, toolsReg, toolPE, skillsLoader, hasMemory, traceCollector, workspace, cfg.Gateway.InjectionAction, cfg, sandboxMgr, dynamicLoader)
		agentsH, skillsH, tracesH, mcpH, customToolsH, channelInstancesH, providersH, delegationsH, builtinToolsH := wireManagedHTTP(managedStores, cfg.Gateway.Token, msgBus, toolsReg, providerRegistry, permPE.IsOwner)
		if agentsH != nil {
			server.SetAgentsHandler(agentsH)
		}
		if skillsH != nil {
			server.SetSkillsHandler(skillsH)
		}
		if tracesH != nil {
			server.SetTracesHandler(tracesH)
		}
		if mcpH != nil {
			server.SetMCPHandler(mcpH)
		}
		if customToolsH != nil {
			server.SetCustomToolsHandler(customToolsH)
		}
		if channelInstancesH != nil {
			server.SetChannelInstancesHandler(channelInstancesH)
		}
		if providersH != nil {
			server.SetProvidersHandler(providersH)
		}
		if delegationsH != nil {
			server.SetDelegationsHandler(delegationsH)
		}
		if builtinToolsH != nil {
			server.SetBuiltinToolsHandler(builtinToolsH)
		}

		// Seed + apply builtin tool disables
		if managedStores.BuiltinTools != nil {
			seedBuiltinTools(context.Background(), managedStores.BuiltinTools)
			applyBuiltinToolDisables(context.Background(), managedStores.BuiltinTools, toolsReg)
		}
	}

	// Register all RPC methods
	var agentStoreForRPC store.AgentStore
	if isManaged {
		agentStoreForRPC = managedStores.Agents
	}

	// SkillStore for RPC methods: PG in managed mode, file wrapper in standalone.
	var skillStore store.SkillStore
	if managedStores != nil && managedStores.Skills != nil {
		skillStore = managedStores.Skills
	} else {
		skillStore = file.NewFileSkillStore(skillsLoader)
	}

	var configSecretsStore store.ConfigSecretsStore
	if managedStores != nil {
		configSecretsStore = managedStores.ConfigSecrets
	}

	var teamStoreForRPC store.TeamStore
	if managedStores != nil {
		teamStoreForRPC = managedStores.Teams
	}

	pairingMethods := registerAllMethods(server, agentRouter, sessStore, cronStore, pairingStore, cfg, cfgPath, workspace, dataDir, msgBus, execApprovalMgr, agentStoreForRPC, isManaged, skillStore, configSecretsStore, teamStoreForRPC)

	// Channel manager
	channelMgr := channels.NewManager(msgBus)

	// Wire channel sender on message tool (now that channelMgr exists)
	if t, ok := toolsReg.Get("message"); ok {
		if cs, ok := t.(tools.ChannelSenderAware); ok {
			cs.SetChannelSender(channelMgr.SendToChannel)
		}
	}

	// Managed mode: load channel instances from DB first.
	var instanceLoader *channels.InstanceLoader
	if managedStores != nil && managedStores.ChannelInstances != nil {
		instanceLoader = channels.NewInstanceLoader(managedStores.ChannelInstances, managedStores.Agents, channelMgr, msgBus, pairingStore)
		instanceLoader.RegisterFactory("telegram", telegram.FactoryWithStores(managedStores.Agents, managedStores.Teams))
		instanceLoader.RegisterFactory("discord", discord.Factory)
		instanceLoader.RegisterFactory("feishu", feishu.Factory)
		instanceLoader.RegisterFactory("zalo_oa", zalo.Factory)
		instanceLoader.RegisterFactory("whatsapp", whatsapp.Factory)
		if err := instanceLoader.LoadAll(context.Background()); err != nil {
			slog.Error("failed to load channel instances from DB", "error", err)
		}
	}

	// Register config-based channels as fallback (standalone mode only).
	// In managed mode, channels are loaded from DB via instanceLoader — skip config-based registration.
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.Token != "" && instanceLoader == nil {
		tg, err := telegram.New(cfg.Channels.Telegram, msgBus, pairingStore, nil, nil)
		if err != nil {
			slog.Error("failed to initialize telegram channel", "error", err)
		} else {
			channelMgr.RegisterChannel("telegram", tg)
			slog.Info("telegram channel enabled (config)")
		}
	}

	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.Token != "" && instanceLoader == nil {
		dc, err := discord.New(cfg.Channels.Discord, msgBus, nil)
		if err != nil {
			slog.Error("failed to initialize discord channel", "error", err)
		} else {
			channelMgr.RegisterChannel("discord", dc)
			slog.Info("discord channel enabled (config)")
		}
	}

	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.BridgeURL != "" && instanceLoader == nil {
		wa, err := whatsapp.New(cfg.Channels.WhatsApp, msgBus, nil)
		if err != nil {
			slog.Error("failed to initialize whatsapp channel", "error", err)
		} else {
			channelMgr.RegisterChannel("whatsapp", wa)
			slog.Info("whatsapp channel enabled (config)")
		}
	}

	if cfg.Channels.Zalo.Enabled && cfg.Channels.Zalo.Token != "" && instanceLoader == nil {
		z, err := zalo.New(cfg.Channels.Zalo, msgBus, pairingStore)
		if err != nil {
			slog.Error("failed to initialize zalo channel", "error", err)
		} else {
			channelMgr.RegisterChannel("zalo", z)
			slog.Info("zalo channel enabled (config)")
		}
	}

	if cfg.Channels.Feishu.Enabled && cfg.Channels.Feishu.AppID != "" && instanceLoader == nil {
		f, err := feishu.New(cfg.Channels.Feishu, msgBus, pairingStore)
		if err != nil {
			slog.Error("failed to initialize feishu channel", "error", err)
		} else {
			channelMgr.RegisterChannel("feishu", f)
			slog.Info("feishu/lark channel enabled (config)")
		}
	}

	// Register channels RPC methods (after channelMgr is initialized with all channels)
	methods.NewChannelsMethods(channelMgr).Register(server.Router())

	// Register channel instances WS RPC methods (managed mode only)
	if managedStores != nil && managedStores.ChannelInstances != nil {
		methods.NewChannelInstancesMethods(managedStores.ChannelInstances, msgBus).Register(server.Router())
	}

	// Register agent links WS RPC methods (managed mode only)
	if managedStores != nil && managedStores.AgentLinks != nil && managedStores.Agents != nil {
		methods.NewAgentLinksMethods(managedStores.AgentLinks, managedStores.Agents, agentRouter).Register(server.Router())
	}

	// Register agent teams WS RPC methods (managed mode only)
	if managedStores != nil && managedStores.Teams != nil {
		methods.NewTeamsMethods(managedStores.Teams, managedStores.Agents, managedStores.AgentLinks, agentRouter).Register(server.Router())
	}

	// Cache invalidation: reload channel instances on changes.
	// Runs in a goroutine because Reload() is heavy (stops channels, waits for polling exit,
	// sleeps 500ms, reloads from DB, starts new channels) and Broadcast handlers must be non-blocking.
	if instanceLoader != nil {
		msgBus.Subscribe("cache:channel_instances", func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != "channel_instances" {
				return
			}
			go instanceLoader.Reload(context.Background())
		})
	}

	// Wire pairing approval notification → channel (matching TS notifyPairingApproved).
	botName := cfg.ResolveDisplayName("default")
	pairingMethods.SetOnApprove(func(ctx context.Context, channel, chatID string) {
		msg := fmt.Sprintf("✅ %s access approved. Send a message to start chatting.", botName)
		if err := channelMgr.SendToChannel(ctx, channel, chatID, msg); err != nil {
			slog.Warn("failed to send pairing approval notification", "channel", channel, "chatID", chatID, "error", err)
		}
	})

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Skills directory watcher — auto-detect new/removed/modified skills at runtime.
	if skillsWatcher, err := skills.NewWatcher(skillsLoader); err != nil {
		slog.Warn("skills watcher unavailable", "error", err)
	} else {
		if err := skillsWatcher.Start(ctx); err != nil {
			slog.Warn("skills watcher start failed", "error", err)
		} else {
			defer skillsWatcher.Stop()
		}
	}

	// Start channels
	if err := channelMgr.StartAll(ctx); err != nil {
		slog.Error("failed to start channels", "error", err)
	}

	// Create lane-based scheduler (matching TS CommandLane pattern).
	// The RunFunc resolves the agent from the RunRequest metadata.
	// Must be created before cron setup so cron jobs route through the scheduler.
	sched := scheduler.NewScheduler(
		scheduler.DefaultLanes(),
		scheduler.DefaultQueueConfig(),
		makeSchedulerRunFunc(agentRouter, cfg),
	)
	defer sched.Stop()

	// Start cron service with job handler (routes through scheduler's cron lane)
	cronStore.SetOnJob(makeCronJobHandler(sched, msgBus, cfg))
	if err := cronStore.Start(); err != nil {
		slog.Warn("cron service failed to start", "error", err)
	}

	// Start heartbeat service (matching TS heartbeat-runner.ts).
	heartbeatSvc := setupHeartbeat(cfg, agentRouter, sessStore, msgBus, workspace, managedStores)
	if heartbeatSvc != nil {
		heartbeatSvc.Start()
	}

	// Adaptive throttle: reduce per-session concurrency when nearing the summary threshold.
	// This prevents concurrent runs from racing with summarization.
	// Uses calibrated token estimation (actual prompt tokens from last LLM call)
	// and the agent's real context window (cached on session by the Loop).
	sched.SetTokenEstimateFunc(func(sessionKey string) (int, int) {
		history := sessStore.GetHistory(sessionKey)
		lastPT, lastMC := sessStore.GetLastPromptTokens(sessionKey)
		tokens := agent.EstimateTokensWithCalibration(history, lastPT, lastMC)
		cw := sessStore.GetContextWindow(sessionKey)
		if cw <= 0 {
			cw = 200000 // fallback for sessions not yet processed
		}
		return tokens, cw
	})

	// Subscribe to agent events for channel streaming/reaction forwarding.
	// Events emitted by agent loops are broadcast to the bus; we forward them
	// to the channel manager which routes to StreamingChannel/ReactionChannel.
	msgBus.Subscribe("channel-streaming", func(event bus.Event) {
		if event.Name != protocol.EventAgent {
			return
		}
		agentEvent, ok := event.Payload.(agent.AgentEvent)
		if !ok {
			return
		}
		channelMgr.HandleAgentEvent(agentEvent.Type, agentEvent.RunID, agentEvent.Payload)
	})

	// Start inbound message consumer (channel → scheduler → agent → channel)
	var consumerTeamStore store.TeamStore
	if managedStores != nil {
		consumerTeamStore = managedStores.Teams
	}
	go consumeInboundMessages(ctx, msgBus, agentRouter, cfg, sched, channelMgr, consumerTeamStore)

	go func() {
		sig := <-sigCh
		slog.Info("graceful shutdown initiated", "signal", sig)

		// Broadcast shutdown event
		server.BroadcastEvent(*protocol.NewEvent(protocol.EventShutdown, nil))

		// Stop channels, cron, and heartbeat
		channelMgr.StopAll(context.Background())
		cronStore.Stop()
		if heartbeatSvc != nil {
			heartbeatSvc.Stop()
		}

		// Stop sandbox pruning + release containers
		if sandboxMgr != nil {
			sandboxMgr.Stop()
			slog.Info("releasing sandbox containers...")
			sandboxMgr.ReleaseAll(context.Background())
		}

		cancel()
	}()

	gatewayMode := "standalone"
	if cfg.Database.Mode == "managed" {
		gatewayMode = "managed"
	}
	slog.Info("goclaw gateway starting",
		"version", Version,
		"protocol", protocol.ProtocolVersion,
		"mode", gatewayMode,
		"agents", agentRouter.List(),
		"tools", toolsReg.Count(),
		"channels", channelMgr.GetEnabledChannels(),
	)

	// Tailscale listener: build the mux first, then pass it to initTailscale
	// so the same routes are served on both the main listener and Tailscale.
	// Compiled via build tags: `go build -tags tsnet` to enable.
	mux := server.BuildMux()
	tsCleanup := initTailscale(ctx, cfg, mux)
	if tsCleanup != nil {
		defer tsCleanup()
	}

	// Phase 1: suggest localhost binding when Tailscale is active
	if cfg.Tailscale.Hostname != "" && cfg.Gateway.Host == "0.0.0.0" {
		slog.Info("Tailscale enabled. Consider setting GOCLAW_HOST=127.0.0.1 for localhost-only + Tailscale access")
	}

	if err := server.Start(ctx); err != nil {
		slog.Error("gateway error", "error", err)
		os.Exit(1)
	}
}
