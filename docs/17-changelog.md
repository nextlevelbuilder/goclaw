# Changelog

All notable changes to GoClaw Gateway are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [v2.66.0] — 2026-04-05

### Security
- **Session IDOR fix**: All 5 chat.* WS methods (send, history, inject, abort, session.status) now verify session ownership. Non-admin users cannot read, write, or disrupt other users' sessions
- **`requireSessionOwner` helper**: Extracted shared ownership check to `access.go` (DRY — pattern was repeated 9x in sessions.go)

### Added
- **BytePlus ModelArk provider**: Seedream image generation + Seedance video generation via BytePlus/Volcengine API
- **Per-agent CLI grants**: Secure CLI binaries can now be granted/denied per agent with setting overrides
- **Beta release pipeline**: `release-beta.yaml` — push `v*-beta*` tag from dev to create prerelease with Docker images + binaries

### Fixed
- **Scheduler test hang**: Defer ordering fix prevents CI timeout when test fails before unblocking goroutines
- **Semantic-release branch**: `--no-ci` flag bypasses default branch check (repo default is dev, releases cut from main)
- **OpenAI compat**: Together/Mistral reasoning, streaming, and vision gating; Mistral tool call ID normalization

### Changed
- **Docker builds**: Removed redundant `docker-publish.yaml` — `release.yaml` handles all Docker builds on release
- **Desktop prerelease**: `release-desktop.yaml` auto-detects beta/rc tags and marks as prerelease

### Refactored
- **Web UI**: React-arch audit — RHF+Zod forms, Zustand persist, adapter layer, component modularization
- **Desktop UI**: React-arch audit — schemas, RHF forms, file splits, services, store cleanup

---

## [Unreleased]

### Security

#### Tenant-Scope Hotfix (2026-04-12)

3 privilege-escalation vulnerabilities closed, same class as `b419f352` (Phase 1 `config.*` hotfix):

- **CRITICAL** `PUT /v1/tools/builtin/{name}` — non-master admin could corrupt global tool defaults
- **CRITICAL** `POST /v1/packages/install|uninstall` — non-master admin could run `pip`/`npm`/`apk` server-wide
- **HIGH** `POST /v1/api-keys/{id}/revoke` (HTTP + WS) — tenant admin could revoke NULL-tenant system keys

Fix adds shared `store.IsMasterScope(ctx)` predicate + `http.requireMasterScope` guard on all three endpoints. `APIKeyStore.Delete` dropped (YAGNI + dormant same-class vuln). WS router now injects role into ctx. Tests: 17 new unit tests. Audit: `plans/reports/debugger-260412-0922-tenant-scope-audit.md`.

### Added

#### Per-Tenant Tool Configuration — 4-Tier Overlay (2026-04-12)

Tenant admins can override tool configuration without affecting other tenants. Overlay: `per-agent > tenant > global > hardcoded`, resolved at Execute time via `tools.BuiltinToolSettingsFromCtx(ctx)` — no Tool interface changes. See `docs/03-tools-system.md` § 14. Applies to `web_search`, `web_fetch`, `tts`. Web UI dialog is tenant-scope aware.
- **Phase 5** (1e5e84d5): Builtin tools settings editor on web UI
- **Phase 7 rest** (30a40bbe): Exa + Tavily web search providers with ranked ordering via `provider_order` config. Credit: @kaitranntt for original PR 825 work, ported to tenant settings storage pattern
  - New files: `internal/tools/{web_search_exa,web_search_tavily,web_search_config}.go`
  - 11 new unit tests for provider chain + normalization
  - Provider-selection helper: `NormalizeWebSearchProviderOrder(order []string) → []string` (DuckDuckGo always last as free fallback)
- **Phase 8** (def1712f, 43ee918b): Tenant-aware singleton pool pattern for stateful tools
  - `web_fetch`: Domain policy override via `resolvePolicy(ctx)` reads tenant config; 6 new unit tests
  - `tts`: Primary provider override via `resolvePrimary(ctx, mgr)` reads tenant config; 5 new unit tests
  - Feature flag: `config.Tools.TenantScopedSingletons` (default: false) gates per-tenant pool instances with LRU eviction (64 tenants) + 30 min idle timeout

### Fixed

