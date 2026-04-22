# Changelog

All notable changes to GoClaw Gateway are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

### ACTOR vs SCOPE — #915 group permission fix + propagation (2026-04-16)

Resolves Issue #915 (Telegram group `write_file` permission denied after `/addwriter`) and closes an adjacent silent-privilege-bypass discovered during the audit.

#### Security (breaking behavior in group/guild context)

- **`store.CheckFileWriterPermission` / `CheckCronPermission`** no longer fail-open on empty or synthetic `SenderID` when the context scope is a group/guild. Previous fail-open allowed subagent/delegate/team/dashboard/cron system turns to write files in group chats without a writer grant — silent privilege bypass. Post-fix: empty or synthetic-prefix senders (`subagent:`, `notification:`, `teammate:`, `system:`, `ticker:`, `session_send_tool`) are DENIED in group context. DM / HTTP paths unchanged.
- **`bus.IsInternalSender`** now also recognises `subagent:` prefix (previously missed, causing subagent senders to be treated as real users in permission checks).

#### Added — propagation of the acting sender through re-ingress

- `tools.MetaOriginSenderID` metadata key (`origin_sender_id`) — carries the real acting sender through synthetic-sender announce/dispatch paths.
- `tools.AnnounceMetadata.OriginSenderID` field — subagent/delegate announce queue.
- `tools.SubagentTask.OriginSenderID` field — populated at spawn from `store.SenderIDFromContext(ctx)`.
- `cmd.subagentAnnounceRouting.SenderID` — carries sender from handler into the re-ingress `RunRequest`.
- `tools.DelegateRequest.SenderID` field — same propagation for delegate announcements.
- `store.ActorIDFromContext(ctx)` helper — returns `SenderID` if set, else `UserID`. Clarifies ACTOR vs SCOPE at call sites.

#### Changed — ACTOR migration (call sites now use `ActorIDFromContext`)

- `internal/tools/publish_skill.go`: skill owner = actor (individual, not group principal).
- `internal/tools/skill_manage.go` (create, patch, delete): owner = actor on create; patch/delete ownership check accepts actor or legacy `UserIDFromContext` for backward compatibility with skills created before this change.
- `internal/tools/delegate_tool.go`: `DelegateRequest.UserID` and `DomainEvent.UserID` = actor for audit trails.
- `internal/tools/team_tasks_blocker.go`: blocker attribution and escalation `UserID` = actor.
- `internal/tools/team_tool_cache.go`: team access-policy check uses actor (fixes group-chat member access where per-user allow/deny lists previously never matched the group principal).
- `internal/tools/team_tool_dispatch.go`: teammate-dispatch metadata now carries `MetaOriginSenderID` so the teammate's turn has the original user's sender.
- `internal/tools/sessions_send.go`: session_send metadata now carries `MetaOriginSenderID` to preserve user identity across agent-to-agent flows.

#### Scope-intentional (unchanged, commented)

- `internal/tools/cron.go`: cron jobs remain per-group-scope (memory-model parity; migrating would break collaborative `/cron list/add/remove`).
- `internal/tools/team_tasks_create.go`: team task `UserID` remains per-group (team visibility is per-chat, not per-user).

#### Tests

