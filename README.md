<p align="center">
  <img src="_statics/goclaw-logo.svg" alt="GoClaw" height="200" />
</p>

<p align="center"><strong>Multi-Tenant AI Agent Platform</strong></p>

<p align="center">
Multi-agent AI gateway built in Go. 20+ LLM providers. 7 channels. Multi-tenant PostgreSQL.<br/>
Single binary. Production-tested. Agents that orchestrate for you.
</p>

<p align="center">
  <a href="https://docs.goclaw.sh">Documentation</a> •
  <a href="https://docs.goclaw.sh/#quick-start">Quick Start</a> •
  <a href="https://x.com/nlb_io">Twitter / X</a>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go_1.26-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go" /></a>
  <a href="https://www.postgresql.org/"><img src="https://img.shields.io/badge/PostgreSQL_18-316192?style=flat-square&logo=postgresql&logoColor=white" alt="PostgreSQL" /></a>
  <a href="https://www.docker.com/"><img src="https://img.shields.io/badge/Docker-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker" /></a>
  <a href="https://developer.mozilla.org/en-US/docs/Web/API/WebSocket"><img src="https://img.shields.io/badge/WebSocket-010101?style=flat-square&logo=socket.io&logoColor=white" alt="WebSocket" /></a>
  <a href="https://opentelemetry.io/"><img src="https://img.shields.io/badge/OpenTelemetry-000000?style=flat-square&logo=opentelemetry&logoColor=white" alt="OpenTelemetry" /></a>
  <a href="https://www.anthropic.com/"><img src="https://img.shields.io/badge/Anthropic-191919?style=flat-square&logo=anthropic&logoColor=white" alt="Anthropic" /></a>
  <a href="https://openai.com/"><img src="https://img.shields.io/badge/OpenAI_Compatible-412991?style=flat-square&logo=openai&logoColor=white" alt="OpenAI" /></a>
  <img src="https://img.shields.io/badge/License-CC%20BY--NC%204.0-lightgrey?style=flat-square" alt="License: CC BY-NC 4.0" />
</p>

