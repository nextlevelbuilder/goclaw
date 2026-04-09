# GoClaw Gateway

PostgreSQL multi-tenant AI agent gateway with WebSocket RPC + HTTP API.

## Language

Always respond in the same language as the user's prompt. If the user writes in Vietnamese, respond in Vietnamese. If in English, respond in English. Match the user's language naturally.

## Tech Stack

**Backend:** Go 1.26, Cobra CLI, gorilla/websocket, pgx/v5 (database/sql, no ORM), golang-migrate, go-rod/rod, telego (Telegram)
**Web UI:** React 19, Vite 6, TypeScript, Tailwind CSS 4, Radix UI, Zustand, React Router 7. Located in `ui/web/`. **Use `pnpm` (not npm).**
**Desktop UI:** React 19, Vite 6, TypeScript, Tailwind CSS 4, Zustand, Framer Motion. Located in `ui/desktop/frontend/`. **Use `pnpm`.**
**Desktop App:** Wails v2 (`//go:build sqliteonly`). Located in `ui/desktop/`. Embeds gateway + React frontend in single binary.
**Database:** PostgreSQL 18 with pgvector (standard). SQLite via `modernc.org/sqlite` (desktop/lite). Raw SQL with `$1, $2` (PG) or `?` (SQLite) positional params. Nullable columns: `*string`, `*time.Time`, etc.

## Project Structure

```
cmd/                          CLI commands, gateway startup, onboard wizard, migrations
internal/
├── agent/                    Agent loop (think→act→observe), router, resolver, input guard
├── bootstrap/                System prompt files (SOUL.md, IDENTITY.md) + seeding + per-user seed
├── bus/                      Event bus system
├── cache/                    Caching layer
├── channels/                 Channel manager: Telegram, Feishu/Lark, Zalo, Discord, WhatsApp
├── config/                   Config loading (JSON5) + env var overlay
├── crypto/                   AES-256-GCM encryption for API keys
├── cron/                     Cron scheduling (at/every/cron expr)
├── gateway/                  WS + HTTP server, client, method router
│   └── methods/              RPC handlers (chat, agents, sessions, config, skills, cron, pairing)
├── hooks/                    Hook system for extensibility
├── http/                     HTTP API (/v1/chat/completions, /v1/agents, /v1/skills, etc.)
├── i18n/                     Message catalog: T(locale, key, args...) + per-locale catalogs (en/vi/zh)
├── knowledgegraph/           Knowledge graph storage and traversal
├── mcp/                      Model Context Protocol bridge/server
├── media/                    Media handling utilities
├── memory/                   Memory system (pgvector)
├── oauth/                    OAuth authentication
├── permissions/              RBAC (admin/operator/viewer)
├── providers/                LLM providers: Anthropic (native HTTP+SSE), OpenAI-compat (HTTP+SSE), DashScope (Alibaba Qwen), Claude CLI (stdio+MCP bridge), ACP (Anthropic Console Proxy), Codex (OpenAI)
├── sandbox/                  Docker-based code sandbox
├── scheduler/                Lane-based concurrency (main/subagent/cron)
├── sessions/                 Session management
├── skills/                   SKILL.md loader + BM25 search
├── store/                    Store interfaces + pg/ (PostgreSQL) implementations
├── tasks/                    Task management
├── tools/                    Tool registry, filesystem, exec, web, memory, subagent, MCP bridge
├── tracing/                  LLM call tracing + optional OTel export (build-tag gated)
├── tts/                      Text-to-Speech (OpenAI, ElevenLabs, Edge, MiniMax)
├── upgrade/                  Database schema version tracking
pkg/protocol/                 Wire types (frames, methods, errors, events)
pkg/browser/                  Browser automation (Rod + CDP)
migrations/                   PostgreSQL migration files
ui/web/                       React SPA (pnpm, Vite, Tailwind, Radix UI)
```

## Key Patterns

