package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bgalert"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/channelmemory"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/bitrix24"
	"github.com/nextlevelbuilder/goclaw/internal/channels/discord"
	"github.com/nextlevelbuilder/goclaw/internal/channels/facebook"
	"github.com/nextlevelbuilder/goclaw/internal/channels/feishu"
	"github.com/nextlevelbuilder/goclaw/internal/channels/pancake"
	slackchannel "github.com/nextlevelbuilder/goclaw/internal/channels/slack"
	"github.com/nextlevelbuilder/goclaw/internal/channels/telegram"
	"github.com/nextlevelbuilder/goclaw/internal/channels/whatsapp"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo"
	zalopersonal "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/consolidation"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	kg "github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	mcpoauth "github.com/nextlevelbuilder/goclaw/internal/mcp/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
	usagepricing "github.com/nextlevelbuilder/goclaw/internal/usage/pricing"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"

	// Register workstation backend factories via init().
	_ "github.com/nextlevelbuilder/goclaw/internal/workstation/backends"
)

func gatewayLogOutput() io.Writer {
	logFile := strings.TrimSpace(os.Getenv("GOCLAW_LOG_FILE"))
	if logFile == "" {
		if st, err := os.Stat("/var/log/goclaw"); err == nil && st.IsDir() {
			logFile = "/var/log/goclaw/goclaw.log"
		}
	}
	if logFile == "" {
		return os.Stdout
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot open GOCLAW_LOG_FILE=%q: %v\n", logFile, err)
		return os.Stdout
	}
	fmt.Fprintf(os.Stderr, "logging to %s\n", logFile)
	return io.MultiWriter(os.Stdout, f)
}

type traceCostBackfiller interface {
	BackfillLLMCosts(context.Context) (store.TraceCostBackfillStats, error)
}

type traceUsageAggregateReconciler interface {
	ReconcileTraceUsageAggregates(context.Context) (store.TraceUsageAggregateStats, error)
}

type usageEventCostBackfiller interface {
	BackfillUsageEventCosts(context.Context) (store.UsageEventCostBackfillStats, error)
}

type snapshotCostBackfiller interface {
	BackfillSnapshotCosts(context.Context) (store.SnapshotCostBackfillStats, error)
}

type snapshotBucketRefresher interface {
	RefreshBuckets(context.Context, []time.Time) (int, error)
}

