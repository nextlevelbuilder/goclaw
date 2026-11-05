# Changelog

All notable changes to GoClaw are documented here. For full documentation, see [docs.goclaw.sh](https://docs.goclaw.sh).

## Unreleased

### Changed

- **Dashboard accessibility and theming pass (web + desktop UI).** Dark and light
  themes now meet WCAG 2.2 AA for text (`primary`, `destructive`, `success`, `warning`,
  `sidebar-accent` pairs) and for form-control boundaries via a new `--control-border`
  token — `--input` is unchanged so existing `dark:bg-input/30` fills keep working. A
  new `ui/web/src/__tests__/theme-contrast.test.ts` parses the OKLCH tokens and fails if
  any pair regresses. `prefers-reduced-motion` is honoured globally (spinners stay
  visible, slowed) and `MotionConfig reducedMotion="user"` covers Framer Motion.
  Icon-only buttons, clickable rows/cards/tree nodes, and `<Label>`/`<InfoLabel>`
  associations were swept across both frontends: triggers are `role="button"` with
  Enter/Space activation (`getClickableProps`/`useClickable`), trees use
  `role="tree"`/`treeitem` with roving `tabIndex` and arrow keys (`use-tree-nav`), and
  the file tree's dnd-kit drag no longer starts on activation. Mobile drawers
  (app nav, chat sessions, task panel, vault) use a new `components/ui/sheet.tsx`
  (focus trap, Escape, focus restoration), the app shell gained a skip link and
  route-change focus on `<main>`, connection banners are live regions, `window.confirm`
  was replaced with the in-app dialog, and the login forms gained `autoComplete`,
  password toggles and `aria-describedby` error links. `ui/web/scripts/a11y-audit.mjs`
  (TypeScript-AST audit, `pnpm a11y:audit`, wired into CI) gates 3 issue classes against
  a reviewed allowlist. Inter and KaTeX are self-hosted (`@fontsource-variable/inter`,
  `katex`) — the Google Fonts/jsDelivr `<link>`s and their CSP origins are gone, with
  the pre-paint theme applied by `public/theme-init.js`.

- **Semantic status colour tokens.** Web and desktop themes now expose
  `--success-text`/`--success-subtle`, `--warning-text`/`--warning-subtle`,
  `--info`/`--info-text`/`--info-subtle`, and `--destructive-text`/`--destructive-subtle`
  (desktop: `--color-error-text`/`-subtle`) that meet WCAG 2.2 AA on both
  background and card in light and dark. `Badge` success/warning/info variants and
  `StatusBadge` consume them. Hardcoded Tailwind palette text (`text-green-600`,
  `text-amber-700`, …) was migrated off the dashboard surfaces that failed 4.5:1.
  New `Spinner` primitive and `focus-ring` utility. Desktop `text-[11px]`/`text-[8px]`
  map to `--text-2xs` (11px).


- **Provider declaration scaffolding (no behaviour change yet).** Providers now declare
  *what they are* instead of it being inferred in Go: `llm_providers` gains `wire_api`
  (transport family), `auth_kind` (how credentials are obtained), `exec_path` and a
  versioned `settings` blob, backfilled from the existing `provider_type` values
  (migration `000100`; SQLite schema version 61). Two new tables carry the data that was
  previously code or absent: `llm_models` (per-model context/output caps, cost, modalities,
  reasoning, tokenizer, compat, provenance) and `provider_quirks` (declared compatibility,
  nullable `tenant_id` for tenant overrides).
  Also removed the unused provider abstractions that had drifted from the live path:
  `ProviderAdapter`/`AdapterFactory`/`AdapterRegistry` (`DefaultAdapterRegistry` had no
  production caller) and `RunWithFailover`/`FailoverConfig`, while rescuing the live
  helpers they had accumulated (`usageFromCodexUsage`, `ModelCandidate`,
  `FailoverAttempt`, `FailoverSummaryError`).
- **Wire-protocol dispatch registry.** Provider construction now dispatches on the
  declared `wire_api`, not a 28-branch switch on `provider_type`: `internal/providers/wire`
  is a single registry (`Descriptor`/`Register`/`Lookup`/`Build`) that every brand's
  base URL, default model, headers, and construction hook come from as data. Deleted
  `cmd/gateway_providers.go`'s two brand switches and `openAIProviderDefaults`. An
  unregistered `wire_api` is now logged (`provider.wire_api.unknown`) and skipped instead
  of silently building an OpenAI-compatible client. `internal/store` keeps `WireAPI*`/
  `AuthKind*` constants and the `ValidWireAPIs`/`ValidAuthKinds` sets as aliases of the
  registry, so the validated values and the dispatchable values cannot drift.
  `llm_providers.settings.timeout_sec` now bounds the chat path (previously only
  verify/models-list; the 300s response-header default still applies when unset).
  **Fix:** a provider created (not migrated) after this change — through HTTP, MCP, the
  onboarding wizard, or OAuth — now has its `wire_api`/`auth_kind` derived from its
  `provider_type`'s brand, instead of defaulting to OpenAI-compatible/api_key regardless
  of type. Repointing a provider's `provider_type` on update re-derives the declaration
  unless the caller states `wire_api`/`auth_kind` explicitly. `POST /v1/providers` no
  longer accepts a client-supplied `exec_path` (it selects the executable the gateway
  runs for CLI-delegated providers); `PUT /v1/providers/{id}` now accepts `wire_api`/
  `auth_kind` but still excludes `exec_path`.
- **Per-model catalog, discovery, and `/v1/models`.** `llm_models` rows (context window,
  max tokens, cost, capabilities, tokenizer, compat) are seeded from a bundled snapshot
  on first read and refreshed through per-wire discovery (OpenAI-shaped models-list,
  Ollama native, proxy/litellm) with a fingerprint-cached result and a
  `discovered_models_authoritative` toggle between gap-fill and replace-membership.
  New endpoints: `GET /v1/models` (OpenAI-shaped, tenant-scoped) and
  `GET /v1/models/{provider}/{model}`; `GET /v1/providers/{id}/models` now reports
  `stale`/`error`/`error_class` on a discovery failure instead of silently returning an
  empty list.
- **Compat as data and tool-call dialects.** `provider_quirks` rows (seeded from the
  previous string-sniffing heuristics) replace `isOllamaEndpoint`/`isTogetherEndpoint`/
  `isFireworksEndpoint`/`isDashScopeAPIBase`'s name/URL matching: a provider named
  `ollama-proxy` no longer receives Ollama-specific request shaping unless it is
  actually declared as one. `internal/providers/compat` resolves the request-shaping
  fragment once per catalog build (pointer swap at request time, no per-request
  allocation); `internal/providers/dialect` adds a table-driven tool-call parser
  (qwen3, deepseek-v3, kimi-k2, glm, hermes/xml, harmony) selectable per model or via
  `GOCLAW_TOOL_DIALECT`. Retry/fallback no longer discards a stream that already
  emitted content, an image, or a tool call, but does retry a thinking-only partial.
  Strict-tool rejection is now scoped to `(provider, base_url, model)` instead of the
  whole process.
- **Provider-level fallback chains, durable cooldown, and a provider health surface.**
  `llm_providers.settings.fallback_chain` declares an ordered provider-level default chain
  (`{"provider":"…","model":"…"}` entries, or compact `"provider/model"` strings); it is
  merged *after* an agent's own `model_fallback` candidates (the agent wins on a duplicate
  pair) when the runtime provider is resolved, so a default chain no longer has to be
  repeated on every agent. Cooldown is now durable: `provider_health` (consecutive failures,
  `cooldown_until`, last error class, last probe) and `provider_error_counts` (error-class
  histogram) carry the state that used to live only in the fallback wrapper's in-memory map
  — migration `000102`, SQLite schema version 63. The in-memory tracker stays the fast path
  but reads the persisted deadline whenever a key has no local state and writes every
  failure/probe/success through, so a restart no longer forgets an active cooldown (and no
  longer burns a fresh probe on boot). Cooldown stays bounded: `providers.MaxCooldown` (1h)
  clamps both the in-memory deadline and the persisted one. New `GET /v1/providers/{id}/health`
  (and `POST /v1/providers/{id}/health` with `{"reset":true}` or `{"probe":true,"model":"…"}`)
  plus `goclaw providers health [id] [--json] [--reset <id>] [--probe <id>]` report the state
  and give operators the manual reset for a cooldown that outlived the outage that caused it.
  The active probe reuses the existing verify path and runs only when explicitly requested —
  there is no background prober.
