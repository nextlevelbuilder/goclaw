# GoClaw Gateway

PostgreSQL multi-tenant AI agent gateway with WebSocket RPC + HTTP API.

## Tech Stack

**Backend:** Go 1.25, Cobra CLI, gorilla/websocket, pgx/v5 (database/sql, no ORM), golang-migrate, go-rod/rod, telego (Telegram)
**Web UI:** React 19, Vite 6, TypeScript, Tailwind CSS 4, Radix UI, Zustand, React Router 7. Located in `ui/web/`. **Use `pnpm` (not npm).**
**Database:** PostgreSQL 15+ with pgvector. Raw SQL with `$1, $2` positional params. Nullable columns: `*string`, `*time.Time`, etc.

## Project Structure

```
cmd/                          CLI commands, gateway startup, onboard wizard, migrations
internal/
├── gateway/                  WS + HTTP server, client, method router
│   └── methods/              RPC handlers (chat, agents, sessions, config, skills, cron, pairing)
├── agent/                    Agent loop (think→act→observe), router, resolver, input guard
├── providers/                LLM providers: Anthropic (native HTTP+SSE), OpenAI-compat (HTTP+SSE)
├── tools/                    Tool registry, filesystem, exec, web, memory, subagent, MCP bridge
├── store/                    Store interfaces + pg/ (PostgreSQL) implementations
├── bootstrap/                System prompt files (SOUL.md, IDENTITY.md) + seeding + per-user seed
├── config/                   Config loading (JSON5) + env var overlay
├── channels/                 Channel manager: Telegram, Feishu/Lark, Zalo, Discord, WhatsApp
├── http/                     HTTP API (/v1/chat/completions, /v1/agents, /v1/skills, etc.)
├── skills/                   SKILL.md loader + BM25 search
├── memory/                   Memory system (pgvector)
├── tracing/                  LLM call tracing + optional OTel export (build-tag gated)
├── scheduler/                Lane-based concurrency (main/subagent/cron)
├── cron/                     Cron scheduling (at/every/cron expr)
├── permissions/              RBAC (admin/operator/viewer)
├── pairing/                  Browser pairing (8-char codes)
├── crypto/                   AES-256-GCM encryption for API keys
├── sandbox/                  Docker-based code sandbox
├── tts/                      Text-to-Speech (OpenAI, ElevenLabs, Edge, MiniMax)
├── i18n/                     Message catalog: T(locale, key, args...) + per-locale catalogs (en/vi/zh)
pkg/protocol/                 Wire types (frames, methods, errors, events)
pkg/browser/                  Browser automation (Rod + CDP)
migrations/                   PostgreSQL migration files
ui/web/                       React SPA (pnpm, Vite, Tailwind, Radix UI)
```

## Key Patterns

- **Store layer:** Interface-based (`store.SessionStore`, `store.AgentStore`, etc.) with pg/ (PostgreSQL) implementations. Uses `database/sql` + `pgx/v5/stdlib`, raw SQL, `execMapUpdate()` helper in `pg/helpers.go`
- **Agent types:** `open` (per-user context, 7 files) vs `predefined` (shared context + USER.md per-user)
- **Context files:** `agent_context_files` (agent-level) + `user_context_files` (per-user), routed via `ContextFileInterceptor`
- **Providers:** Anthropic (native HTTP+SSE) and OpenAI-compat (generic). Both use `RetryDo()` for retries. Loads from `llm_providers` table with encrypted API keys
- **Agent loop:** `RunRequest` → think→act→observe → `RunResult`. Events: `run.started`, `run.completed`, `chunk`, `tool.call`, `tool.result`. Auto-summarization at >75% context
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
```

## Post-Implementation Checklist

After implementing or modifying Go code, run these checks:

```bash
go fix ./...                        # Apply Go version upgrades (run before commit)
go build ./...                      # Compile check
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

## Deep Dive Docs

Detailed documentation lives in `docs/`. Read these files when working on the corresponding subsystem:

| File | Topic | Read when... |
|------|-------|-------------|
| `docs/00-architecture-overview.md` | System architecture, component map, data flow | Understanding overall design or adding new subsystems |
| `docs/01-agent-loop.md` | Agent loop (think→act→observe), RunRequest/RunResult, auto-summarization | Modifying agent execution, context management, or event flow |
| `docs/02-providers.md` | LLM providers (Anthropic, OpenAI-compat), streaming, retry logic | Adding/modifying LLM providers or changing streaming behavior |
| `docs/03-tools-system.md` | Tool registry, built-in tools, MCP bridge, tool definitions | Adding new tools, modifying tool behavior, or MCP integration |
| `docs/04-gateway-protocol.md` | WebSocket v3 protocol, frame types, method routing | Changing WS protocol, adding RPC methods, or debugging connections |
| `docs/05-channels-messaging.md` | Channel manager (Telegram, Feishu, Zalo, Discord, WhatsApp), message formatting | Adding channels, fixing message rendering, or formatting issues |
| `docs/06-store-data-model.md` | Store interfaces, PostgreSQL schema, data model, migrations | Modifying DB schema, adding stores, or writing migrations |
| `docs/07-bootstrap-skills-memory.md` | System prompt bootstrap, SKILL.md loader, BM25 search, memory (pgvector) | Changing system prompts, skill loading, or memory retrieval |
| `docs/08-scheduling-cron.md` | Lane-based scheduler, cron expressions (at/every/cron) | Modifying scheduling, concurrency lanes, or cron jobs |
| `docs/09-security.md` | Security: rate limiting, input guard, CORS, SSRF, encryption | Security review, adding protection, or auditing vulnerabilities |
| `docs/10-tracing-observability.md` | LLM call tracing, OTel export, build-tag gating | Adding tracing, debugging performance, or observability setup |
| `docs/11-agent-teams.md` | Multi-agent teams, delegation, team orchestration | Implementing agent teams or modifying team communication |
| `docs/12-extended-thinking.md` | Extended thinking / chain-of-thought support | Adding or modifying extended thinking features |
| `docs/13-ws-team-events.md` | WebSocket events for agent teams | Modifying team event flow or debugging team WS communication |
| `docs/14-skills-runtime.md` | Skill runtime execution environment | Changing how skills execute or adding runtime capabilities |
| `docs/15-core-skills-system.md` | Core built-in skills architecture | Modifying core skills or adding new built-in skills |
| `docs/16-skill-publishing.md` | Skill publishing and distribution | Implementing skill publishing or marketplace features |
| `docs/17-mcp-builder-skill.md` | MCP Builder skill for creating MCP servers | Working on MCP builder or MCP server generation |
| `docs/17-changelog.md` | Recent changes and version history | Understanding recent modifications |
| `docs/18-http-api.md` | HTTP API endpoints (/v1/*) | Adding/modifying HTTP endpoints or API behavior |
| `docs/19-websocket-rpc.md` | WebSocket RPC method reference | Adding RPC methods or debugging WS communication |
| `docs/20-api-keys-auth.md` | API key management, authentication, AES-256-GCM encryption | Modifying auth, API keys, or encryption |
