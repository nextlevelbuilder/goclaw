package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/consolidation"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	kg "github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/channels/discord"
	"github.com/nextlevelbuilder/goclaw/internal/channels/feishu"
	slackchannel "github.com/nextlevelbuilder/goclaw/internal/channels/slack"
	"github.com/nextlevelbuilder/goclaw/internal/channels/telegram"
	"github.com/nextlevelbuilder/goclaw/internal/channels/whatsapp"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo"
	zalopersonal "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func runGateway() {
	// Setup structured logging
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	// Env override (docker/K8s friendly, default: info): GOCLAW_LOG_LEVEL=debug|info|warn|error
	if lvl := os.Getenv("GOCLAW_LOG_LEVEL"); lvl != "" {
		switch strings.ToLower(lvl) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		default:
			fmt.Fprintf(os.Stderr, "warning: unknown GOCLAW_LOG_LEVEL=%q, using info\n", lvl)
		}
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

	// Edition override: explicit GOCLAW_EDITION takes precedence over auto-detection.
	// Auto-detection happens later in setupStoresAndTracing (sqlite → lite).
	if edName := os.Getenv("GOCLAW_EDITION"); edName != "" {
		switch edName {
		case "lite":
			edition.SetCurrent(edition.Lite)
			slog.Info("edition: lite (explicit)")
		case "standard":
			edition.SetCurrent(edition.Standard)
			slog.Info("edition: standard (explicit)")
		default:
			slog.Warn("unknown GOCLAW_EDITION, using standard", "value", edName)
		}
	}

	// Create core components
	msgBus := bus.New()

	// V3 domain event bus for consolidation pipeline (episodic → semantic → dreaming)
	domainBus := eventbus.NewDomainEventBus(eventbus.Config{
		QueueSize:   1000,
		WorkerCount: 2,
	})
	domainBus.Start(context.Background())
	defer func() {
		if err := domainBus.Drain(10 * time.Second); err != nil {
			slog.Warn("domain event bus drain timeout", "error", err)
		}
	}()

	// Create model registry with forward-compat resolvers (shared across all providers)
	modelReg := providers.NewInMemoryRegistry()
	modelReg.RegisterResolver("anthropic", &providers.AnthropicForwardCompat{})
	modelReg.RegisterResolver("openai", &providers.OpenAIForwardCompat{})

	// Create provider registry
	providerRegistry := providers.NewRegistry(store.TenantIDFromContext)
	registerProviders(providerRegistry, cfg, modelReg)

	// Resolve workspace (must be absolute for system prompt + file tool path resolution)
	workspace := config.ExpandHome(cfg.Agents.Defaults.Workspace)
	if !filepath.IsAbs(workspace) {
		workspace, _ = filepath.Abs(workspace)
	}
	os.MkdirAll(workspace, 0755)

	// Detect server IPs for output scrubbing (prevents IP leaks via web_fetch, exec, etc.)
	// Skip for desktop/lite — localhost-only, no multi-tenant exposure risk
	if !edition.Current().IsLimited() {
		tools.DetectServerIPs(context.Background())
	}

	toolsReg, execApprovalMgr, mcpMgr, sandboxMgr, browserMgr, webFetchTool, ttsTool, permPE, toolPE, dataDir, agentCfg := setupToolRegistry(cfg, workspace, providerRegistry)
	if browserMgr != nil {
		defer browserMgr.Close()
	}
	if mcpMgr != nil {
		defer mcpMgr.Stop()
	}

	pgStores, traceCollector, snapshotWorker := setupStoresAndTracing(cfg, dataDir, msgBus)
	if traceCollector != nil {
		defer traceCollector.Stop()
		// OTel OTLP export: compiled via build tags. Build with 'go build -tags otel' to enable.
		initOTelExporter(context.Background(), cfg, traceCollector)
	}
	if snapshotWorker != nil {
		defer snapshotWorker.Stop()
	}

	// Redis cache: compiled via build tags. Build with 'go build -tags redis' to enable.
	redisClient := initRedisClient(cfg)
	defer shutdownRedis(redisClient)

	// Register providers from DB (overrides config providers).
	if pgStores.Providers != nil {
		dbGatewayAddr := loopbackAddr(cfg.Gateway.Host, cfg.Gateway.Port)
		registerProvidersFromDB(providerRegistry, pgStores.Providers, pgStores.ConfigSecrets, dbGatewayAddr, cfg.Gateway.Token, pgStores.MCP, cfg, modelReg)
	}
	slog.Info("model registry initialized", "anthropic_models", len(modelReg.Catalog("anthropic")), "openai_models", len(modelReg.Catalog("openai")))

	// Warn if deprecated session scope settings are configured
	if cfg.Sessions.Scope != "" && cfg.Sessions.Scope != "per-sender" {
		slog.Warn("sessions.scope config is deprecated and ignored — fixed to per-sender", "configured", cfg.Sessions.Scope)
	}
	if cfg.Sessions.DmScope != "" && cfg.Sessions.DmScope != "per-channel-peer" {
		slog.Warn("sessions.dm_scope config is deprecated and ignored — fixed to per-channel-peer", "configured", cfg.Sessions.DmScope)
	}

	seedSystemConfigs(pgStores.SystemConfigs, pgStores.Tenants, cfg)
	// Read back system_configs from DB and overlay onto in-memory config.
	if pgStores.SystemConfigs != nil {
		if sysConfigs, err := pgStores.SystemConfigs.List(
			store.WithTenantID(context.Background(), store.MasterTenantID),
		); err == nil && len(sysConfigs) > 0 {
			cfg.ApplySystemConfigs(sysConfigs)
			slog.Info("system_configs applied to in-memory config", "keys", len(sysConfigs))
		}
	}
	setupMemoryEmbeddings(pgStores, providerRegistry)

	// V3: Wire consolidation pipeline (episodic → semantic → KG → dreaming)
	if pgStores.Episodic != nil {
		var consolidationProvider providers.Provider
		if names := providerRegistry.ListForTenant(providers.MasterTenantID); len(names) > 0 {
			consolidationProvider, _ = providerRegistry.GetForTenant(providers.MasterTenantID, names[0])
		}
		if consolidationProvider != nil {
			// Create KG extractor for semantic worker (entity/relation extraction from episodic summaries)
			var kgExtractor *kg.Extractor
			if pgStores.KnowledgeGraph != nil {
				kgExtractor = kg.NewExtractor(consolidationProvider, consolidationProvider.DefaultModel(), 0)
			}
			cleanupConsolidation := consolidation.Register(consolidation.ConsolidationDeps{
				EpisodicStore: pgStores.Episodic,
				MemoryStore:   pgStores.Memory,
				KGStore:       pgStores.KnowledgeGraph,
				SessionStore:  pgStores.Sessions,
				EventBus:      domainBus,
				Provider:      consolidationProvider,
				Model:         consolidationProvider.DefaultModel(),
				Extractor:     kgExtractor,
			})
			defer cleanupConsolidation()
			slog.Info("consolidation pipeline registered")
		} else {
			slog.Warn("consolidation pipeline skipped: no provider available")
		}
	}

	// V3: Wire vault enrichment worker (async summary + embedding + auto-linking).
	// Resolves provider independently from consolidation pipeline.
	if pgStores.Vault != nil {
		var vaultProvider providers.Provider
		if names := providerRegistry.ListForTenant(providers.MasterTenantID); len(names) > 0 {
			vaultProvider, _ = providerRegistry.GetForTenant(providers.MasterTenantID, names[0])
		}
		if vaultProvider != nil {
			cleanupVaultEnrich := vault.RegisterEnrichWorker(vault.EnrichWorkerDeps{
				VaultStore: pgStores.Vault,
				Provider:   vaultProvider,
				Model:      vaultProvider.DefaultModel(),
				EventBus:   domainBus,
			})
			defer cleanupVaultEnrich()
			slog.Info("vault enrichment worker registered")
		}
	}

	loadBootstrapFiles(pgStores, workspace, agentCfg)

	// Backfill CAPABILITIES.md for pre-v3 agents that don't have it yet.
	if count, err := bootstrap.BackfillCapabilities(context.Background(), pgStores.DB); err != nil {
		slog.Warn("bootstrap: capabilities backfill failed", "error", err)
	} else if count > 0 {
		slog.Info("bootstrap: capabilities backfill complete", "agents", count)
	}

	// Subagent system
	subagentMgr := setupSubagents(providerRegistry, cfg, msgBus, toolsReg, workspace, sandboxMgr)
	if subagentMgr != nil {
		// Wire announce queue for batched subagent result delivery (matching TS debounce pattern).
		announceQueue := tools.NewAnnounceQueue(1000, 20, makeDelegateAnnounceCallback(subagentMgr, msgBus))
		subagentMgr.SetAnnounceQueue(announceQueue)
		if pgStores.SubagentTasks != nil {
			subagentMgr.SetTaskStore(pgStores.SubagentTasks)
		}

		toolsReg.Register(tools.NewSpawnTool(subagentMgr, "default", 0))
		slog.Info("subagent system enabled", "tools", []string{"spawn"})
	}

	skillsLoader, skillSearchTool, globalSkillsDir, bundledSkillsDir, builtinSkillsDir := setupSkillsSystem(cfg, workspace, dataDir, pgStores, toolsReg, providerRegistry, msgBus)
	_ = skillSearchTool // used via wireExtras → skillsLoader; kept for type clarity

	// Register cron/heartbeat/session/message tools, aliases, allow-paths, store wiring.
	heartbeatTool, hasMemory := wireExtraTools(pgStores, toolsReg, msgBus, workspace, dataDir, agentCfg, globalSkillsDir, builtinSkillsDir)

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
	server.SetOAuthHandler(httpapi.NewOAuthHandler(pgStores.Providers, pgStores.ConfigSecrets, providerRegistry, msgBus))

	// contextFileInterceptor is created inside wireExtras.
	// Declared here so it can be passed to registerAllMethods → AgentsMethods
	// for immediate cache invalidation on agents.files.set.
	var contextFileInterceptor *tools.ContextFileInterceptor

	// Set agent store for tools_invoke context injection + wire extras
	if pgStores.Agents != nil {
		server.SetAgentStore(pgStores.Agents)
	}

	var mcpPool *mcpbridge.Pool
	var mediaStore *media.Store
	var postTurn tools.PostTurnProcessor
	contextFileInterceptor, mcpPool, mediaStore, postTurn = wireExtras(pgStores, agentRouter, providerRegistry, msgBus, pgStores.Sessions, toolsReg, toolPE, skillsLoader, hasMemory, traceCollector, workspace, cfg.Gateway.InjectionAction, cfg, sandboxMgr, redisClient, domainBus)
	if mcpPool != nil {
		defer mcpPool.Stop()
	}

	// Populate shared deps struct used by extracted helper methods.
	deps := &gatewayDeps{
		cfg:              cfg,
		server:           server,
		msgBus:           msgBus,
		pgStores:         pgStores,
		providerRegistry: providerRegistry,
		agentRouter:      agentRouter,
		toolsReg:         toolsReg,
		skillsLoader:     skillsLoader,
		workspace:        workspace,
		dataDir:          dataDir,
	}

	gatewayAddr := loopbackAddr(cfg.Gateway.Host, cfg.Gateway.Port)
	var mcpToolLister httpapi.MCPToolLister
	if mcpMgr != nil {
		mcpToolLister = mcpMgr
	}
	httpapi.InitGatewayToken(cfg.Gateway.Token)
	exportTokenStore := httpapi.InitExportTokenStore()
	defer exportTokenStore.Stop()
	agentsH, skillsH, tracesH, mcpH, channelInstancesH, providersH, builtinToolsH, pendingMessagesH, teamEventsH, secureCLIH, secureCLIGrantH, mcpUserCredsH := wireHTTP(pgStores, cfg.Agents.Defaults.Workspace, dataDir, bundledSkillsDir, msgBus, toolsReg, providerRegistry, permPE.IsOwner, gatewayAddr, mcpToolLister)

	// Wire dependencies for system prompt preview parity.
	if agentsH != nil {
		agentsH.SetPreviewDeps(toolsReg, skillsLoader)
		agentsH.SetPreviewStores(pgStores.Teams, pgStores.AgentLinks)
	}

	// External wake/trigger API
	wakeH := httpapi.NewWakeHandler(agentRouter)
	if postTurn != nil {
		wakeH.SetPostTurnProcessor(postTurn)
	}

	// Wire all server.Set*Handler() calls via extracted helper.
	deps.wireHTTPHandlersOnServer(
		httpHandlers{
			agents:           agentsH,
			skills:           skillsH,
			traces:           tracesH,
			mcp:              mcpH,
			channelInstances: channelInstancesH,
			providers:        providersH,
			builtinTools:     builtinToolsH,
			pendingMessages:  pendingMessagesH,
			teamEvents:       teamEventsH,
			secureCLI:        secureCLIH,
			secureCLIGrant:   secureCLIGrantH,
			mcpUserCreds:     mcpUserCredsH,
		},
		wakeH,
		mcpPool,
		postTurn,
		mediaStore,
	)

	// System backup API — admin + owner only, SSE progress streaming.
	server.SetBackupHandler(httpapi.NewBackupHandler(cfg, cfg.Database.PostgresDSN, Version, permPE.IsOwner))

	// System restore API — admin + owner only, multipart upload + SSE progress.
	server.SetRestoreHandler(httpapi.NewRestoreHandler(cfg, cfg.Database.PostgresDSN, permPE.IsOwner))

	// S3 backup integration — admin + owner only.
	server.SetBackupS3Handler(httpapi.NewBackupS3Handler(cfg, cfg.Database.PostgresDSN, Version, pgStores.ConfigSecrets, permPE.IsOwner))

	// Tenant-scoped backup/restore — owner or tenant admin.
	if pgStores.Tenants != nil {
		server.SetTenantBackupHandler(httpapi.NewTenantBackupHandler(pgStores.DB, cfg, pgStores.Tenants, Version, permPE.IsOwner))
	}

	// Register all RPC methods
	server.SetLogTee(logTee)
	pairingMethods, heartbeatMethods, chatMethods := registerAllMethods(server, agentRouter, pgStores.Sessions, pgStores.Cron, pgStores.Pairing, cfg, cfgPath, workspace, dataDir, msgBus, execApprovalMgr, pgStores.Agents, pgStores.Skills, pgStores.ConfigSecrets, pgStores.Teams, contextFileInterceptor, logTee, pgStores.Heartbeats, pgStores.ConfigPermissions, pgStores.SystemConfigs, pgStores.Tenants, pgStores.SkillTenantCfgs)

	// Wire post-turn processor for team task dispatch (WS chat.send + HTTP API paths).
	if postTurn != nil {
		chatMethods.SetPostTurnProcessor(postTurn)
		server.SetPostTurnProcessor(postTurn) // HTTP: /v1/chat/completions, /v1/responses
		wakeH.SetPostTurnProcessor(postTurn)  // HTTP: /v1/agents/{id}/wake
	}

	// Wire pairing event broadcasts to all WS clients.
	pairingMethods.SetBroadcaster(server.BroadcastEvent)
	// Wire pairing request callback — works for both PG and SQLite stores.
	type pairingRequestNotifier interface {
		SetOnRequest(func(code, senderID, channel, chatID string))
	}
	if ps, ok := pgStores.Pairing.(pairingRequestNotifier); ok {
		ps.SetOnRequest(func(code, senderID, channel, chatID string) {
			server.BroadcastEvent(*protocol.NewEvent(protocol.EventDevicePairReq, map[string]any{
				"code": code, "sender_id": senderID, "channel": channel, "chat_id": chatID,
			}))
		})
	}

	// Channel manager
	channelMgr := channels.NewManager(msgBus)
	deps.channelMgr = channelMgr

	// Wire channel sender + tenant checker on message tool (now that channelMgr exists)
	if t, ok := toolsReg.Get("message"); ok {
		if cs, ok := t.(tools.ChannelSenderAware); ok {
			cs.SetChannelSender(channelMgr.SendToChannel)
		}
		if tc, ok := t.(tools.ChannelTenantCheckerAware); ok {
			tc.SetChannelTenantChecker(channelMgr.ChannelTenantID)
		}
	}
	// Wire group member lister on list_group_members tool
	if t, ok := toolsReg.Get("list_group_members"); ok {
		if gl, ok := t.(tools.GroupMemberListerAware); ok {
			gl.SetGroupMemberLister(channelMgr.ListGroupMembers)
		}
	}

	// Load channel instances from DB.
	var instanceLoader *channels.InstanceLoader
	if pgStores.ChannelInstances != nil {
		instanceLoader = channels.NewInstanceLoader(pgStores.ChannelInstances, pgStores.Agents, channelMgr, msgBus, pgStores.Pairing)
		instanceLoader.SetProviderRegistry(providerRegistry)
		instanceLoader.SetPendingCompactionConfig(cfg.Channels.PendingCompaction)
		instanceLoader.RegisterFactory(channels.TypeTelegram, telegram.FactoryWithStores(pgStores.Agents, pgStores.ConfigPermissions, pgStores.Teams, pgStores.SubagentTasks, pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeDiscord, discord.FactoryWithStores(pgStores.Agents, pgStores.ConfigPermissions, pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeFeishu, feishu.FactoryWithPendingStore(pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeZaloOA, zalo.Factory)
		instanceLoader.RegisterFactory(channels.TypeZaloPersonal, zalopersonal.FactoryWithPendingStore(pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeWhatsApp, whatsapp.FactoryWithDB(pgStores.DB, pgStores.PendingMessages, "pgx"))
		instanceLoader.RegisterFactory(channels.TypeSlack, slackchannel.FactoryWithPendingStore(pgStores.PendingMessages))
		if err := instanceLoader.LoadAll(context.Background()); err != nil {
			slog.Error("failed to load channel instances from DB", "error", err)
		}
	}

	// Register config-based channels as fallback when no DB instances loaded.
	registerConfigChannels(cfg, channelMgr, msgBus, pgStores, instanceLoader)

	// Register channels/instances/links/teams RPC methods
	wireChannelRPCMethods(server, pgStores, channelMgr, agentRouter, msgBus, workspace)

	// Wire channel event subscribers (cache invalidation, pairing, cascade disable)
	wireChannelEventSubscribers(msgBus, server, pgStores, channelMgr, instanceLoader, pairingMethods, cfg)

	// Audit log subscriber + team task event subscribers.
	auditCh := deps.wireAuditSubscriber()
	deps.wireEventSubscribers()

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server.StartUpdateChecker(ctx)

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
	// Must be created before cron setup so cron jobs route through the scheduler.
	sched := scheduler.NewScheduler(
		scheduler.DefaultLanes(),
		scheduler.DefaultQueueConfig(),
		makeSchedulerRunFunc(agentRouter, cfg),
	)
	defer sched.Stop()

	// Start cron + heartbeat ticker, wire wake functions and adaptive throttle.
	heartbeatTicker := startCronAndHeartbeat(pgStores, server, sched, msgBus, providerRegistry, channelMgr, cfg, heartbeatTool, heartbeatMethods)

	// Subscribe to agent events for channel streaming/reaction forwarding.
	deps.wireChannelStreamingSubscriber()

	// Slow tool notification subscriber — direct outbound when tool exceeds adaptive threshold.
	wireSlowToolNotifySubscriber(msgBus)

	// Inbound message consumer setup
	consumerTeamStore := pgStores.Teams

	// Quota checker: enforces per-user/group request limits.
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
	methods.NewQuotaMethods(quotaChecker, pgStores.DB).Register(server.Router())

	// API key management RPC
	if pgStores.APIKeys != nil {
		methods.NewAPIKeysMethods(pgStores.APIKeys).Register(server.Router())
	}

	// Tenant management RPC + HTTP
	if pgStores.Tenants != nil {
		methods.NewTenantsMethods(pgStores.Tenants, msgBus, workspace).Register(server.Router())
		server.SetTenantsHandler(httpapi.NewTenantsHandler(pgStores.Tenants, msgBus, workspace))
		server.Router().SetTenantStore(pgStores.Tenants)
		// Permission cache for tenant membership checks
		permCache := cache.NewPermissionCache()
		msgBus.Subscribe("permission-cache", func(e bus.Event) {
			if p, ok := e.Payload.(bus.CacheInvalidatePayload); ok {
				permCache.HandleInvalidation(p)
			}
		})
		server.Router().SetPermissionCache(permCache)
		httpapi.InitTenantStore(pgStores.Tenants, msgBus)
		httpapi.InitOwnerIDs(cfg.Gateway.OwnerIDs)
	}

	// Wire lifecycle: config-reload subscribers, consumer, task recovery, shutdown, server start.
	deps.runLifecycle(ctx, cancel, lifecycleDeps{
		sched:             sched,
		heartbeatTicker:   heartbeatTicker,
		quotaChecker:      quotaChecker,
		webFetchTool:      webFetchTool,
		ttsTool:           ttsTool,
		sandboxMgr:        sandboxMgr,
		postTurn:          postTurn,
		subagentMgr:       subagentMgr,
		consumerTeamStore: consumerTeamStore,
		auditCh:           auditCh,
		sigCh:             sigCh,
	})
}
