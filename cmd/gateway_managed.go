package cmd

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
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
) (*tools.ContextFileInterceptor, *tools.DelegateManager) {
	// 1. Context file interceptor (created before resolver so callbacks can reference it)
	var contextFileInterceptor *tools.ContextFileInterceptor
	var delegateMgr *tools.DelegateManager
	if stores.Agents != nil {
		contextFileInterceptor = tools.NewContextFileInterceptor(stores.Agents, workspace)
	}

	// 1b. Group writer cache (wraps ListGroupFileWriters with TTL cache)
	var groupWriterCache *store.GroupWriterCache
	if stores.Agents != nil {
		groupWriterCache = store.NewGroupWriterCache(stores.Agents)
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

	// 5. Set up agent resolver: lazy-creates Loops from DB
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
		GroupWriterCache:       groupWriterCache,
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
			if stores.Memory != nil {
				ia.SetMemoryInterceptor(tools.NewMemoryInterceptor(stores.Memory, workspace))
			}
		}
	}
	if editTool, ok := toolsReg.Get("edit"); ok {
		if ia, ok := editTool.(tools.InterceptorAware); ok {
			if contextFileInterceptor != nil {
				ia.SetContextFileInterceptor(contextFileInterceptor)
			}
			if stores.Memory != nil {
				ia.SetMemoryInterceptor(tools.NewMemoryInterceptor(stores.Memory, workspace))
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
			})
			if err != nil {
				return nil, err
			}
			dr := &tools.DelegateRunResult{
				Content:      result.Content,
				Iterations:   result.Iterations,
				Deliverables: result.Deliverables,
			}
			for _, m := range result.Media {
				dr.MediaPaths = append(dr.MediaPaths, m.Path)
			}
			return dr, nil
		}
		delegateMgr = tools.NewDelegateManager(runAgentFn, stores.AgentLinks, stores.Agents, msgBus)
		if stores.Teams != nil {
			delegateMgr.SetTeamStore(stores.Teams)
		}
		delegateMgr.SetSessionStore(stores.Sessions)

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
		toolsReg.Register(tools.NewHandoffTool(delegateMgr, stores.Teams, stores.Sessions, msgBus))

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
			if embProvider := resolveEmbeddingProvider(appCfg, memCfg); embProvider != nil {
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

	// Register team tools (team_tasks + team_message) if team store is available.
	if stores.Teams != nil && stores.Agents != nil {
		teamMgr := tools.NewTeamToolManager(stores.Teams, stores.Agents, msgBus)
		if delegateMgr != nil {
			teamMgr.SetDelegateManager(delegateMgr)
		}
		toolsReg.Register(tools.NewTeamTasksTool(teamMgr))
		toolsReg.Register(tools.NewTeamMessageTool(teamMgr))

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
	return contextFileInterceptor, delegateMgr
}

// wireHTTP creates HTTP handlers (agents + skills + traces + MCP + custom tools + channel instances + providers + delegations + builtin tools).
func wireHTTP(stores *store.Stores, token string, msgBus *bus.MessageBus, toolsReg *tools.Registry, providerReg *providers.Registry, isOwner func(string) bool) (*httpapi.AgentsHandler, *httpapi.SkillsHandler, *httpapi.TracesHandler, *httpapi.MCPHandler, *httpapi.CustomToolsHandler, *httpapi.ChannelInstancesHandler, *httpapi.ProvidersHandler, *httpapi.DelegationsHandler, *httpapi.BuiltinToolsHandler) {
	var agentsH *httpapi.AgentsHandler
	var skillsH *httpapi.SkillsHandler
	var tracesH *httpapi.TracesHandler
	var mcpH *httpapi.MCPHandler
	var customToolsH *httpapi.CustomToolsHandler
	var channelInstancesH *httpapi.ChannelInstancesHandler
	var providersH *httpapi.ProvidersHandler
	var delegationsH *httpapi.DelegationsHandler
	var builtinToolsH *httpapi.BuiltinToolsHandler

	if stores != nil && stores.Agents != nil {
		var summoner *httpapi.AgentSummoner
		if providerReg != nil {
			summoner = httpapi.NewAgentSummoner(stores.Agents, providerReg, msgBus)
		}
		agentsH = httpapi.NewAgentsHandler(stores.Agents, token, msgBus, summoner, isOwner)
	}

	if stores != nil && stores.Skills != nil {
		if pgSkills, ok := stores.Skills.(*pg.PGSkillStore); ok {
			dirs := pgSkills.Dirs()
			if len(dirs) > 0 {
				skillsH = httpapi.NewSkillsHandler(pgSkills, dirs[0], token, msgBus)
			}
		}
	}

	if stores != nil && stores.Tracing != nil {
		tracesH = httpapi.NewTracesHandler(stores.Tracing, token)
	}

	if stores != nil && stores.MCP != nil {
		mcpH = httpapi.NewMCPHandler(stores.MCP, token, msgBus)
	}

	if stores != nil && stores.CustomTools != nil {
		customToolsH = httpapi.NewCustomToolsHandler(stores.CustomTools, token, msgBus, toolsReg)
	}

	if stores != nil && stores.ChannelInstances != nil {
		channelInstancesH = httpapi.NewChannelInstancesHandler(stores.ChannelInstances, stores.Agents, token, msgBus)
	}

	if stores != nil && stores.Providers != nil {
		providersH = httpapi.NewProvidersHandler(stores.Providers, token, providerReg)
	}

	if stores != nil && stores.Teams != nil {
		delegationsH = httpapi.NewDelegationsHandler(stores.Teams, token)
	}

	if stores != nil && stores.BuiltinTools != nil {
		builtinToolsH = httpapi.NewBuiltinToolsHandler(stores.BuiltinTools, token, msgBus)
	}

	return agentsH, skillsH, tracesH, mcpH, customToolsH, channelInstancesH, providersH, delegationsH, builtinToolsH
}
