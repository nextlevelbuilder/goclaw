package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/google/uuid"

	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/orchestration"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	kg "github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	memorypkg "github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// wireExtras wires components that require PG stores:
// agent resolver (lazy-creates Loops from DB), virtual FS interceptors, memory tools,
// and cache invalidation event subscribers.
// PG store creation and tracing are handled in gateway.go before this is called.
// Returns the ContextFileInterceptor so callers can pass it to AgentsMethods
// for immediate cache invalidation on agents.files.set.
func wireExtras(
	stores *store.Stores,
	agentRouter *agent.Router,
	providerReg *providers.Registry,
	msgBus *bus.MessageBus,
	sessStore store.SessionStore,
	toolsReg *tools.Registry,
	toolPE *tools.PolicyEngine,
	skillsLoader *skills.Loader,
	hasMemory bool,
	traceCollector *tracing.Collector,
	workspace string,
	injectionAction string,
	appCfg *config.Config,
	sandboxMgr sandbox.Manager,
	redisClient any, // nil when built without -tags redis or when Redis is unconfigured
	domainBus eventbus.DomainEventBus,
) (*tools.ContextFileInterceptor, *mcpbridge.Pool, *media.Store, tools.PostTurnProcessor) {
	// 1. Build cache instances (in-memory or Redis depending on build tags)
	agentCtxCache, userCtxCache := makeCaches(redisClient)

	// 1a. Context file interceptor (created before resolver so callbacks can reference it)
	var contextFileInterceptor *tools.ContextFileInterceptor
	if stores.Agents != nil {
		contextFileInterceptor = tools.NewContextFileInterceptor(stores.Agents, workspace, agentCtxCache, userCtxCache)
	}

	// 1c. Persistent media storage for cross-turn image/document access
	mediaStore, err := media.NewStore(filepath.Join(workspace, ".media"))
	if err != nil {
		slog.Warn("media store creation failed, images will not persist across turns", "error", err)
	}

	// Wire media cleanup on session delete.
	if mediaStore != nil {
		if pgSess, ok := sessStore.(*pg.PGSessionStore); ok {
			pgSess.OnDelete = func(sessionKey string) {
				_ = mediaStore.DeleteSession(sessionKey)
			}
		}
		// Register media analysis tools (need mediaStore for file access).
		toolsReg.Register(tools.NewReadDocumentTool(providerReg, mediaStore))
		toolsReg.Register(tools.NewReadAudioTool(providerReg, mediaStore))
		toolsReg.Register(tools.NewReadVideoTool(providerReg, mediaStore))
		toolsReg.Register(tools.NewCreateVideoTool(providerReg))
		slog.Info("media tools registered", "tools", "read_document,read_audio,read_video,create_video")
	}

	// 1e. Wire secure CLI store into exec tool for credentialed exec
	if stores.SecureCLI != nil {
		if execTool, ok := toolsReg.Get("exec"); ok {
			if et, ok := execTool.(*tools.ExecTool); ok {
				et.SetSecureCLIStore(stores.SecureCLI)
			}
		}
	}

	// 2. Per-user profile + context file seeding callbacks
	var ensureUserProfile agent.EnsureUserProfileFunc
	var seedUserFiles agent.SeedUserFilesFunc
	if stores.Agents != nil {
		ensureUserProfile = buildEnsureUserProfile(stores.Agents)
		seedUserFiles = buildSeedUserFiles(stores.Agents)
	}

	// 3. Context file loader callback: loads per-user context files dynamically
	var contextFileLoader agent.ContextFileLoaderFunc
	if contextFileInterceptor != nil {
		contextFileLoader = buildContextFileLoader(contextFileInterceptor)
	}

	// 4. Compute global sandbox defaults for resolver
	sandboxEnabled := sandboxMgr != nil
	sandboxContainerDir := ""
	sandboxWorkspaceAccess := ""
	if sandboxEnabled {
		sbCfg := appCfg.Agents.Defaults.Sandbox
		if sbCfg != nil {
			resolved := sbCfg.ToSandboxConfig()
			sandboxContainerDir = resolved.ContainerWorkdir()
			sandboxWorkspaceAccess = string(resolved.WorkspaceAccess)
		}
	}

	// 5. Shared MCP connection pool (eliminates duplicate connections across agents)
	var mcpPool *mcpbridge.Pool
	if stores.MCP != nil {
		mcpPool = mcpbridge.NewPool(mcpbridge.DefaultPoolConfig())
	}

	// 6. Set up agent resolver: lazy-creates Loops from DB
	var skillAccessStore store.SkillAccessStore
	if sas, ok := stores.Skills.(store.SkillAccessStore); ok {
		skillAccessStore = sas
	}

	// V3 auto-inject: create AutoInjector if episodic store is available.
	var autoInjector memorypkg.AutoInjector
	if stores.Episodic != nil {
		autoInjector = memorypkg.NewAutoInjector(stores.Episodic, stores.EvolutionMetrics)
	}

	resolver := agent.NewManagedResolver(agent.ResolverDeps{
		AgentStore:             stores.Agents,
		ProviderStore:          stores.Providers,
		ProviderReg:            providerReg,
		Bus:                    msgBus,
		Sessions:               sessStore,
		Tools:                  toolsReg,
		ToolPolicy:             toolPE,
		Skills:                 skillsLoader,
		SkillAccessStore:       skillAccessStore,
		HasMemory:              hasMemory,
		TraceCollector:         traceCollector,
		EnsureUserProfile:      ensureUserProfile,
		SeedUserFiles:          seedUserFiles,
		ContextFileLoader:      contextFileLoader,
		BootstrapCleanup:       buildBootstrapCleanup(stores.Agents),
		CacheInvalidate:        buildCacheInvalidate(contextFileInterceptor),
		InjectionAction:        injectionAction,
		MaxMessageChars:        appCfg.Gateway.MaxMessageChars,
		CompactionCfg:          appCfg.Agents.Defaults.Compaction,
		ContextPruningCfg:      appCfg.Agents.Defaults.ContextPruning,
		SandboxEnabled:         sandboxEnabled,
		SandboxContainerDir:    sandboxContainerDir,
		SandboxWorkspaceAccess: sandboxWorkspaceAccess,
		AgentLinkStore:         stores.AgentLinks,
		TeamStore:              stores.Teams,
		DataDir:                workspace,
		SecureCLIStore:         stores.SecureCLI,
		BuiltinToolStore:       stores.BuiltinTools,
		MCPStore:               stores.MCP,
		MCPPool:                mcpPool,
		ConfigPermStore:        stores.ConfigPermissions,
		MediaStore:             mediaStore,
		ModelPricing:           appCfg.Telemetry.ModelPricing,
		TracingStore:           stores.Tracing,
		MemoryStore:            stores.Memory,
		ContactStore:           stores.Contacts,
		TenantStore:            stores.Tenants,
		BuiltinToolTenantCfgs:  stores.BuiltinToolTenantCfgs,
		SkillTenantCfgs:        stores.SkillTenantCfgs,
		Workspace:              workspace,
		AutoInjector:           autoInjector,
		EvolutionMetricsStore:  stores.EvolutionMetrics,
		DomainBus:              domainBus,
		OnEvent: func(event agent.AgentEvent) {
			// Sign /v1/files/ and /v1/media/ URLs in content before delivery.
			// Sessions store clean paths; signing happens only at delivery time.
			secret := httpapi.FileSigningKey()
			switch m := event.Payload.(type) {
			case map[string]string:
				if c, has := m["content"]; has && strings.Contains(c, "/v1/") {
					m["content"] = httpapi.SignFileURLs(c, secret)
				}
			case map[string]any:
				// Sign /v1/ URLs in content text (run.completed payload is map[string]any).
				if c, ok := m["content"].(string); ok && strings.Contains(c, "/v1/") {
					m["content"] = httpapi.SignFileURLs(c, secret)
				}
				// Convert media local paths → signed /v1/files/{full_path}?ft=hash
				if rawMedia, ok := m["media"].([]agent.MediaResult); ok {
					// Clone slice — the original is shared with RunResult.Media;
					// mutating in-place corrupts paths for downstream consumers
					// (announce queue, outbound channels) that expect local paths.
					signed := make([]agent.MediaResult, len(rawMedia))
					for i, mr := range rawMedia {
						signed[i] = mr
						// Use full path so backend resolves directly via os.Stat,
						// no findInWorkspace fallback needed.
						urlPath := strings.TrimPrefix(filepath.Clean(mr.Path), "/")
						url := "/v1/files/" + urlPath
						ft := httpapi.SignFileToken(url, secret, httpapi.FileTokenTTL)
						signed[i].Path = url + "?ft=" + ft
					}
					m["media"] = signed
				}
			}
			msgBus.Broadcast(bus.Event{
				Name:     protocol.EventAgent,
				Payload:  event,
				TenantID: event.TenantID,
			})
		},
	})
	agentRouter.SetResolver(resolver)

	// Wire virtual FS interceptors: route context + memory file reads/writes to DB.
	// Share ONE ContextFileInterceptor instance between read_file and write_file
	// so they share the same cache.
	// Write-capable tools share a memory interceptor with optional KG extraction hook.
	var writeMemIntc *tools.MemoryInterceptor
	if stores.Memory != nil {
		writeMemIntc = tools.NewMemoryInterceptor(stores.Memory, workspace)
		// Hook KG extraction on memory writes if KG store is available
		if stores.KnowledgeGraph != nil && stores.BuiltinTools != nil {
			writeMemIntc.SetKGExtractFunc(buildKGExtractFunc(stores.KnowledgeGraph, stores.BuiltinTools, providerReg))
		}
	}
	if readTool, ok := toolsReg.Get("read_file"); ok {
		if ia, ok := readTool.(tools.InterceptorAware); ok {
			if contextFileInterceptor != nil {
				ia.SetContextFileInterceptor(contextFileInterceptor)
			}
			if stores.Memory != nil {
				ia.SetMemoryInterceptor(tools.NewMemoryInterceptor(stores.Memory, workspace))
			}
		}
	}
	if writeTool, ok := toolsReg.Get("write_file"); ok {
		if ia, ok := writeTool.(tools.InterceptorAware); ok {
			if contextFileInterceptor != nil {
				ia.SetContextFileInterceptor(contextFileInterceptor)
			}
			if writeMemIntc != nil {
				ia.SetMemoryInterceptor(writeMemIntc)
			}
		}
	}
	if editTool, ok := toolsReg.Get("edit"); ok {
		if ia, ok := editTool.(tools.InterceptorAware); ok {
			if contextFileInterceptor != nil {
				ia.SetContextFileInterceptor(contextFileInterceptor)
			}
			if writeMemIntc != nil {
				ia.SetMemoryInterceptor(writeMemIntc)
			}
		}
	}
	if listTool, ok := toolsReg.Get("list_files"); ok {
		if ia, ok := listTool.(tools.InterceptorAware); ok {
			if stores.Memory != nil {
				ia.SetMemoryInterceptor(tools.NewMemoryInterceptor(stores.Memory, workspace))
			}
		}
	}

	// Wire config perm store for file writer permission checks
	if stores.ConfigPermissions != nil {
		for _, toolName := range []string{"read_file", "write_file", "edit", "cron"} {
			if t, ok := toolsReg.Get(toolName); ok {
				if cpa, ok := t.(tools.ConfigPermAware); ok {
					cpa.SetConfigPermStore(stores.ConfigPermissions)
				}
			}
		}
		if contextFileInterceptor != nil {
			contextFileInterceptor.SetConfigPermStore(stores.ConfigPermissions)
		}
	}

	// Wire memory store on memory tools (search + get)
	if stores.Memory != nil {
		if searchTool, ok := toolsReg.Get("memory_search"); ok {
			if ms, ok := searchTool.(tools.MemoryStoreAware); ok {
				ms.SetMemoryStore(stores.Memory)
			}
		}
		if getTool, ok := toolsReg.Get("memory_get"); ok {
			if ms, ok := getTool.(tools.MemoryStoreAware); ok {
				ms.SetMemoryStore(stores.Memory)
			}
		}
		slog.Info("memory layering enabled")
	}

	// V3: Wire episodic store + evolution metrics on memory tools (search + expand)
	if stores.Episodic != nil {
		if searchTool, ok := toolsReg.Get("memory_search"); ok {
			if mst, ok := searchTool.(*tools.MemorySearchTool); ok {
				mst.SetEpisodicStore(stores.Episodic)
				if stores.EvolutionMetrics != nil {
					mst.SetEvolutionMetricsStore(stores.EvolutionMetrics)
				}
			}
		}
		if expandTool, ok := toolsReg.Get("memory_expand"); ok {
			if met, ok := expandTool.(*tools.MemoryExpandTool); ok {
				met.SetEpisodicStore(stores.Episodic)
			}
		}
		slog.Info("v3 episodic memory wired to tools")
	}

	// Wire knowledge graph store on KG tool + hint in memory_search results
	if stores.KnowledgeGraph != nil {
		if kgTool, ok := toolsReg.Get("knowledge_graph_search"); ok {
			if kgt, ok := kgTool.(*tools.KnowledgeGraphSearchTool); ok {
				kgt.SetKGStore(stores.KnowledgeGraph)
			}
		}
		// Enable KG hint in memory_search results
		if searchTool, ok := toolsReg.Get("memory_search"); ok {
			if mst, ok := searchTool.(*tools.MemorySearchTool); ok {
				mst.SetHasKG(true)
			}
		}
		slog.Info("knowledge graph tool wired (Postgres)")
	}

	// Wire vault tools and interceptors (conditional on vault store availability)
	wireVault(stores, toolsReg, workspace, domainBus)

	// Wire delegate tool for inter-agent delegation via agent_links.
	if stores.AgentLinks != nil && stores.Agents != nil {
		delegateRunFn := func(ctx context.Context, req tools.DelegateRequest) (tools.DelegateResult, error) {
			loop, err := agentRouter.Get(ctx, req.ToAgentKey)
			if err != nil {
				return tools.DelegateResult{}, fmt.Errorf("target agent %q not found: %w", req.ToAgentKey, err)
			}
			sessionKey := fmt.Sprintf("delegate:%s:%s:%s",
				req.FromAgentID.String()[:8], req.ToAgentKey, req.DelegationID)

			// Link delegate trace to parent trace
			delegateCtx := tracing.WithDelegateParentTraceID(ctx, tracing.TraceIDFromContext(ctx))

			runReq := agent.RunRequest{
				RunID:         uuid.New().String(),
				SessionKey:    sessionKey,
				Message:       req.Task,
				UserID:        req.UserID,
				Channel:       "delegate",
				RunKind:       "delegate",
				DelegationID:  req.DelegationID,
				ParentAgentID: req.FromAgentKey,
			}
			result, err := loop.Run(delegateCtx, runReq)
			if err != nil {
				return tools.DelegateResult{}, err
			}
			cr := orchestration.CaptureFromRunResult(result, 0)
			return tools.DelegateResult{Content: cr.Content, Media: cr.Media}, nil
		}
		delegateTool := tools.NewDelegateTool(stores.AgentLinks, stores.Agents, domainBus, delegateRunFn)
		delegateTool.SetMsgBus(msgBus)
		toolsReg.Register(delegateTool)
		slog.Info("delegate tool wired")
	}

	// --- Cache invalidation event subscribers ---

	// Context file cache: invalidate on agent/context data changes
	if contextFileInterceptor != nil {
		msgBus.Subscribe(bus.TopicCacheBootstrap, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok {
				return
			}
			if payload.Kind == bus.CacheKindBootstrap || payload.Kind == bus.CacheKindAgent {
				if payload.Key != "" {
					agentID, err := uuid.Parse(payload.Key)
					if err == nil {
						contextFileInterceptor.InvalidateAgent(agentID)
					}
				} else {
					contextFileInterceptor.InvalidateAll()
				}
			}
		})
	}

	// Agent router: invalidate Loop cache on agent config changes
	msgBus.Subscribe(bus.TopicCacheAgent, func(event bus.Event) {
		if event.Name != protocol.EventCacheInvalidate {
			return
		}
		payload, ok := event.Payload.(bus.CacheInvalidatePayload)
		if !ok || payload.Kind != bus.CacheKindAgent {
			return
		}
		if payload.Key != "" {
			agentRouter.InvalidateAgent(payload.Key)
		}
	})

	// Skills cache: bump version on skill changes
	if stores.Skills != nil {
		msgBus.Subscribe(bus.TopicCacheSkills, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindSkills {
				return
			}
			stores.Skills.BumpVersion()
		})
	}

	// Skill grants cache: invalidate all agent caches when grants change
	msgBus.Subscribe(bus.TopicCacheSkillGrants, func(event bus.Event) {
		if event.Name != protocol.EventCacheInvalidate {
			return
		}
		payload, ok := event.Payload.(bus.CacheInvalidatePayload)
		if !ok || payload.Kind != bus.CacheKindSkillGrants {
			return
		}
		agentRouter.InvalidateAll()
	})

	// MCP cache: invalidate all agent caches when MCP servers/grants change
	msgBus.Subscribe(bus.TopicCacheMCP, func(event bus.Event) {
		if event.Name != protocol.EventCacheInvalidate {
			return
		}
		payload, ok := event.Payload.(bus.CacheInvalidatePayload)
		if !ok || payload.Kind != bus.CacheKindMCP {
			return
		}
		agentRouter.InvalidateAll()
	})

	// Cron cache: invalidate job cache on cron changes
	if ci, ok := stores.Cron.(store.CacheInvalidatable); ok {
		msgBus.Subscribe(bus.TopicCacheCron, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindCron {
				return
			}
			ci.InvalidateCache()
		})
	}

	// Heartbeat cache: invalidate due cache on config changes
	if hi, ok := stores.Heartbeats.(store.CacheInvalidatable); ok {
		msgBus.Subscribe(bus.TopicCacheHeartbeat, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindHeartbeat {
				return
			}
			hi.InvalidateCache()
		})
	}

	// Config permissions cache: invalidate on grant/revoke changes
	if pi, ok := stores.ConfigPermissions.(store.CacheInvalidatable); ok {
		msgBus.Subscribe(bus.TopicCacheConfigPerms, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindConfigPerms {
				return
			}
			pi.InvalidateCache()
		})
	}

	// Builtin tools cache: re-apply disables on settings/enabled changes
	if stores.BuiltinTools != nil {
		msgBus.Subscribe(bus.TopicCacheBuiltinTools, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindBuiltinTools {
				return
			}
			applyBuiltinToolDisables(context.Background(), stores.BuiltinTools, toolsReg)
			agentRouter.InvalidateAll()
		})
	}

	// V3 evolution: daily suggestion engine + weekly evaluation cron (background goroutine).
	if stores.EvolutionMetrics != nil && stores.EvolutionSuggestions != nil {
		sugEngine := agent.NewSuggestionEngine(stores.EvolutionMetrics, stores.EvolutionSuggestions)
		go runEvolutionCron(stores, sugEngine)
	}

	// Register team tools (team_tasks + workspace interceptor) if team store is available.
	var postTurn tools.PostTurnProcessor
	if stores.Teams != nil && stores.Agents != nil {
		teamMgr := tools.NewTeamToolManager(stores.Teams, stores.Agents, msgBus, workspace)
		postTurn = teamMgr
		var teamPolicy tools.TeamActionPolicy = tools.FullTeamPolicy{}
		if !edition.Current().TeamFullMode {
			teamPolicy = tools.LiteTeamPolicy{}
		}
		toolsReg.Register(tools.NewTeamTasksTool(teamMgr, teamPolicy))
		// Wire workspace interceptor into write_file so team workspace validation
		// and event broadcasting happen transparently via existing file tools.
		wsInterceptor := tools.NewWorkspaceInterceptor(teamMgr)
		if writeTool, ok := toolsReg.Get("write_file"); ok {
			if wia, ok := writeTool.(tools.WorkspaceInterceptorAware); ok {
				wia.SetWorkspaceInterceptor(wsInterceptor)
			}
		}
		slog.Info("team tools registered", "workspace", workspace)

		// Team cache invalidation via pub/sub
		msgBus.Subscribe(bus.TopicCacheTeam, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindTeam {
				return
			}
			teamMgr.InvalidateTeam()
		})

		// Agent cache invalidation: clear TeamToolManager's agent lookup cache
		// when agent data changes (update/delete via WS or HTTP).
		msgBus.Subscribe("cache.agent.team_mgr", func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindAgent {
				return
			}
			teamMgr.InvalidateAgentCache()
		})
		slog.Info("team tools registered")
	}

	// User workspace cache: invalidate per-user workspace path on profile changes
	msgBus.Subscribe(bus.TopicCacheUserWorkspace, func(event bus.Event) {
		if event.Name != protocol.EventCacheInvalidate {
			return
		}
		payload, ok := event.Payload.(bus.CacheInvalidatePayload)
		if !ok || payload.Kind != bus.CacheKindUserWorkspace {
			return
		}
		if payload.Key != "" {
			agentRouter.InvalidateUserWorkspace(payload.Key)
		}
	})

	// Provider cache: re-register ACP providers on create/update/delete
	msgBus.Subscribe(bus.TopicCacheProvider, func(event bus.Event) {
		if event.Name != protocol.EventCacheInvalidate {
			return
		}
		payload, ok := event.Payload.(bus.CacheInvalidatePayload)
		if !ok || payload.Kind != bus.CacheKindProvider {
			return
		}
		if payload.Key == "" {
			return
		}
		// Re-register from DB if provider still exists and is ACP type
		provCtx := store.WithTenantID(context.Background(), event.TenantID)
		p, err := stores.Providers.GetProviderByName(provCtx, payload.Key)
		if err != nil {
			// Provider was deleted or not found — already unregistered by handler
			return
		}
		if p.ProviderType != store.ProviderACP {
			return
		}
		// Unregister old instance (closes ProcessPool) then re-register
		providerReg.Unregister(p.Name)
		if p.Enabled {
			registerACPFromDB(providerReg, *p)
		}
	})

	slog.Info("resolver + interceptors + cache subscribers wired")
	return contextFileInterceptor, mcpPool, mediaStore, postTurn
}