#### Feishu/Lark Writer Management Commands — Issue #818 Closed (2026-04-11)
- **Issue #818 resolution**: Closes UX gap where users saw `/addwriter` error messages but Feishu had no handler
- **Phase 1 — Thread reply routing**: Inbound messages with `thread_id` now properly route responses back to the same Feishu thread via `/open-apis/im/v1/messages/{id}/reply`. New `feishu_reply_target_id` metadata key included in `routingMetaKeys` allowlist. Graceful fallback to `SendMessage()` if thread root deleted
- **Phase 2 — Document auto-fetch**: Pasted Lark docx URLs auto-detected and fetched via `/open-apis/docx/v1/documents/{id}/raw_content`. Content injected as `[Lark Doc: URL]` markers. LRU cache (128 entries, 5-min TTL) + 8000-rune truncation per document. Requires `docx:document:readonly` permission + owner grant
- **Phase 3 — Writer management commands**: Added `/addwriter <@user or reply>`, `/removewriter`, `/writers` for group file-write permission control. Group-only (DMs rejected early). Requires existing writer authorization. Last-writer guard prevents removing final writer. Empty-writer groups allow bootstrap via explicit `/addwriter @self`. 10s timeout bounds Feishu API calls

### Added

### Testing

#### Test Speed-Up + Coverage Ratchet Removal (2026-04-11)
- **Philosophy shift**: Signal over coverage %. Reject mock-heavy/slow/low-signal tests even if % drops. Coverage ratchet gate removed — was creating pressure to write forced tests instead of fast, valuable ones
- **`internal/vault` retry tests**: 16.3s → 0.6s. New `fastBackoffsForTest(t)` helper overrides package-level `enrichRetryBackoffs`/`enrichRetryTimeouts` so retry tests don't wait through real exponential backoff (was 6s per all-retry test). Production behavior unchanged
- **`internal/cron` scheduler tests**: 11.7s → 1.5s. `runLoopTickInterval` extracted as package var (default 1s, unchanged); test-only `setFastTick(t)` helper overrides to 20ms so 6 scheduler tests don't sleep 1.5s each waiting for ticks
- **`internal/channels/facebook` retry tests**: 6.3s → 3.0s. `graphBackoffBase` extracted as package var (default 1s, unchanged); `newFakeGraph` helper overrides to 1ms so HTTP retry tests don't burn 3+2+1s of real waits
- **Vault duplicates removed**: `TestCallClassifyWithRetry_FirstAttemptSuccess` (dup of `_Success`) and `_MaxRetriesConstant` (dup of `_RetriesAndBackoffs`)
- **Total saved**: ~29s wall-clock (3 packages: 34.3s → 5.1s). Full `go test -race ./...` now runs in ~57s (was ≥90s with hangs)
- **Removed**: `scripts/check_coverage.go` + `scripts/coverage_thresholds.json` + "Coverage ratchet gate" CI step. Coverage profile + `go tool cover -func` summary preserved as informational only

#### Test Coverage Improvement — Wave 1-3 (2026-04-11)
- **CI ratchet gate**: `scripts/check_coverage.go` parses `coverage.out` per package and fails CI if coverage drops below stored floors in `scripts/coverage_thresholds.json`. `--update` flag ratchets thresholds upward when coverage improves. 61 packages locked.
- **`-coverpkg=./...`**: CI now runs `go test -race -coverpkg=./...` so integration tests in `tests/integration/` are attributed to the source packages under test.
- **`internal/testutil`**: shared helpers — `TestDB()` (integration-tagged), context builders (`TenantCtx`, `UserCtx`, `AgentCtx`, `FullCtx`), mockgen generate hooks for `SessionStore`/`AgentStore`/`ContactStore`.
- **~663 new test functions across 36 files**:
  - Wave 1 — `store/pg` integration test depth (session pagination/isolation, agent context files/profiles, agent_links CRUD, cron CRUD+state, vault CRUD/search, memory BM25/isolation); `gateway` unit tests (ratelimit, event_filter, server auth); `gateway/methods` handlers (sessions, skills, cron); `http` auth helpers + path security; `tasks.TaskTicker` (lifecycle, recoverAll, followupInterval); `agent` (pruning, extractive memory, intent classify, loop utils, inject, evolution guardrails).
  - Wave 2 — `config` (normalize, expand/contract home, env overlays, system configs); `skills` (BM25 tokenize/index/search/rebuild, frontmatter parser, loader/context); `mcp` (pool, manager status, bridge BM25, env resolution); `backup` (ArchiveDirectory, SanitizeDSN, WritePgpass, Backup.Run); `channels/slack` (mention, user cache, classifyMime); `channels/discord` (resolveDisplayName, command routing, classifyMediaType); `channels/telegram` (markdown→HTML, table rendering, detectMention, service message); `channels/whatsapp` (extractTextContent, chunkText, markdown→WhatsApp, mimeToExt, classifyDownloadError).
  - Wave 3 — `cache.PermissionCache` (9 methods + invalidation); `sessions` key builders + manager edge cases; `knowledgegraph` extractor (mock provider success/filter/error/invalid-JSON/long-text chunking), splitChunks, mergeResults.
