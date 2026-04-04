package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
	"github.com/nextlevelbuilder/goclaw/pkg/browser"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// setupToolRegistry creates the tool registry and registers all tools.
// Returns the registry, exec approval manager, MCP manager, sandbox manager,
// browser manager (caller must defer Close), web fetch tool, TTS tool,
// permission policy engine, tool policy engine, data directory, and resolved agent defaults.
func setupToolRegistry(
	cfg *config.Config,
	workspace string,
	providerRegistry *providers.Registry,
) (
	toolsReg *tools.Registry,
	execApprovalMgr *tools.ExecApprovalManager,
	mcpMgr *mcpbridge.Manager,
	sandboxMgr sandbox.Manager,
	browserMgr *browser.Manager,
	webFetchTool *tools.WebFetchTool,
	ttsTool *tools.TtsTool,
	permPE *permissions.PolicyEngine,
	toolPE *tools.PolicyEngine,
	dataDir string,
	agentCfg config.AgentDefaults,
) {
	// Create tool registry with all tools
	toolsReg = tools.NewRegistry()
	agentCfg = cfg.ResolveAgent("default")

	// Sandbox manager (optional — routes tools through Docker containers)
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
	if cfg.Tools.Browser.Enabled {
		var opts []browser.Option
		if cfg.Tools.Browser.RemoteURL != "" {
			opts = append(opts, browser.WithRemoteURL(cfg.Tools.Browser.RemoteURL))
			slog.Info("browser tool enabled", "remote", cfg.Tools.Browser.RemoteURL)
		} else {
			opts = append(opts, browser.WithHeadless(cfg.Tools.Browser.Headless))
			slog.Info("browser tool enabled", "headless", cfg.Tools.Browser.Headless)
		}
		if cfg.Tools.Browser.ActionTimeoutMs > 0 {
			opts = append(opts, browser.WithActionTimeout(time.Duration(cfg.Tools.Browser.ActionTimeoutMs)*time.Millisecond))
		}
		if cfg.Tools.Browser.IdleTimeoutMs > 0 {
			opts = append(opts, browser.WithIdleTimeout(time.Duration(cfg.Tools.Browser.IdleTimeoutMs)*time.Millisecond))
		} else if cfg.Tools.Browser.IdleTimeoutMs < 0 {
			// Explicitly disable idle reaper with negative value
			opts = append(opts, browser.WithIdleTimeout(0))
		}
		if cfg.Tools.Browser.MaxPages > 0 {
			opts = append(opts, browser.WithMaxPages(cfg.Tools.Browser.MaxPages))
		}
		browserMgr = browser.New(opts...)
		toolsReg.Register(browser.NewBrowserTool(browserMgr))
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
	webFetchTool = tools.NewWebFetchTool(tools.WebFetchConfig{
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

	// TTS (text-to-speech) system — always create TtsTool so config reload can populate it later
	ttsMgr := setupTTS(cfg)
	if ttsMgr == nil {
		ttsMgr = tts.NewManager(tts.ManagerConfig{})
	}
	ttsTool = tools.NewTtsTool(ttsMgr)
	toolsReg.Register(ttsTool)
	if ttsMgr.HasProviders() {
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
	if len(cfg.Tools.McpServers) > 0 {
		mcpMgr = mcpbridge.NewManager(toolsReg, mcpbridge.WithConfigs(cfg.Tools.McpServers))
		if err := mcpMgr.Start(context.Background()); err != nil {
			slog.Warn("mcp.startup_errors", "error", err)
		}
		slog.Info("MCP servers initialized", "configured", len(cfg.Tools.McpServers), "tools", len(mcpMgr.ToolNames()))
	}

	// Exec approval system — always active (deny patterns + safe bins + configurable ask mode)
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
	permPE = permissions.NewPolicyEngine(cfg.Gateway.OwnerIDs)

	// Tool policy engine (7-step tool filtering pipeline)
	toolPE = tools.NewPolicyEngine(&cfg.Tools)

	// Data directory for Phase 2 services
	dataDir = cfg.ResolvedDataDir()
	os.MkdirAll(dataDir, 0755)

	// Block exec from accessing sensitive directories (data dir, .goclaw, config file).
	// Prevents `cp /app/data/config.json workspace/` and similar exfiltration.
	// Exception: .goclaw/skills-store/ is allowed (skills may contain executable scripts).
	if execTool, ok := toolsReg.Get("exec"); ok {
		if et, ok := execTool.(*tools.ExecTool); ok {
			et.DenyPaths(dataDir, ".goclaw/")
			et.AllowPathExemptions(".goclaw/skills-store/")
			// Harden: block access to internal workspace files via shell commands.
			// Prevents `cat ../config.json`, `cat memory.db` etc. from user workspaces.
			et.DenyPaths(
				filepath.Join(workspace, "memory.db"),
				filepath.Join(workspace, "memory.db-wal"),
				filepath.Join(workspace, "memory.db-shm"),
				filepath.Join(workspace, "config.json"),
				filepath.Join(workspace, "delegate"),
				filepath.Join(dataDir, "goclaw.db"),
				filepath.Join(dataDir, "goclaw.db-wal"),
				filepath.Join(dataDir, "goclaw.db-shm"),
			)
			if cfgPath := os.Getenv("GOCLAW_CONFIG"); cfgPath != "" {
				et.DenyPaths(cfgPath)
			}
		}
	}

	// Block filesystem tools from accessing internal system files within the workspace.
	// Shared-workspace agents have workspace = dataDir root, exposing config.json,
	// memory.db, .media/, delegate/ etc. via list_files/read_file.
	// Non-shared agents are already isolated by resolvePath boundary check, but
	// deny paths add defense-in-depth.
	internalDenyPaths := []string{
		"config.json", "memory.db", "memory.db-wal", "memory.db-shm",
		"goclaw.db", "goclaw.db-wal", "goclaw.db-shm",
		"memory/", ".media/", ".uploads/", "delegate/",
	}
	// read_file: allow .media/ access (uploaded documents accessed via AllowPaths
	// for backward compat; new uploads go to per-user .uploads/ within workspace).
	readFileDenyPaths := []string{
		"config.json", "memory.db", "memory.db-wal", "memory.db-shm",
		"goclaw.db", "goclaw.db-wal", "goclaw.db-shm",
		"memory/", "delegate/",
	}
	if rf, ok := toolsReg.Get("read_file"); ok {
		if t, ok := rf.(*tools.ReadFileTool); ok {
			t.DenyPaths(readFileDenyPaths...)
		}
	}
	if wf, ok := toolsReg.Get("write_file"); ok {
		if t, ok := wf.(*tools.WriteFileTool); ok {
			t.DenyPaths(internalDenyPaths...)
		}
	}
	if lf, ok := toolsReg.Get("list_files"); ok {
		if t, ok := lf.(*tools.ListFilesTool); ok {
			t.DenyPaths(internalDenyPaths...)
		}
	}
	if ed, ok := toolsReg.Get("edit"); ok {
		if t, ok := ed.(*tools.EditTool); ok {
			t.DenyPaths(internalDenyPaths...)
		}
	}

	return
}

// wireTracingAndCron sets up tracing collector, snapshot worker, and cron config
// on an already-created store set. Shared between PG and SQLite build variants.
func wireTracingAndCron(
	cfg *config.Config,
	stores *store.Stores,
	msgBus *bus.MessageBus,
	dataDir string,
) (*tracing.Collector, *tracing.SnapshotWorker) {
	var traceCollector *tracing.Collector
	if stores.Tracing != nil {
		traceCollector = tracing.NewCollector(stores.Tracing)
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

	// Start snapshot worker for hourly usage aggregation
	var snapshotWorker *tracing.SnapshotWorker
	if stores.Snapshots != nil {
		snapshotWorker = tracing.NewSnapshotWorker(stores.DB, stores.Snapshots)
		snapshotWorker.Start()

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

	// Wire cron config from config.json
	cronRetryCfg := cfg.Cron.ToRetryConfig()
	if stores.Cron != nil {
		stores.Cron.SetOnJob(nil) // ensure initialized; actual handler set below
		_ = cronRetryCfg          // config available; cron store reads it internally
		if cfg.Cron.DefaultTimezone != "" {
			stores.Cron.SetDefaultTimezone(cfg.Cron.DefaultTimezone)
		}
	}

	// Load secrets from config_secrets table before env overrides.
	// Precedence: config.json → DB secrets → env vars (highest).
	if stores.ConfigSecrets != nil {
		if secrets, err := stores.ConfigSecrets.GetAll(context.Background()); err == nil && len(secrets) > 0 {
			cfg.ApplyDBSecrets(secrets)
			cfg.ApplyEnvOverrides()
			slog.Info("config secrets loaded from DB", "count", len(secrets))
		}
	}

	return traceCollector, snapshotWorker
}

// setupMemoryEmbeddings wires embedding provider to PGMemoryStore and triggers backfill.
// Resolves embedding provider from DB providers with settings.embedding.enabled.
func setupMemoryEmbeddings(
	pgStores *store.Stores,
	providerRegistry *providers.Registry,
) {
	if pgStores.Memory != nil {
		if embProvider := resolveEmbeddingProvider(pgStores.Providers, providerRegistry, pgStores.SystemConfigs); embProvider != nil {
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

			// Wire embedding provider into team store for semantic task search.
			if pgTeamStore, ok := pgStores.Teams.(*pg.PGTeamStore); ok {
				pgTeamStore.SetEmbeddingProvider(embProvider)
				go func() {
					if count, err := pgTeamStore.BackfillTaskEmbeddings(context.Background()); err != nil {
						slog.Warn("task embeddings backfill failed", "error", err)
					} else if count > 0 {
						slog.Info("task embeddings backfill complete", "tasks_updated", count)
					}
				}()
			}

			// Wire embedding provider into KG store for entity semantic search.
			if pgKG, ok := pgStores.KnowledgeGraph.(*pg.PGKnowledgeGraphStore); ok {
				pgKG.SetEmbeddingProvider(embProvider)
				go func() {
					if count, err := pgKG.BackfillKGEmbeddings(context.Background()); err != nil {
						slog.Warn("KG embeddings backfill failed", "error", err)
					} else if count > 0 {
						slog.Info("KG embeddings backfill complete", "entities_updated", count)
					}
				}()
			}
		} else {
			slog.Warn("memory embeddings disabled (no API key), chunks stored without vectors")
		}
	}
}

// seedSystemConfigs ensures system_configs has all expected keys for all tenants.
// Inserts missing keys from config.json without overwriting existing values.
func seedSystemConfigs(sc store.SystemConfigStore, ts store.TenantStore, cfg *config.Config) {
	syncSystemConfigs(sc, ts, cfg, true) // onlyMissing=true
}

// loadBootstrapFiles loads bootstrap files for the default agent's system prompt from DB.
// Seeds if empty; falls back to filesystem as last resort.
func loadBootstrapFiles(
	pgStores *store.Stores,
	workspace string,
	agentCfg config.AgentDefaults,
) []bootstrap.ContextFile {
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

	return contextFiles
}

// setupSkillsSystem creates the skills loader, registers skill tools, wires skills-store,
// seeds bundled skills, and enables embedding-based skill search.
func setupSkillsSystem(
	cfg *config.Config,
	workspace string,
	dataDir string,
	pgStores *store.Stores,
	toolsReg *tools.Registry,
	providerRegistry *providers.Registry,
	msgBus *bus.MessageBus,
) (*skills.Loader, *tools.SkillSearchTool, string, string, string) {
	var bundledSkillsDir string // resolved later; returned for HTTP handler fallback

	// Skills loader + search tool
	// Global skills live under ~/.goclaw/skills/ (user-managed), not data/skills/.
	globalSkillsDir := os.Getenv("GOCLAW_SKILLS_DIR")
	if globalSkillsDir == "" {
		globalSkillsDir = filepath.Join(dataDir, "skills")
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
	slog.Info("skill_search tool registered", "skills", len(skillsLoader.ListSkills(context.Background())))

	// Wire skills-store directory into filesystem loader so agents
	// can discover uploaded skills in their system prompt and BM25 search index.
	if pgStores.Skills != nil {
		storeDirs := pgStores.Skills.Dirs()
		if len(storeDirs) > 0 {
			skillsLoader.SetManagedDir(storeDirs[0])
			slog.Info("skills-store directory wired into loader", "dir", storeDirs[0])

			// Seed system/bundled skills into DB
			bundledSkillsDir = os.Getenv("GOCLAW_BUNDLED_SKILLS_DIR")
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
				if seederStore, ok := pgStores.Skills.(skills.SystemSkillStore); ok {
					seeder := skills.NewSeeder(bundledSkillsDir, storeDirs[0], seederStore)
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

	// Publish skill tool — lets agents register created skills in the database.
	// Disabled in lite edition: agents should not self-manage skills on desktop.
	if pgStores.Skills != nil && edition.Current().TeamFullMode {
		if manageStore, ok := pgStores.Skills.(store.SkillManageStore); ok {
			storeDirs := pgStores.Skills.Dirs()
			if len(storeDirs) > 0 {
				toolsReg.Register(tools.NewPublishSkillTool(manageStore, storeDirs[0], dataDir, skillsLoader))
				slog.Info("publish_skill tool registered")
				toolsReg.Register(tools.NewSkillManageTool(manageStore, storeDirs[0], dataDir, skillsLoader))
				slog.Info("skill_manage tool registered")
			}
		}
	}

	// Wire embedding-based skill search + per-agent access filtering
	if pgStores.Skills != nil {
		if sas, ok := pgStores.Skills.(store.SkillAccessStore); ok {
			skillSearchTool.SetSkillAccessStore(sas)
		}
		if pgSkills, ok := pgStores.Skills.(*pg.PGSkillStore); ok {
			if embProvider := resolveEmbeddingProvider(pgStores.Providers, providerRegistry, pgStores.SystemConfigs); embProvider != nil {
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

	return skillsLoader, skillSearchTool, globalSkillsDir, bundledSkillsDir, builtinSkillsDir
}