- **Store layer:** Interface-based (`store.SessionStore`, `store.AgentStore`, etc.) with pg/ (PostgreSQL) implementations. Uses `database/sql` + `pgx/v5/stdlib`, raw SQL, `execMapUpdate()` helper in `pg/helpers.go`
- **Agent types:** `open` (per-user context, 7 files) vs `predefined` (shared context + USER.md per-user)
- **Context files:** `agent_context_files` (agent-level) + `user_context_files` (per-user), routed via `ContextFileInterceptor`
- **Providers:** Anthropic (native HTTP+SSE), OpenAI-compat (HTTP+SSE), DashScope (Alibaba Qwen), Claude CLI (stdio+MCP bridge), ACP (Anthropic Console Proxy), Codex (OpenAI). All use `RetryDo()` for retries. Loads from `llm_providers` table with encrypted API keys
- **Agent loop:** `RunRequest` → think→act→observe → `RunResult`. Events: `run.started`, `run.completed`, `chunk`, `tool.call`, `tool.result`. Auto-summarization at >85% context (token-based only)
- **Context propagation:** `store.WithAgentType(ctx)`, `store.WithUserID(ctx)`, `store.WithAgentID(ctx)`, `store.WithLocale(ctx)`
- **WebSocket protocol (v3):** Frame types `req`/`res`/`event`. First request must be `connect`
- **Config:** JSON5 at `GOCLAW_CONFIG` env. Secrets in `.env.local` or env vars, never in config.json
- **Security:** Rate limiting, input guard (detection-only), CORS, shell deny patterns, SSRF protection, path traversal prevention, AES-256-GCM encryption. All security logs: `slog.Warn("security.*")`
- **Telegram formatting:** LLM output → `SanitizeAssistantContent()` → `markdownToTelegramHTML()` → `chunkHTML()` → `sendHTML()`. Tables rendered as ASCII in `<pre>` tags
- **i18n:** Web UI uses `i18next` with namespace-split locale files in `ui/web/src/i18n/locales/{lang}/`. Backend uses `internal/i18n` message catalog with `i18n.T(locale, key, args...)`. Locale propagated via `store.WithLocale(ctx)` — WS `connect` param `locale`, HTTP `Accept-Language` header. Supported: en (default), vi, zh. New user-facing strings: add key to `internal/i18n/keys.go`, add translations to all 3 catalog files. New UI strings: add key to all 3 locale dirs. Bootstrap templates (SOUL.md, etc.) stay English-only (LLM consumption).

## Running

```bash
go build -o goclaw . && ./goclaw onboard && source .env.local && ./goclaw
./goclaw migrate up                 # DB migrations
go test -v ./tests/integration/     # Integration tests

cd ui/web && pnpm install && pnpm dev   # Web dashboard (dev)

# Desktop (Wails + SQLite)
cd ui/desktop && wails dev -tags sqliteonly  # Dev mode with hot reload (direct)
make desktop-dev                             # Same as above via Makefile
make desktop-build VERSION=0.1.0             # Build .app (macOS) or .exe (Windows)
make desktop-dmg VERSION=0.1.0               # Create .dmg installer (macOS only)
```

## CI/CD & Releases

### Workflows

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `ci.yaml` | push main, PR→main/dev | Go build+test+vet, Web build |
| `release.yaml` | push main | semantic-release → binaries + Docker (4 variants + web) + Discord |
| `release-beta.yaml` | tag `v*-beta*` / `v*-rc*` | Beta binaries + Docker + GitHub prerelease |
| `release-desktop.yaml` | tag `lite-v*` | Desktop app (macOS+Windows), auto prerelease for `-beta`/`-rc` tags |

### Creating Releases

**Standard release** — merge `dev` → `main`. `go-semantic-release` auto-creates version from conventional commits.

**Beta release** (from dev):
```bash
git tag v2.67.0-beta.1 && git push origin v2.67.0-beta.1   # standard beta
git tag lite-v1.2.0-beta.1 && git push origin lite-v1.2.0-beta.1  # lite beta
```

**Desktop release:**
```bash
git tag lite-v1.1.0 && git push origin lite-v1.1.0   # stable
git tag lite-v1.1.0-beta.1 && git push origin lite-v1.1.0-beta.1  # beta (prerelease)
```

### Docker Images

Published to GHCR (`ghcr.io/nextlevelbuilder/goclaw`) and Docker Hub (`digitop/goclaw`).