- **Coverage deltas** (local `go test`, no DB):
  - `internal/knowledgegraph` 47.1% → 91.8% (+44.7pp)
  - `internal/skills` 7.7% → 37.5% (+29.8pp)
  - `internal/config` 19.3% → 48.2% (+28.9pp)
  - `internal/cache` 72.9% → 96.9% (+24.0pp)
  - `internal/sessions` 70.7% → 94.4% (+23.7pp)
  - `internal/gateway` 0% → 15.1%
  - `internal/mcp` 12.1% → 26.3% (+14.2pp)
  - `internal/channels/whatsapp` 8.8% → 21.3% (+12.5pp)
  - `internal/channels/discord` 15.6% → 27.7% (+12.1pp)
  - `internal/tasks` 0% → 55.4%
  - `internal/agent` 28.8% → 36.8%
  - store/pg integration test depth improved — coverage attribution requires live pgvector in CI
- **Deferred to separate plans**: `channels/feishu` (0%, 102 funcs), `providers/acp` (0%, 41 funcs), `channels/zalo` (regressed to 5%), `providers` (56%, 325 funcs), `channels/facebook` (31.8%)

#### Deferred Coverage Waves A-C — Resolved (2026-04-11)
Follow-up to Wave 1-3 above. Addresses the 6 modules deferred as too-large/greenfield/regression. Plan: `plans/260411-2020-deferred-coverage-waves/`.
- **Wave A** — `internal/providers` 57.0 → 62.5% (hotspot tests for adapter/retry/SSE); `channels/zalo` 7.2 → 65.3% (regression fix + parse/policy/HTTP coverage)
- **Wave B** — `channels/facebook` 23.1 → 81.9% (full bot lifecycle, media, policy); `store/pg` 1.3 → 3.5% (⚠️ capped at unit-test-only; 30% target requires CI integration wiring + pre-existing failing tenant-isolation tests — deferred separately)
- **Wave C** — `providers/acp` 0.0 → **80.0%** greenfield (7 test files, 2560 LOC; JSON-RPC framing with adversarial input fuzz, terminal sandbox + allowlist + deny-pattern enforcement, ProcessPool lifecycle, tool_bridge with 3 permission modes); `channels/feishu` 20.6 → **63.9%** (15 test files; AES-CBC webhook decrypt + tamper detection, WS proto framing, larkclient HTTP error paths, media send/receive, bot parse/policy, lifecycle)
- **Security tests added**: ACP JSON-RPC parser no-panic on 7 adversarial inputs; sandbox path-traversal + binary allowlist + deny-pattern (`rm -rf` even under `bash`); env sanitization strips 8 prefixes + 13 exact-name vars (GOCLAW/ANTHROPIC/OPENAI/DATABASE/AWS/GITHUB/SSH/STRIPE/DB_DSN/PG*/NPM_TOKEN/SECRET_KEY/JWT_SECRET). Feishu AES-CBC tamper detection + token mismatch drop; no real credentials in any fixture
- **Scale**: ~375 new test functions across 22 files (~5858 LOC); zero source modifications — pure additive coverage
- **Ratchet bumped**: `scripts/coverage_thresholds.json` — feishu 0 → 63.89, acp 0 → 80.05

### Added

#### Episodic Memory Weighted Scoring — Dreaming Enhancement (2026-04-10, Phase 10)
- **Recall signal tracking**: `episodic_summaries` table gains 3 columns: `recall_count INT`, `recall_score DOUBLE PRECISION`, `last_recalled_at TIMESTAMPTZ` to track usefulness of each memory
- **ComputeRecallScore formula**: 4-component running average (30% frequency + 35% relevance + 20% recency + 15% freshness, 14-day half-life) quantifies memory value
- **DreamingWorker prioritization**: `ListUnpromotedScored()` queries sort by `recall_score DESC` instead of `created_at ASC`, promoting high-signal summaries for synthesis
- **fire-and-forget updates**: `memory_search` tool fire-and-forget tasks increment recall counts asynchronously without blocking search results
- **Index optimization**: New partial index `idx_episodic_recall_unpromoted ON episodic_summaries(agent_id, user_id, recall_score DESC) WHERE promoted_at IS NULL` for efficient DreamingWorker queries
- **Migration 000045**: PG schema v44→45 + SQLite schema v12→13

#### Compaction Telemetry — Message Context Tracking (2026-04-10, Phase 5 Follow-up)
- **Session metadata tracking**: `sessions.metadata` JSONB gains well-known key `last_compaction_at` (RFC3339 timestamp) after successful message compaction
- **Dual execution paths**: Both v3 `PruneStage.CompactMessages` and v2 legacy `maybeSummarize` update timestamp on successful compaction
- **Operator visibility**: `GetSessionMetadata()` exposes compaction timestamp; web UI shows in context-usage tooltip
- **Go constant export**: `agent.SessionMetaKeyLastCompactionAt = "last_compaction_at"`