- **Bitrix24 channel migrated to imbot v2 messaging API** — outbound text now uses
  `imbot.v2.Chat.Message.send` (replacing `imbot.message.add`); bot verification/lookup
  uses `imbot.v2.Bot.list` (replacing `imbot.bot.list` + the legacy `imbot.list` fallback);
  bot teardown uses `imbot.v2.Bot.unregister` (replacing `imbot.unregister`). Bot
  registration intentionally stays on v1 `imbot.register` — v2 `imbot.v2.Bot.register`
  changes the event-delivery model (per-event handler URLs → `eventMode`), which would
  require rewriting the inbound event parser. No user-facing behavior change.
- **Per-model capabilities now drive the request path.** `llm_models.capabilities`
  (keys `tool_calling`, `vision`, `stream_with_tools`, `cache_control`) plus
  `max_context_window` were declared but inert; they are now resolved per request as
  `provider.Capabilities()` overlaid with the catalogue row
  (`providers.ResolveModelCapabilities`) and consumed by the pipeline:
  `tool_calling=false` drops the tool schemas from the outgoing request and injects an
  in-band `[System]` notice so the model does not invent calls it cannot execute (the
  execution allowlist is emptied, not left nil); `vision=false` strips image blocks from
  the outgoing request only (the conversation buffer keeps them, so persisted history and
  later vision runs are unaffected) and emits a user-visible notice once per run
  (`chat.model_without_vision_notice` in en/vi/zh/ko/ru); `max_context_window` clamps the
  request budget with min() semantics — a row never raises an agent's configured window;
  `stream_with_tools=false` forces the non-stream transport for requests that carry tools,
  which is how DashScope's tools-plus-stream limitation is now declared per model instead
  of sniffed from the provider name/type (and a tool-free request to the same model still
  streams); `cache_control=true`, or a resolved compat object declaring
  `system_cache_control`/`tool_prefix_cache`, enables the prompt-cache parameters for any
  wire the resolver declares cache-capable, replacing the Codex/ChatGPT-only Go type switch
  (Anthropic's own block-level `cache_control` is untouched). The shipped model snapshot
  seeds today's behaviour: anthropic/openai entries declare tool calling, streaming and
  cache explicitly with their context window, and every DashScope chat model declares
  `stream_with_tools:false`. A provider that declares no capabilities (no
  `CapabilitiesAware`) is never gated — "undeclared" is not "unsupported".
- **Per-request model routing and per-agent model roles.** `chat.send` accepts `model`
  (and an optional `provider` name) and `POST /v1/chat/completions` honours an
  `X-GoClaw-Model` header: the run uses that model — and, when a provider is named, that
  provider — instead of the agent's own, and the explicit override bypasses the model
  fallback chain (the semantics heartbeat/cron per-run overrides already had). A named
  provider that is not in the registry fails the request (`provider not found: <name>`)
  instead of silently running somewhere else; with no registry wired the pin is logged and
  ignored while the model override still applies, and the response reports the effective
  model. `agents.model_roles` (JSONB, PG migration `000101`, SQLite schema version 62)
  declares role → `provider/model` targets per agent (e.g.
  `{"summarizer":"anthropic/claude-haiku-4-5"}`), settable through the agent update
  endpoint, validated on write (`role names are [A-Za-z0-9_.-]`, values must split into
  provider + model on the first `/`) and carried by agent export/import. A run that
  requests a role (`agent.WithModelRole`, for callers that already know which workload they
  are running) picks the role's provider/model per run — resolved to live registry entries
  at agent-resolution time but applied per run, never baked into the cached agent — with the
  precedence: explicit per-request override → agent role → agent primary → provider default
  → global default. An undeclared role, or one whose provider is not in the registry,
  degrades to the agent primary instead of routing to a provider the operator did not name.

- **Declared capabilities reach the clients, and `/v1/chat/completions` is a real
  OpenAI surface.** `GET /v1/providers/capabilities` serves the declaration DTO the UIs
  build their pickers from — per provider `wire_api`, `auth_kind`, where its model list came
  from (`bundled` snapshot vs `discovered`), `default_model_id`, `stale`/`last_refreshed_at`,
  and per model the context window, output cap, thinking levels, capabilities, cost and
  per-model staleness — and carries no transport, credential or compat internals (a test
  pins the key set). `goclaw providers capabilities [id] [--json] [--models]` renders the
  same DTO, so the CLI cannot disagree with what the UI offers. The web and desktop UIs now
  source their model pickers from that catalogue instead of a per-provider models call:
  provider rows and detail pages show `wire_api`/`auth_kind` next to the brand
  `provider_type`, a degraded (stale/failed) catalogue is shown with a refresh affordance
  rather than as an empty list, and the chat header carries a per-request model selector
  whose value is the `<provider>/<model>` identity.
  `POST /v1/chat/completions` now behaves like the API it advertises: streamed responses
  forward the model's real deltas as they are produced (the same run-event broadcast the WS
  clients and channels consume) instead of one buffered chunk, closing with the provider's
  `finish_reason` and, on `stream_options.include_usage`, a usage chunk; `temperature`,
  `max_tokens` (clamped by the provider layer) and `tool_choice` apply to that run only;
  the whole `messages` transcript is replayed so a stateless caller's conversation actually
  reaches the model, and a transcript ending in a `role:"tool"` result continues from it
  instead of being rejected or answered with a fabricated user turn. Caller-declared `tools`
  are honoured as a passthrough: they replace the agent's tool surface for that run, the
  model's calls come back as OpenAI `tool_calls` with `finish_reason:"tool_calls"` (streamed
  in the indexed delta shape), and they are never executed here — not even when a declared
  name collides with a real agent tool (both `ThinkStage` and `ToolStage` refuse to dispatch
  a client-owned call). `tool_choice` is forwarded verbatim, including OpenAI's object form
  that names a function. Parameters this endpoint cannot honour (`n>1`, `stop`, a malformed
  `tool_choice` object, non-function tool types, a trailing `system`/`assistant` message)
  fail with an OpenAI-shaped error envelope instead of silently changing behaviour.
  A `model` of `<provider>/<model>` — or `X-GoClaw-Model` — is a per-request override whose
  prefix pins the provider when it names a registered one, so a vendor model id containing a
  `tool_choice` is translated for an Anthropic-backed agent (`auto`/`required`/named function map to
  Anthropic's `auto`/`any`/`tool`, and `none` withholds the tool list) instead of being silently
  dropped, a tool-calling turn still returns the text the model produced with its calls, and the SSE
  tap queues deltas without loss, so a slow client delays its own stream rather than truncating it.
  slash (`openrouter`'s `openai/gpt-5.5`) still works, while a `model` value that is neither an
  agent form nor a resolvable reference keeps selecting the agent exactly as before (a caller
  passing a placeholder is not silently re-routed); the same split now applies to
  `chat.send`, where a reference naming one provider while `provider` names another is
  rejected rather than misrouted. Assistant turns record the model that produced them
  (`model` as the `<provider>/<model>` identity plus `provider`), so `chat.history` and the
  session surfaces can show it per turn.

### Added

- **Task detail dialog shows the full task UUID with one-click copy** — the
  short identifier (`T-015-cc8e`) carries only the last four hex characters of
  the UUID, while the agent-facing `team_tasks` tool and RPCs take the full
  UUID, so a human had no way to name a task to the lead from the dashboard.

- **Dashboard can cancel and retry stuck team tasks** — new WS RPCs
  `teams.tasks.cancel` (any task not yet completed/cancelled; optional reason is
  posted as a comment) and `teams.tasks.retry` (stale / failed / cancelled /
  in_review, and blocked tasks that nothing blocks any more; a human comment is
  required and is both posted on the task and appended to the assignment prompt).
  Task detail dialog gets Retry and Cancel buttons. Previously these transitions
  were reachable only through the lead agent's `team_tasks` tool, so a task that
  ended up blocked or stale could not be recovered from the UI (#506).

- **Behavior UX sidecar delivery overrides** — Adds sidecar-generated Quick
  Acknowledgement and Intermediate Replies with provider/model, timeout, token,
  and char caps. Effective config resolves Channel > Agent > Workspace, with
  agent overrides stored in `other_config.delivery_behavior`.

- **Built-in skill `workspace-organizing`** — closes #71. Discipline skill that
  teaches agents to keep personal, team, and delegate workspaces tidy.
  Enforces a purpose-based folder convention with two modes: flat
  (`notes/`, `data/`, `outputs/`, `scripts/`, `archive/`) for ad-hoc work
  and project (`projects/<slug>/{docs,assets,source,reports,research}/`)
  for named multi-file work. Per-agent namespacing under
  `shared/<agent_key>/` prevents collisions in team workspaces. Integrates
  pre-write discovery via `vault_search`, `memory_search`, and
  `knowledge_graph_search` to surface related files before writing and
  avoid duplicates; documents Vault scope mirroring and id-routing rules.
- **Bitrix24 channel 2-way media (file) transfer** — Inbound media downloads via
  `imbot.v2.File.download` (one-time authenticated URL) with MIME preservation for
  images, PDFs, audio, and video. Outbound uploads via `imbot.v2.File.upload` (base64).
  Shared `media_max_mb` config knob (default 20 MB) caps both directions. Requires
  `imbot` OAuth scope (no `disk` scope needed). Inbound handled by new
  `internal/channels/bitrix24/download.go`; outbound by `send_media.go`. New
  `BaseChannel.HandleMessageMedia()` method centralizes media-aware message handling.
  See `docs/05-channels-messaging.md` § 16 (Bitrix24) for configuration.

- **Skill agent manage grants** — Adds per-agent skill edit/delete grants with
  backend checks, HTTP/WS support, SQLite and PostgreSQL schema updates, and web
  dashboard controls for granting and revoking manage access.

- **Packages Update Flow (Phase 2a: pip + npm)** — closes #900 (Phase 2a). Extends
  Phase 1 update infrastructure to pip and npm package sources. `/v1/packages/updates`
  now returns mixed-source results with an `availability: {github, pip, npm}` map.
  Multi-source UI with per-source filter pills; unavailable sources (binary not on PATH
  or Lite edition) hidden automatically. apk deferred to Phase 2b.
  See `docs/packages-pip-npm.md` for command matrix, runbook, and min versions.

- **Packages Update Flow (Phase 1: GitHub binaries)** — closes #900. Proactive
  "N updates available" badge + per-row `[Update]` + `[Update All]` on the
  Runtime & Packages page. Backend endpoints under `/v1/packages/updates*`
  (master-scope). ETag-aware polling (304 responses don't burn rate limit),
  stale-while-revalidate cache, atomic two-phase `.bak` swap with rollback.
  Pre-release detection via regex + GitHub API flag; semver ordering via
  `golang.org/x/mod/semver`; non-semver tags use string-inequality fallback
  with downgrade protection. WebSocket events `package.update.*` for owner
  clients. See `docs/packages-github.md` § "Updating Installed Packages".

### Changed

- **Behavior UX simplification** — Retires user-facing Tool Status Messages and
  deterministic tool-status channel text. Show Reasoning remains separate for
  debugging/testing, while Quick Acknowledgement and Intermediate Replies are
  delivery-only sidecar messages. Legacy `block_reply` config remains readable
  as an inherited Intermediate Replies default but is no longer exposed as a
  separate Web UI control.

- **ChatGPT Subscription (OAuth)** — default model and backend-owned model catalog
  now prefer `gpt-5.5`, with reasoning metadata and context-window defaults updated
  for provider-first model selection.

### Fixed

- **Channel reload no longer hangs after a failed channel start (#1627)** —
  `PendingHistory.StopFlusher` waited on the flush goroutine's `stopped`
  channel even when `StartFlusher` had never run. Telegram starts its flusher
  only after `getMe` and long polling succeed, so a Telegram instance whose
  `Start()` failed (network down at boot) blocked `InstanceLoader.Reload`
  forever inside `Stop()` with the loader mutex held — every later channel
  save from the dashboard queued silently behind it and only a process
  restart recovered. The same wait sat in `LoadInstanceByID`, loader `Stop`
  and the timed-out-start cleanup path. `StopFlusher` now returns immediately
  when the flusher was never started and is idempotent (`sync.Once` around
  `close(stopCh)`, so a second `Stop()` no longer panics with
  `close of closed channel`); `StartFlusher` spawns at most one loop.

- **SQLite (desktop/lite) agent export and import: shared export SQL assumed
  PostgreSQL.** The export helpers take a `*sql.DB` and run on both backends, but
  only ever had PostgreSQL as a caller. On the desktop/lite build the failures were
  a dropped connection (skills export), a 500 (agent export once the agent had KG
  rows), or a silently missing section — the archive builder logs and continues for
  most sections, so an export could look successful while carrying no memory, vault,
  schedule or team data. Ten defect sites, all of the same shape:
  1. **Export helpers dereferenced a package-level `*sqlx.DB` that SQLite never
     initializes** (`initSqlx` runs from the PostgreSQL factory only) — a nil
     receiver panic that closed the connection mid-response (`/v1/skills/export`),
     and a 500 on `/v1/agents/{id}/export` as soon as `kg_entities` had rows. The
     helpers now wrap the handle they were given (`sqlxFor`), reusing the package
     handle when it already wraps that pool, so the PostgreSQL path is unchanged.
  2. **Row structs declared PostgreSQL-native scan types.** `time.Time`,
     `*time.Time`, `pq.StringArray` (a `{a,b}` literal) and `json.RawMessage` (the
     driver returns TEXT) cannot scan what the SQLite driver hands back, so the row
     was skipped with a warning — or aborted the section. Timestamps, string arrays
     and JSON columns now use tolerant types (`pgTime`, `ExportStringArray`,
     `ExportJSON`) that accept both representations.
  3. **`to_char(... AT TIME ZONE 'UTC', ...)` projections** (cron jobs, evolution
     metrics and suggestions, episodic summaries, vault documents and links) — SQLite
     has no `to_char`, so those statements failed and the section was dropped. The
     raw column is selected and normalised in Go (`normalizeArchiveTime`), which
     produces the same RFC3339 UTC form on both backends.
  4. **The skills selection filter used `id = ANY($1)` with `pq.Array`** —
     PostgreSQL-only. It is now a positional `IN` list, accepted by both.
  5. **Unqualified `tenant_id` scope clauses in queries that join tables carrying
     it.** `ExportTeamTasks` (ambiguous against its two `agents` joins — broken on
     PostgreSQL too), its parent-resolution query, `ExportVaultLinks` (also broken on
     PostgreSQL: `vault_links` has no `tenant_id` of its own), and the vault subquery
     of `ExportPreviewCounts`, whose error was discarded so vault counts always read
     zero.
  6. **Nullable `source_id` read into a non-pointer string** (episodic summaries, KG
     entities) — a NULL dropped the row on both backends.
  7. **Shared import statements used PostgreSQL's `NOW()`** (skill grants, MCP
     servers, MCP grants) — the statement aborted on SQLite while the section
     reported success. Timestamps now come from Go.
  8. **The episodic and vault import sections were dead code on every backend.** Both
     were gated on `h.episodicStore`/`h.vaultStore`, which nothing ever set, so an
     archive's `episodic/` and `vault/` entries were parsed and then dropped. The
     stores are wired in `wireHTTP`.
  9. **The SQLite activity log scanned its `details` JSON column into
     `json.RawMessage`**, making `GET /v1/activity` fail with a scan error; it now
     uses the `sqliteJSONValue` scanner already used by the other SQLite row structs.
  10. **`secure_cli.List` referenced `a.agent_key` from a derived table that no
     longer exposes `a`** (`no such column: a.agent_key`), so listing CLI binaries —
     reached when creating an agent with gateway-operator access — returned an error
     on SQLite.
  Regression tests: `internal/store/pg/export_sqlite_test.go` calls every shared
  export function against an in-memory SQLite schema seeded with one row per table it
  reads (values asserted, not just counts: JSON columns, JSON arrays, NULL
  `source_id`, TEXT timestamps). Verified end-to-end against the lite binary: an
  agent export with `?sections=all` returns cron, episodic, evolution,
  knowledge-graph, memory, team, user-override/profile and vault sections with
  normalised timestamps, and importing that archive back reports 1 episodic summary,
  2 vault documents and 1 vault link — 0/0/0 before, because those sections were
  unreachable.

- **Agent import wrote unparseable timestamps.** An archive without `created_at` on
  an evolution metric or suggestion bound an empty string into a `NOT NULL` column;
  the row landed but every later read of it failed (`sqliteTime: cannot parse ""`),
  turning a list endpoint into a 500. Empty archive timestamps now coalesce to the
  current time, and the SQLite timestamp scanner reads a blank cell as "unset"
  instead of aborting the query.

- **SQLite (desktop/lite) startup and agent import: shared SQL relied on
  PostgreSQL-only defaults.** Three defect sites, all of the same shape — SQL
  written once for both dialects but only valid on PostgreSQL, where the failure
  is either a logged warning or a silently missing row:
  1. **Tenant/system-config seed aborted the boot-time read** (`sql: Scan error
     ... unsupported Scan, storing driver.Value type string into type
     *jsontext.Value`). SQLite returns JSON columns as text; the row structs
     declared `json.RawMessage` and `database/sql` cannot scan into it. Those
     fields now use the SQLite scanner types (`sqliteJSONValue`) already used by
     the other row structs.
  2. **Capability backfill failed on every boot** (`no such function:
     uuid_generate_v7`) — `internal/bootstrap` inserted rows without an `id`,
     relying on PostgreSQL's `DEFAULT uuid_generate_v7()`. Ids now come from Go.
  3. **Agent import dropped rows on SQLite** — `cron_jobs`,
     `user_agent_overrides`, `agent_evolution_metrics` and
     `agent_evolution_suggestions` inserted without `id` (PostgreSQL default
     only), and `team_tasks` bound NULL into `metadata`/`task_type`/
     `task_number`, which are `NOT NULL` in *both* dialects. Each statement
     aborted with a `NOT NULL`/`FOREIGN KEY` error, the import logged a warning
     and continued, and the summary still reported success — so a team import
     lost its task, and with it every comment and event (FK). Ids now come from
     Go and the NULL-bound columns coalesce to their DDL defaults (`{}`,
     `general`, `0`).
  4. **Multi-row import batches bound the wrong parameters.** The team member,
     comment and event batches declared a per-row stride one short of their
     placeholder and argument count (`cols = 4/8/7` against 5/9/8 binds), so from
     the second row on, the placeholder indices overlapped and each row took a
     neighbour's value — the first row was always correct, which is why
     single-row fixtures and the earlier smoke missed it. Strides now match the
     bind count.
  5. **Import batches exceeded SQLite's bound-parameter ceiling.** SQLite allows
     32766 parameters per statement (32767 fails with `too many SQL variables`);
     the cron batch bound 11 × 5000 = 55000 and the user-override batch
     7 × 5000 + 1 = 35001, so any sizeable import lost whole sections on the
     desktop/lite build. Chunk sizes are now derived from a shared ceiling
     (`maxBindVars`) instead of a fixed row count.
  6. **Import counters counted failed writes as imported.** `CronJobs`,
     `UserProfiles` and `UserOverrides` were incremented per chunk regardless of
     the `ExecContext` result, and the team phase reported the archive's row
     count rather than what landed — which is what made the failures above look
     like a clean import. Counters now advance only on a successful statement,
     and the team section logs a `partial import` warning with per-table
     landed/expected counts.

     Regression tests: `internal/http/agents_import_sqlite_test.go`
     (`-tags sqliteonly`) drives the cron, user-override, evolution and team
     sections against an in-memory SQLite schema and asserts row counts *and*
     values (the stride bug keeps counts intact and corrupts data). Verified
     failing before each fix — reverted strides lose rows, a fixed 5000-row chunk
     fails with `too many SQL variables` — and end-to-end against the lite
     gateway binary with a hand-built archive: 11 import tables populated with
     the right values, versus only `agents` and `agent_context_files` before.

- **`GET /v1/vault/documents/{id}` returned 500 instead of 404 for a missing
  document.** The handler propagated the raw `sql.ErrNoRows` from the store as an
  internal error; it now maps `sql.ErrNoRows` to a 404 `document not found`
  response.

- **SQLite (desktop/lite): bound `time.Time` values used a different text
  encoding than the schema's own column defaults, corrupting time ordering.**
  modernc.org/sqlite formats a bound `time.Time` with `time.Time.String()`
  (`"2026-09-27 06:13:53.0844211 +0000 UTC"`, space-separated, sometimes with a
  trailing monotonic reading), while every `created_at`/`updated_at` column
  default writes `strftime('%Y-%m-%dT%H:%M:%fZ','now')`
  (`"2026-09-27T06:43:39.231Z"`, `T`-separated). SQLite compares `TEXT`
  lexically, so a row written by binding a `time.Time` sorted *before* any
  row written by a column default on the same day (`' '` 0x20 < `'T'` 0x54)
  regardless of which happened first — breaking `ORDER BY created_at` and
  range filters (`created_at >= ?`) on every table populated by both write
  paths (`agents`, `team_tasks`, `activity_logs`, and others) — including
  `api_keys.expires_at`, which is compared against the matching `strftime`
  form for the "not expired" filter, so an unnormalised nullable expiry could
  reject a key that had not actually expired yet. The SQLite driver connection
  now runs every bound argument through `driver.DefaultParameterConverter`
  before checking the result: that resolves the `*time.Time` (AGENTS.md's
  convention for every nullable timestamp column) and `sql.NullTime` shapes
  down to a plain `time.Time` the same way `database/sql` itself would, so
  the check catches pointers and `Valuer`s, not only a bare `time.Time`
  argument. Matches are reformatted to the `T`-separated,
  millisecond-precision, UTC encoding the DDL defaults use; the two
  remaining SQL-side `datetime('now')` expressions (API key expiry, trace
  span end time) were switched to the matching `strftime` form. Regression
  tests `TestSQLiteBoundTimestampsMatchDDLDefaults` (bare `time.Time`) and
  `TestSQLiteNullableTimestampMatchesDDLDefault` (`*time.Time`, through the
  real `api_keys` store) fail without the fix and pass with it — the
  nullable-timestamp test's before-fix failure is exactly the regression
  above: a key expiring two hours from now is not found by `GetByHash`.
  Rows written before this fix keep their original encoding; only new
  writes are affected.

- **`/v1/skills/{id}/grants/*` (list, grant, revoke — agent and user) returned
  500 instead of 404 for a well-formed but non-existent or cross-tenant skill
  id.** `verifySkillInGrantScope` (both stores) returned a bare
  `fmt.Errorf("skill not found")` for a missing row and for a tenant mismatch,
  which `errors.Is` could never match, so every handler on this path fell
  through to its generic 500 branch. Added `store.ErrSkillNotFound`; both
  stores now return it from `sql.ErrNoRows` and the tenant-mismatch branch
  (any other error is still wrapped through unchanged), and all six handlers
  reachable through it map it to 404. `TestSQLiteSkillGrantsReturnErrSkillNotFound`
  (all six store methods) and `TestSQLiteSkillGrantsCrossTenantReturnsErrSkillNotFound`
  fail without the fix and pass with it; verified end-to-end against the lite
  binary. The PG store received the identical fix (same query shape, `$1` vs
  `?`) but was verified by code symmetry and a clean build only — the
  integration Postgres container was not running this session.

- **Schema migration 57→58 (restore custom skills misclassified by the
  bundled skill seeder) never repaired `file_path` on Windows.** The repair
  matched the trailing version segment with `file_path LIKE '%/' || version`,
  hardcoding `/` as the separator. Real desktop `file_path` values are
  OS-native (every caller persists them via `filepath.Join`), so on Windows
  the pattern never matched: `version` was correctly decremented but
  `file_path` kept pointing at the pre-repair version directory, leaving the
  row internally inconsistent. The `LIKE` now matches either `/` or `\`
  before the version suffix. Installs that already ran this migration under
  the old pattern keep their stale `file_path` — this only fixes installs
  that have not yet crossed schema version 58.

- **`POST /v1/files/sign` returned a `url` that could not be requested as an
  actual HTTP URL when the underlying path needed escaping** — most commonly
  a space in a Windows username (`C:\Users\Jane Doe\...`), on any OS. Two
  separate bugs in `fileURLPath`: it used `filepath.Separator` to build a
  `/`-delimited URL path, so on Windows the whole path came out as one opaque
  segment of literal backslashes instead of nested URL segments; and it never
  percent-encoded the result, so a literal space broke request-line parsing
  outright. The canonical (decoded) path — still what gets signed and what
  `r.PathValue` reconstructs server-side — now always uses `/`
  (`filepath.ToSlash`); a new `escapedFileURL` percent-encodes each segment
  for the string actually returned to clients, leaving the signed value
  unchanged.

- **Docker sandbox path containment (`FsBridge.resolvePath`,
  `fsBridgePathWithin`) used the OS-native `path/filepath` package on paths
  that are always inside the Linux container, never the host.** On a Windows
  host, `filepath.Clean("/workspace/agent-a")` rewrote the container path to
  `\workspace\agent-a` — corrupting every path handed to `docker exec` and
  breaking the workspace-escape check it exists to enforce. Switched to the
  POSIX-only `path` package, which matches the container's filesystem
  regardless of host OS.

- **Claude CLI provider failed every follow-up turn with `Session ID ... is already
  in use`** — `sessionFileExists` encoded the work directory into the Claude CLI's
  `~/.claude/projects/<encoded-path>` name with a narrow replacement set
  (separators, `_`, `.`, `:`), but the CLI replaces every character outside
  `[A-Za-z0-9]` with `-` (per UTF-16 code unit). On any host whose data dir
  contains a space (e.g. `C:\Users\Jane Doe\.goclaw\data`) the lookup missed the
  real session file, so the provider passed `--session-id` for an ID the CLI
  already owned and the turn died in `iter 0 think` — on every turn after the
  first. Fixed by mirroring the CLI encoding (`encodeClaudeProjectDir`; verified
  on Windows against Claude Code 2.1.282 — unix paths follow the same rule but
  were not probed) and by
  retrying once with `--resume` if the CLI still reports a duplicate session ID.
  `ResetCLISession` now shares the same path helper.

- **Quick Acknowledgement generated mode** — Generated acknowledgements now use
  the sidecar delivery generator instead of always falling back to fixed
  templates. Sidecar failures stay non-blocking and fall back to templates.

- **Intermediate Replies are sidecar-generated** — Tool-call progress no longer
  appends fixed "I'll use ..." text or relies on main-pipeline assistant content.
  Visible progress is generated from bounded delivery metadata and is kept out
  of session history.

- **Multi-attachment messages no longer trigger N agent replies (#63).**
  Three coalescing surfaces hardened so a single user action produces ONE
  agent run regardless of how the platform delivers attachments:
  1. **Bus debouncer** — removed the media-bypass shortcut that fired
     immediately for any message with attachments; media now goes through
     the same per-(channel, chatID, senderID, agentID) silence window as
     text. Media-floor (`max(configured, mediaFloor)`) guarantees a
     minimum window when attachments are present so multi-file uploads
     coalesce. Dedup seed prevents the same `MessageID` from being
     buffered twice on bursty arrivals.
  2. **Web Chat debouncer** (`internal/gateway/methods/chat_debounce.go`) —
     parallel structure for `/v1/chat/completions` streaming: per-session
     buffer + media floor + Take/Discard semantics for flush/cancel
     control. Merges queued payloads at flush time (latest params win;
     text concatenated newline-separated).
  3. **Telegram album aggregator** (`internal/channels/telegram/album_aggregator.go`) —
     channel-layer coalescing for albums. Telegram delivers a media-group
     (multiple photos/videos shared as one user action) as N separate
     `Message` updates sharing a `MediaGroupID`. The aggregator buffers
     by `(chatID, MediaGroupID)` after all access gates pass, pins the
     sender on first arrival as a security tripwire, and dispatches ONE
     `processResolvedMessage` call with all members on a 500ms silence
     window. `Stop()` synchronously drains pending buffers before
     `pollCancel` so in-flight albums always publish.

  Cross-surface invariants (see CONTRIBUTING.md → "Multi-attachment
  coalescing"): no media bypass, media floor on every surface,
  drop-and-log dual caps, no `time.Timer.Reset` (use `AfterFunc` +
  `Stop`), sender pin on first arrival, post-stop pushes rejected
  with warn log.

- **Upstream critical security remediation** — hardens gateway no-token fallback,
  Feishu/Lark and Pancake webhooks, sandbox path/write handling, tenant-admin
  checks for mutable HTTP surfaces, and Lite hook schema migration verification.

- **SecureCLI runtime npm binaries** — binary discovery and credentialed exec now
  resolve tools installed under the GoClaw runtime directories, including
  `{runtimeDir}/npm-global/bin`, and support single-binary npm package aliases
  such as `openrouter-cli` exposing `orc`.

### Breaking Changes

- **Context pruning now opt-in.** Previously tool-result trimming ran by default
  for all providers; now requires explicit `contextPruning.mode: "cache-ttl"` in
  `config.agents.defaults` to enable. Matches upstream TS design and prevents
  silent prompt-cache invalidation on Anthropic.

  Migration — add to `config.json5`:
  ```json5
  agents: {
    defaults: {
      contextPruning: { mode: "cache-ttl" }
    }
  }
  ```

### New Features

- **Pancake private-reply (comment → DM).** Enables a one-time DM to commenters
  after the public reply. Stateless on GoClaw side — no DB dedup table, no
  in-memory state:
  - Config: `features.private_reply` (bool) + `private_reply_message` (text).
  - **Template variables** `{{commenter_name}}` and `{{post_title}}` with
    literal-replace semantics (pre-sanitizes `{{`/`}}` from var values to
    prevent var-in-var substitution).
  - Empty `private_reply_message` → English fallback constant.
  - **Dedup strategy**: webhook-level comment_id dedup (already in
    `comment_handler.go`) + Facebook's per-comment idempotent `private_replies`
    endpoint handle duplicates platform-side. No GoClaw state required.
  - No DB migration.

### Improvements

- **Context pruning cleanup.** Removed redundant Pass 0 (per-result 30% guard),
  deduplicated double prune call per iteration, added SanitizeHistory to
  PruneStage for broken tool_use/tool_result pair cleanup.
- **Context pruning config backfill (migration).** Agents with existing custom
  `context_pruning` config (e.g., `softTrimRatio`, `keepLastAssistants`) but
  missing a `mode` field get auto-backfilled with `mode: "cache-ttl"` to
  preserve their intent after the opt-in flip. Rows with NULL config stay
  NULL (new opt-in default applies). PG migration 51; SQLite schema v19.
- **Pancake channel metadata routing.** Whitelist in
  `internal/channels/routing_metadata.go` now preserves `post_id` and
  `display_name` across the inbound → outbound hop so the private-reply
  template variables survive the agent pipeline round-trip.

### Fixed

- **Skill grant tenant isolation.** Agent skill grants now validate both the
  skill and agent tenant scope before insert, revoke, grant listing, or
  can-manage checks. Visibility auto-promote/auto-demote updates are scoped to
  the calling tenant or system skills so one tenant cannot mutate another
  tenant's skill.

- **Agent provider switching.** Saving an agent after changing provider/model now
  handles cleared ChatGPT OAuth routing config without writing SQL NULL into
  NOT NULL JSON config columns.

- **Windows gateway could hang forever during startup.** `artifactNtOpen` (the
  NT-native open behind the delegation-artifact secure root) omitted
  `FILE_SYNCHRONOUS_IO_NONALERT`, so directory handles landed in asynchronous
  mode. Go's `os.File.ReadDir` issues a synchronous `NtQueryDirectoryFile` that
  waits on the file object and never completes for such a handle. Because
  delegation artifact recovery enumerates
  `workspace/collaboration/delegations` while wiring the delegate tool, any
  Windows gateway whose workspace contained that directory stalled after channel
  loading: no listener on the gateway port and no further log lines. Handles are
  now opened synchronously, matching Win32 `CreateFile` semantics for the
  `os.File` callers.

- **Claude CLI sessions were recreated on every turn** ("Session ID <uuid> is
  already in use"). Resuming depended on a `~/.claude/projects/<workdir>` lookup
  whose path encoding did not match the CLI's for common Windows paths (spaces,
  dots, non-ASCII segments), so every turn after the first passed `--session-id`
  for an ID the CLI already owned and the turn failed with exit status 1. The
  provider now encodes the project directory exactly as the CLI does
  (per-UTF-16-code-unit replacement of non-alphanumerics, verified against CLI
  session files), passes `--resume` for existing sessions, and retries once with
  `--resume` if the CLI still reports the collision. The model allowlist also
  accepts the current granular CLI model IDs.

## Project Status

### Implemented & Tested in Production

- **Agent management & configuration** — Create, update, delete agents via API and web dashboard. Agent types (`open` / `predefined`), agent routing, and lazy resolution all tested.
- **Telegram channel** — Full integration tested: message handling, streaming responses, rich formatting (HTML, tables, code blocks), reactions, media, chunked long messages.
- **Seed data & bootstrapping** — Auto-onboard, DB seeding, migration pipeline tested end-to-end.
- **User-scope & content files** — Per-user context files (`user_context_files`), agent-level context files (`agent_context_files`), virtual FS interceptors, per-user seeding (`SeedUserFiles`), and user-agent profile tracking all implemented and tested.
- **Core built-in tools** — File system tools (`read_file`, `write_file`, `edit_file`, `list_files`, `search`, `glob`), shell execution (`exec`), web tools (`web_search`, `web_fetch`), and session management tools tested in real agent loops.
- **Memory system** — Long-term memory with pgvector hybrid search (FTS + vector) implemented and tested with real conversations.
- **Agent loop** — Think-act-observe cycle, tool use, session history, auto-summarization, and subagent spawning tested in production.
- **WebSocket RPC protocol (v3)** — Connect handshake, chat streaming, event push all tested with web dashboard and integration tests.
- **Store layer (PostgreSQL)** — All PG stores (sessions, agents, providers, skills, cron, pairing, tracing, memory, teams) implemented and running.
- **Browser automation** — Rod/CDP integration for headless Chrome, tested in production agent workflows.
- **Lane-based scheduler** — Main/subagent/team/cron lane isolation with concurrent execution tested. Group chats support up to 3 concurrent agent runs per session with adaptive throttle and deferred session writes for history isolation.
- **Security hardening** — Rate limiting, prompt injection detection, CORS, shell deny patterns, SSRF protection, credential scrubbing all implemented and verified.
- **Web dashboard** — Channel management, agent management, pairing approval, traces & spans viewer, skills, MCP, cron, sessions, teams, and config pages all implemented and working.
- **Prompt caching** — Anthropic (explicit `cache_control`), OpenAI/MiniMax/OpenRouter (automatic). Cache metrics tracked in trace spans and displayed in web dashboard.
- **Agent delegation** — Inter-agent task delegation with permission links, sync/async modes, per-user restrictions, concurrency limits, and hybrid agent search. Tested in production.
- **Agent teams** — Team creation with lead/member roles, shared task board (create, claim, complete, search, blocked_by dependencies), team mailbox (send, broadcast, read). Tested in production.
- **Evaluate loop** — Generator-evaluator feedback cycles with configurable max rounds and pass criteria. Tested in production.
- **Delegation history** — Queryable audit trail of inter-agent delegations. Tested in production.
- **Skill system** — BM25 search, ZIP upload, SKILL.md parsing, and embedding hybrid search. Tested in production.
- **MCP integration** — stdio, SSE, and streamable-http transports with per-agent/per-user grants. Tested in production.
- **Cron scheduling** — `at`, `every`, and cron expression scheduling. Tested in production.
- **Docker sandbox** — Isolated code execution in containers. Tested in production.
- **Text-to-Speech** — OpenAI, ElevenLabs, Edge, MiniMax providers. Tested in production.
- **HTTP API** — `/v1/chat/completions`, `/v1/agents`, `/v1/skills`, etc. Tested in production. Interactive Swagger UI at `/docs`.
- **API key management** — Multi-key auth with RBAC scopes, SHA-256 hashed storage, show-once pattern, optional expiry, revocation. HTTP + WebSocket CRUD. Web UI for management.
- **Hooks system** — Event-driven hooks with command evaluators (shell exit code) and agent evaluators (delegate to reviewer). Blocking gates with auto-retry and recursion-safe evaluation.
- **Media tools** — `create_image` (DashScope, MiniMax), `create_audio` (OpenAI, ElevenLabs, MiniMax, Suno), `create_video` (MiniMax, Veo), `read_document` (Gemini File API), `read_image`, `read_audio`, `read_video`. Persistent media storage with lazy-loaded MediaRef.
- **Additional provider modes** — Claude CLI (Anthropic via stdio + MCP bridge), Codex (OpenAI gpt-5.3-codex via OAuth).
- **Google Cloud Vertex AI provider** — Enterprise GCP integration via Vertex OpenAI-compatible endpoint. OAuth2 service account auth (inline JSON or file path) with automatic token refresh, plus Application Default Credentials (ADC) for GKE/Cloud Run/Compute Engine. Regional endpoints for data residency (e.g. `asia-southeast1`, `us-central1`). Addresses [#576](https://github.com/nextlevelbuilder/goclaw/issues/576).
- **Knowledge graph** — LLM-powered entity extraction, graph traversal, force-directed visualization, and `knowledge_graph_search` agent tool.
- **Memory management** — Admin dashboard for memory documents (CRUD, semantic search, chunk/embedding details, bulk re-indexing).
- **Persistent pending messages** — Channel messages persisted to PostgreSQL with auto-compaction (LLM summarization) and monitoring dashboard.
- **Heartbeat system** — Periodic agent check-ins via HEARTBEAT.md checklists with suppress-on-OK, active hours, retry logic, and channel delivery.

### Implemented but Not Fully Tested

- **Slack** — Channel integration implemented, not yet validated with real users.
- **Other messaging channels** — Discord, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp channel adapters are implemented but have not been tested end-to-end in production. Only Telegram has been validated with real users.
- **OpenTelemetry export** — OTLP gRPC/HTTP exporter implemented (build-tag gated). In-app tracing works; external OTel export not validated in production.
- **Tailscale integration** — tsnet listener implemented (build-tag gated). Not tested in a real deployment.
- **Redis cache** — Optional distributed cache backend (build-tag gated). Not tested in production.
- **Browser pairing** — Pairing code flow implemented with CLI and web UI approval. Basic flow tested but not validated at scale.
