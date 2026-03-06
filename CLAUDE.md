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
- **Context propagation:** `store.WithAgentType(ctx)`, `store.WithUserID(ctx)`, `store.WithAgentID(ctx)`
- **WebSocket protocol (v3):** Frame types `req`/`res`/`event`. First request must be `connect`
- **Config:** JSON5 at `GOCLAW_CONFIG` env. Secrets in `.env.local` or env vars, never in config.json
- **Security:** Rate limiting, input guard (detection-only), CORS, shell deny patterns, SSRF protection, path traversal prevention, AES-256-GCM encryption. All security logs: `slog.Warn("security.*")`
- **Telegram formatting:** LLM output → `SanitizeAssistantContent()` → `markdownToTelegramHTML()` → `chunkHTML()` → `sendHTML()`. Tables rendered as ASCII in `<pre>` tags

## Running

```bash
go build -o goclaw . && ./goclaw onboard && source .env.local && ./goclaw
./goclaw migrate up                 # DB migrations
go test -v ./tests/integration/     # Integration tests

cd ui/web && pnpm install && pnpm dev   # Web dashboard (dev)
```