func backfillTraceCostsAfterPricingSync(ctx context.Context, stores *store.Stores, snapshots snapshotBucketRefresher) {
	if stores == nil {
		return
	}
	backfillCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if stores.Tracing != nil {
		backfiller, ok := stores.Tracing.(traceCostBackfiller)
		if ok {
			stats, err := backfiller.BackfillLLMCosts(backfillCtx)
			if err != nil {
				slog.Warn("usage_pricing.trace_cost_backfill_failed", "error", err)
			} else {
				refreshedBuckets := 0
				if snapshots != nil && len(stats.SnapshotBuckets) > 0 {
					refreshedBuckets, err = snapshots.RefreshBuckets(backfillCtx, stats.SnapshotBuckets)
					if err != nil {
						slog.Warn("usage_pricing.trace_cost_snapshot_refresh_failed", "error", err, "buckets", len(stats.SnapshotBuckets))
					}
				}
				if stats.SpanRowsUpdated > 0 || stats.TraceRowsUpdated > 0 || refreshedBuckets > 0 {
					slog.Info("usage_pricing.trace_cost_backfill_complete",
						"spans", stats.SpanRowsUpdated,
						"traces", stats.TraceRowsUpdated,
						"snapshot_buckets", refreshedBuckets,
					)
				}
			}
		}
	}

	if stores.Tracing != nil {
		reconciler, ok := stores.Tracing.(traceUsageAggregateReconciler)
		if ok {
			stats, err := reconciler.ReconcileTraceUsageAggregates(backfillCtx)
			if err != nil {
				slog.Warn("usage_pricing.trace_usage_aggregate_reconcile_failed", "error", err)
			} else if stats.TraceRowsUpdated > 0 {
				slog.Info("usage_pricing.trace_usage_aggregate_reconcile_complete", "traces", stats.TraceRowsUpdated)
			}
		}
	}

	if stores.Snapshots != nil {
		backfiller, ok := stores.Snapshots.(snapshotCostBackfiller)
		if ok {
			stats, err := backfiller.BackfillSnapshotCosts(backfillCtx)
			if err != nil {
				slog.Warn("usage_pricing.snapshot_cost_backfill_failed", "error", err)
			} else if stats.SnapshotRowsUpdated > 0 {
				slog.Info("usage_pricing.snapshot_cost_backfill_complete", "snapshots", stats.SnapshotRowsUpdated)
			}
		}
	}

	if stores.UsageEvents != nil {
		backfiller, ok := stores.UsageEvents.(usageEventCostBackfiller)
		if ok {
			stats, err := backfiller.BackfillUsageEventCosts(backfillCtx)
			if err != nil {
				slog.Warn("usage_pricing.usage_event_cost_backfill_failed", "error", err)
				return
			}
			if stats.EventRowsUpdated > 0 || len(stats.RollupBuckets) > 0 {
				slog.Info("usage_pricing.usage_event_cost_backfill_complete",
					"events", stats.EventRowsUpdated,
					"rollup_buckets", len(stats.RollupBuckets),
				)
			}
		}
	}
}

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
	logOutput := gatewayLogOutput()
	textHandler := slog.NewTextHandler(logOutput, &slog.HandlerOptions{Level: logLevel})
	logTee := gateway.NewLogTee(textHandler)
	slog.SetDefault(slog.New(logTee))

	// Load config
	cfgPath := resolveConfigPath()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if err := config.ValidateGatewayAuth(cfg.Gateway); err != nil {
		slog.Error("unsafe gateway auth configuration", "error", err)
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

	slog.Debug("creating mcpMgr via setupToolRegistry")
	toolsReg, execApprovalMgr, mcpMgr, sandboxMgr, browserMgr, webFetchTool, ttsTool, audioMgr, permPE, toolPE, dataDir, agentCfg := setupToolRegistry(cfg, workspace, providerRegistry)
	slog.Debug("setupToolRegistry completed", "mcpMgr_nil", mcpMgr == nil)
	if browserMgr != nil {
		defer browserMgr.Close()
	}
	if mcpMgr != nil {
		defer mcpMgr.Stop()
	}

	pgStores, traceCollector, snapshotWorker := setupStoresAndTracing(cfg, dataDir, msgBus)
	if browserMgr != nil && pgStores != nil && pgStores.BrowserCookies != nil && cfg.Tools.Browser.CookieSyncEnabled {
		browserMgr.SetCookieProvider(newStoreBrowserCookieProvider(pgStores.BrowserCookies))
	}

	// Recover from crashes: flip ghost 'summoning' rows to 'summon_failed'.
	// Summon goroutines don't survive process restart; stale DB rows would trap the UI.
	if pgStores.Agents != nil {
		if n, err := pgStores.Agents.ResetStuckSummoning(context.Background()); err != nil {
			slog.Warn("agents.reset_stuck_summoning_failed", "err", err)
		} else if n > 0 {
			slog.Info("agents.reset_stuck_summoning", "count", n)
		}
	}

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

	// Re-apply tool rate limiter using DB-overlaid config. setupToolRegistry
	// initialised the limiter from the JSON5 default before ApplySystemConfigs
	// ran, so DB-driven changes to tools.rate_limit_per_hour were lost. Replace
	// the limiter object now that cfg reflects the DB value. Safe: server has
	// not started, no in-flight tool calls.
	if cfg.Tools.RateLimitPerHour > 0 {
		toolsReg.SetRateLimiter(tools.NewToolRateLimiter(cfg.Tools.RateLimitPerHour))
		slog.Info("tool rate limiting reapplied from system_configs", "per_hour", cfg.Tools.RateLimitPerHour)
	} else {
		toolsReg.SetRateLimiter(nil)
	}

	// Re-apply user-configured allowed paths for the same reason as the rate
	// limiter above: setupToolRegistry wired the filesystem tools' AllowPaths
	// from the JSON5 default before ApplySystemConfigs overlaid
	// system_configs['allowed_paths'], so DB-driven paths never reached the tools.
	// Re-run now that cfg reflects the DB value. Safe: server has not started, no
	// in-flight tool calls.
	if paths := cfg.Agents.Defaults.AllowedPaths; len(paths) > 0 {
		applyUserAllowedPaths(toolsReg, paths)
		slog.Info("filesystem allowed paths reapplied from system_configs", "paths", len(paths))
	}
	// MCP servers: load from database (single source of truth).
	// pgStores.MCP is nil on SQLite/desktop builds that don't support MCP tables.
	// Apply store to MCP manager so ListToolsForAgent can query DB.
	// mcpMgr is created before pgStores is available, so the store must be set here.
	if pgStores.MCP != nil && mcpMgr != nil {
		mcpMgr.SetStore(pgStores.MCP)
		slog.Info("applied store to MCPManager")
	}
	slog.Debug("checking MCP store availability", "pgStores_MCP_nil", pgStores == nil || pgStores.MCP == nil, "mcpMgr_nil", mcpMgr == nil)
	if pgStores.MCP != nil {
		slog.Debug("initializing MCP from database")
		if err := initMCPFromDB(context.Background(), mcpMgr, pgStores.MCP); err != nil {
			slog.Warn("mcp.db_load_errors", "error", err)
		} else {
			slog.Debug("initMCPFromDB completed successfully")
		}
		if mcpMgr != nil {
			slog.Info("MCP manager started", "tools", len(mcpMgr.ToolNames()))
		}
	} else {
		slog.Debug("skipping MCP database init: pgStores.MCP is nil")
	}

	teamWorkEmbedder := setupMemoryEmbeddings(pgStores, providerRegistry)
	usageCapSvc := usagecaps.NewService(pgStores.UsageCaps, pgStores.Providers)

	// Resolve background provider for consolidation + vault enrichment.
	// Fallback: background.provider → agent.default_provider → first registered provider.
	bgProvider, bgModel := resolveBackgroundProvider(cfg, providerRegistry)

	// V3: Wire consolidation pipeline (episodic → semantic → KG → dreaming)
	if pgStores.Episodic != nil {
		if bgProvider != nil {
			var kgExtractor *kg.Extractor
			if pgStores.KnowledgeGraph != nil {
				kgExtractor = kg.NewExtractor(bgProvider, bgModel, 0)
				kgExtractor.SetUsageCapService(usageCapSvc)
			}
			cleanupConsolidation := consolidation.Register(consolidation.ConsolidationDeps{
				EpisodicStore: pgStores.Episodic,
				MemoryStore:   pgStores.Memory,
				KGStore:       pgStores.KnowledgeGraph,
				SessionStore:  pgStores.Sessions,
				EventBus:      domainBus,
				SystemConfigs: pgStores.SystemConfigs,
				Registry:      providerRegistry,
				Extractor:     kgExtractor,
				AlertDeps:     bgalert.AlertDeps{SystemConfigs: pgStores.SystemConfigs, MsgBus: msgBus},
				UsageCaps:     usageCapSvc,
				AgentStore:    pgStores.Agents,
			})
			defer cleanupConsolidation()
			slog.Info("consolidation pipeline registered", "provider", bgProvider.Name(), "model", bgModel)
		} else {
			slog.Warn("consolidation pipeline skipped: no provider available")
		}
	}

	var channelMemorySvc *channelmemory.Service
	if memorySvc := makeChannelMemoryService(pgStores, domainBus, providerRegistry, usageCapSvc); memorySvc != nil {
		channelMemorySvc = memorySvc
		cleanupChannelMemory := (&channelmemory.Worker{Service: channelMemorySvc}).Start(context.Background())
		defer cleanupChannelMemory()
		slog.Info("channel memory extraction worker registered")
	}

	// V3: Wire vault enrichment worker (async summary + embedding + auto-linking).
	// Provider is resolved per-tenant at runtime — no static provider needed.
	var enrichProgress *vault.EnrichProgress
	var enrichWorker *vault.EnrichWorker
	if pgStores.Vault != nil && providerRegistry != nil {
		cleanupVaultEnrich, ep, ew := vault.RegisterEnrichWorker(vault.EnrichWorkerDeps{
			VaultStore:    pgStores.Vault,
			SystemConfigs: pgStores.SystemConfigs,
			Registry:      providerRegistry,
			EventBus:      domainBus,
			MsgBus:        msgBus,
			TeamStore:     pgStores.Teams,
			AlertDeps:     bgalert.AlertDeps{SystemConfigs: pgStores.SystemConfigs, MsgBus: msgBus},
			UsageCaps:     usageCapSvc,
		})
		enrichProgress = ep
		enrichWorker = ew
		defer cleanupVaultEnrich()
		slog.Info("vault enrichment worker registered (per-tenant provider resolution)")
	}

	loadBootstrapFiles(pgStores, workspace, agentCfg)

	// Backfill CAPABILITIES.md for pre-v3 agents that don't have it yet.
	if count, err := bootstrap.BackfillCapabilities(context.Background(), pgStores.DB); err != nil {
		slog.Warn("bootstrap: capabilities backfill failed", "error", err)
	} else if count > 0 {
		slog.Info("bootstrap: capabilities backfill complete", "agents", count)
	}

	if readImage, ok := toolsReg.Get("read_image"); ok {
		if t, ok := readImage.(*tools.ReadImageTool); ok {
			t.SetUsageCapService(usageCapSvc)
		}
	}

	// Subagent system (secureCLI store wired so subagent ExecTools enforce the gate)
	subagentMgr := setupSubagents(providerRegistry, cfg, msgBus, toolsReg, workspace, sandboxMgr, pgStores.SecureCLI, usageCapSvc)
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
	heartbeatTool, hasMemory := wireExtraTools(pgStores, toolsReg, msgBus, workspace, dataDir, agentCfg, globalSkillsDir, builtinSkillsDir, cfg.Cron.CommandEnabled)

	// Register workstation_exec + claude_remote tools (Standard edition only; deny-all until Phase 6).
	// cleanupWorkstation stops the activity sink retention goroutine and drains the write buffer.
	cleanupWorkstation := wireWorkstationTools(pgStores, toolsReg, domainBus)
	defer cleanupWorkstation()

	// Create all agents — resolved lazily from database by the managed resolver.
	agentRouter := agent.NewRouter()
	if traceCollector != nil {
		agentRouter.SetTraceCollector(traceCollector)
	}
	slog.Info("agents will be resolved lazily from database")

	// Create gateway server and wire enforcement
	server := gateway.NewServer(cfg, msgBus, agentRouter, pgStores.Sessions, toolsReg)
	server.SetVersion(Version)
	server.SetDB(pgStores.DB)
	server.SetPolicyEngine(permPE)
	server.SetToolPolicy(toolPE)
	server.SetPairingService(pgStores.Pairing)
	server.SetMessageBus(msgBus)
	server.SetExecApprovalManager(execApprovalMgr)
	server.SetOAuthHandler(httpapi.NewOAuthHandler(pgStores.Providers, pgStores.ConfigSecrets, providerRegistry, msgBus))

	// contextFileInterceptor is created inside wireExtras.
	// Declared here so it can be passed to registerAllMethods → AgentsMethods
	// for immediate cache invalidation on agents.files.set.
	var contextFileInterceptor *tools.ContextFileInterceptor

	// Set agent store for tools_invoke context injection + wire extras
	if pgStores.Agents != nil {
		server.SetAgentStore(pgStores.Agents)
	}
	// Wire the skill/cron stores used by the CRUD MCP server (see
	// internal/mcp/crud_server.go, mounted at /api/mcp/ in BuildMux()).
	if pgStores.Skills != nil {
		server.SetSkillStore(pgStores.Skills)
	}
	if pgStores.Cron != nil {
		server.SetCronStore(pgStores.Cron)
	}
	if pgStores.AgentLinks != nil {
		server.SetAgentLinkStore(pgStores.AgentLinks)
	}
	if pgStores.ConfigPermissions != nil {
		server.SetConfigPermissionStore(pgStores.ConfigPermissions)
	}
	if pgStores.BitrixPortals != nil {
		server.SetBitrixPortalStore(pgStores.BitrixPortals)
	}
	if pgStores.RunTimeline != nil {
		server.SetRunTimelineStore(pgStores.RunTimeline)
	}
	if pgStores.Teams != nil {
		server.SetTeamStore(pgStores.Teams)
	}
	if pgStores.ChannelInstances != nil {
		server.SetChannelInstanceStore(pgStores.ChannelInstances)
	}
	if pgStores.Heartbeats != nil {
		server.SetHeartbeatStore(pgStores.Heartbeats)
	}
	if pgStores.Providers != nil {
		server.SetProviderStore(pgStores.Providers)
	}
	if pgStores.Tenants != nil {
		server.SetTenantStore(pgStores.Tenants)
	}
	if pgStores.Memory != nil {
		server.SetMemoryStore(pgStores.Memory)
	}
	if pgStores.KnowledgeGraph != nil {
		server.SetKnowledgeGraphStore(pgStores.KnowledgeGraph)
	}
	if pgStores.Tracing != nil {
		server.SetTracingStore(pgStores.Tracing)
	}
	if pgStores.Contacts != nil {
		server.SetContactStore(pgStores.Contacts)
	}
	if pgStores.PendingMessages != nil {
		server.SetPendingMessageStore(pgStores.PendingMessages)
	}
	if pgStores.Activity != nil {
		server.SetActivityStore(pgStores.Activity)
	}
	if pgStores.SystemConfigs != nil {
		server.SetSystemConfigStore(pgStores.SystemConfigs)
	}
	if pgStores.SecureCLI != nil {
		server.SetSecureCLIStore(pgStores.SecureCLI)
	}
	server.SetSQLDB(pgStores.DB)

	// Build OAuth token refresher before wireExtras so the resolver can inject tokens.
	var mcpOAuthRefresher mcpbridge.OAuthTokenProvider
	if pgStores != nil && pgStores.MCPOAuthTokens != nil {
		mcpOAuthRefresher = mcpoauth.NewRefresher(pgStores.MCPOAuthTokens, security.NewSafeClient(15*time.Second))
	}

	var mcpPool *mcpbridge.Pool
	var mediaStore *media.Store
	var postTurn tools.PostTurnProcessor
	contextFileInterceptor, mcpPool, mediaStore, postTurn = wireExtras(pgStores, agentRouter, providerRegistry, modelReg, msgBus, pgStores.Sessions, toolsReg, toolPE, skillsLoader, hasMemory, traceCollector, workspace, cfg.Gateway.InjectionAction, cfg, sandboxMgr, redisClient, domainBus, usageCapSvc, mcpOAuthRefresher)
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
		enrichProgress:   enrichProgress,
		enrichWorker:     enrichWorker,
		channelMemorySvc: channelMemorySvc,
		workspace:        workspace,
		dataDir:          dataDir,
		domainBus:        domainBus,
		usageCapSvc:      usageCapSvc,
		audioMgr:         audioMgr,
		teamWorkEmbedder: teamWorkEmbedder,
	}

	gatewayAddr := loopbackAddr(cfg.Gateway.Host, cfg.Gateway.Port)
	var mcpToolLister httpapi.MCPToolLister
	if mcpMgr != nil {
		mcpToolLister = mcpMgr
	}
	httpapi.InitGatewayToken(cfg.Gateway.Token)
	mcpbridge.SetAllowedHosts(cfg.Gateway.MCPAllowedHosts) // operator allowlist: trusted MCP hosts exempt from private-IP SSRF block
	httpapi.InitGatewayNoAuthFallbackAllowed(config.GatewayNoAuthFallbackAllowed(cfg.Gateway))
	exportTokenStore := httpapi.InitExportTokenStore()
	defer exportTokenStore.Stop()
	agentsH, skillsH, tracesH, mcpH, channelInstancesH, providersH, builtinToolsH, pendingMessagesH, teamEventsH, secureCLIH, secureCLIGrantH, mcpUserCredsH := wireHTTP(pgStores, cfg.Agents.Defaults.Workspace, dataDir, bundledSkillsDir, msgBus, domainBus, toolsReg, providerRegistry, modelReg, permPE.IsOwner, gatewayAddr, mcpToolLister, usageCapSvc, cfg, cfg.Skills)

	// Wire dependencies for system prompt preview parity.
	if agentsH != nil {
		agentsH.SetPreviewDeps(toolsReg, skillsLoader)
		agentsH.SetPreviewToolPolicy(toolPE)
		var skillAccess store.SkillAccessStore
		if pgStores.Skills != nil {
			skillAccess, _ = pgStores.Skills.(store.SkillAccessStore)
		}
		agentsH.SetPreviewStores(pgStores.Teams, pgStores.AgentLinks, skillAccess)
		slog.Debug("wiring MCP preview manager", "mcpMgr_nil", mcpMgr == nil)
		if mcpMgr != nil {
			agentsH.SetPreviewMCPManager(httpapi.NewMCPPreviewAdapter(mcpMgr))
			slog.Debug("set MCP preview manager on agentsH")
		}
	}

	// External wake/trigger API
	wakeH := httpapi.NewWakeHandler(agentRouter)
	if postTurn != nil {
		wakeH.SetPostTurnProcessor(postTurn)
	}

	// MCP OAuth handler — per-server OAuth 2.1 client flows.
	var mcpOAuthH *httpapi.MCPOAuthHandler
	if pgStores != nil && pgStores.MCP != nil && pgStores.MCPOAuthTokens != nil {
		safeHTTPClient := security.NewSafeClient(15 * time.Second)
		var oauthRefresher *mcpoauth.Refresher
		if r, ok := mcpOAuthRefresher.(*mcpoauth.Refresher); ok {
			oauthRefresher = r
		}
		mcpOAuthH = httpapi.NewMCPOAuthHandler(httpapi.MCPOAuthHandlerDeps{
			MCPStore:    pgStores.MCP,
			OAuthStore:  pgStores.MCPOAuthTokens,
			Discoverer:  mcpoauth.NewDiscoverer(safeHTTPClient),
			FlowMgr:     mcpoauth.NewFlowManager(safeHTTPClient),
			Refresher:   oauthRefresher,
			EventBus:    msgBus,
			PublicURL:   cfg.Gateway.PublicURL,
			Port:        cfg.Gateway.Port,
			TenantStore: pgStores.Tenants,
		})
		// Inject OAuth token provider into MCP tools handler so on-demand tool
		// discovery can authenticate against OAuth-protected MCP servers.
		if mcpH != nil && mcpOAuthRefresher != nil {
			mcpH.SetOAuthProvider(mcpOAuthRefresher)
		}
		// Inject the OAuth token store so the update handler can purge stale tokens
		// when a server's URL or OAuth config changes.
		if mcpH != nil {
			mcpH.SetOAuthStore(pgStores.MCPOAuthTokens)
		}
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
			mcpOAuth:         mcpOAuthH,
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
	server.SetRuntimeLogsHandler(httpapi.NewRuntimeLogsHandler(logTee))
	pairingMethods, heartbeatMethods, chatMethods, cfgPermsMethods := registerAllMethods(server, agentRouter, pgStores.Sessions, pgStores.Tracing, pgStores.RunTimeline, pgStores.Cron, pgStores.Pairing, cfg, cfgPath, workspace, dataDir, msgBus, execApprovalMgr, pgStores.Agents, pgStores.Skills, pgStores.ConfigSecrets, pgStores.Teams, pgStores.AgentLinks, contextFileInterceptor, logTee, pgStores.Heartbeats, pgStores.ConfigPermissions, pgStores.SystemConfigs, pgStores.Tenants, pgStores.SkillTenantCfgs, audioMgr, usageCapSvc, providerRegistry, teamWorkEmbedder)

	// Phase 3: Agent hooks RPC methods (hooks.list/create/update/delete/toggle/test/history).
	if hs, ok := pgStores.Hooks.(hooks.HookStore); ok && hs != nil {
		hm := methods.NewHookMethods(hs, edition.Current())
		// Reuse dispatcher handlers for dry-run test runner so UI test panel
		// exercises the exact code that will run in production.
		if sharedHookHandlers != nil {
			hm.SetTestRunner(methods.NewDispatcherTestRunner(sharedHookHandlers))
		}
		hm.Register(server.Router())
		server.SetHookStore(hs)
		slog.Info("registered hooks RPC methods")
	}

	// Workstations WS methods — Standard edition only.
	// Lite (desktop/SQLite) must NOT expose workstation RPC methods.
	if edition.Current().Name != "lite" && pgStores.Workstations != nil && pgStores.WorkstationLinks != nil {
		wsMethods := methods.NewWorkstationsMethods(pgStores.Workstations, pgStores.WorkstationLinks)
		if pgStores.WorkstationPermissions != nil {
			wsMethods.SetPermStore(pgStores.WorkstationPermissions)
		}
		if pgStores.WorkstationActivity != nil {
			wsMethods.SetActivityStore(pgStores.WorkstationActivity)
		}
		wsMethods.Register(server.Router())
		slog.Info("registered workstations RPC methods")
	}

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
	channelMgr.SetSystemMessages(systemmessages.NewResolver(cfg))
	deps.channelMgr = channelMgr
	server.SetChannelManager(channelMgr)

	// Wire channel member resolver into permission grant paths (WS + HTTP) so
	// file_writer grants coming from the Web UI auto-enrich their metadata.
	cfgPermsMethods.SetMemberResolver(channelMgr)
	if channelInstancesH != nil {
		channelInstancesH.SetMemberResolver(channelMgr)
		// Setter (not constructor) because wireHTTP runs before channelMgr is
		// created — required for handleDelete to invoke ChannelDestroyer on
		// Bitrix24 channels (imbot.unregister bot cleanup).
		channelInstancesH.SetChannelManager(channelMgr)
	}
	if deps.channelMemorySvc != nil {
		deps.channelMemorySvc.ContextResolver = channelmemory.ContextResolverFunc(func(ctx context.Context, inst *store.ChannelInstanceData, group store.PendingMessageGroup) (channelmemory.ExtractionContext, error) {
			return resolveChannelMemoryExtractionContext(ctx, channelMgr, inst, group)
		})
	}

	// Wire channel sender + tenant checker on message tool (now that channelMgr exists)
	if t, ok := toolsReg.Get("message"); ok {
		if cs, ok := t.(tools.ChannelSenderAware); ok {
			cs.SetChannelSender(channelMgr.SendToChannel)
		}
		if ce, ok := t.(tools.ChannelEditorAware); ok {
			ce.SetChannelEditor(channelMgr.EditChannelMessage)
		}
		if rs, ok := t.(tools.ReactionSetterAware); ok {
			rs.SetReactionSetter(channelMgr.ReactToMessage)
		}
		if tr, ok := t.(tools.TopicResolverAware); ok && pgStores != nil && pgStores.Contacts != nil {
			contacts := pgStores.Contacts
			tr.SetTopicResolver(func(ctx context.Context, channel, chatID, topicName string) (string, bool) {
				list, err := contacts.ListContacts(ctx, store.ContactListOpts{
					ChannelInstance: channel,
					ContactType:     "topic",
					Limit:           500,
				})
				if err != nil {
					return "", false
				}
				want := strings.ToLower(strings.TrimSpace(topicName))
				for _, c := range list {
					if c.SenderID != chatID || c.ThreadID == nil || c.DisplayName == nil {
						continue
					}
					if strings.ToLower(strings.TrimSpace(*c.DisplayName)) == want {
						return *c.ThreadID, true
					}
				}
				return "", false
			})
		}
		if tp, ok := t.(tools.TopicPosterAware); ok {
			tp.SetTopicPoster(channelMgr.PostToTopic)
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
	// Wire group lister on zalo_list_groups tool
	if t, ok := toolsReg.Get("zalo_list_groups"); ok {
		if gl, ok := t.(tools.GroupListerAware); ok {
			gl.SetGroupLister(channelMgr.ListGroups)
		}
	}
	// Wire Telegram manager on telegram_manager tool.
	for _, toolName := range []string{"telegram_manager", "create_forum_topic"} {
		if t, ok := toolsReg.Get(toolName); ok {
			if tm, ok := t.(tools.TelegramManagerAware); ok {
				tm.SetTelegramManager(channelMgr.ManageTelegram)
			}
		}
	}

	// Load channel instances from DB.
	var instanceLoader *channels.InstanceLoader
	if pgStores.ChannelInstances != nil {
		instanceLoader = channels.NewInstanceLoader(pgStores.ChannelInstances, pgStores.Agents, channelMgr, msgBus, pgStores.Pairing)
		instanceLoader.SetProviderRegistry(providerRegistry)
		instanceLoader.SetPendingCompactionConfig(cfg.Channels.PendingCompaction)
		instanceLoader.SetUsageCapService(usageCapSvc)
		instanceLoader.RegisterFactory(channels.TypeTelegram, telegram.FactoryWithStoresAndAudio(pgStores.Agents, pgStores.ConfigPermissions, pgStores.Teams, pgStores.SubagentTasks, pgStores.PendingMessages, audioMgr))
		instanceLoader.RegisterFactory(channels.TypeDiscord, discord.FactoryWithStoresAndAudio(pgStores.Agents, pgStores.ConfigPermissions, pgStores.PendingMessages, audioMgr))
		instanceLoader.RegisterFactory(channels.TypeFeishu, feishu.FactoryWithPendingStoreAndAudio(pgStores.PendingMessages, audioMgr))
		instanceLoader.RegisterFactory(channels.TypeZaloOA, zalo.Factory)
		instanceLoader.RegisterFactory(channels.TypeZaloPersonal, zalopersonal.FactoryWithPendingStore(pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeWhatsApp, whatsapp.FactoryWithDBAudio(pgStores.DB, pgStores.PendingMessages, "pgx", audioMgr, pgStores.BuiltinTools))
		instanceLoader.RegisterFactory(channels.TypeSlack, slackchannel.FactoryWithPendingStore(pgStores.PendingMessages))
		instanceLoader.RegisterFactory(channels.TypeFacebook, facebook.Factory)
		instanceLoader.RegisterFactory(channels.TypePancake, pancake.Factory)
		// Bitrix24: factory needs the portal store + encKey injected so each
		// Channel can resolve its portal on Start(). The encKey here mirrors
		// the one used by pg.NewPGStores → NewPGBitrixPortalStore.
		bitrixEncKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
		// Use the MCP-aware factory variant so channels that opt into
		// lazy per-user credential provisioning (via mcp_server_id — or
		// the legacy mcp_server_name + mcp_base_url pair — in their
		// instance config) can reach the partner's
		// MCPServerStore. The MCP server authenticates each onboard call
		// via the caller-supplied Bitrix access_token (the "Bitrix24
		// OAuth → existing mcp_user_credentials bridge" — Bitrix-specific
		// glue, not a generic MCP architecture pattern) — no shared admin
		// secret is required. Channels with none of those set operate
		// identically to before — the MCPStore arg is nil-safe inside the
		// factory.
		instanceLoader.RegisterFactory(channels.TypeBitrix24, bitrix24.FactoryWithPortalStoreAndMCP(pgStores.BitrixPortals, pgStores.MCP, bitrixEncKey))
		if err := instanceLoader.LoadAll(context.Background()); err != nil {
			slog.Error("failed to load channel instances from DB", "error", err)
		}

		// Bitrix24 portal management RPC (self-service onboarding).
		// Registers bitrix.portals.list/create/get_install_url/delete methods
		// on the WS router; install URL is built from the gateway's observed
		// public URL via Server.PublicURLSnapshot().
		if pgStores.BitrixPortals != nil {
			methods.NewBitrixPortalsMethods(
				pgStores.BitrixPortals,
				pgStores.ChannelInstances,
				server.PublicURLSnapshot().Get,
				bitrixEncKey,
			).Register(server.Router())
		}

		// Warm the shared Bitrix24 router with every portal row so inbound
		// webhooks land on the right *Portal even before a channel instance
		// is loaded for that portal. Idempotent; no-op on sqlite-lite.
		if pgStores.BitrixPortals != nil {
			if err := bitrix24.BootstrapPortals(context.Background(), pgStores.BitrixPortals, bitrixEncKey); err != nil {
				// Surface the missing-table case loudly so an operator notices
				// without having to grep logs — bitrix24 channels silently
				// no-op until `goclaw migrate up` runs migration 000058.
				if strings.Contains(err.Error(), "bitrix_portals") &&
					(strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "no such table")) {
					slog.Warn("bitrix24 bootstrap skipped — bitrix_portals table missing; run `goclaw migrate up` (migration 000068) to enable Bitrix24 channels",
						"err", err)
				} else {
					slog.Warn("bitrix24 bootstrap failed", "err", err)
				}
			}
		}
	}

	// Register config-based channels as fallback when no DB instances loaded.
	registerConfigChannels(cfg, channelMgr, msgBus, pgStores, instanceLoader, audioMgr)

	// Register channels/instances/links/teams RPC methods
	chInstancesM := wireChannelRPCMethods(server, pgStores, channelMgr, instanceLoader, agentRouter, msgBus, cfg, workspace)

	// Bitrix24 orphan-bot cleaner. Fires from channel_instances delete handler
	// when the channel is no longer loaded in the Manager (typical scenario:
	// admin disabled the channel earlier so InstanceLoader.Reload removed it).
	// Without this, deleting a disabled Bitrix24 channel would orphan the bot
	// on the portal.
	if pgStores.BitrixPortals != nil {
		bitrixEncKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
		orphanCleaner := func(ctx context.Context, tenantID uuid.UUID, cfg []byte) error {
			return bitrix24.DestroyOrphanBot(ctx, pgStores.BitrixPortals, bitrixEncKey, tenantID, cfg)
		}
		if channelInstancesH != nil {
			channelInstancesH.RegisterOrphanCleaner(channels.TypeBitrix24, orphanCleaner)
		}
		if chInstancesM != nil {
			chInstancesM.RegisterOrphanCleaner(channels.TypeBitrix24, orphanCleaner)
		}
	}

	// Wire channel event subscribers (cache invalidation, pairing, cascade disable)
	wireChannelEventSubscribers(msgBus, server, pgStores, channelMgr, instanceLoader, pairingMethods, cfg)

	// Audit log subscriber + team task event subscribers.
	auditCh := deps.wireAuditSubscriber()
	deps.wireEventSubscribers()

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go backfillTraceCostsAfterPricingSync(ctx, pgStores, snapshotWorker)
	usagepricing.StartOpenRouterCatalogAutoSync(ctx, pgStores.UsageCaps, usagepricing.DefaultOpenRouterCatalogSyncInterval, func(syncCtx context.Context, _ int) {
		backfillTraceCostsAfterPricingSync(syncCtx, pgStores, snapshotWorker)
	})
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
	server.SetQuotaChecker(quotaChecker)

	// API key management RPC
	if pgStores.APIKeys != nil {
		methods.NewAPIKeysMethods(pgStores.APIKeys).Register(server.Router())
	}

	// Tenant management RPC + HTTP
	if pgStores.Tenants != nil {
		methods.NewTenantsMethods(pgStores.Tenants, msgBus, workspace).Register(server.Router())
		server.SetTenantsHandler(httpapi.NewTenantsHandler(pgStores.Tenants, msgBus, workspace))
		server.Router().SetTenantStore(pgStores.Tenants)
		// Permission cache for tenant membership checks. Store on deps so
		// lifecycle shutdown can call Close() to stop the sweep goroutines.
		permCache := cache.NewPermissionCache()
		deps.permCache = permCache
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

// resolveBackgroundProvider picks the LLM provider+model for background workers
// (vault enrichment, consolidation). Fallback chain:
//
//	background.provider/model → agent.default_provider/model → first registered provider.
func resolveBackgroundProvider(cfg *config.Config, reg *providers.Registry) (providers.Provider, string) {
	try := func(name, model string) (providers.Provider, string, bool) {
		if name == "" {
			return nil, "", false
		}
		p, err := reg.GetForTenant(providers.MasterTenantID, name)
		if err != nil || p == nil {
			return nil, "", false
		}
		if model == "" {
			model = p.DefaultModel()
		}
		return p, model, true
	}

	// 1. Explicit background config
	if p, m, ok := try(cfg.Gateway.BackgroundProvider, cfg.Gateway.BackgroundModel); ok {
		return p, m
	}
	// 2. Agent default provider
	if p, m, ok := try(cfg.Agents.Defaults.Provider, cfg.Agents.Defaults.Model); ok {
		return p, m
	}
	// 3. First registered provider (legacy fallback)
	if names := reg.ListForTenant(providers.MasterTenantID); len(names) > 0 {
		if p, m, ok := try(names[0], ""); ok {
			return p, m
		}
	}
	return nil, ""
}