| Variant | Tag | Contents |
|---------|-----|----------|
| latest | `:latest`, `:vX.Y.Z` | Backend + web UI + Python |
| base | `:base`, `:vX.Y.Z-base` | Backend only, no UI/runtimes |
| full | `:full`, `:vX.Y.Z-full` | All runtimes + skills pre-installed |
| otel | `:otel`, `:vX.Y.Z-otel` | Latest + OpenTelemetry tracing |
| web | `-web:latest` | Standalone web UI (Nginx) |
| beta | `:beta`, `:vX.Y.Z-beta.N` | Beta builds from dev |

### Tag Pattern Safety

- `release.yaml`: branch-triggered (push main) → `go-semantic-release` creates clean `vX.Y.Z` tags
- `release-beta.yaml`: tag-triggered (`v*-beta*`, `v*-rc*`) — never matches clean semver
- `release-desktop.yaml`: tag-triggered (`lite-v*`) — `lite-` prefix prevents overlap
- No workflow triggers overlap — each tag pattern is distinct

## Desktop Edition (Lite)

- **Build tag:** `//go:build sqliteonly` — desktop binary includes only SQLite, no PostgreSQL
- **Edition system:** `internal/edition/edition.go` — `Lite` preset auto-selected for SQLite backend. Check `edition.Current()` for feature limits
- **Entry point:** `ui/desktop/main.go` + `ui/desktop/app.go` — Wails bindings, embedded gateway
- **Secrets:** OS keyring (`go-keyring`) with file fallback at `~/.goclaw/secrets/`
- **Data dir:** `~/.goclaw/data/` (SQLite DB, configs)
- **Workspace:** `~/.goclaw/workspace/` (agent files, team workspace)
- **Port:** 18790 (localhost only), configurable via `GOCLAW_PORT`
- **WS params:** All WS method params use **camelCase** (`teamId`, `taskId`, `sessionKey`) — match Go struct `json:"..."` tags
- **Version:** `cmd.Version` set via `-ldflags` at build time. Frontend calls `wails.getVersion()`
- **Auto-update:** `internal/updater/updater.go` checks GitHub Releases for `lite-v*` tags. Frontend `UpdateBanner` shows notification
- **Releases:** Tag `lite-v*` triggers `.github/workflows/release-desktop.yaml` → builds macOS (arm64+amd64) + Windows → GitHub Release
- **Install scripts:** `scripts/install-lite.sh` (macOS), `scripts/install-lite.ps1` (Windows PowerShell)
- **Lite limits:** 5 agents, 1 team, 5 members, 50 sessions. No channels, heartbeat, file storage UI, skill self-manage, KG, RBAC, multi-tenant
- **Tool gating:** `TeamActionPolicy` in `internal/tools/team_action_policy.go` — lite blocks comment/review/approve/reject/attach/ask_user. `skill_manage`/`publish_skill` not registered in lite
- **File serving:** 2-layer path isolation in `internal/http/files.go` — workspace boundary (all editions) + tenant scope (standard only with RBAC)

## Post-Implementation Checklist

After implementing or modifying Go code, run these checks:

```bash
go fix ./...                        # Apply Go version upgrades (run before commit)
go build ./...                      # Compile check (PG build)
go build -tags sqliteonly ./...     # Compile check (Desktop/SQLite build)
go vet ./...                        # Static analysis
go test -race ./tests/integration/  # Integration tests with race detector
```