#### Provider Reasoning Content Stripping (2026-04-10, Phase 6)
- **Auto-strip known leakers**: Models known to leak chain-of-thought at effort="off" (Kimi family, DeepSeek-Reasoner) auto-enable `StripThinking` so user-visible `ChatResponse.Thinking` stays empty
- **Multi-provider support**: Guard clauses in Anthropic streaming, Anthropic non-streaming `Chat()`, OpenAI `ChatStream`/`Chat`, Codex `processSSEEvent`; DashScope inherits via OpenAIProvider embedding
- **Billing-safe invariants**: `Usage.ThinkingTokens` still counted from raw bytes; `RawAssistantContent` untouched so Anthropic tool-use passback continues to work
- **Option plumbing**: New `providers.OptStripThinking` key propagated via `ChatRequest.Options`; `ReasoningDecision.StripThinking` auto-set in `ResolveReasoningDecision` defer
- **Helper**: `modelLeaksReasoning(model string) bool` — extensible allowlist

#### Dreaming Config Per-Agent (2026-04-10, Phase 8)
- **Per-agent overrides**: `MemoryConfig.Dreaming` JSONB on `agents.memory_config` (nested, no migration) controls dreaming worker behaviour per-agent
- **Fields**: `Enabled *bool`, `DebounceMs int`, `Threshold int`, `VerboseLog *bool` — all pointer/zero-default for partial-override merge semantics
- **Resolver pattern**: `DreamingConfigResolver func(ctx, agentID) *DreamingConfig` wired via `newAgentStoreResolver(AgentStore)`. `ConsolidationDeps` gains optional `AgentStore store.AgentCRUDStore`
- **Backward compat**: Nil resolver → struct defaults; empty JSONB → defaults via zero-value short-circuit
- **Merge helper**: `mergeDreamingConfig` applies override fields only when explicitly set, preserving base values

#### Per-Provider Context Window & Hardening (2026-04-10, Phase 4, commit 8d37dc45)
- **EffectiveContextWindow**: Resolved once per run in ContextStage from `ModelRegistry` (via provider+model lookup); `PruneStage` uses it with fallback to `Config.ContextWindow`
- **ReserveTokens safety buffer**: New `PipelineConfig.ReserveTokens` subtracted from history budget so PruneStage compacts slightly before the hard limit
- **InMemoryCache hardening**: TTL sweep (60s) + max-size cap (10k) with oldest-first eviction; `Close()` wired to gateway shutdown
- **ContactCollector tenant fix**: Cache key now includes `tenantID + channelInstance` (was silently skipping upserts for same sender across tenants)

#### Context-Aware Auto-Inject Query (2026-04-10, Phase 9, commit 2731f99a)
- **RecentContext enrichment**: `InjectParams.RecentContext` field supplies last 1-2 user turns; `pgAutoInjector.Inject` prepends "Context:" frame before "Query:" focus
- **Helper**: `pipeline.buildRecentContext()` walks history backward, picks last 2 user turns, caps at 300 chars
- **Callback signature**: `AutoInject func(ctx, msg, userID, recentContext string)` — additive, backward compatible
- **Why**: Vector search on "what's my favorite?" alone returns nothing useful; context-aware query captures conversational intent

#### Cost Calculation Thinking Tokens Fix (2026-04-10, Phase 1, commit 77a80680)
- **Critical billing bug**: `CalculateCost` now properly handles `ThinkingTokens` as sub-count of `CompletionTokens` (not double-counted for OpenAI o3/o4-mini, Codex/GPT-5; properly split for Anthropic extended thinking)
- **Provider-aware**: Splits only when `ReasoningPerMillion > 0`, otherwise leaves as-is (default matches provider billing)

#### Web UI Enhancements (2026-04-10, Phase 11)
- **Context usage badge**: Chat top bar shows `{used}/{max} ({percent}%)` with color ramp (amber ≥75%, destructive ≥90%); hidden on mobile via `hidden sm:flex`
- **Compaction indicator**: Context badge tooltip includes compaction count + last compaction timestamp (read from `session.metadata.last_compaction_at`)
- **DreamingConfig UI**: Agent detail MemorySection renders nested dreaming block (4 controls: enabled, threshold, debounce_ms, verbose_log)
- **i18n**: New keys in en/vi/zh: `agents.configSections.dreaming.*`, `chat.contextUsage.*`
- **Not shipped (YAGNI)**: "Memory recall config section" (existing MemorySection already covers all MemoryConfig fields), "Session types extension" (fields already present or out of scope)

### Refactored

