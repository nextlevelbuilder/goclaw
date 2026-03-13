package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	kg "github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/media"
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
	dynamicLoader *tools.DynamicToolLoader,
	redisClient any, // nil when built without -tags redis or when Redis is unconfigured
) (*tools.ContextFileInterceptor, *tools.DelegateManager, *mcpbridge.Pool, *media.Store) {
	// 1. Build cache instances (in-memory or Redis depending on build tags)
	agentCtxCache, userCtxCache, gwCache := makeCaches(redisClient)

	// 1a. Context file interceptor (created before resolver so callbacks can reference it)
	var contextFileInterceptor *tools.ContextFileInterceptor
	var delegateMgr *tools.DelegateManager
	if stores.Agents != nil {
		contextFileInterceptor = tools.NewContextFileInterceptor(stores.Agents, workspace, agentCtxCache, userCtxCache)
	}

	// 1b. Group writer cache (wraps ListGroupFileWriters with TTL cache)
	var groupWriterCache *store.GroupWriterCache
	if stores.Agents != nil {
		groupWriterCache = store.NewGroupWriterCache(stores.Agents, gwCache)
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

	// 2. User seeding callback: seeds per-user context files on first chat
	var ensureUserFiles agent.EnsureUserFilesFunc
	if stores.Agents != nil {
		ensureUserFiles = buildEnsureUserFiles(stores.Agents, msgBus)
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
		mcpPool = mcpbridge.NewPool()
	}

	// 6. Set up agent resolver: lazy-creates Loops from DB
	var skillAccessStore store.SkillAccessStore
	if sas, ok := stores.Skills.(store.SkillAccessStore); ok {
		skillAccessStore = sas
	}

	resolver := agent.NewManagedResolver(agent.ResolverDeps{
		AgentStore:             stores.Agents,
		ProviderReg:            providerReg,
		Bus:                    msgBus,
		Sessions:               sessStore,
		Tools:                  toolsReg,
		ToolPolicy:             toolPE,
		Skills:                 skillsLoader,
		SkillAccessStore:       skillAccessStore,
		HasMemory:              hasMemory,
		TraceCollector:         traceCollector,
		EnsureUserFiles:        ensureUserFiles,
		ContextFileLoader:      contextFileLoader,
		BootstrapCleanup:       buildBootstrapCleanup(stores.Agents),
		InjectionAction:        injectionAction,
		MaxMessageChars:        appCfg.Gateway.MaxMessageChars,
		CompactionCfg:          appCfg.Agents.Defaults.Compaction,
		ContextPruningCfg:      appCfg.Agents.Defaults.ContextPruning,
		SandboxEnabled:         sandboxEnabled,
		SandboxContainerDir:    sandboxContainerDir,
		SandboxWorkspaceAccess: sandboxWorkspaceAccess,
		DynamicLoader:          dynamicLoader,
		AgentLinkStore:         stores.AgentLinks,
		TeamStore:              stores.Teams,
		BuiltinToolStore:       stores.BuiltinTools,
		MCPStore:               stores.MCP,
		MCPPool:                mcpPool,
		GroupWriterCache:       groupWriterCache,
		MediaStore:             mediaStore,
		ModelPricing:           appCfg.Telemetry.ModelPricing,
		TracingStore:           stores.Tracing,
		OnEvent: func(event agent.AgentEvent) {
			msgBus.Broadcast(bus.Event{
				Name:    protocol.EventAgent,
				Payload: event,
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

	// Wire group writer cache for permission checks
	if groupWriterCache != nil {
		for _, toolName := range []string{"read_file", "write_file", "edit", "cron"} {
			if t, ok := toolsReg.Get(toolName); ok {
				if gwa, ok := t.(tools.GroupWriterAware); ok {
					gwa.SetGroupWriterCache(groupWriterCache)
				}
			}
		}
		if contextFileInterceptor != nil {
			contextFileInterceptor.SetGroupWriterCache(groupWriterCache)
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
		slog.Info("memory layering enabled (Postgres)")
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

	// Custom tools cache: reload global tools on create/update/delete
	if dynamicLoader != nil {
		msgBus.Subscribe(bus.TopicCacheCustomTools, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindCustomTools {
				return
			}
			dynamicLoader.ReloadGlobal(context.Background(), toolsReg)
			// Invalidate all agent caches so they re-resolve with updated tools
			agentRouter.InvalidateAll()
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

	// Register delegate tool (inter-agent delegation) if link store is available.
	// Uses a callback to bridge tools.DelegateRunRequest → agent.RunRequest,
	// avoiding import cycle between tools and agent packages.
	if stores.AgentLinks != nil && stores.Agents != nil {
		runAgentFn := func(ctx context.Context, agentKey string, req tools.DelegateRunRequest) (*tools.DelegateRunResult, error) {
			loop, err := agentRouter.Get(agentKey)
			if err != nil {
				return nil, err
			}
			result, err := loop.Run(ctx, agent.RunRequest{
				SessionKey:        req.SessionKey,
				Message:           req.Message,
				Media:             req.Media,
				UserID:            req.UserID,
				Channel:           req.Channel,
				ChatID:            req.ChatID,
				PeerKind:          req.PeerKind,
				RunID:             req.RunID,
				Stream:            req.Stream,
				ExtraSystemPrompt: req.ExtraSystemPrompt,
				MaxIterations:     req.MaxIterations,
				RunKind:           "delegation",
				DelegationID:      req.DelegationID,
				TeamID:            req.TeamID,
				TeamTaskID:        req.TeamTaskID,
				ParentAgentID:     req.ParentAgentID,
				WorkspaceChannel:  req.WorkspaceChannel,
				WorkspaceChatID:   req.WorkspaceChatID,
			})
			if err != nil {
				return nil, err
			}
			var drMedia []bus.MediaFile
			for _, m := range result.Media {
				drMedia = append(drMedia, bus.MediaFile{Path: m.Path, MimeType: m.ContentType})
			}
			dr := &tools.DelegateRunResult{
				Content:      result.Content,
				Iterations:   result.Iterations,
				Deliverables: result.Deliverables,
				Media:        drMedia,
			}
			return dr, nil
		}
		delegateMgr = tools.NewDelegateManager(runAgentFn, stores.AgentLinks, stores.Agents, msgBus)
		if stores.Teams != nil {
			delegateMgr.SetTeamStore(stores.Teams)
		}
		delegateMgr.SetSessionStore(stores.Sessions)
		if mediaStore != nil {
			delegateMgr.SetMediaLoader(mediaStore)
		}

		// Hook engine (quality gates)
		hookEngine := hooks.NewEngine()
		hookEngine.RegisterEvaluator(hooks.HookTypeCommand, hooks.NewCommandEvaluator(workspace))
		agentEvalFn := func(ctx context.Context, agentKey, task string) (string, error) {
			result, err := delegateMgr.Delegate(hooks.WithSkipHooks(ctx, true), tools.DelegateOpts{
				TargetAgentKey: agentKey, Task: task, Mode: "sync",
			})
			if err != nil {
				return "", err
			}
			return result.Content, nil
		}
		hookEngine.RegisterEvaluator(hooks.HookTypeAgent, hooks.NewAgentEvaluator(agentEvalFn))
		delegateMgr.SetHookEngine(hookEngine)

		// Evaluate-optimize loop tool
		toolsReg.Register(tools.NewEvaluateLoopTool(delegateMgr))

		// Handoff tool (agent-to-agent conversation transfer)
		toolsReg.Register(tools.NewHandoffTool(delegateMgr, stores.Teams, stores.Sessions, msgBus, workspace))

		// Inject delegation capability into existing SpawnTool
		if st, ok := toolsReg.Get("spawn"); ok {
			if spawnTool, ok := st.(*tools.SpawnTool); ok {
				spawnTool.SetDelegateManager(delegateMgr)
				slog.Info("spawn tool: delegation enabled")
			}
		}

		// Register delegate_search tool (hybrid FTS + semantic agent discovery)
		var delegateEmbProvider store.EmbeddingProvider
		if agentStore, ok := stores.Agents.(*pg.PGAgentStore); ok {
			memCfg := appCfg.Agents.Defaults.Memory
			if embProvider := resolveEmbeddingProvider(appCfg, memCfg, providerReg); embProvider != nil {
				agentStore.SetEmbeddingProvider(embProvider)
				delegateEmbProvider = embProvider
				slog.Info("agent embeddings enabled")

				// Backfill embeddings for existing agents with frontmatter
				go func() {
					count, err := agentStore.BackfillAgentEmbeddings(context.Background())
					if err != nil {
						slog.Warn("agent embeddings backfill failed", "error", err)
					} else if count > 0 {
						slog.Info("agent embeddings backfill complete", "updated", count)
					}
				}()
			}
		}
		toolsReg.Register(tools.NewDelegateSearchTool(stores.AgentLinks, delegateEmbProvider))
		slog.Info("delegate + delegate_search tools registered")
	}

	// Register team tools (team_tasks + team_message + workspace) if team store is available.
	if stores.Teams != nil && stores.Agents != nil {
		teamMgr := tools.NewTeamToolManager(stores.Teams, stores.Agents, msgBus)
		if delegateMgr != nil {
			teamMgr.SetDelegateManager(delegateMgr)
		}
		toolsReg.Register(tools.NewTeamTasksTool(teamMgr))
		toolsReg.Register(tools.NewTeamMessageTool(teamMgr))
		toolsReg.Register(tools.NewWorkspaceWriteTool(teamMgr, workspace))
		toolsReg.Register(tools.NewWorkspaceReadTool(teamMgr, workspace))
		slog.Info("team + workspace tools registered", "workspace", workspace)

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

	// Group writer cache: invalidate on writer list changes
	if groupWriterCache != nil {
		msgBus.Subscribe(bus.TopicCacheGroupFileWriters, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindGroupFileWriters {
				return
			}
			if payload.Key != "" {
				groupWriterCache.Invalidate(payload.Key)
			} else {
				groupWriterCache.InvalidateAll()
			}
		})
	}

	slog.Info("resolver + interceptors + cache subscribers wired")
	return contextFileInterceptor, delegateMgr, mcpPool, mediaStore
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

		p, err := providerReg.Get(settings.ExtractionProvider)
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
		if err := kgStore.IngestExtraction(ctx, agentID, userID, result.Entities, result.Relations); err != nil {
			slog.Warn("kg extract: ingest failed", "agent", agentID, "error", err)
			return
		}
		slog.Info("kg extract: ingested from memory write", "agent", agentID, "entities", len(result.Entities), "relations", len(result.Relations))
	}
}