// kgSettings holds KG extraction settings from the builtin_tools table.
type kgSettings struct {
	ExtractOnMemoryWrite bool    `json:"extract_on_memory_write"`
	ExtractionProvider   string  `json:"extraction_provider"`
	ExtractionModel      string  `json:"extraction_model"`
	MinConfidence        float64 `json:"min_confidence"`
}

// buildKGExtractFunc returns a callback that extracts entities from memory content.
// Settings are read from the builtin_tools table on each invocation (not cached),
// so changes take effect immediately without restart.
func buildKGExtractFunc(kgStore store.KnowledgeGraphStore, bts store.BuiltinToolStore, providerReg *providers.Registry) tools.KGExtractFunc {
	return func(ctx context.Context, agentID, userID, content string) {
		slog.Info("kg extract: triggered", "agent", agentID, "user", userID, "content_len", len(content))
		// Read settings from DB on each call so admin changes take effect immediately
		raw, err := bts.GetSettings(ctx, "knowledge_graph_search")
		if err != nil || raw == nil {
			slog.Warn("kg extract: no settings found", "error", err)
			return
		}
		var settings kgSettings
		if err := json.Unmarshal(raw, &settings); err != nil {
			slog.Warn("kg extract: invalid settings", "error", err)
			return
		}
		if !settings.ExtractOnMemoryWrite || settings.ExtractionProvider == "" || settings.ExtractionModel == "" {
			return
		}

		p, err := providerReg.Get(ctx, settings.ExtractionProvider)
		if err != nil {
			slog.Warn("kg extract: provider not found", "provider", settings.ExtractionProvider, "error", err)
			return
		}
		extractor := kg.NewExtractor(p, settings.ExtractionModel, settings.MinConfidence)
		result, err := extractor.Extract(ctx, content)
		if err != nil {
			slog.Warn("kg extract: extraction failed", "agent", agentID, "error", err)
			return
		}
		if len(result.Entities) == 0 && len(result.Relations) == 0 {
			return
		}
		for i := range result.Entities {
			result.Entities[i].AgentID = agentID
			result.Entities[i].UserID = userID
		}
		for i := range result.Relations {
			result.Relations[i].AgentID = agentID
			result.Relations[i].UserID = userID
		}
		entityIDs, err := kgStore.IngestExtraction(ctx, agentID, userID, result.Entities, result.Relations)
		if err != nil {
			slog.Warn("kg extract: ingest failed", "agent", agentID, "error", err)
			return
		}
		slog.Info("kg extract: ingested from memory write", "agent", agentID, "entities", len(result.Entities), "relations", len(result.Relations))

		// Run inline dedup on newly upserted entities (best-effort, non-blocking)
		if len(entityIDs) > 0 {
			merged, flagged, dedupErr := kgStore.DedupAfterExtraction(ctx, agentID, userID, entityIDs)
			if dedupErr != nil {
				slog.Warn("kg extract: dedup failed", "agent", agentID, "error", dedupErr)
			} else if merged > 0 || flagged > 0 {
				slog.Info("kg extract: dedup completed", "agent", agentID, "auto_merged", merged, "candidates_flagged", flagged)
			}
		}
	}
}