A Go port of [OpenClaw](https://github.com/openclaw/openclaw) with enhanced security, multi-tenant PostgreSQL, and production-grade observability.

🌐 **Languages:**
[🇨🇳 简体中文](_readmes/README.zh-CN.md) ·
[🇯🇵 日本語](_readmes/README.ja.md) ·
[🇰🇷 한국어](_readmes/README.ko.md) ·
[🇻🇳 Tiếng Việt](_readmes/README.vi.md) ·
[🇵🇭 Tagalog](_readmes/README.tl.md) ·
[🇪🇸 Español](_readmes/README.es.md) ·
[🇧🇷 Português](_readmes/README.pt.md) ·
[🇮🇹 Italiano](_readmes/README.it.md) ·
[🇩🇪 Deutsch](_readmes/README.de.md) ·
[🇫🇷 Français](_readmes/README.fr.md) ·
[🇸🇦 العربية](_readmes/README.ar.md) ·
[🇮🇳 हिन्दी](_readmes/README.hi.md) ·
[🇷🇺 Русский](_readmes/README.ru.md) ·
[🇧🇩 বাংলা](_readmes/README.bn.md) ·
[🇮🇱 עברית](_readmes/README.he.md) ·
[🇵🇱 Polski](_readmes/README.pl.md) ·
[🇨🇿 Čeština](_readmes/README.cs.md) ·
[🇳🇱 Nederlands](_readmes/README.nl.md) ·
[🇹🇷 Türkçe](_readmes/README.tr.md) ·
[🇺🇦 Українська](_readmes/README.uk.md) ·
[🇮🇩 Bahasa Indonesia](_readmes/README.id.md) ·
[🇹🇭 ไทย](_readmes/README.th.md) ·
[🇵🇰 اردو](_readmes/README.ur.md) ·
[🇷🇴 Română](_readmes/README.ro.md) ·
[🇸🇪 Svenska](_readmes/README.sv.md) ·
[🇬🇷 Ελληνικά](_readmes/README.el.md) ·
[🇭🇺 Magyar](_readmes/README.hu.md) ·
[🇫🇮 Suomi](_readmes/README.fi.md) ·
[🇩🇰 Dansk](_readmes/README.da.md) ·
[🇳🇴 Norsk](_readmes/README.nb.md)

## What Makes It Different

- **Agent Teams & Orchestration** — Teams with shared task boards, inter-agent delegation (sync/async), and hybrid agent discovery
- **Multi-Tenant PostgreSQL** — Per-user workspaces, per-user context files, encrypted API keys (AES-256-GCM), isolated sessions
- **Single Binary** — ~25 MB static Go binary, no Node.js runtime, <1s startup, runs on a $5 VPS
- **Production Security** — 5-layer permission system (gateway auth → global tool policy → per-agent → per-channel → owner-only) plus rate limiting, prompt injection detection, SSRF protection, shell deny patterns, and AES-256-GCM encryption
- **20+ LLM Providers** — Anthropic (native HTTP+SSE with prompt caching), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope, Bailian, Zai, Ollama, Ollama Cloud, Claude CLI, Codex, ACP, and any OpenAI-compatible endpoint
- **7 Messaging Channels** — Telegram, Discord, Slack, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp
- **Extended Thinking** — Per-provider thinking mode (Anthropic budget tokens, OpenAI reasoning effort, DashScope thinking budget) with streaming support
- **Heartbeat System** — Periodic agent check-ins via HEARTBEAT.md checklists with suppress-on-OK, active hours, retry logic, and channel delivery
- **Scheduling & Cron** — `at`, `every`, and cron expressions for automated agent tasks with lane-based concurrency
- **Observability** — Built-in LLM call tracing with spans and prompt cache metrics, optional OpenTelemetry OTLP export

## Claw Ecosystem

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Language        | TypeScript      | Rust     | Go       | **Go**                                  |
| Binary size     | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (base) / **~36 MB** (+ OTel) |
| Docker image    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (idle)      | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Startup         | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Target hardware | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

| Feature                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| MCP integration            | — (uses ACP)                         | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Agent teams                | —                                    | —                                            | —                                     | ✅ Task board + mailbox        |
| Security hardening         | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Basic (workspace restrict, exec deny) | ✅ 5-layer defense             |
| OTel observability         | ✅ (opt-in extension)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| Prompt caching             | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Knowledge graph            | —                                    | —                                            | —                                     | ✅ LLM extraction + traversal  |
| Skill system               | ✅ Embeddings/semantic               | ✅ SKILL.md + TOML                           | ✅ Basic                              | ✅ BM25 + pgvector hybrid      |
| Lane-based scheduler       | ✅                                   | Bounded concurrency                          | —                                     | ✅ (main/subagent/team/cron)   |
| Messaging channels         | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Companion apps             | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web dashboard                  |
| Live Canvas / Voice        | ✅ (A2UI + TTS/STT)                  | —                                            | Voice transcription                   | TTS (4 providers)              |
| LLM providers              | 10+                                  | 8 native + 29 compat                         | 13+                                   | **20+**                        |
| Per-user workspaces        | ✅ (file-based)                      | —                                            | —                                     | ✅ (PostgreSQL)                |
| Encrypted secrets          | — (env vars only)                    | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM in DB           |

## Architecture

<p align="center">
  <img src="_statics/architecture.jpg" alt="GoClaw Architecture" width="800" />
</p>

<p align="center">
  <img src="_statics/goclaw_multi_tenant.png" alt="GoClaw Multi-Tenant" width="800" />
</p>

## Quick Start

**Prerequisites:** Go 1.26+, PostgreSQL 18 with pgvector, Docker (optional)

### From Source

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git && cd goclaw
make build
./goclaw onboard        # Interactive setup wizard
source .env.local && ./goclaw
```

### With Docker

```bash
# Generate .env with auto-generated secrets
chmod +x prepare-env.sh && ./prepare-env.sh

# Add at least one GOCLAW_*_API_KEY to .env, then:
make up

# Web Dashboard at http://localhost:3000
# Health check: curl http://localhost:18790/health
```

`make up` creates a Docker network, embeds the correct version from git tags, builds and starts all services, and runs database migrations automatically.

**Common commands:**

```bash
make up                # Start all services (build + migrate)
make down              # Stop all services
make logs              # Tail logs (goclaw service)
make reset             # Wipe volumes and rebuild from scratch
```

**Optional services** — enable with `WITH_*` flags:

| Flag | Service | What it does |
|------|---------|-------------|
| `WITH_BROWSER=1` | Headless Chrome | Enables `browser` tool for web scraping, screenshots, automation |
| `WITH_OTEL=1` | Jaeger | OpenTelemetry tracing UI for debugging LLM calls and latency |
| `WITH_SANDBOX=1` | Docker sandbox | Isolated container for running untrusted code from agents |
| `WITH_TAILSCALE=1` | Tailscale | Expose gateway over Tailscale private network |
| `WITH_REDIS=1` | Redis | Redis-backed caching layer |

Flags can be combined and work with all commands:

```bash
# Start with browser automation and tracing
make up WITH_BROWSER=1 WITH_OTEL=1

# Stop everything including optional services
make down WITH_BROWSER=1 WITH_OTEL=1
```

When `GOCLAW_*_API_KEY` environment variables are set, the gateway auto-onboards without interactive prompts — detects provider, runs migrations, and seeds default data.

> For detailed configuration and Docker image tags, see the [Deployment Guide](https://docs.goclaw.sh/#deploy-docker-compose).

## Multi-Agent Orchestration

GoClaw supports agent teams and inter-agent delegation — each agent runs with its own identity, tools, LLM provider, and context files.

### Agent Delegation

<p align="center">
  <img src="_statics/agent-delegation.jpg" alt="Agent Delegation" width="700" />
</p>

| Mode | How it works | Best for |
|------|-------------|----------|
| **Sync** | Agent A asks Agent B and **waits** for the answer | Quick lookups, fact checks |
| **Async** | Agent A asks Agent B and **moves on**. B announces later | Long tasks, reports, deep analysis |

Agents communicate through explicit **permission links** with direction control (`outbound`, `inbound`, `bidirectional`) and concurrency limits at both per-link and per-agent levels.

### Agent Teams

<p align="center">
  <img src="_statics/agent-teams.jpg" alt="Agent Teams Workflow" width="800" />
</p>

- **Shared task board** — Create, claim, complete, search tasks with `blocked_by` dependencies
- **Team mailbox** — Direct peer-to-peer messaging and broadcasts
- **Tools**: `team_tasks` for task management, `team_message` for mailbox

> For delegation details, permission links, and concurrency control, see the [Agent Teams docs](https://docs.goclaw.sh/#teams-what-are-teams).

## Built-in Tools

| Tool               | Group         | Description                                                  |
| ------------------ | ------------- | ------------------------------------------------------------ |
| `read_file`        | fs            | Read file contents (with virtual FS routing)                 |
| `write_file`       | fs            | Write/create files                                           |
| `edit_file`        | fs            | Apply targeted edits to existing files                       |
| `list_files`       | fs            | List directory contents                                      |
| `search`           | fs            | Search file contents by pattern                              |
| `glob`             | fs            | Find files by glob pattern                                   |
| `exec`             | runtime       | Execute shell commands (with approval workflow)              |
| `web_search`       | web           | Search the web (Brave, DuckDuckGo)                           |
| `web_fetch`        | web           | Fetch and parse web content                                  |
| `memory_search`    | memory        | Search long-term memory (FTS + vector)                       |
| `memory_get`       | memory        | Retrieve memory entries                                      |
| `skill_search`     | —             | Search skills (BM25 + embedding hybrid)                      |
| `knowledge_graph_search` | memory  | Search entities and traverse knowledge graph relationships   |
| `create_image`     | media         | Image generation (DashScope, MiniMax)                        |
| `create_audio`     | media         | Audio generation (OpenAI, ElevenLabs, MiniMax, Suno)         |
| `create_video`     | media         | Video generation (MiniMax, Veo)                              |
| `read_document`    | media         | Document reading (Gemini File API, provider chain)           |
| `read_image`       | media         | Image analysis                                               |
| `read_audio`       | media         | Audio transcription and analysis                             |
| `read_video`       | media         | Video analysis                                               |
| `message`          | messaging     | Send messages to channels                                    |
| `tts`              | —             | Text-to-Speech synthesis                                     |
| `spawn`            | —             | Spawn a subagent                                             |
| `subagents`        | sessions      | Control running subagents                                    |
| `team_tasks`       | teams         | Shared task board (list, create, claim, complete, search)    |
| `team_message`     | teams         | Team mailbox (send, broadcast, read)                         |
| `sessions_list`    | sessions      | List active sessions                                         |
| `sessions_history` | sessions      | View session history                                         |
| `sessions_send`    | sessions      | Send message to a session                                    |
| `sessions_spawn`   | sessions      | Spawn a new session                                          |
| `session_status`   | sessions      | Check session status                                         |
| `cron`             | automation    | Schedule and manage cron jobs                                |
| `gateway`          | automation    | Gateway administration                                       |
| `browser`          | ui            | Browser automation (navigate, click, type, screenshot)       |
| `announce_queue`   | automation    | Async result announcement (for async delegations)            |

## Documentation

Full documentation at **[docs.goclaw.sh](https://docs.goclaw.sh)** — or browse the source in [`goclaw-docs/`](https://github.com/nextlevelbuilder/goclaw-docs)

| Section | Topics |
|---------|--------|
| [Getting Started](https://docs.goclaw.sh/#what-is-goclaw) | Installation, Quick Start, Configuration, Web Dashboard Tour |
| [Core Concepts](https://docs.goclaw.sh/#how-goclaw-works) | Agent Loop, Sessions, Tools, Memory, Multi-Tenancy |
| [Agents](https://docs.goclaw.sh/#creating-agents) | Creating Agents, Context Files, Personality, Sharing & Access |
| [Providers](https://docs.goclaw.sh/#providers-overview) | Anthropic, OpenAI, OpenRouter, Gemini, DeepSeek, +15 more |
| [Channels](https://docs.goclaw.sh/#channels-overview) | Telegram, Discord, Slack, Feishu, Zalo, WhatsApp, WebSocket |
| [Agent Teams](https://docs.goclaw.sh/#teams-what-are-teams) | Teams, Task Board, Messaging, Delegation & Handoff |
| [Advanced](https://docs.goclaw.sh/#custom-tools) | Custom Tools, MCP, Skills, Cron, Sandbox, Hooks, RBAC |
| [Deployment](https://docs.goclaw.sh/#deploy-docker-compose) | Docker Compose, Database, Security, Observability, Tailscale |
| [Reference](https://docs.goclaw.sh/#cli-commands) | CLI Commands, REST API, WebSocket Protocol, Environment Variables |

## Testing

```bash
go test ./...                                    # Unit tests
go test -v ./tests/integration/ -timeout 120s    # Integration tests (requires running gateway)
```

## Project Status

See [CHANGELOG.md](CHANGELOG.md) for detailed feature status including what's been tested in production and what's still in progress.

## Acknowledgments

GoClaw is built upon the original [OpenClaw](https://github.com/openclaw/openclaw) project. We are grateful for the architecture and vision that inspired this Go port.

## License

[CC BY-NC 4.0](LICENSE) — Creative Commons Attribution-NonCommercial 4.0 International