Go conventions to follow:
- Use `errors.Is(err, sentinel)` instead of `err == sentinel`
- Use `switch/case` instead of `if/else if` chains on the same variable
- Use `append(dst, src...)` instead of loop-based append
- Always handle errors; don't ignore return values
- **Migrations:** When adding a new SQL migration file in `migrations/`, bump `RequiredSchemaVersion` in `internal/upgrade/version.go` to match the new migration number
- **i18n strings:** When adding user-facing error messages, add key to `internal/i18n/keys.go` and translations to `catalog_en.go`, `catalog_vi.go`, `catalog_zh.go`. For UI strings, add to all locale JSON files in `ui/web/src/i18n/locales/{en,vi,zh}/`
- **SQL safety:** When implementing or modifying SQL store code (`store/pg/*.go`), always verify: (1) All user inputs use parameterized queries (`$1, $2, ...`), never string concatenation — prevents SQL injection. (2) Queries are optimized — no N+1 queries, no unnecessary full table scans. (3) WHERE clauses, JOINs, and ORDER BY columns use existing indices — check migration files for available indexes
- **DB query reuse:** Before adding a new DB query for key entities (teams, agents, sessions, users), check if the same data is already fetched earlier in the current flow/pipeline. Prefer passing resolved data through context, event payloads, or function params rather than re-querying. Duplicate queries waste DB resources and add latency
- **Solution design:** When designing a fix or feature, identify the root cause first — don't just patch symptoms. Think through production scenarios (high concurrency, multi-tenant isolation, failure cascades, long-running sessions) to ensure the solution holds up. Prefer explicit configuration over runtime heuristics. Prefer the simplest solution that addresses the root cause directly

## Mobile UI/UX Rules

When implementing or modifying web UI components, follow these rules to ensure mobile compatibility:

- **Viewport height:** Use `h-dvh` (dynamic viewport height), never `h-screen`. `h-screen` causes content to hide behind mobile browser chrome and virtual keyboards
- **Input font-size:** All `<input>`, `<textarea>`, `<select>` must use `text-base md:text-sm` (16px on mobile). Font-size < 16px triggers iOS Safari auto-zoom on focus
- **Safe areas:** Root layout must use `viewport-fit=cover` meta tag. Apply `safe-top`, `safe-bottom`, `safe-left`, `safe-right` utility classes on edge-anchored elements (app shell, sidebar, toasts, chat input) for notched devices
- **Touch targets:** Icon buttons must have ≥44px hit area on touch devices. CSS in `index.css` uses `@media (pointer: coarse)` with `::after` pseudo-elements to expand targets
- **Tables:** Always wrap `<table>` in `<div className="overflow-x-auto">` and set `min-w-[600px]` on the table for horizontal scroll on narrow screens
- **Grid layouts:** Use mobile-first responsive grids: `grid-cols-1 sm:grid-cols-2 lg:grid-cols-N`. Never use fixed `grid-cols-N` without a mobile breakpoint
- **Dialogs:** Full-screen on mobile with slide-up animation (`max-sm:inset-0`), centered with zoom on desktop (`sm:max-w-lg`). Handled in `ui/dialog.tsx`
- **Virtual keyboard:** Chat input uses `useVirtualKeyboard()` hook + `var(--keyboard-height, 0px)` CSS var to stay above the keyboard
- **Scroll behavior:** Use `overscroll-contain` on scrollable areas to prevent background scroll. Auto-scroll: smooth for incoming messages, instant on user send
- **Landscape:** Use `landscape-compact` class on top bars to reduce padding in phone landscape orientation (`max-height: 500px`)
- **Portal dropdowns in dialogs:** Custom dropdown components using `createPortal(content, document.body)` MUST add `pointer-events-auto` class to the dropdown element. Radix Dialog sets `pointer-events: none` on `document.body` — without this class, dropdowns are unclickable. Radix-native portals (Select, Popover) handle this automatically
- **Timezone:** User timezone stored in Zustand (`useUiStore`). Charts use `formatBucketTz()` from `lib/format.ts` with native `Intl.DateTimeFormat` — no date-fns-tz dependency
- **ErrorBoundary key:** `AppLayout` uses `<ErrorBoundary key={stableErrorBoundaryKey(pathname)}>` which strips dynamic segments (`/chat/session-A` → `/chat`). NEVER use `key={location.pathname}` on ErrorBoundary/Suspense wrapping `<Outlet>` — it causes full page remount on param changes. Pages with sub-navigation (chat sessions, detail pages) must share a stable key
- **Route params as source of truth:** For pages with URL params (e.g. `/chat/:sessionKey`), derive state from `useParams()` — do NOT duplicate into `useState`. Dual state causes race conditions between `setState` and `navigate()` leading to UI flash (state bounces: B→A→B). Use optional params (`/chat/:sessionKey?`) instead of two separate routes