- New `tests/integration/telegram_group_write_file_permission_test.go` — 9 sub-tests covering granted-sender, ungranted-sender, no-agent fail-open branch, DM no-op, `|`-delimited sender, empty-sender deny, synthetic-prefix deny (7 sub-cases), propagated-sender allow, DM empty-sender passes.
- Regression coverage for both the user-reported denial (#915 BUG-B) and the silent-bypass (#915 BUG-A).

#### Notes

- No DB migration shipped: historical `skills.owner_id` rows that were created with a `group:*` / `guild:*` value remain accessible via the patch/delete legacy fallback. Re-publishing a skill transfers ownership to the individual actor.
- Audit report: `plans/reports/audit-260416-1240-actor-scope-comprehensive.md`.

---

## [Unreleased] — 2026-04-15

#### Agent Hooks System — Phase 3: Prompt Handler + Web UI (2026-04-15)

Third handler type (`prompt`) with full cost safeguards, WS RPC surface, complete Web UI, 3-language i18n, and production wiring. Closes Issue #875 feature parity with Claude-Code-style hooks.

### Added
- **`internal/hooks/handlers/prompt.go`**: LLM hook handler with structured tool-call output (`decide` tool), anti-injection system preamble, fenced user-input delimiter, in-memory decision cache (60s TTL keyed by `sha256(hook_id||version||tool_name||tool_input)`), per-turn invocation cap (default 5), provider resolver abstraction
- **`internal/hooks/handlers/prompt_resolver.go`**: `RegistryResolver` maps model aliases (haiku/sonnet/opus, gpt-*, gemini-*, qwen-*) to tenant providers; falls back to system configs (`hooks.prompt.provider`, `background.provider`) then first registered
- **`internal/hooks/budget/` package**: `Store` + `Dialect` interface for atomic per-tenant monthly token budget deduction; `ShouldWarn` helper at 20% threshold
- **Budget store implementations**: `pg.PGHookBudget` (single UPDATE + RETURNING with UPSERT seed on month rollover) and `sqlitestore.SqliteHookBudget` (tx-wrapped equivalent)
- **WS RPC methods** (`internal/gateway/methods/hooks.go`): `hooks.list` (viewer+), `hooks.create`/`update`/`delete`/`toggle` (admin+, master-scope for global), `hooks.test` (operator+, dryRun no audit write), `hooks.history` (viewer+, stub pending phase 4 pagination)
- **Protocol constants**: `MethodHooksList/Create/Update/Delete/Toggle/Test/History` in `pkg/protocol/methods.go`
- **Web UI `/hooks` page**: single route with `useParams().id` — list view when no id, detail view when present (CLAUDE.md "route params as source of truth"). Tabs: Overview | Config | Test | History
- **Web UI components**: `hook-list-row`, `hook-form-dialog` (3 sub-forms per handler type; command radio disabled on Standard with tooltip), `hook-test-panel` (fires `dryRun=true`, decision badge + duration + stdout/stderr/status), `hook-diff-viewer` (side-by-side JSON diff via highlight.js), `hook-history-table`, `hook-overview-tab`
- **Zod + TanStack Query**: `schemas/hooks.schema.ts` (prompt requires matcher|if_expr + prompt_template), `hooks/use-hooks.ts` (useHooksList/Hook/CreateHook/UpdateHook/DeleteHook/ToggleHook/TestHook/HookHistory)
- **i18n backend**: 6 new keys (`hook.invalid_matcher`, `hook.command_disabled_standard`, `hook.prompt_requires_matcher`, `hook.circuit_breaker_tripped`, `hook.budget_exceeded`, `hook.per_turn_cap_reached`) in en/vi/zh catalogs
- **i18n UI**: `ui/web/src/i18n/locales/{en,vi,zh}/hooks.json` with identical key ordering (per CLAUDE.md memory rule)
- **Production wiring**: `cmd/gateway_managed.go` registers `HandlerPrompt` in dispatcher handlers map; `cmd/gateway.go` registers `HookMethods` in router
- **Sidebar nav**: "Hooks" entry with `Webhook` icon in Capabilities group

### Security / cost safeguards
- **Structured output enforcement**: evaluator MUST call the `decide` tool; free-text responses fail-closed to block
- **Sanitized input**: only `tool_input` reaches the evaluator inside a fenced `USER INPUT` block; raw user message never included
- **Injection detection flag**: evaluator can signal `injection_detected=true` via structured output
- **Version-busted cache**: hook config edits bump `version` → cache key changes → forces re-evaluation
- **Atomic budget deduct**: no select-then-update race (L2 mitigation)

### Testing
- **Unit**: `prompt_test.go` (14 tests — cache hit/miss, version bust, per-turn cap, fail-closed paths, schema assertions), `prompt_injection_test.go` (4 tests — hostile inputs, unicode homoglyphs, nested JSON, fenced delimiter integrity), `budget_test.go` (9 tests — atomic deduct, exceed-returns-err, month rollover, zero-cost, nil tenant, warn threshold)
- **WS RPC unit**: `gateway/methods/hooks_test.go` (param parsing + HookTestResult struct round-trip)
- **UI**: 56/56 vitest passing including 20 new hook-specific tests (schema validation, list rendering, test panel, diff viewer, i18n contracts)

#### Agent Hooks System — Phase 4: Hardening + Docs (2026-04-15)

Integration / chaos / RBAC / tracing coverage + user documentation + copy-paste example hooks library. Merge gate.

### Added
- **Integration tests** (`tests/integration/hooks_e2e_test.go`): 7 events × HTTP/command handler coverage, tenant isolation (cross-tenant events don't hit other tenants' hooks), context-update injection path, edition gate at dispatch time
- **Chaos tests** (`hooks_chaos_test.go`): provider down (fail-closed block on blocking event), per-hook timeout respected, circuit breaker auto-disables after 3 consecutive blocks and persists `enabled=false`, `ErrLoopDepthExceeded` when depth > `MaxLoopDepth` (M5), `dedup_key` unique index suppresses double audit rows on retry (H6), 5xx retry-once-then-error
- **RBAC tests** (`hooks_rbac_test.go`): store tenant isolation (List/Get/Update/Delete all respect tenant_id), global-scope visible to all tenants, ResolveForEvent unions tenant + global hooks, `HasMinRole` matrix over the hooks.* method surface
- **Tracing tests** (`hooks_tracing_test.go`): `EmitHookSpan` writes row with canonical name `hook.<handler_type>.<event>`; dispatcher + traced-handler wrapper emits spans end-to-end
- **User docs** `docs/agent-hooks.md`: concepts, security model, handler reference, matcher guide, safeguard table, Web UI walkthrough, observability (audit table + tracing spans + slog keys), troubleshooting, migration notes, known limitations
- **Example library** `examples/hooks/`: `block-rm-rf.json` (command, Lite), `auto-lint-after-write.json` (http async), `audit-tool-usage.json` (tenant-wide audit), `session-context-injector.json` (context bootstrap), `notify-discord-on-stop.json` (webhook notification), `README.md` with safety guidance

### Fixed
- **`PGHookStore.GetByID` + `SqliteHookStore.GetByID`** now enforce tenant scope: non-master callers only see own + global rows. Previously returned any row by UUID which leaked cross-tenant.
- **Existing pipeline integration tests** now call `security.SetAllowLoopbackForTest(true)` via shared helper; no longer flake on SSRF block for httptest endpoints.
- **Vault**: Shared docs (`scope='shared'`, `agent_id=NULL`) are now visible to agents via `vault_search`, `ListDocuments`, `CountDocuments`. Previously excluded by strict `agent_id = $N` predicate after migration 000046 made `agent_id` nullable. Also fixes single-team filter to include shared docs. Adds CHECK invariant (migration 000055) to prevent future scope/ownership drift. Affects PostgreSQL and SQLite (desktop edition). (#917)

### Testing
- All hook integration tests pass under `TEST_DATABASE_URL` PG container (7 new test files, 18 new top-level tests)
- `go build` + `go build -tags sqliteonly` + `go vet` clean
- `pnpm test` 56/56 pass
- Race detector clean for `internal/hooks/...` + `internal/gateway/methods/`

### Deferred (post-MVP)
- Cluster-wide prompt decision cache via Redis
- Skill-frontmatter hooks
- `agent` handler type (inter-agent delegation via prompt instead)
- Desktop Wails UI parity for hooks page
- Load/throughput benchmarks (explicitly dropped from Phase 4 scope)

---

#### Agent Hooks System — Phase 1: Foundation (2026-04-15)

Lifecycle hook infrastructure: event dispatcher (sync/async paths), audit logging, database schema (PostgreSQL + SQLite), store interface, config validation, edition gating. Handlers + pipeline integration deferred to Phase 2.

### Added
- **`internal/hooks/` package**: Foundational types (HookConfig, HookExecution, Event), StdDispatcher (fire-and-decide with sequential execution), CEL+regex matcher, audit writer (PII-redacted, encrypted), circuit breaker with auto-disable
- **PostgreSQL migration 000052**: `agent_hooks` table (id, tenant_id, agent_id, event, handler_type, config, matcher, if_expr, priority, enabled, created_at, updated_at) + `hook_executions` table (hook_id, session_id, event, input_hash, decision, duration_ms, error, created_at) with unique dedup_key index
- **SQLite schema entries**: Full schema in `schema.sql` + migration map bump for Lite/desktop edition
- **HookStore interface + implementations**: `PGHookStore` (database/sql + pgx/v5) and `SQLiteHookStore` (modernc.org/sqlite) with List, Create, Update, Delete, LogExecution methods
- **Security primitives**: Loop-depth guard (M5 mitigation, MaxLoopDepth=3), per-hook timeout, chain budget (10s wall-time), circuit breaker with auto-disable (C4 mitigation), fail-closed policy for blocking events
- **Edition gating**: `HookEditionPolicy` allows command handler on Lite only; master-scope blocks command on Standard tenant
- **Config validation**: CEL cost limits, required matcher for prompt handlers, bounded queue per-turn limits

### Changed
- **Store layer**: Added `store.Hooks` field (typed as `any` to avoid import cycles)
- **Base store**: `TablesWithUpdatedAt["agent_hooks"] = true` for touch-all update helpers

### Testing
- **Integration test suite**: `tests/integration/v3_hooks_store_test.go` covers multi-tenant isolation, dedup key uniqueness, schema migration up/down for both PG + SQLite

### Deferred to Phase 3+
- Prompt handler (modifies input/context dynamically)
- Web UI (CRUD, test panel, history)
- Tracing integration
- i18n keys (en/vi/zh)

---

#### Agent Hooks System — Phase 2: Handlers + Pipeline Integration (2026-04-15)

Concrete handler implementations + pipeline wiring. Command handler (shell, Lite-only), HTTP handler (SSRF-hardened), synced at ContextStage/ToolStage/FinalizeStage, async at SessionStart/PostToolUse/Stop, delegate bridging (SubagentStart/SubagentStop).

### Added
- **`internal/hooks/handlers/` package**: `CommandHandler` (exec wrapper, JSON I/O, env allowlist, edition policy recheck), `HTTPHandler` (SSRF-hardened HTTP client, retry-once on 5xx, 4xx error no-retry, response parsing for decision/context/updatedInput)
- **Pipeline dispatcher integration**: `PipelineDeps.HookDispatcher` field (nil-safe noop fallback); stages call `deps.Hooks.Fire(ctx, evt)` at: ContextStage (SessionStart async + UserPromptSubmit sync), ToolStage (PreToolUse sync before exec + PostToolUse async), FinalizeStage (Stop async)
- **Copy-on-write staging**: UserPromptSubmit + PreToolUse updatedInput buffered per-call; commit only when ALL sync hooks succeed; rejection/timeout discards buffer (H2)
- **Delegate event bridge**: `delegate_bridge.go` subscribes DelegateCompleted/DelegateFailed events from domain bus, fires SubagentStop async; SubagentStart sync fired directly in delegate_tool pre-call
- **Command handler edition gating**: Re-checks `HookEditionPolicy` at dispatch (defense-in-depth); Lite-only with master-scope block on Standard tenant
- **HTTP handler SSRF hardening**: Uses caller-supplied net.Dialer that pins resolved IP, blocks loopback/link-local/private ranges; auth headers (e.g. Authorization) encrypted at rest in cfg.Config["headers"], decrypted only at send-time; no redirects allowed (CheckRedirect returns ErrUseLastResponse)
- **Server wiring**: `cmd/gateway_managed.go` constructs `hooks.NewStdDispatcher` with both handlers registered; passes dispatcher through `agent.ResolverDeps.HookDispatcher` and `delegateTool.SetHookDispatcher(...)`; falls back to `hooks.NewNoopDispatcher()` when `stores.Hooks` is nil
- **Encryption key handling**: Reads `GOCLAW_ENCRYPTION_KEY` env for HTTP handler header decryption; optional (empty → no decryption)

### Testing
- **Unit tests**: `internal/hooks/handlers/command_test.go` (8 tests — exit codes, timeout, env allowlist, edition gate, ctx cancel), `http_test.go` (9 tests — response parsing, retry logic, SSRF blocking pre+post DNS, no redirects, auth header security)
- **Integration tests**: `tests/integration/hooks_pipeline_test.go` (4 tests — SessionStart async, UserPromptSubmit block + context + updatedInput COW, PreToolUse block + args mutation, PostToolUse async, zero-overhead no-hooks path); `tests/integration/hooks_delegate_test.go` — SubagentStart/Stop with loop-depth guard M5

### Changed
- **Pipeline architecture**: 8-stage loop now fires hooks via `PipelineDeps.Dispatcher`; stage callbacks support nil-safe dispatcher (noop when not configured)
- **Delegate tool**: `delegateTool.SetHookDispatcher(...)` enables async SubagentStop via existing domain events

---

#### Desktop Voice Management + STT Admin (2026-04-15)

### Added
- **Desktop voice picker**: Agent detail panel now includes a voice selector with live preview. Users can browse ElevenLabs voices, hear a sample, and persist the choice per agent on the desktop app.
- **Desktop STT admin form**: Speech-to-text provider configuration (API keys, providers, WhatsApp opt-in toggle + privacy banner) available in desktop tool settings.
- **Singleton voice preview**: Only one voice sample plays at a time; preview button hides automatically when a voice has no sample URL.
- **Desktop i18n `tts` namespace**: Voice-management strings translated to English, Vietnamese, and Chinese; STT form labels added to the tools namespace.
- **Desktop test harness**: Vitest + Testing Library infrastructure; voice picker and STT form covered by unit tests.

### Changed
- **Agent voice persistence (desktop)**: Voice selection merges into `other_config` without overwriting sibling fields.
- **Tool settings routing (desktop)**: Speech-to-text tool opens the dedicated admin form instead of the generic JSON editor.

---

#### ElevenLabs Audio Manager Refactor — Phase 5 (2026-04-15)

Channel STT migration (Telegram/Feishu/Discord) + WhatsApp voice transcription with tenant opt-in (default disabled due to E2E encryption trade-off, resolves privacy red-team finding).

### Added
- **WhatsApp voice transcription**: `internal/channels/whatsapp/stt.go` — opt-in per-tenant via `builtin_tools[stt].settings.whatsapp_enabled` (default `false` per Decision 6)
- **Synchronous 10s timeout**: All 4 channels (Telegram/Feishu/Discord/WhatsApp) enforce ctx timeout for voice message processing; timeout/error → fallback label
- **i18n fallback key**: `MsgVoiceMessageFallback` ("voice message" placeholder in en/vi/zh) rendered when STT unavailable or disabled
- **Privacy banner**: Admin UI displays E2E encryption trade-off warning when enabling WhatsApp STT

### Changed
- **Channel STT handlers**: All 4 channels now call `audio.Manager.Transcribe()` with 10s timeout instead of per-channel `transcribeAudio()` helpers
- **Channel factory signatures**: All 4 factories (`NewChannel`) accept `audioMgr *audio.Manager` parameter
- **WhatsApp messaging**: Voice messages now transcribed (opt-in) and appended to message text for agent understanding

### Removed
- **Per-channel STT helpers**: Deleted `internal/channels/{telegram,feishu,discord}/stt.go` (consolidated into Manager)
- **Telegram STT tests**: Deleted `internal/channels/telegram/stt_test.go` — coverage preserved in `internal/audio/proxy_stt/provider_test.go` (12 ported scenarios, deliberate deletion documented)

### Resolved
- **Audit finding P5-B1**: Gate G4 verified; Manager.Transcribe chain wired
- **Audit finding P5-H1**: All transcribeAudio call sites migrated; grep confirms zero direct channel STT calls
- **Audit finding P5-H2** + **Decision 6** (privacy): WhatsApp STT opt-in with default=false; privacy banner + docs note E2E trade-off
- **Cross-phase XP-3**: Telegram test ported/deleted with 12-scenario coverage guarantee
- **Cross-phase XP-4**: `MsgVoiceMessageFallback` pre-wired in Phase 5 step 1 (blocking)

### Security / Privacy
- **WhatsApp E2E encryption**: Voice transcription breaks end-to-end guarantee. **Tenant opt-in required** with explicit admin acknowledgment (privacy banner in UI)
- **Temp file cleanup**: Existing `scheduleMediaCleanup` preserves file handles during 10s STT window; no race condition risk
- **Audit logging**: WhatsApp STT invocation logged at info level for audit trail

---

#### ElevenLabs Audio Manager Refactor — Phase 4 (2026-04-15)

STT via ElevenLabs Scribe + proxy fallback with tenant config migration + admin UI form. Legacy per-channel STT config bridged with 2-week deprecation grace.

### Added
- **ElevenLabs Scribe STT provider**: `internal/audio/elevenlabs/stt.go` — POST `/v1/speech-to-text`, multipart upload, 20MB cap, configurable language + diarization
- **Proxy STT wrapper**: `internal/audio/proxy_stt/provider.go` — backward-compat bridge for legacy `media.TranscribeAudio` with automatic temp-file handling
- **STT admin form**: `ui/web/src/pages/builtin-tools/stt-provider-form.tsx` — mirrors TTS form pattern, includes tenant config (API keys, providers list, WhatsApp opt-in toggle)
- **`audio.Manager.Transcribe()`**: Chain-based STT dispatch with channel-override precedence (per-channel STTProxyURL wins over tenant builtin_tools[stt])
- **i18n keys**: `MsgSTTAllProvidersFailed`, `MsgSTTLegacyConfigDeprecated`, `MsgSTTWhatsappPrivacyWarning` across en/vi/zh

### Changed
- **STT config location**: Migrated from per-channel fields (Telegram/Feishu/Discord `STTProxyURL`) to tenant `builtin_tools[stt].settings` (Decision 1 per audit)
- **`builtin_tools[stt]` table entry**: New seed row via migration 000050 (PG) + schema.go incremental (SQLite)
- **Legacy bridge**: Startup-time scan of per-channel STT configs → auto-registers `proxy_stt.Provider` with deprecation warn when tenant lacks `builtin_tools[stt]` (2-week grace period)

### Resolved
- **Audit finding P4-H1** (channel-override precedence): Decision 2 — per-channel config takes priority; tenant fallback when channel config absent
- **Audit finding P4-H2** (Scribe endpoint verification): ElevenLabs Scribe v2 `/v1/speech-to-text` confirmed with xi-api-key auth
- **Audit finding P4-B3** (explicit bridge loop): 3-channel explicit loop (Telegram, Feishu, Discord) replaces ambiguous iteration
- **Cross-phase XP-3** (12 test ported): All 12 Telegram STT scenarios (NoProxy, EmptyFile, Success, Error, etc.) ported into `proxy_stt/provider_test.go` before Phase 5 channel migration

### Deprecated
- Per-channel `STTProxyURL`, `STTAPIKey`, `STTTenantID` fields (Telegram, Feishu, Discord configs) — soft deprecation with 2-week grace; migration to `builtin_tools[stt]` required for Phase 5+ channels

### Fixed
- **JSON schema divergence**: `duration_secs` → `audio_duration_secs` in Scribe response parsing (matches ElevenLabs API)
- **Provider registration**: Scribe + SetSTTChain wired into setupAudioExtras for Phase 5 channel integration

---

#### ElevenLabs Audio Manager Refactor — Phase 3 (2026-04-14)

Music generation via ElevenLabs + MiniMax with fallback chain. Suno provider fully removed from codebase (no official API, ToS violation risk).

### Added
- **ElevenLabs Music provider**: `internal/audio/elevenlabs/music.go` — POST `/v1/music`, model `music_v1`, binary MP3 response, 300s timeout, prompt + optional lyrics
- **MiniMax Music provider**: `internal/audio/minimax/music.go` — POST `{base}/music_generation`, 2-step URL-download pattern, 200 MB cap, optional instrumentation toggle

### Changed
- **`create_audio` tool simplified**: Now thin Manager dispatcher (`NewCreateAudioTool(audioMgr *audio.Manager)`), removed inline provider logic + per-provider switch cases
- **`audio.Manager.GenerateMusic/GenerateSFX`**: Chain-based resolution (elevenlabs → minimax for music, elevenlabs-only for SFX)
- **`createAudio` builtin tool**: Unified dispatch via Manager instead of `providers.Registry` dependency injection

### Removed
- **Suno provider**: Fully excised (10 atomic locations) — research confirms no official API + ToS violations. Files deleted: `create_audio_suno.go`, `create_audio_minimax.go`, `create_audio_elevenlabs.go` shim. HTTP route removed. TS schema entry removed. Config provider removed.

### Types
- **`audio.MusicOptions`**: Added `Instrumental bool`, `Model string`, `TimeoutSec int` fields
- **`audio.AudioResult`**: Added `Provider string` field for observability (tool span metadata)

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

### Added

#### ElevenLabs Audio Manager Refactor — Phase 1 (2026-04-14)

Unified audio provider management via new `internal/audio/` package with pluggable interface-based architecture. Phase 1 wires TTS providers (ElevenLabs, OpenAI, Edge, MiniMax); STT/Music/SFX interfaces defined for Phase 3-4.

**What changed:**
- **`internal/audio/` package**: Central `Manager` orchestrates 4 provider kinds via interfaces (`TTSProvider`, `STTProvider`, `MusicProvider`, `SFXProvider`)
- **Provider organization**: Implementations in `internal/audio/{elevenlabs,openai,edge,minimax}/`. ElevenLabs shared HTTP client (`xi-api-key` header) for both TTS and SFX subproviders
- **`internal/tts/` → backward-compat alias**: 24-symbol package (15 types + 6 consts + 5 constructors + 5 signature guards). All pre-refactor callers compile unchanged, zero breaking changes
- **Config extension**: `config.Audio` optional pointer (nil-safe) added for future STT/Music subsections. `cfg.Tts` retained unchanged
- **ElevenLabs SFX tool**: `internal/tools/create_audio_elevenlabs.go` rewritten as thin shim calling `elevenlabs.NewSFXProvider(...).GenerateSFX(ctx, audio.SFXOptions{...})`
- **WS `tts.*` namespace**: 6 methods unchanged externally

**Impact**: Existing TTS flows fully compatible. New code can import `internal/audio` directly. STT/Music/SFX wiring deferred to Phase 3-4.

#### ElevenLabs Audio Manager Enhancements — Phase 2 (2026-04-14)

Voice discovery and agent-level audio config via new backend endpoints, in-memory cache, and web UI picker. Bundles producer/consumer context pattern (`store.WithAgentAudio`) for seamless voice/model resolution throughout the tool execution pipeline.

**What changed:**
- **Voice cache** (`internal/audio/voice_cache.go`): In-memory LRU (cap 1000 tenants) with TTL 1h, shared between HTTP + WS handlers, thread-safe under concurrent access
- **Streaming TTS interface** (`internal/audio/types.go`): New `StreamingTTSProvider` optional interface for ElevenLabs `/v1/text-to-speech/{voice_id}/stream` chunked playback
- **ElevenLabs enhancements**: Model allowlist (11_v3, eleven_flash_v2_5, eleven_multilingual_v2, eleven_turbo_v2_5), `SynthesizeStream()` method, `ListVoices()` via `/v1/voices`
- **Agent audio context** (`internal/store/context.go`): New `WithAgentAudio` / `AgentAudioFromCtx` bundle; producer wires snapshot at dispatcher level (internal/agent/), consumer (`TtsTool.Execute`) reads voice/model overrides from agent config
- **Agent config extension** (`agents.other_config` JSONB): New `tts_voice_id` and `tts_model_id` fields with resolution precedence: args → agent → tenant → provider default
- **HTTP + WS endpoints**: GET /v1/voices (cached), POST /v1/voices/refresh (admin-only), WS method `voices.list` + `voices.refresh`
- **Web voice picker** (`ui/web/src/components/voice-picker.tsx`): Combobox with search, preview button (HTML audio + onError → refresh), embedded in PromptSettingsSection
- **i18n**: 10 new frontend keys (voice_label, voice_placeholder, voice_refresh, voice_preview, model_label, etc.) + 2 new backend keys (MsgTtsUnknownModel, MsgVoicesListFailed) across en/vi/zh

**Impact**: Existing TTS callers fully compatible (backward-compat via Phase 1 alias layer). Web UI gains voice discovery + per-agent voice/model overrides. Zero breaking changes. Integration test validates producer+consumer context flow.

#### Trace Stop/Abort Redesign — Cascading 4-Layer Fix (2026-04-14)

The Stop button on the traces page now reliably aborts running traces. Previous implementation had independent race conditions across HTTP streaming, agent router, trace persistence, and UI polling; this redesign fixes all four layers atomically.

**What changed:**
- **HTTP streaming ctx-aware**: Provider clients use transport-level `ResponseHeaderTimeout` + `IdleConnTimeout` instead of socket-level `Client.Timeout`. SSE body closes immediately on ctx cancel via goroutine-based wrapper (prevents 5-minute socket block).
- **Router 2-phase abort**: `AbortRun()` atomically transitions to aborting state, waits 3s for goroutine exit via `Done` channel, then force-marks trace `cancelled` if timeout. No orphaned goroutines, no "not found" race.
- **Trace status persistence**: `SetTraceStatus()` detached from request context with 5s timeout, 3-try exponential backoff, and bounded in-memory retry queue (10 max tries). Stale recovery worker runs every 30s, catches zombie traces in 2 minutes instead of 30.
- **Real-time UI updates**: New WS event `trace.status` broadcasts status changes immediately after persist succeeds. UI drops 60s polling interval, subscribes to events instead.
- **Tool execution audit**: Shell commands use process-group kill (`SIGTERM`→3s→`SIGKILL`). Browser automation (Rod) closes pages on ctx cancel. MCP delegates timeout after 5s.
- **i18n**: 6 abort toast variants (success/timeout/not-found/already-done/db-error/unknown) + translations for en/vi/zh.

**Impact**: Existing traces and sessions unaffected. UI now reflects backend state accurately. Zero breaking changes.

#### Preserve User-Provided Filenames for Media Uploads (2026-04-14)

Filenames provided by users for chat media uploads now survive the channel adapter → agent → disk persistence round trip, enabling vault enrichment to process human-readable document names instead of falling back to generic UUID-only storage.

**Why**: Vault enrichment was skipping UUID-only disk names (design safety to avoid noisy auto-generated files), causing documents with Vietnamese or CJK stems to remain unenriched and lose semantic context.

**What changed**:
- **`bus.MediaFile.Filename` field**: Channel adapters now populate this field when source provides original filename (e.g., user-selected file upload, Telegram file_name, WhatsApp display_name)
- **Sanitizer** (`internal/agent/media_filename.go`): Derives safe stems via `sanitizeFilename()` with:
  - **Vietnamese pre-NFD map**: `đ/Đ → d` (Unicode NFD does not decompose these precomposed letters)
  - **CJK passthrough**: Dominant-script heuristic detects Vietnamese/CJK inputs and preserves original runes (no ASCII slugification)
  - **Filesystem safety**: Removes control chars, path traversal markers (`..`, `/`, `\\`), and reserved names
  - **Max length**: 60 runes (script-aware, not byte-based) to avoid platform path limits
- **Disk naming scheme**: `{sanitized-stem}-{8hex}{ext}` (e.g., `bao-cao-q4-a1b2c3d4.pdf`) when sanitizer returns non-empty stem; UUID fallback `{uuid}{ext}` for empty stems (voice notes, clipboard pastes, tool-generated media)
- **Vault enrichment gating** (`internal/vault/enrich_skip_filter.go`): Now skips UUID-only filenames (matching `^[0-9a-f]{8}-...$` pattern) while processing named stems
- **6 channel adapters wired**: Telegram, Slack, Discord, Feishu/Lark, Zalo OA, WhatsApp all set `MediaFile.Filename` when available
- **Tools + orchestration**: `web_search` tool (PDF downloads), delegate/subagent media propagation, all preserve filenames end-to-end

**Impact**: Existing flows with empty `Filename` are unaffected (UUID-named as before). New flows with filenames produce human-readable, enrichable disk names. Zero breaking changes.

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

#### Pancake Platform Field — Mandatory Select (2026-04-14)

- **`platform` field changed from optional text to required dropdown**: `channel-schemas.ts` replaces the free-text `platform` field with a `select` type (11 options: facebook, instagram, threads, tiktok, youtube, shopee, line, google, chat_plugin, lazada, tokopedia). `required: true` is now enforced by the form dialog on create.
- **Config required-field validation (create-only)**: `channel-instance-form-dialog.tsx` now enforces `required` on config fields at submit time. Validation is scoped to create flows only — edit flows are unchanged to avoid silent breakage on existing channels with no stored `platform`.
- **i18n**: Platform field label options and config field metadata added to `channels.json` for en/vi/zh.
- **Auto-detect log level**: `pancake.go` auto-detect block demoted from `slog.Info` to `slog.Debug` to stop log noise on every channel start; auto-detect remains as a fallback for existing channels with no stored `platform`.

#### Feishu/Lark Writer Management Commands — Issue #818 Closed (2026-04-11)
- **Issue #818 resolution**: Closes UX gap where users saw `/addwriter` error messages but Feishu had no handler
- **Phase 1 — Thread reply routing**: Inbound messages with `thread_id` now properly route responses back to the same Feishu thread via `/open-apis/im/v1/messages/{id}/reply`. New `feishu_reply_target_id` metadata key included in `routingMetaKeys` allowlist. Graceful fallback to `SendMessage()` if thread root deleted
- **Phase 2 — Document auto-fetch**: Pasted Lark docx URLs auto-detected and fetched via `/open-apis/docx/v1/documents/{id}/raw_content`. Content injected as `[Lark Doc: URL]` markers. LRU cache (128 entries, 5-min TTL) + 8000-rune truncation per document. Requires `docx:document:readonly` permission + owner grant
- **Phase 3 — Writer management commands**: Added `/addwriter <@user or reply>`, `/removewriter`, `/writers` for group file-write permission control. Group-only (DMs rejected early). Requires existing writer authorization. Last-writer guard prevents removing final writer. Empty-writer groups allow bootstrap via explicit `/addwriter @self`. 10s timeout bounds Feishu API calls

### Added

#### Vault & Knowledge Graph 10k Optimization (2026-04-12)
- **Graph visualization endpoints**: `GET /v1/vault/graph` (cross-tenant) + `GET /v1/agents/{agentID}/kg/graph/compact` for rendering document relationships and semantic entities with support for up to 10k nodes (up from 2k default limit)
- **FA2 layout optimization**: Graph layout computation moved to web worker (non-blocking frontend rendering)
- **Semantic zoom**: UI-level semantic zoom support for graph visualization
- **`DEFAULT_NODE_LIMIT` increase**: 200 → 2000 nodes per graph view to support larger knowledge bases

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