#### V3 Architecture Refactor — Phase 6 Completion (2026-04-08)
- **Store unification**: Created `internal/store/base/` with shared Dialect interface, common helpers (NilStr, BuildMapUpdate, BuildScopeClause, execMapUpdate, etc.). PostgreSQL (`pg/`) and SQLite (`sqlitestore/`) now use base/ abstractions via type aliases, eliminating code duplication
- **Orchestration module**: New `internal/orchestration/` with orchestration primitives: BatchQueue[T] generic for result aggregation, ChildResult structure for capturing child agent outputs, media conversion helpers
- **Forced V3 pipeline**: Deleted legacy v2 `runLoop()` (~745 LOC). Removed `v3PipelineEnabled` conditional flag — all agents now always execute the unified 8-stage pipeline (context→history→prompt→think→act→observe→memory→summarize)
- **Gateway decomposition**: Split monolithic gateway.go (1295 LOC → 476 LOC) into focused modules: gateway_deps.go, gateway_http_wiring.go, gateway_events.go, gateway_lifecycle.go, gateway_tools_wiring.go for better maintainability
- **SSE extraction**: Created shared SSEScanner in `providers/sse_reader.go` — unified streaming implementation used by OpenAI, Codex, and Anthropic streaming providers, eliminating provider-level duplication
- **UI cleanup**: Removed v2/v3 toggle from web UI settings since v3 is now the only execution path
- **Build compatibility**: All builds (PostgreSQL standard + SQLite desktop) compile cleanly. Dual-DB store pattern enables seamless database backend switching

### Added

#### Knowledge Vault UI/Backend Enhancements (2026-04-09)
- **Doc type inference**: `vault_link` tool now infers document type from file path instead of hardcoding "note"
- **Link type parameter**: `vault_link` accepts optional `link_type` param (wikilink or reference, default wikilink)
- **Pagination support**: `/v1/vault/documents` and `/v1/agents/{id}/vault/documents` return `{documents: [...], total: N}` for pagination
- **CountDocuments store method**: Added to VaultStore interface with PostgreSQL and SQLite implementations
- **Frontend pagination UI**: Vault documents table shows 100 items per page with Previous/Next navigation, "Showing X-Y of Z" indicator
- **Team filter dropdown**: Vault page has team selector alongside agent selector for multi-team document filtering
- **Graph view upgrade**: Independent graph data fetching (limit 500) with KG-level features:
  - Node click highlight + neighbor emphasis + dim non-neighbors
  - Double-click opens document detail dialog
  - Zoom controls (ZoomIn/ZoomOut buttons + percentage display)
  - Node limit selector (100/200/300/500 by degree centrality)
  - Link labels on highlighted links + directional particles
  - Stats bar showing doc/link counts
  - Fit-to-view button to auto-center graph
  - Background click clears selection
  - Works in all-agents mode (shows nodes without agent-specific links)
- **VaultDocument type updates**: Added team_id, summary, custom_scope, media type fields for richer metadata
- **Files modified**:
  - `internal/tools/vault_link.go` — doc type inference + link_type param
  - `internal/http/vault_handlers.go` — pagination response wrapper
  - `internal/store/vault_store.go`, `pg/vault_documents.go`, `sqlitestore/vault_documents.go` — CountDocuments
  - `ui/web/src/pages/vault/*` — pagination, team filter, graph upgrade
  - `ui/web/src/adapters/vault-graph-adapter.ts` — degree centrality limiting
  - `ui/web/src/i18n/locales/{en,vi,zh}/*` — pagination + vault strings

#### Vault Enrich Worker — Auto Summary + Semantic Linking (2026-04-09)
- **Async document enrichment**: EventBus-driven worker auto-summarizes new/updated vault documents via LLM
- **Vector embeddings**: Document summaries automatically embedded and indexed for semantic search
- **Auto-linking**: Vector similarity search (0.7 threshold, top-5 neighbors) auto-creates bidirectional vault links
- **Efficient batching**: BatchQueue[T] batches documents by tenantID:agentID, bounded dedup map (10K cap) prevents memory leaks
- **Provider independence**: Separate provider resolution from consolidation pipeline, reuses master tenant provider
- **Dual-DB support**: PostgreSQL includes full embed+link workflow; SQLite (desktop) summarizes only (no vector ops)
- **Files added**:
  - `internal/vault/enrich_worker.go` — BatchQueue-driven worker with bounded dedup
  - `internal/eventbus/event_types.go` — EventVaultDocUpserted event type
  - Updated `internal/store/vault_store.go` with UpdateSummaryAndReembed, FindSimilarDocs methods
  - Updated PostgreSQL and SQLite vault document stores


#### WhatsApp Native Protocol Integration (2026-04-06)
- **Direct protocol migration**: Replaced Node.js Baileys bridge with direct in-process WhatsApp connectivity
- **Database auth persistence**: Auth state, device keys, and client metadata stored in PostgreSQL (standard) or SQLite (desktop)
- **QR authentication**: Interactive QR code authentication for device linking without external bridge relay
- **No more bridge_url**: Removed `bridge_url` configuration, eliminated `docker-compose.whatsapp.yml`, removed `bridge/whatsapp/` sidecar service
- **Enhanced media handling**: Direct media download/upload to WhatsApp servers with automatic type detection and streaming
- **Improved mention detection**: Group mention detection now uses LID (Local ID) + JID (standard format) for robust message routing
- **Files added**:
  - `internal/channels/whatsapp/factory.go` — Dialect detection and channel factory
  - `internal/channels/whatsapp/qr_methods.go` — QR code generation and authentication flow
  - `internal/channels/whatsapp/format.go` — HTML-to-WhatsApp message formatting
  - Database-backed auth persistence for cross-platform support

