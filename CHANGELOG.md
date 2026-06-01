# Changelog

All notable changes to GoClaw are documented here. For full documentation, see [docs.goclaw.sh](https://docs.goclaw.sh).

## Unreleased

### Added

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

- **ChatGPT Subscription (OAuth)** — default model and backend-owned model catalog
  now prefer `gpt-5.5`, with reasoning metadata and context-window defaults updated
  for provider-first model selection.

### Fixed

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
