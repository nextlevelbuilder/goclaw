package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/discord"
	"github.com/nextlevelbuilder/goclaw/internal/channels/feishu"
	slackchannel "github.com/nextlevelbuilder/goclaw/internal/channels/slack"
	"github.com/nextlevelbuilder/goclaw/internal/channels/telegram"
	"github.com/nextlevelbuilder/goclaw/internal/channels/whatsapp"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo"
	zalopersonal "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
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
	textHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	logTee := gateway.NewLogTee(textHandler)
	slog.SetDefault(slog.New(logTee))

	// Load config
	cfgPath := resolveConfigPath()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
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

	// Bootstrap files live in Postgres.

	// Detect server IPs for output scrubbing (prevents IP leaks via web_fetch, exec, etc.)
	tools.DetectServerIPs(context.Background())

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

	// Memory tools — PG-backed; always registered (PG memory is always available)
	toolsReg.Register(tools.NewMemorySearchTool())
	toolsReg.Register(tools.NewMemoryGetTool())
	toolsReg.Register(tools.NewKnowledgeGraphSearchTool())
	slog.Info("memory + knowledge graph tools registered (PG-backed)")

	// Browser automation tool
	var browserMgr *browser.Manager
	if cfg.Tools.Browser.Enabled {
		var opts []browser.Option
		if cfg.Tools.Browser.RemoteURL != "" {
			opts = append(opts, browser.WithRemoteURL(cfg.Tools.Browser.RemoteURL))
			slog.Info("browser tool enabled", "remote", cfg.Tools.Browser.RemoteURL)
		} else {
			opts = append(opts, browser.WithHeadless(cfg.Tools.Browser.Headless))
			slog.Info("browser tool enabled", "headless", cfg.Tools.Browser.Headless)
		}
		browserMgr = browser.New(opts...)
		toolsReg.Register(browser.NewBrowserTool(browserMgr))
		defer browserMgr.Close()
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
	webFetchTool := tools.NewWebFetchTool(tools.WebFetchConfig{
		Policy:         cfg.Tools.WebFetch.Policy,
		AllowedDomains: cfg.Tools.WebFetch.AllowedDomains,
		BlockedDomains: cfg.Tools.WebFetch.BlockedDomains,
	})
	toolsReg.Register(webFetchTool)
	slog.Info("web_fetch tool enabled", "policy", cfg.Tools.WebFetch.Policy, "blocked", len(cfg.Tools.WebFetch.BlockedDomains))

	// Vision fallback tool (for non-vision providers like MiniMax)
	toolsReg.Register(tools.NewReadImageTool(providerRegistry))
	toolsReg.Register(tools.NewCreateImageTool(providerRegistry))

	// Audio generation tool (MiniMax music + ElevenLabs sound effects)
	toolsReg.Register(tools.NewCreateAudioTool(providerRegistry,
		cfg.Tts.ElevenLabs.APIKey, cfg.Tts.ElevenLabs.BaseURL))

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

	// MCP servers (config-based: shared across all agents)
	var mcpMgr *mcpbridge.Manager
	if len(cfg.Tools.McpServers) > 0 {
		mcpMgr = mcpbridge.NewManager(toolsReg, mcpbridge.WithConfigs(cfg.Tools.McpServers))
		if err := mcpMgr.Start(context.Background()); err != nil {
			slog.Warn("mcp.startup_errors", "error", err)
		}
		defer mcpMgr.Stop()
		slog.Info("MCP servers initialized", "configured", len(cfg.Tools.McpServers), "tools", len(mcpMgr.ToolNames()))
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

	// Block exec from accessing sensitive directories (data dir, .goclaw, config file).
	// Prevents `cp /app/data/config.json workspace/` and similar exfiltration.
	// Exception: .goclaw/skills-store/ is allowed (skills may contain executable scripts).
	if execTool, ok := toolsReg.Get("exec"); ok {
		if et, ok := execTool.(*tools.ExecTool); ok {
			et.DenyPaths(dataDir, ".goclaw/")
			et.AllowPathExemptions(".goclaw/skills-store/")
			if cfgPath := os.Getenv("GOCLAW_CONFIG"); cfgPath != "" {
				et.DenyPaths(cfgPath)
			}
		}
	}

	// --- Store creation (Postgres) ---
	if cfg.Database.PostgresDSN == "" {
		slog.Error("GOCLAW_POSTGRES_DSN is required. Set it in your environment or .env.local file.")
		os.Exit(1)
	}

	var traceCollector *tracing.Collector

	// Schema compatibility check: ensure DB schema matches this binary.
	if err := checkSchemaOrAutoUpgrade(cfg.Database.PostgresDSN); err != nil {
		slog.Error("schema compatibility check failed", "error", err)
		os.Exit(1)
	}

	storeCfg := store.StoreConfig{
		PostgresDSN:   cfg.Database.PostgresDSN,
		EncryptionKey: os.Getenv("GOCLAW_ENCRYPTION_KEY"),
	}
	pgStores, pgErr := pg.NewPGStores(storeCfg)
	if pgErr != nil {
		slog.Error("failed to create PG stores", "error", pgErr)
		os.Exit(1)
	}
	if pgStores.Tracing != nil {
		traceCollector = tracing.NewCollector(pgStores.Tracing)
		traceCollector.OnFlush = func(traceIDs []uuid.UUID) {
			ids := make([]string, len(traceIDs))
			for i, id := range traceIDs {
				ids[i] = id.String()
			}
			msgBus.Broadcast(bus.Event{
				Name:    protocol.EventTraceUpdated,
				Payload: map[string]any{"trace_ids": ids},
			})
		}
		traceCollector.Start()
		slog.Info("LLM tracing enabled")
	}
	if traceCollector != nil {
		defer traceCollector.Stop()
		// OTel OTLP export: compiled via build tags. Build with 'go build -tags otel' to enable.
		initOTelExporter(context.Background(), cfg, traceCollector)
	}

	// Start snapshot worker for hourly usage aggregation
	if pgStores.Snapshots != nil {
		snapshotWorker := tracing.NewSnapshotWorker(pgStores.DB, pgStores.Snapshots)
		snapshotWorker.Start()
		defer snapshotWorker.Stop()

		// Backfill historical data in background
		go func() {
			count, err := snapshotWorker.Backfill(context.Background())
			if err != nil {
				slog.Warn("snapshot backfill failed", "error", err)
			} else if count > 0 {
				slog.Info("snapshot backfill complete", "hours", count)
			}
		}()
	}

	// Redis cache: compiled via build tags. Build with 'go build -tags redis' to enable.
	redisClient := initRedisClient(cfg)
	defer shutdownRedis(redisClient)

	// Wire cron config from config.json
	cronRetryCfg := cfg.Cron.ToRetryConfig()
	// Apply retry config via type assertion on the concrete cron store.
	pgStores.Cron.SetOnJob(nil) // ensure initialized; actual handler set below
	_ = cronRetryCfg            // config available; pg cron store reads it internally
	if cfg.Cron.DefaultTimezone != "" {
		pgStores.Cron.SetDefaultTimezone(cfg.Cron.DefaultTimezone)
	}

	// Load secrets from config_secrets table before env overrides.
	// Precedence: config.json → DB secrets → env vars (highest).
	if pgStores.ConfigSecrets != nil {
		if secrets, err := pgStores.ConfigSecrets.GetAll(context.Background()); err == nil && len(secrets) > 0 {
			cfg.ApplyDBSecrets(secrets)
			cfg.ApplyEnvOverrides()
			slog.Info("config secrets loaded from DB", "count", len(secrets))
		}
	}

	// Register providers from DB (overrides config providers).
	if pgStores.Providers != nil {
		dbGatewayAddr := loopbackAddr(cfg.Gateway.Host, cfg.Gateway.Port)
		registerProvidersFromDB(providerRegistry, pgStores.Providers, pgStores.ConfigSecrets, dbGatewayAddr, cfg.Gateway.Token, pgStores.MCP)
	}

	// Wire embedding provider to PGMemoryStore so IndexDocument generates vectors.
	// Per-agent DB config takes priority over config file defaults.
	if pgStores.Memory != nil {
		memCfg := cfg.Agents.Defaults.Memory
		if pgStores.Agents != nil {
			if defaultAgent, agErr := pgStores.Agents.GetByKey(context.Background(), "default"); agErr == nil {
				if agentMemCfg := defaultAgent.ParseMemoryConfig(); agentMemCfg != nil {
					memCfg = agentMemCfg
					slog.Debug("using per-agent memory config from DB", "agent", defaultAgent.AgentKey)
				}
			}
		}
		if embProvider := resolveEmbeddingProvider(cfg, memCfg, providerRegistry); embProvider != nil {
			pgStores.Memory.SetEmbeddingProvider(embProvider)
			slog.Info("memory embeddings enabled", "provider", embProvider.Name(), "model", embProvider.Model())

			// Backfill embeddings for existing chunks that were stored without vectors.
			type backfiller interface {
				BackfillEmbeddings(ctx context.Context) (int, error)
			}
			if bf, ok := pgStores.Memory.(backfiller); ok {
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
			slog.Warn("memory embeddings disabled (no API key), chunks stored without vectors")
		}
	}

	// Load bootstrap files for default agent's system prompt from DB.
	// Seeds if empty; falls back to filesystem as last resort.
	var contextFiles []bootstrap.ContextFile

	if pgStores.Agents != nil {
		bgCtx := context.Background()
		defaultAgent, agErr := pgStores.Agents.GetByKey(bgCtx, "default")
		if agErr == nil {
			dbFiles := bootstrap.LoadFromStore(bgCtx, pgStores.Agents, defaultAgent.ID)
			if len(dbFiles) > 0 {
				contextFiles = dbFiles
				slog.Info("bootstrap loaded from store", "count", len(dbFiles))
			} else {
				// DB empty → seed templates, then load
				if _, seedErr := bootstrap.SeedToStore(bgCtx, pgStores.Agents, defaultAgent.ID, defaultAgent.AgentType); seedErr != nil {
					slog.Warn("failed to seed bootstrap to store", "error", seedErr)
				} else {
					contextFiles = bootstrap.LoadFromStore(bgCtx, pgStores.Agents, defaultAgent.ID)
					slog.Info("bootstrap seeded and loaded from store", "count", len(contextFiles))
				}
			}
		}
	}

	if len(contextFiles) == 0 {
		// DB fallback: load from workspace filesystem
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
				batchMeta := map[string]string{
					"origin_channel":      meta.OriginChannel,
					"origin_peer_kind":    meta.OriginPeerKind,
					"parent_agent":        meta.ParentAgent,
					"subagent_label":      label,
					"origin_trace_id":     meta.OriginTraceID,
					"origin_root_span_id": meta.OriginRootSpanID,
				}
				if meta.OriginLocalKey != "" {
					batchMeta["origin_local_key"] = meta.OriginLocalKey
				}
				if meta.OriginSessionKey != "" {
					batchMeta["origin_session_key"] = meta.OriginSessionKey
				}
				// Collect media from all items in the batch.
				var batchMedia []bus.MediaFile
				for _, item := range items {
					batchMedia = append(batchMedia, item.Media...)
				}
				msgBus.PublishInbound(bus.InboundMessage{
					Channel:  "system",
					SenderID: senderID,
					ChatID:   meta.OriginChatID,
					Content:  content,
					UserID:   meta.OriginUserID,
					Metadata: batchMeta,
					Media:    batchMedia,
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

	// Skills loader + search tool
	// Global skills live under ~/.goclaw/skills/ (user-managed), not data/skills/.
	globalSkillsDir := os.Getenv("GOCLAW_SKILLS_DIR")
	if globalSkillsDir == "" {
		globalSkillsDir = filepath.Join(config.ExpandHome("~/.goclaw"), "skills")
	}
	// Bundled skills: shipped with the Docker image at /app/bundled-skills/.
	// Lowest priority — managed (skills-store) and user-uploaded skills override these.
	builtinSkillsDir := os.Getenv("GOCLAW_BUILTIN_SKILLS_DIR")
	if builtinSkillsDir == "" {
		builtinSkillsDir = "/app/bundled-skills"
	}
	skillsLoader := skills.NewLoader(workspace, globalSkillsDir, builtinSkillsDir)
	skillSearchTool := tools.NewSkillSearchTool(skillsLoader)
	toolsReg.Register(skillSearchTool)
	toolsReg.Register(tools.NewUseSkillTool())
	slog.Info("skill_search tool registered", "skills", len(skillsLoader.ListSkills()))

	// Wire skills-store directory into filesystem loader so agents
	// can discover uploaded skills in their system prompt and BM25 search index.
	if pgStores.Skills != nil {
		storeDirs := pgStores.Skills.Dirs()
		if len(storeDirs) > 0 {
			skillsLoader.SetManagedDir(storeDirs[0])
			slog.Info("skills-store directory wired into loader", "dir", storeDirs[0])

			// Seed system/bundled skills into DB
			bundledSkillsDir := os.Getenv("GOCLAW_BUNDLED_SKILLS_DIR")
			if bundledSkillsDir == "" {
				// Check common locations: Docker default, then local dev
				for _, candidate := range []string{"bundled-skills", "/app/bundled-skills", "skills"} {
					if info, err := os.Stat(candidate); err == nil && info.IsDir() {
						bundledSkillsDir = candidate
						break
					}
				}
			}
			if bundledSkillsDir != "" {
				if pgSkills, ok := pgStores.Skills.(*pg.PGSkillStore); ok {
					seeder := skills.NewSeeder(bundledSkillsDir, storeDirs[0], pgSkills)
					seeded, skipped, seededSkills, err := seeder.Seed(context.Background())
					if err != nil {
						slog.Warn("system skills seed failed", "error", err)
					} else {
						if seeded > 0 {
							slog.Info("system skills seeded", "seeded", seeded, "skipped", skipped)
						}
						// Check dependencies asynchronously — does not block startup.
						// Emits WS events per-skill so UI updates in realtime.
						if len(seededSkills) > 0 {
							seeder.CheckDepsAsync(seededSkills, msgBus)
						}
					}
				}
			}
		}
	}

	// Publish skill tool — lets agents register created skills in the database
	if pgStores.Skills != nil {
		if pgSkills, ok := pgStores.Skills.(*pg.PGSkillStore); ok {
			storeDirs := pgStores.Skills.Dirs()
			if len(storeDirs) > 0 {
				toolsReg.Register(tools.NewPublishSkillTool(pgSkills, storeDirs[0], skillsLoader))
				slog.Info("publish_skill tool registered")
			}
		}
	}

	// Wire embedding-based skill search + per-agent access filtering
	if pgStores.Skills != nil {
		if sas, ok := pgStores.Skills.(store.SkillAccessStore); ok {
			skillSearchTool.SetSkillAccessStore(sas)
		}
		if pgSkills, ok := pgStores.Skills.(*pg.PGSkillStore); ok {
			memCfg := cfg.Agents.Defaults.Memory
			if embProvider := resolveEmbeddingProvider(cfg, memCfg, providerRegistry); embProvider != nil {
				pgSkills.SetEmbeddingProvider(embProvider)
				skillSearchTool.SetEmbeddingSearcher(pgSkills, embProvider)
				slog.Info("skill embeddings enabled", "provider", embProvider.Name())

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
	toolsReg.Register(tools.NewCronTool(pgStores.Cron))
	slog.Info("cron tool registered")

	// Session tools (list, status, history, send)
	toolsReg.Register(tools.NewSessionsListTool())
	toolsReg.Register(tools.NewSessionStatusTool())
	toolsReg.Register(tools.NewSessionsHistoryTool())
	toolsReg.Register(tools.NewSessionsSendTool())

	// Message tool (send to channels)
	toolsReg.Register(tools.NewMessageTool(workspace, agentCfg.RestrictToWorkspace))
	slog.Info("session + message tools registered")

	// Register legacy tool aliases (backward-compat names from policy.go).
	for alias, canonical := range tools.LegacyToolAliases() {
		toolsReg.RegisterAlias(alias, canonical)
	}

	// Register Claude Code tool aliases so Claude Code skills work without modification.
	// LLM calls alias name → registry resolves to canonical tool → executes.
	for alias, canonical := range map[string]string{
		"Read":       "read_file",
		"Write":      "write_file",
		"Edit":       "edit",
		"Bash":       "exec",
		"WebFetch":   "web_fetch",
		"WebSearch":  "web_search",
		"Agent":      "spawn",
		"Skill":      "use_skill",
		"ToolSearch": "mcp_tool_search",
	} {
		toolsReg.RegisterAlias(alias, canonical)
	}
	slog.Info("tool aliases registered", "count", len(toolsReg.Aliases()))

	// Allow read_file to access skills directories and CLI workspaces (outside workspace).
	// Skills can live in ~/.goclaw/skills/, ~/.agents/skills/, ~/.goclaw/skills-store/, etc.
	// CLI workspaces live in ~/.goclaw/cli-workspaces/ (agent working files).
	homeDir, _ := os.UserHomeDir()
	if readTool, ok := toolsReg.Get("read_file"); ok {
		if pa, ok := readTool.(tools.PathAllowable); ok {
			pa.AllowPaths(globalSkillsDir)
			if homeDir != "" {
				pa.AllowPaths(filepath.Join(homeDir, ".agents", "skills"))
				pa.AllowPaths(filepath.Join(homeDir, ".goclaw", "cli-workspaces"))
			}
			// Also allow the skills store directory (uploaded skill content).
			if pgStores.Skills != nil {
				pa.AllowPaths(pgStores.Skills.Dirs()...)
			}
		}
	}

	// Memory tools are PG-backed; always available.
	hasMemory := true

	// Wire SessionStoreAware + BusAware on tools that need them
	for _, name := range []string{"sessions_list", "session_status", "sessions_history", "sessions_send"} {
		if t, ok := toolsReg.Get(name); ok {
			if sa, ok := t.(tools.SessionStoreAware); ok {
				sa.SetSessionStore(pgStores.Sessions)
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

	// Create all agents — resolved lazily from database by the managed resolver.
	agentRouter := agent.NewRouter()
	slog.Info("agents will be resolved lazily from database")

	// Create gateway server and wire enforcement
	server := gateway.NewServer(cfg, msgBus, agentRouter, pgStores.Sessions, toolsReg)
	server.SetVersion(Version)
	server.SetDB(pgStores.DB)
	server.SetPolicyEngine(permPE)
	server.SetPairingService(pgStores.Pairing)
	server.SetMessageBus(msgBus)
	server.SetOAuthHandler(httpapi.NewOAuthHandler(cfg.Gateway.Token, pgStores.Providers, pgStores.ConfigSecrets, providerRegistry, msgBus))

	// contextFileInterceptor is created inside wireExtras.
	// Declared here so it can be passed to registerAllMethods → AgentsMethods
	// for immediate cache invalidation on agents.files.set.
	var contextFileInterceptor *tools.ContextFileInterceptor
	var delegateMgr *tools.DelegateManager

	// Set agent store for tools_invoke context injection + wire extras
	if pgStores.Agents != nil {
		server.SetAgentStore(pgStores.Agents)
	}

	// Dynamic custom tools: load global tools from DB before resolver
	var dynamicLoader *tools.DynamicToolLoader
	if pgStores.CustomTools != nil {
		dynamicLoader = tools.NewDynamicToolLoader(pgStores.CustomTools, workspace)
		if err := dynamicLoader.LoadGlobal(context.Background(), toolsReg); err != nil {
			slog.Warn("failed to load global custom tools", "error", err)
		}
	}

	var mcpPool *mcpbridge.Pool
	var mediaStore *media.Store
	contextFileInterceptor, delegateMgr, mcpPool, mediaStore = wireExtras(pgStores, agentRouter, providerRegistry, msgBus, pgStores.Sessions, toolsReg, toolPE, skillsLoader, hasMemory, traceCollector, workspace, cfg.Gateway.InjectionAction, cfg, sandboxMgr, dynamicLoader, redisClient)
	if mcpPool != nil {
		defer mcpPool.Stop()
	}
	gatewayAddr := loopbackAddr(cfg.Gateway.Host, cfg.Gateway.Port)
	var mcpToolLister httpapi.MCPToolLister
	if mcpMgr != nil {
		mcpToolLister = mcpMgr
	}
	agentsH, skillsH, tracesH, mcpH, customToolsH, channelInstancesH, providersH, delegationsH, builtinToolsH, pendingMessagesH := wireHTTP(pgStores, cfg.Gateway.Token, msgBus, toolsReg, providerRegistry, permPE.IsOwner, gatewayAddr, mcpToolLister)
	if agentsH != nil {
		server.SetAgentsHandler(agentsH)
	}
	if skillsH != nil {
		server.SetSkillsHandler(skillsH)
	}
	if tracesH != nil {
		server.SetTracesHandler(tracesH)
	}
	// External wake/trigger API
	wakeH := httpapi.NewWakeHandler(agentRouter, cfg.Gateway.Token)
	server.SetWakeHandler(wakeH)
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
	if pendingMessagesH != nil {
		if pc := cfg.Channels.PendingCompaction; pc != nil {
			pendingMessagesH.SetKeepRecent(pc.KeepRecent)
			pendingMessagesH.SetMaxTokens(pc.MaxTokens)
			pendingMessagesH.SetProviderModel(pc.Provider, pc.Model)
		}
		server.SetPendingMessagesHandler(pendingMessagesH)
	}

	// Activity audit log API
	if pgStores.Activity != nil {
		server.SetActivityHandler(httpapi.NewActivityHandler(pgStores.Activity, cfg.Gateway.Token))
	}

	// Usage analytics API
	if pgStores.Snapshots != nil {
		server.SetUsageHandler(httpapi.NewUsageHandler(pgStores.Snapshots, pgStores.DB, cfg.Gateway.Token))
	}

	// Memory management API (wired directly, only needs MemoryStore + token)
	if pgStores != nil && pgStores.Memory != nil {
		server.SetMemoryHandler(httpapi.NewMemoryHandler(pgStores.Memory, cfg.Gateway.Token))
	}

	// Knowledge graph API
	if pgStores != nil && pgStores.KnowledgeGraph != nil {
		server.SetKnowledgeGraphHandler(httpapi.NewKnowledgeGraphHandler(pgStores.KnowledgeGraph, providerRegistry, cfg.Gateway.Token))
	}

	// Workspace file serving endpoint — serves files by absolute path, auth-token protected.
	// Supports media from any agent workspace (each agent has its own workspace from DB).
	server.SetFilesHandler(httpapi.NewFilesHandler(cfg.Gateway.Token))

	// Storage file management — browse/delete files under ~/.goclaw/ (excluding skills dirs).
	server.SetStorageHandler(httpapi.NewStorageHandler(config.ExpandHome("~/.goclaw"), cfg.Gateway.Token))

	// Media upload endpoint — accepts multipart file uploads, returns temp path + MIME type.
	server.SetMediaUploadHandler(httpapi.NewMediaUploadHandler(cfg.Gateway.Token))

	// Media serve endpoint — serves persisted media files by ID for WS/web clients.
	if mediaStore != nil {
		server.SetMediaServeHandler(httpapi.NewMediaServeHandler(mediaStore, cfg.Gateway.Token))
	}

	// Seed + apply builtin tool disables
	if pgStores.BuiltinTools != nil {
		seedBuiltinTools(context.Background(), pgStores.BuiltinTools)
		migrateBuiltinToolSettings(context.Background(), pgStores.BuiltinTools)
		applyBuiltinToolDisables(context.Background(), pgStores.BuiltinTools, toolsReg)
	}

	// Register all RPC methods
	server.SetLogTee(logTee)
	pairingMethods := registerAllMethods(server, agentRouter, pgStores.Sessions, pgStores.Cron, pgStores.Pairing, cfg, cfgPath, workspace, dataDir, msgBus, execApprovalMgr, pgStores.Agents, pgStores.Skills, pgStores.ConfigSecrets, pgStores.Teams, contextFileInterceptor, logTee)

	// Wire pairing event broadcasts to all WS clients.
	pairingMethods.SetBroadcaster(server.BroadcastEvent)
	if ps, ok := pgStores.Pairing.(*pg.PGPairingStore); ok {
		ps.SetOnRequest(func(code, senderID, channel, chatID string) {
			server.BroadcastEvent(*protocol.NewEvent(protocol.EventDevicePairReq, map[string]any{
				"code": code, "sender_id": senderID, "channel": channel, "chat_id": chatID,
			}))
		})
	}

	// Channel manager
	channelMgr := channels.NewManager(msgBus)

	// Wire channel sender on message tool (now that channelMgr exists)
	if t, ok := toolsReg.Get("message"); ok {
		if cs, ok := t.(tools.ChannelSenderAware); ok {
			cs.SetChannelSender(channelMgr.SendToChannel)
		}
	}

	// Load channel instances from DB.
	var instanceLoader *channels.InstanceLoader
	if pgStores.ChannelInstances != nil {
		instanceLoader = channels.NewInstanceLoader(pgStores.ChannelInstances, pgStores.Agents, channelMgr, msgBus, pgStores.Pairing)
		instanceLoader.SetProviderRegistry(providerRegistry)
		instanceLoader.SetPendingCompactionConfig(cfg.Channels.PendingCompaction)
		instanceLoader.RegisterFactory(channels.TypeTelegram, telegram.FactoryWithStores(pgStores.Agents, pgStores.Teams, pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeDiscord, discord.FactoryWithPendingStore(pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeFeishu, feishu.FactoryWithPendingStore(pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeZaloOA, zalo.Factory)
		instanceLoader.RegisterFactory(channels.TypeZaloPersonal, zalopersonal.FactoryWithPendingStore(pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeWhatsApp, whatsapp.Factory)
		instanceLoader.RegisterFactory(channels.TypeSlack, slackchannel.FactoryWithPendingStore(pgStores.PendingMessages))
		if err := instanceLoader.LoadAll(context.Background()); err != nil {
			slog.Error("failed to load channel instances from DB", "error", err)
		}
	}

	// Register config-based channels as fallback when no DB instances loaded.
	registerConfigChannels(cfg, channelMgr, msgBus, pgStores, instanceLoader)

	// Register channels/instances/links/teams RPC methods
	wireChannelRPCMethods(server, pgStores, channelMgr, agentRouter, msgBus)

	// Wire channel event subscribers (cache invalidation, pairing, cascade disable)
	wireChannelEventSubscribers(msgBus, server, pgStores, channelMgr, instanceLoader, pairingMethods, cfg)

	// Audit log subscriber — persists audit events to activity_logs table.
	// Uses a buffered channel with a single worker to avoid unbounded goroutines.
	var auditCh chan bus.AuditEventPayload
	if pgStores.Activity != nil {
		auditCh = make(chan bus.AuditEventPayload, 256)
		msgBus.Subscribe(bus.TopicAudit, func(evt bus.Event) {
			if evt.Name != protocol.EventAuditLog {
				return
			}
			payload, ok := evt.Payload.(bus.AuditEventPayload)
			if !ok {
				return
			}
			select {
			case auditCh <- payload:
			default:
				slog.Warn("audit.queue_full", "action", payload.Action)
			}
		})
		go func() {
			for payload := range auditCh {
				if err := pgStores.Activity.Log(context.Background(), &store.ActivityLog{
					ActorType:  payload.ActorType,
					ActorID:    payload.ActorID,
					Action:     payload.Action,
					EntityType: payload.EntityType,
					EntityID:   payload.EntityID,
					IPAddress:  payload.IPAddress,
					Details:    payload.Details,
				}); err != nil {
					slog.Warn("audit.log_failed", "action", payload.Action, "error", err)
				}
			}
		}()
		slog.Info("audit subscriber registered")
	}

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
	pgStores.Cron.SetOnJob(makeCronJobHandler(sched, msgBus, cfg, channelMgr))
	pgStores.Cron.SetOnEvent(func(event store.CronEvent) {
		server.BroadcastEvent(*protocol.NewEvent(protocol.EventCron, event))
	})
	if err := pgStores.Cron.Start(); err != nil {
		slog.Warn("cron service failed to start", "error", err)
	}

	// Adaptive throttle: reduce per-session concurrency when nearing the summary threshold.
	// This prevents concurrent runs from racing with summarization.
	// Uses calibrated token estimation (actual prompt tokens from last LLM call)
	// and the agent's real context window (cached on session by the Loop).
	sched.SetTokenEstimateFunc(func(sessionKey string) (int, int) {
		history := pgStores.Sessions.GetHistory(sessionKey)
		lastPT, lastMC := pgStores.Sessions.GetLastPromptTokens(sessionKey)
		tokens := agent.EstimateTokensWithCalibration(history, lastPT, lastMC)
		cw := pgStores.Sessions.GetContextWindow(sessionKey)
		if cw <= 0 {
			cw = 200000 // fallback for sessions not yet processed
		}
		return tokens, cw
	})

	// Subscribe to agent events for channel streaming/reaction forwarding.
	// Events emitted by agent loops are broadcast to the bus; we forward them
	// to the channel manager which routes to StreamingChannel/ReactionChannel.
	msgBus.Subscribe(bus.TopicChannelStreaming, func(event bus.Event) {
		if event.Name != protocol.EventAgent {
			return
		}
		agentEvent, ok := event.Payload.(agent.AgentEvent)
		if !ok {
			return
		}
		channelMgr.HandleAgentEvent(agentEvent.Type, agentEvent.RunID, agentEvent.Payload)

		// Route activity events to Router (status registry) and DelegateManager (progress tracking).
		if agentEvent.Type == protocol.AgentEventActivity {
			payloadMap, _ := agentEvent.Payload.(map[string]any)
			phase, _ := payloadMap["phase"].(string)
			tool, _ := payloadMap["tool"].(string)
			iteration := 0
			if v, ok := payloadMap["iteration"].(int); ok {
				iteration = v
			}

			// Update Router activity registry (for status queries via LLM classify)
			if sessionKey := agentRouter.SessionKeyForRun(agentEvent.RunID); sessionKey != "" {
				agentRouter.UpdateActivity(sessionKey, agentEvent.RunID, phase, tool, iteration)
			}

			// Update DelegateManager activity tracking (for enriched progress notifications)
			if delegateMgr != nil && agentEvent.DelegationID != "" {
				delegateMgr.HandleActivityEvent(agentEvent.DelegationID, phase, tool)
			}
		}

		// Clear activity on terminal events
		if agentEvent.Type == protocol.AgentEventRunCompleted || agentEvent.Type == protocol.AgentEventRunFailed {
			if sessionKey := agentRouter.SessionKeyForRun(agentEvent.RunID); sessionKey != "" {
				agentRouter.ClearActivity(sessionKey)
			}
		}
	})

	// Start inbound message consumer (channel → scheduler → agent → channel)
	consumerTeamStore := pgStores.Teams

	// Quota checker: enforces per-user/group request limits.
	// Merge per-group quotas from channel configs into gateway.quota.groups.
	config.MergeChannelGroupQuotas(cfg)
	var quotaChecker *channels.QuotaChecker
	if cfg.Gateway.Quota != nil && cfg.Gateway.Quota.Enabled {
		quotaChecker = channels.NewQuotaChecker(pgStores.DB, *cfg.Gateway.Quota)
		defer quotaChecker.Stop()
		slog.Info("channel quota enabled",
			"default_hour", cfg.Gateway.Quota.Default.Hour,
			"default_day", cfg.Gateway.Quota.Default.Day,
			"default_week", cfg.Gateway.Quota.Default.Week,
		)
	}

	// Register quota usage RPC.
	// Pass DB so summary cards still work when quota is disabled (queries traces directly).
	methods.NewQuotaMethods(quotaChecker, pgStores.DB).Register(server.Router())

	// Reload quota config on config changes via pub/sub.
	if quotaChecker != nil {
		msgBus.Subscribe("quota-config-reload", func(evt bus.Event) {
			if evt.Name != bus.TopicConfigChanged {
				return
			}
			updatedCfg, ok := evt.Payload.(*config.Config)
			if !ok || updatedCfg.Gateway.Quota == nil {
				return
			}
			config.MergeChannelGroupQuotas(updatedCfg)
			quotaChecker.UpdateConfig(*updatedCfg.Gateway.Quota)
			slog.Info("quota config reloaded via pub/sub")
		})
	}

	// Reload cron default timezone on config changes via pub/sub.
	msgBus.Subscribe("cron-config-reload", func(evt bus.Event) {
		if evt.Name != bus.TopicConfigChanged {
			return
		}
		updatedCfg, ok := evt.Payload.(*config.Config)
		if !ok {
			return
		}
		pgStores.Cron.SetDefaultTimezone(updatedCfg.Cron.DefaultTimezone)
	})

	// Reload web_fetch domain policy on config changes via pub/sub.
	msgBus.Subscribe("webfetch-config-reload", func(evt bus.Event) {
		if evt.Name != bus.TopicConfigChanged {
			return
		}
		updatedCfg, ok := evt.Payload.(*config.Config)
		if !ok {
			return
		}
		webFetchTool.UpdatePolicy(updatedCfg.Tools.WebFetch.Policy, updatedCfg.Tools.WebFetch.AllowedDomains, updatedCfg.Tools.WebFetch.BlockedDomains)
	})

	// Contact collector: auto-collect user info from channels with in-memory dedup cache.
	var contactCollector *store.ContactCollector
	if pgStores.Contacts != nil {
		contactCollector = store.NewContactCollector(pgStores.Contacts, cache.NewInMemoryCache[bool]())
		channelMgr.SetContactCollector(contactCollector) // propagate to all channel handlers
	}

	go consumeInboundMessages(ctx, msgBus, agentRouter, cfg, sched, channelMgr, consumerTeamStore, quotaChecker, delegateMgr, pgStores.Sessions, pgStores.Agents, contactCollector)

	go func() {
		sig := <-sigCh
		slog.Info("graceful shutdown initiated", "signal", sig)

		// Broadcast shutdown event
		server.BroadcastEvent(*protocol.NewEvent(protocol.EventShutdown, nil))

		// Stop channels and cron
		channelMgr.StopAll(context.Background())
		pgStores.Cron.Stop()

		// Drain audit log queue before closing DB
		if auditCh != nil {
			close(auditCh)
		}

		// Close provider resources (e.g. Claude CLI temp files)
		providerRegistry.Close()

		// Stop sandbox pruning + release containers
		if sandboxMgr != nil {
			sandboxMgr.Stop()
			slog.Info("releasing sandbox containers...")
			sandboxMgr.ReleaseAll(context.Background())
		}

		cancel()
	}()

	slog.Info("goclaw gateway starting",
		"version", Version,
		"protocol", protocol.ProtocolVersion,
		"agents", agentRouter.List(),
		"tools", toolsReg.Count(),
		"channels", channelMgr.GetEnabledChannels(),
	)

	// Tailscale listener: build the mux first, then pass it to initTailscale
	// so the same routes are served on both the main listener and Tailscale.
	// Compiled via build tags: `go build -tags tsnet` to enable.
	mux := server.BuildMux()

	// Mount channel webhook handlers on the main mux (e.g. Feishu /feishu/events).
	// This allows webhook-based channels to share the main server port.
	for _, route := range channelMgr.WebhookHandlers() {
		mux.Handle(route.Path, route.Handler)
		slog.Info("webhook route mounted on gateway", "path", route.Path)
	}

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