### Refactored

#### Parallel Sub-Agent Enhancement (#600) (2026-03-31)
- **Smart leader delegation**: Conditional leader delegation prompt instead of forced delegation for all subagent spawns
- **Compaction prompt persistence**: Preserves pending subagent and team task state across context summarization to maintain work continuity
- **DB persistence**: `subagent_tasks` table (migration 000034) with `SubagentTaskStore` interface and PostgreSQL implementation. Write-through persistence from SubagentManager ensures durable task tracking
- **Token cost tracking**: Per-subagent input/output token accumulation. Token costs included in announce messages and persisted in DB for billing/observability
- **Per-edition rate limiting**: `MaxSubagentConcurrent` and `MaxSubagentDepth` limits on Edition struct. Tenant-scoped concurrency prevents single tenant from hogging subagent resources
- **WaitAll orchestration**: `spawn(action=wait, timeout=N)` blocks parent until all spawned children complete. Enables coordinated multi-step workflows
- **Auto-retry with backoff**: Configurable `MaxRetries` (default 2) with linear backoff for LLM failures. Improves reliability without manual intervention
- **Producer-consumer announce queue**: Merges staggered subagent results into single LLM run announcement. Reduces token overhead vs per-result notifications
- **Telegram subagent commands**: `/subagents` lists all active subagent tasks with status. `/subagent <id>` shows detailed view from DB
- **Subagent blocking in subagents**: `SubagentDenyAlways` blocks `team_tasks` tool to prevent nested task delegation
- **Functional options pattern**: Telegram provider refactored to `telegram.New()` with `WithXxxStore()` option setters for cleaner initialization
- **File organization**: Subagent code split into focused modules: `subagent.go`, `subagent_roster.go`, `subagent_spawn.go`. Spawn tool split: `spawn_tool.go` + `spawn_tool_actions.go`

#### Runtime & Packages Management (2026-03-17)
- **Packages page**: New "Packages" page in Web UI under System group for managing installed packages
- **HTTP API endpoints**: GET/POST `/v1/packages`, `/v1/packages/install`, `/v1/packages/uninstall`, GET `/v1/packages/runtimes`
- **Three package categories**: System (apk), Python (pip), Node (npm) with version tracking
- **pkg-helper binary**: Root-privileged helper service for secure system package management via Unix socket `/tmp/pkg.sock`
- **Package persistence**: System packages persisted to `/app/data/.runtime/apk-packages` for container recreation
- **Input validation**: Regex + MaxBytesReader (4096 bytes) for package names to prevent injection

#### Docker Security Hardening (2026-03-17)
- **Privilege separation**: Entrypoint drops privileges to non-root goclaw user after installing packages
- **pkg-helper service**: Started as root, listens on Unix socket with 0660 permissions (root:goclaw group)
- **Runtime directories**: Python and Node.js packages install to writable `/app/data/.runtime` directories
- **su-exec integration**: Used instead of USER directive for cleaner privilege transition
- **Docker capabilities**: Added SETUID/SETGID/CHOWN/DAC_OVERRIDE for pkg-helper and user switching
- **Environment variables**: PIP_TARGET, NPM_CONFIG_PREFIX, PYTHONPATH configured for runtime installs

#### Auth Fix (2026-03-17)
- **Empty gateway token handling**: When GOCLAW_GATEWAY_TOKEN is empty (dev/single-user mode), all requests get admin role
- **CLI credentials access**: Admin-only endpoints (/v1/cli-credentials) now accessible in dev mode

#### Team Workspace Improvements (2026-03-16)
- **Team workspace resolution**: Lead agents resolve per-team workspace directories for both lead and member agents
- **WorkspaceInterceptor**: Transparently rewrites file tool requests to team workspace context
- **File tool access**: Member agents can access workspace files with automatic path resolution
- **Team workspace UI**: Workspace scope setting UI, file view/download, storage depth control
- **Lazy folder loading**: Improved performance with lazy-load folder UI and SSE size endpoint
- **Task enhancements**: Task snapshots in board view, task delete action, improved task dispatch concurrency
- **Board toolbar**: Moved workspace button and added agent emoji display
- **Status filter**: Default status filter changed to all with page size reduced to 30

#### Agent & Workspace Enhancements (2026-03-16)
- **Agent emoji**: Display emoji icon from `other_config` in agent list and detail views
- **Lead orchestration**: Improved leader orchestration prompt with better team context
- **Task blocking validation**: Validate blocked_by terminal state to prevent circular dependencies
- **Prevent premature task creation**: Team V2 leads cannot manually create tasks before spawn

#### Team System V2 & Task Workflow (2026-03-13 - 2026-03-15)
- **Kanban board layout**: Redesigned team detail page with visual task board
- **Card/list toggle**: Teams list with card/list view toggle
- **Member enrichment**: Team member info enriched with agent metadata
- **Task approval workflow**: Approve/reject/cancel tasks with new statuses and filtering
- **Workspace scope**: Per-agent DM/group/user controls with workspace sharing configuration
- **i18n for channels**: Channel config fields now support internationalization
- **Memory/KG sharing**: Decoupled memory and KG sharing from workspace folder sharing
- **Events API**: New /v1/teams/{id}/events endpoint for task lifecycle events

#### Security & Pairing Hardening (2026-03-16)
- **Browser approval fix**: Fixed browser approval stuck condition
- **Pairing auth hardening**: Fail-closed auth, rate limiting, TTL enforcement for pairing codes
- **DB error handling**: Handle transient DB errors in IsPaired check
- **Transient recovery**: Prevent spurious pair requests

#### Internationalization (i18n) Expansion (2026-03-15)
- **Complete web UI localization**: Full internationalization for en/vi/zh across all UI components
- **Config centralization**: Centralized hardcoded ~/.goclaw paths via config resolution
- **Channel DM streaming**: Enable DM streaming by default with i18n field support

#### Provider Enhancements (2026-03-14 - 2026-03-16)
- **Qwen 3.5 support**: Added Qwen 3.5 series support with per-model thinking capability
- **Anthropic prompt caching**: Corrected Anthropic prompt caching implementation
- **Anthropic model aliases**: Model alias resolution for Anthropic API
- **Datetime tool**: Added datetime tool for provider context
- **DashScope per-model thinking**: Simplified per-model thinking guard logic
- **OpenAI GPT-5/o-series**: Use max_completion_tokens and skip temperature for GPT-5/o-series models

#### ACP Provider (2026-03-14)
- **External coding agents**: ACP provider for orchestrating external agents (Claude Code, Codex CLI, Gemini CLI) as JSON-RPC subprocesses
- **ProcessPool management**: Subprocess lifecycle with idle TTL reaping and crash recovery
- **ToolBridge**: Agent→client requests for filesystem operations and terminal spawning
- **Workspace sandboxing**: Security features with deny pattern matching and permission modes
- **Streaming support**: Both streaming and non-streaming modes with context cancellation

#### Storage & Media Enhancements (2026-03-14)
- **Lazy folder loading**: Lazy-load folder UI for improved performance
- **SSE size endpoint**: Server-sent events endpoint for dynamic size calculation
- **Enhanced file viewer**: Improved file viewing capabilities with media preservation
- **Web fetch enhancement**: Increased limit to 60K with temp file save for oversized content
- **Discord media enrichment**: Persist media IDs for Discord image attachments

#### Knowledge Graph Improvements (2026-03-14)
- **LLM JSON sanitization**: Sanitize LLM JSON output before parsing to handle edge cases

#### CI/CD & Release Pipeline (2026-03-16)
- **Semantic release**: Automated versioning via `go-semantic-release` on push to `main`
- **Cross-platform binaries**: Build and attach `linux/darwin × amd64/arm64` tarballs to GitHub Releases
- **Discord webhook notification**: Post release embed to Discord with changelog, version, Docker pull command, and install script link after successful build
- **Install scripts**: One-liner binary installer (`scripts/install.sh`) and interactive Docker setup (`scripts/setup-docker.sh`) with variant selection (alpine/node/python/full)
- **Docker image publishing**: Publish multi-arch images to GHCR and Docker Hub via GitHub Actions

#### Traces & Observability (2026-03-16)
- **Trace UI improvements**: Added timestamps, copy button, syntax highlighting to trace/span views
- **Trace export**: Added gzip export with recursive sub-trace collection

#### Skills & System Tools (Previous releases)
- **System skills**: Toggle, dependency checking, per-item installation
- **Tool aliases**: Alias registry for Claude Code skill compatibility
- **Multi-skill upload**: Client-side validation for bulk skill uploads
- **Audio handling**: Fixed media tag enrichment and literal <media:audio> handling

#### Credential & Configuration (Previous releases)
- **Credential merge**: Handle DB errors to prevent silent data loss
- **OAuth provider routing**: Complete media provider type routing for Suno, DashScope, OAuth providers
- **API base resolution**: Respect API base when listing Anthropic models
- **Per-agent DB settings**: Honor per-agent restrictions, subagents, memory, sandbox, embedding provider settings

### Changed

- **Docker entrypoint**: Reimplemented for privilege separation with pkg-helper lifecycle management
- **Team workspace refactor**: Removed legacy `workspace_read`/`workspace_write` tools in favor of file tools for team workspace
- **Config hardcoding**: Centralized ~/goclaw paths via config resolution instead of hardcoded values
- **Workspace media files**: Preserve workspace media files during subtree lazy-loading

### Fixed

- **Teams status filter**: Default to all statuses instead of subset, reduced page size to 30
- **Select crash**: Filter empty chat_id scopes to prevent dropdown crash
- **File viewer**: Improved workspace file view/download and storage depth control
- **Pairing DB errors**: Handle transient errors gracefully
- **Provider thinking**: Corrected DashScope per-model thinking logic
- **Pancake Page loop guard**: Narrowed webhook ingress to `messaging` + `INBOX` events and normalized HTML-formatted echoes before short-TTL outbound echo suppression, reducing Facebook Page self-reply loops in Pancake inbox conversations

### Documentation

- Updated `05-channels-messaging.md` — Refreshed `WebhookChannel` / `BlockReplyChannel` implementation tables for Facebook, Pancake, Discord, and Zalo-family channels
- Updated `18-http-api.md` — Added section 17 for Runtime & Packages Management endpoints
- Updated `09-security.md` — Added Docker entrypoint documentation, pkg-helper architecture, privilege separation
- Updated `17-changelog.md` — New entries for packages management, Docker security, and auth fix
- Added `18-http-api.md` — Complete HTTP REST API reference (all endpoints, auth, error codes)
- Added `19-websocket-rpc.md` — Complete WebSocket RPC method catalog (64+ methods, permission matrix)
- Added `20-api-keys-auth.md` — API key authentication, RBAC scopes, security model, usage examples
- Updated `02-providers.md` — ACP provider documentation with architecture, configuration, security model
- Updated `00-architecture-overview.md` — Added ACP provider component and module references

---

## [ACP Provider Release]

### Added

#### ACP Provider (Agent Client Protocol)
- **New provider**: ACP provider enables orchestration of external coding agents (Claude Code, Codex CLI, Gemini CLI) as JSON-RPC 2.0 subprocesses over stdio
- **ProcessPool**: Manages subprocess lifecycle with idle TTL reaping and automatic crash recovery
- **ToolBridge**: Handles agent→client requests for filesystem operations and terminal spawning with workspace sandboxing
- **Security features**: Workspace isolation, deny pattern matching, configurable permission modes (approve-all, approve-reads, deny-all)
- **Streaming support**: Both streaming and non-streaming modes supported with context cancellation
- **Config integration**: New `ACPConfig` struct in configuration with binary, args, model, work_dir, idle_ttl, perm_mode
- **Database providers**: ACP providers can be registered in `llm_providers` table with encrypted credentials
- **Files added**:
  - `internal/providers/acp_provider.go` — ACPProvider implementation
  - `internal/providers/acp/types.go` — ACP protocol types
  - `internal/providers/acp/process.go` — Process pool management
  - `internal/providers/acp/jsonrpc.go` — JSON-RPC 2.0 marshaling
  - `internal/providers/acp/tool_bridge.go` — Request handling
  - `internal/providers/acp/terminal.go` — Terminal lifecycle
  - `internal/providers/acp/session.go` — Session tracking

### Changed

- Updated `02-providers.md` to document ACP provider architecture, configuration, session management, security, and streaming
- Updated `00-architecture-overview.md` component diagram to include ACP provider
- Updated Module Map in architecture overview to reference `internal/providers/acp/` package

### Documentation

- Added comprehensive ACP provider documentation with architecture diagrams, configuration examples, security model, and file reference
- Added `17-changelog.md` for tracking project changes

---

## [Previous Releases]

### v1.0.0 and Earlier

- Initial release of GoClaw Gateway with Anthropic and OpenAI-compatible providers
- WebSocket RPC v3 protocol and HTTP API
- PostgreSQL multi-tenant backend with pgvector embeddings
- Agent loop with think→act→observe cycle
- Tool system: filesystem, exec, web, memory, browser, MCP bridge, custom tools
- Channel adapters: Telegram, Discord, Feishu, Zalo, WhatsApp
- Extended thinking support for Anthropic and select OpenAI models
- Scheduler with lane-based concurrency control
- Cron scheduling system
- Agent teams with task delegation
- Skills system with hot-reload
- Tracing and observability with optional OpenTelemetry export
- Browser automation via Rod
- Code sandbox with Docker
- Text-to-speech (OpenAI, ElevenLabs, Edge, MiniMax)
- i18n support (English, Vietnamese, Chinese)
- RBAC permission system
- Device pairing with 8-character codes
- MCP server integration with stdio, SSE, streamable-HTTP transports
