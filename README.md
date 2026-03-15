<p align="center">
  <img src="_statics/goclaw.png" alt="GoClaw" />
</p>

# GoClaw

[![Go](https://img.shields.io/badge/Go_1.25-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/) [![PostgreSQL](https://img.shields.io/badge/PostgreSQL_18-316192?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/) [![Docker](https://img.shields.io/badge/Docker-2496ED?style=flat-square&logo=docker&logoColor=white)](https://www.docker.com/) [![WebSocket](https://img.shields.io/badge/WebSocket-010101?style=flat-square&logo=socket.io&logoColor=white)](https://developer.mozilla.org/en-US/docs/Web/API/WebSocket) [![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-000000?style=flat-square&logo=opentelemetry&logoColor=white)](https://opentelemetry.io/) [![Anthropic](https://img.shields.io/badge/Anthropic-191919?style=flat-square&logo=anthropic&logoColor=white)](https://www.anthropic.com/) [![OpenAI](https://img.shields.io/badge/OpenAI_Compatible-412991?style=flat-square&logo=openai&logoColor=white)](https://openai.com/) [![License: MIT](https://img.shields.io/badge/License-MIT-yellow?style=flat-square)](LICENSE)

**GoClaw** is a multi-agent AI gateway that connects LLMs to your tools, channels, and data — deployed as a single Go binary with zero runtime dependencies. It orchestrates agent teams, inter-agent delegation, and quality-gated workflows across 13+ LLM providers with full multi-tenant isolation.

A Go port of [OpenClaw](https://github.com/openclaw/openclaw) with enhanced security, multi-tenant PostgreSQL, and production-grade observability.

## What Makes It Different

- **Agent Teams & Orchestration** — Teams with shared task boards, inter-agent delegation (sync/async), conversation handoff, evaluate-loop quality gates, and hybrid agent discovery
- **Multi-Tenant PostgreSQL** — Per-user workspaces, per-user context files, encrypted API keys (AES-256-GCM), isolated sessions — the only Claw project with DB-native multi-tenancy
- **Single Binary** — ~25 MB static Go binary, no Node.js runtime, <1s startup, runs on a $5 VPS
- **Production Security** — 5-layer defense: rate limiting, prompt injection detection, SSRF protection, shell deny patterns, AES-256-GCM encryption
- **13+ LLM Providers** — Anthropic (native HTTP+SSE with prompt caching), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope (Qwen), Bailian Coding + Claude CLI (stdio + MCP bridge), Codex (gpt-5.3-codex via OAuth)
- **7 Messaging Channels** — Telegram (forum topics, STT), Discord, Slack, Zalo OA, Zalo Personal (DM + groups), Feishu/Lark (streaming cards, media), WhatsApp with `/stop` and `/stopall` commands
- **Extended Thinking** — Per-provider thinking mode (Anthropic budget tokens, OpenAI reasoning effort, DashScope thinking budget) with streaming support

## Claw Ecosystem

**Resource Footprint:**

|                 | OpenClaw        | ZeroClaw | PicoClaw | **GoClaw**                              |
| --------------- | --------------- | -------- | -------- | --------------------------------------- |
| Language        | TypeScript      | Rust     | Go       | **Go**                                  |
| Binary size     | 28 MB + Node.js | 3.4 MB   | ~8 MB    | **~25 MB** (base) / **~36 MB** (+ OTel) |
| Docker image    | —               | —        | —        | **~50 MB** (Alpine)                     |
| RAM (idle)      | > 1 GB          | < 5 MB   | < 10 MB  | **~35 MB**                              |
| Startup         | > 5 s           | < 10 ms  | < 1 s    | **< 1 s**                               |
| Target hardware | $599+ Mac Mini  | $10 edge | $10 edge | **$5 VPS+**                             |

**Feature Matrix:**

| Feature                    | OpenClaw                             | ZeroClaw                                     | PicoClaw                              | **GoClaw**                     |
| -------------------------- | ------------------------------------ | -------------------------------------------- | ------------------------------------- | ------------------------------ |
| Multi-tenant (PostgreSQL)  | —                                    | —                                            | —                                     | ✅                             |
| Hooks system               | —                                    | —                                            | —                                     | ✅ Command + agent evaluators  |
| MCP integration            | — (uses ACP)                         | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Agent teams                | —                                    | —                                            | —                                     | ✅ Task board + mailbox        |
| Agent handoff              | —                                    | —                                            | —                                     | ✅ Conversation transfer       |
| Evaluate loop              | —                                    | —                                            | —                                     | ✅ Generator-evaluator cycle   |
| Quality gates              | —                                    | —                                            | —                                     | ✅ Hook-based validation       |
| Security hardening         | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Basic (workspace restrict, exec deny) | ✅ 5-layer defense             |
| OTel observability         | ✅ (opt-in extension)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| Prompt caching             | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Knowledge graph            | —                                    | —                                            | —                                     | ✅ LLM extraction + traversal  |
| Skill system               | ✅ Embeddings/semantic               | ✅ SKILL.md + TOML                           | ✅ Basic                              | ✅ BM25 + pgvector hybrid      |
| Lane-based scheduler       | ✅                                   | Bounded concurrency                          | —                                     | ✅ (main/subagent/delegate/cron + concurrent group runs) |
| Messaging channels         | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Companion apps             | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web dashboard                  |
| Live Canvas / Voice        | ✅ (A2UI + TTS/STT)                  | —                                            | Voice transcription                   | TTS (4 providers)              |
| LLM providers              | 10+                                  | 8 native + 29 compat                         | 13+                                   | **13+**                        |
| Per-user workspaces        | ✅ (file-based)                      | —                                            | —                                     | ✅ (PostgreSQL)                |
| Encrypted secrets          | — (env vars only)                    | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM in DB           |

> **GoClaw unique strengths:** Only project with multi-tenant PostgreSQL, agent teams, conversation handoff, evaluate-loop quality gates, hooks system, knowledge graph, and MCP protocol support.

## Architecture

```mermaid
graph TB
    subgraph Clients
        WEB["Web Dashboard<br/>(React SPA)"]
        TG["Telegram"]
        DC["Discord"]
        SL["Slack"]
        FS["Feishu/Lark"]
        ZL["Zalo OA"]
        ZLP["Zalo Personal"]
        API["HTTP API"]
    end

    subgraph Gateway["GoClaw Gateway"]
        direction TB
        WS["WebSocket RPC"] & REST["HTTP Server"] & CM["Channel Manager"]
        WS & REST & CM --> BUS["Message Bus"]
        BUS --> SCHED["Lane-based Scheduler<br/>main · subagent · delegate · cron"]
        SCHED --> ROUTER["Agent Router"]
        ROUTER --> LOOP["Agent Loop<br/>think → act → observe"]
        LOOP --> TOOLS["Tool Registry<br/>fs · exec · web · memory · delegate · team · mcp · custom"]
        LOOP --> LLM["LLM Providers<br/>Anthropic (native + prompt caching) · OpenAI-compat (12+)"]
    end

    subgraph Storage
        PG["PostgreSQL 18 + pgvector<br/>(multi-tenant)"]
    end

    WEB --> WS
    TG & DC & SL & FS & ZL & ZLP --> CM
    API --> REST
    LOOP --> PG
```

## Multi-Agent Orchestration

GoClaw supports four orchestration patterns for agent collaboration, all managed through explicit permission links.

### Agent Delegation

Agent delegation enables named agents to delegate tasks to other agents — each running with its own identity, tools, LLM provider, and context files. Unlike subagents (anonymous clones of the parent), delegation targets are fully independent agents.

```mermaid
flowchart TD
    USER((User)) -->|"Research competitor pricing"| SUPPORT

    subgraph TEAM["Agent Team"]
        SUPPORT["Support Bot<br/>(Claude Haiku)"]
        RESEARCH["Research Bot<br/>(GPT-4)"]
        WRITER["Content Writer<br/>(Claude Sonnet)"]
        BILLING["Billing Bot<br/>(Gemini)"]
    end

    SUPPORT -->|"sync: wait for answer"| RESEARCH
    RESEARCH -->|"result"| SUPPORT
    SUPPORT -->|"async: don't wait"| WRITER
    WRITER -.->|"announce when done"| SUPPORT
    SUPPORT -.-x|"no link"| BILLING

    SUPPORT -->|"final answer"| USER

    style USER fill:#e1f5fe
    style SUPPORT fill:#fff3e0
    style RESEARCH fill:#e8f5e9
    style WRITER fill:#f3e5f5
    style BILLING fill:#ffebee
```

| Mode | How it works | Best for |
|------|-------------|----------|
| **Sync** | Agent A asks Agent B and **waits** for the answer | Quick lookups, fact checks |
| **Async** | Agent A asks Agent B and **moves on**. B announces the result later | Long tasks, reports, deep analysis |

**Permission Links** — Agents communicate through explicit **agent links** with access control:

```bash
# One-way: support-bot can delegate TO research-bot
agents.links.create {
  "sourceAgent": "support-bot",
  "targetAgent": "research-bot",
  "direction": "outbound",
  "maxConcurrent": 3
}

# Bidirectional: both agents can delegate to each other
agents.links.create {
  "sourceAgent": "support-bot",
  "targetAgent": "content-writer",
  "direction": "bidirectional"
}
```

| Direction | Meaning |
|-----------|---------|
| `outbound` | Source can delegate TO target |
| `inbound` | Target can delegate TO source |
| `bidirectional` | Both agents can delegate to each other |

**Concurrency Control** — Two layers prevent any agent from being overwhelmed:

| Layer | Config | Example |
|-------|--------|---------|
| **Per-link** | `agent_links.max_concurrent` | support → research: max 3 |
| **Per-agent** | `agents.other_config.max_delegation_load` | research-bot: max 5 total |

**Per-User Restrictions** — The `settings` JSONB on agent links supports per-user deny/allow lists.

**Agent Discovery** — Each agent has a `frontmatter` field for discovery. With ≤15 targets, auto-generated `AGENTS.md` is injected into context. With >15 targets, agents use `delegate_search` for hybrid FTS + semantic search.

<details>
<summary>Delegation vs Subagents</summary>

| Aspect | Subagents | Agent Delegation |
|--------|-----------|-----------------|
| Target | Anonymous clone of parent | Named agent with own identity |
| Provider/Model | Inherited from parent | Target's own configuration |
| Tools | Parent's tools minus deny list | Target's own tool registry + policy |
| Context files | Simplified system prompt | Target's own SOUL.md, IDENTITY.md, etc. |
| Session | Shared with parent | Isolated (fresh per delegation) |
| Permission | Depth-based limits only | Explicit `agent_links` with direction |
| User control | None | Per-user deny/allow via settings JSONB |
| Concurrency | Global + per-parent limits | Per-link + per-target-agent limits |

</details>

### Agent Teams

Teams enable coordinated multi-agent workflows with a shared task board and peer-to-peer messaging.

```mermaid
flowchart TD
    USER((User)) -->|message| LEAD

    subgraph TEAM["Agent Team"]
        LEAD["Lead Agent<br/>(orchestrator)"]
        A1["Specialist A"]
        A2["Specialist B"]
        A3["Specialist C"]
    end

    subgraph BOARD["Shared Task Board"]
        T1["Task 1: pending"]
        T2["Task 2: in_progress<br/>owner: A1"]
        T3["Task 3: blocked_by T2"]
    end

    subgraph MAIL["Team Mailbox"]
        M1["A1 → LEAD: status update"]
        M2["LEAD → ALL: broadcast"]
    end

    LEAD -->|"create tasks"| BOARD
    A1 -->|"claim"| T2
    T2 -.->|"auto-unblocks"| T3
    A1 -->|"send message"| MAIL
    LEAD -->|"broadcast"| MAIL
    LEAD -->|final answer| USER

    style USER fill:#e1f5fe
    style LEAD fill:#fff3e0
    style A1 fill:#e8f5e9
    style A2 fill:#e8f5e9
    style A3 fill:#e8f5e9
```

- **Team roles** — Lead agent orchestrates work, member agents execute tasks
- **Shared task board** — Create, claim, complete, search tasks with `blocked_by` dependencies. Atomic claiming prevents double-assignment
- **Team mailbox** — Direct peer-to-peer messaging (send, broadcast, read unread)
- **Tools**: `team_tasks` for task management, `team_message` for mailbox

### Agent Handoff

Handoff transfers conversation control from one agent to another. Unlike delegation (where A stays in control), handoff means B completely takes over the user conversation.

```mermaid
flowchart LR
    subgraph Delegation["Delegation (A stays in control)"]
        direction TB
        DA["Agent A"] -->|"delegate task"| DB["Agent B"]
        DB -->|"return result"| DA
        DA -->|"reply to user"| DU((User))
    end

    subgraph Handoff["Handoff (B takes over)"]
        direction TB
        HA["Agent A"] -->|"handoff"| HB["Agent B"]
        HB -->|"now handles user"| HU((User))
    end
```

- **Routing override** — Sets a routing rule so all future messages go to the target agent
- **Context transfer** — Conversation context is passed to the new agent
- **Revert** — `handoff(action="clear")` returns routing to the original agent

### Evaluate Loop

The evaluate loop orchestrates a generator-evaluator feedback cycle between two agents for quality-gated output.

```mermaid
flowchart LR
    TASK["Task + Criteria"] --> GEN["Generator<br/>Agent"]
    GEN -->|"output"| EVAL{"Evaluator<br/>Agent"}
    EVAL -->|"APPROVED"| RESULT["Final Output"]
    EVAL -->|"REJECTED + feedback"| GEN
    EVAL -.->|"max rounds hit"| WARN["Last output + warning"]
```

- **Configurable rounds** — Default 3, max 5 revision cycles
- **Custom pass criteria** — Define what "approved" means for the evaluator
- **Tool**: `evaluate_loop(generator="writer-bot", evaluator="qa-bot", task="...", pass_criteria="...")`

### Quality Gates

Quality gates validate agent output before it reaches users. Configured in agent `other_config`:

```json
{
  "quality_gates": [
    {
      "event": "delegation.completed",
      "type": "agent",
      "agent": "qa-reviewer",
      "block_on_failure": true,
      "max_retries": 2
    }
  ]
}
```

- **Hook types**: `command` (shell exit code: 0 = pass) or `agent` (delegate to reviewer agent)
- **Blocking** — Failed gates can block output and trigger automatic retry with feedback
- **Recursion-safe** — Quality gate evaluators skip their own gates to prevent infinite loops

## Features

### LLM Providers
- **13+ providers** — OpenRouter, Anthropic, OpenAI, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope (Qwen), Bailian Coding, and any OpenAI-compatible endpoint
- **Anthropic native** — Direct HTTP+SSE integration with prompt caching (`cache_control`) for ~90% cost reduction on repeated prefixes. Also supports Claude CLI mode (stdio + MCP bridge with session management)
- **OpenAI-compatible** — Automatic prompt caching for OpenAI, MiniMax, OpenRouter (cache metrics tracked in traces). Also supports Codex mode (gpt-5.3-codex via OAuth with "phase" metadata)
- **Extended thinking** — Per-provider thinking mode: Anthropic (budget tokens), OpenAI-compat (reasoning effort), DashScope (thinking budget) with streaming support

### Agent Orchestration
- **Agent loop** — Think-act-observe cycle with tool use, session history, and auto-summarization
- **Subagents** — Spawn child agents with different models for parallel task execution
- **Agent delegation** — Sync/async inter-agent task delegation with permission links, concurrency limits, and per-user restrictions
- **Agent teams** — Shared task boards with dependencies, team mailbox, and coordinated multi-agent workflows
- **Agent handoff** — Transfer conversation control between agents with routing overrides
- **Evaluate loop** — Generator-evaluator feedback cycles for quality-gated output
- **Quality gates** — Hook-based output validation with command or agent evaluators
- **Delegation history** — Queryable audit trail of all inter-agent delegations
- **Concurrent execution** — Lane-based scheduler (main/subagent/delegate/cron), adaptive throttle for group chats

### Tools & Integrations
- **30+ built-in tools** — File system, shell exec, web search/fetch, memory, browser automation, TTS, and more
- **MCP integration** — Connect external MCP servers via stdio, SSE, or streamable-http with per-agent/per-user grants
- **Hooks system** — Event-driven hooks with command evaluators (shell exit code) and agent evaluators (delegate to reviewer) for output validation

### Messaging Channels
- **Telegram** — Full integration with streaming, rich formatting (HTML, tables, code blocks), reactions, media, forum topics (per-topic config and session isolation), speech-to-text, bot commands, group file writer restrictions
- **Slack** — Channel integration with bot commands
- **Feishu/Lark** — Streaming card updates, media attachments (images/files), mention resolution, topic session mode
- **Zalo OA** — Official Account integration for DM conversations
- **Zalo Personal** — Unofficial reverse-engineered protocol supporting DM + group messages with restrictive default policies
- **Discord, WhatsApp** — Channel adapters with `/stop` and `/stopall` commands
- **Persistent pending messages** — Group chat messages persisted to PostgreSQL with auto-compaction (LLM summarization) when queues exceed threshold

### Knowledge & Memory
- **Skills** — SKILL.md-based knowledge base with BM25 + embedding hybrid search (pgvector)
- **Long-term memory** — pgvector hybrid search (full-text + vector similarity) with admin dashboard for CRUD, search, and bulk re-indexing
- **Knowledge graph** — LLM-powered entity/relationship extraction from memory, graph traversal (recursive CTE, max depth 3), and force-directed visualization. Agent tool: `knowledge_graph_search`

### Infrastructure
- **Cron scheduling** — `at`, `every`, and cron expression syntax for scheduled agent tasks
- **Browser automation** — Headless Chrome via Rod for web interaction
- **Text-to-Speech** — OpenAI, ElevenLabs, Edge, MiniMax providers
- **Docker sandbox** — Isolated code execution in containers
- **Tracing** — LLM call tracing with cache metrics, span metadata, and optional OpenTelemetry OTLP export
- **Tailscale** — Optional VPN mesh listener for secure remote access (build-tag gated)

### Security
- **Rate limiting** — Token bucket per user/IP, configurable RPM
- **API key management** — Multi-key auth with RBAC scopes (`admin`, `read`, `write`, `approvals`, `pairing`), SHA-256 hashed storage, optional expiry, revocation
- **Prompt injection detection** — 6-pattern regex scanner (detection-only, never blocks)
- **Credential scrubbing** — Auto-redact API keys, tokens, passwords from tool outputs
- **Shell deny patterns** — Blocks `curl|sh`, reverse shells, `eval $()`, `base64|sh`
- **SSRF protection** — DNS pinning, blocked private IPs, blocked hosts
- **AES-256-GCM** — Encrypted provider API keys in database
- **Browser pairing** — Token-free browser auth with admin-approved pairing codes

### Web Dashboard
- Agent management, traces & spans viewer, skills, teams, MCP servers, pairing approval, memory management (CRUD + search + chunking), knowledge graph (table + force-directed visualization), pending messages dashboard, API key management, and interactive API documentation (Swagger UI)

## Quick Start

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git
cd goclaw
```

### From Source

```bash
# Build
make build

# Interactive setup wizard
./goclaw onboard

# Start the gateway
source .env.local && ./goclaw
```

### With Docker

**1. Prepare environment:**

```bash
# Generate .env with auto-generated secrets (GOCLAW_ENCRYPTION_KEY, GOCLAW_GATEWAY_TOKEN)
chmod +x prepare-env.sh
./prepare-env.sh
```

The script creates `.env` from `.env.example`, auto-generates `GOCLAW_ENCRYPTION_KEY` and `GOCLAW_GATEWAY_TOKEN`, and checks for a provider API key. Add at least one `GOCLAW_*_API_KEY` to `.env` before starting.

**2. Start services:**

```bash
# Recommended: Gateway + Web Dashboard (http://localhost:3000)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.selfservice.yml up -d --build

# Without dashboard
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d --build

# + OpenTelemetry tracing (Jaeger at http://localhost:16686)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.otel.yml up -d --build

# + Tailscale (secure remote access)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.tailscale.yml up -d --build
```

When `GOCLAW_*_API_KEY` environment variables are set, the gateway **auto-onboards** without interactive prompts — it detects the provider, generates a gateway token, connects to Postgres, runs migrations, and seeds default data.

**Auto-onboard detects** the first available API key in priority order: OpenRouter → Anthropic → OpenAI → Groq → DeepSeek → Gemini → Mistral → xAI → MiniMax → Cohere → Perplexity. Override with `GOCLAW_PROVIDER` and `GOCLAW_MODEL`. Memory is auto-enabled with embedding support if an OpenAI, OpenRouter, or Gemini key is detected.

**Minimum `.env`:**

```bash
GOCLAW_OPENROUTER_API_KEY=sk-or-your-key    # Required: at least one provider key
GOCLAW_GATEWAY_TOKEN=...                     # Auto-generated by prepare-env.sh
GOCLAW_ENCRYPTION_KEY=...                    # Auto-generated by prepare-env.sh
# GOCLAW_PROVIDER=openrouter                 # Optional: override default provider
# GOCLAW_MODEL=anthropic/claude-sonnet-4     # Optional: override default model
# POSTGRES_PASSWORD=your-secure-password     # Optional: defaults to "goclaw"
```

## Deployment

GoClaw requires PostgreSQL with pgvector. Designed for multi-user and multi-tenant deployments with **per-user isolation** — each user gets their own context files, session history, and workspace.

```bash
# Set up database
export GOCLAW_POSTGRES_DSN="postgres://user:pass@localhost:5432/goclaw?sslmode=disable"
export GOCLAW_ENCRYPTION_KEY=$(openssl rand -hex 32)

# Run database upgrade (schema migrations + data hooks)
./goclaw upgrade

# Start gateway
./goclaw
```

**Features:**

- Per-user context files and workspaces (`user_context_files` table)
- Agent types: `open` (per-user workspace) vs `predefined` (shared context)
- Agent teams, delegation, handoff, evaluate loops, quality gates
- LLM call tracing with spans and prompt cache metrics
- MCP server integration with per-agent and per-user access grants
- Event-driven hooks for agent lifecycle with command and agent evaluators
- Embedding-based skill search (hybrid BM25 + pgvector)
- Web dashboard for agents, traces, skills, teams, and MCP servers
- API key encryption (AES-256-GCM)

## Installation

### Prerequisites

- Go 1.25+
- PostgreSQL 18 with pgvector
- Docker (optional, for sandbox and containerized deployment)

### Build

```bash
# Production build (~25MB binary, static, stripped symbols)
CGO_ENABLED=0 go build -ldflags="-s -w" -o goclaw .

# With OpenTelemetry support (~36MB binary)
CGO_ENABLED=0 go build -ldflags="-s -w" -tags otel -o goclaw .

# With Tailscale support (~54MB binary)
CGO_ENABLED=0 go build -ldflags="-s -w" -tags tsnet -o goclaw .

# With Redis cache backend
CGO_ENABLED=0 go build -ldflags="-s -w" -tags redis -o goclaw .

# With both OTel + Tailscale
CGO_ENABLED=0 go build -ldflags="-s -w" -tags "otel,tsnet" -o goclaw .
```

**Binary size comparison across the Claw ecosystem:**

| Build                    | Binary Size | Docker Image | Notes                                     |
| ------------------------ | ----------- | ------------ | ----------------------------------------- |
| **GoClaw** (base)        | ~25 MB      | ~50 MB       | `CGO_ENABLED=0 go build -ldflags="-s -w"` |
| **GoClaw** (+ OTel)      | ~36 MB      | ~60 MB       | Add `-tags otel` for OTLP export          |
| **GoClaw** (+ Tailscale) | ~54 MB      | ~75 MB       | Add `-tags tsnet` for Tailscale listener  |
| **GoClaw** (+ both)      | ~65 MB      | ~85 MB       | `-tags "otel,tsnet"`                      |
| PicoClaw                 | ~8 MB       | —            | Single Go binary                          |
| ZeroClaw                 | 3.4 MB      | —            | Minimal Rust binary                       |
| OpenClaw                 | 28 MB       | —            | + ~390 MB Node.js runtime required        |

> Optional features are gated behind build tags to avoid binary bloat. OTel adds ~11 MB (gRPC + protobuf). Tailscale adds ~20 MB (tsnet + WireGuard). The base build includes in-app tracing backed by PostgreSQL and localhost-only access.

### Docker Build

```bash
# Standard image (~50MB Alpine)
docker build -t goclaw .

# With OpenTelemetry (~60MB)
docker build --build-arg ENABLE_OTEL=true -t goclaw:otel .

# With Tailscale (~75MB)
docker build --build-arg ENABLE_TSNET=true -t goclaw:tsnet .

# With both OTel + Tailscale (~85MB)
docker build --build-arg ENABLE_OTEL=true --build-arg ENABLE_TSNET=true -t goclaw:full .
```

## Configuration

### Setup Wizard

```bash
./goclaw onboard
```

The wizard configures: provider, model, gateway port, channels, memory, browser, TTS, and tracing. It generates `config.json` (no secrets) and `.env.local` (secrets only).

### Auto-Onboard (Docker / CI)

When `GOCLAW_*_API_KEY` environment variables are set, the gateway automatically configures itself without interactive prompts. It retries Postgres connection (up to 5 attempts), runs migrations, and seeds default data.

### Environment Variables

<details>
<summary><strong>Provider API Keys</strong> (set at least one)</summary>

| Variable                    | Provider                 |
| --------------------------- | ------------------------ |
| `GOCLAW_OPENROUTER_API_KEY` | OpenRouter (recommended) |
| `GOCLAW_ANTHROPIC_API_KEY`  | Anthropic Claude         |
| `GOCLAW_OPENAI_API_KEY`     | OpenAI                   |
| `GOCLAW_GROQ_API_KEY`       | Groq                     |
| `GOCLAW_DEEPSEEK_API_KEY`   | DeepSeek                 |
| `GOCLAW_GEMINI_API_KEY`     | Google Gemini            |
| `GOCLAW_MISTRAL_API_KEY`    | Mistral AI               |
| `GOCLAW_XAI_API_KEY`        | xAI Grok                 |
| `GOCLAW_MINIMAX_API_KEY`    | MiniMax                  |
| `GOCLAW_COHERE_API_KEY`     | Cohere                   |
| `GOCLAW_PERPLEXITY_API_KEY` | Perplexity               |
| `GOCLAW_DASHSCOPE_API_KEY`  | DashScope (Qwen)         |
| `GOCLAW_BAILIAN_API_KEY`    | Bailian Coding           |

</details>

<details>
<summary><strong>Gateway & Application</strong></summary>

| Variable                  | Description                      | Default                      |
| ------------------------- | -------------------------------- | ---------------------------- |
| `GOCLAW_CONFIG`           | Config file path                 | `config.json`                |
| `GOCLAW_GATEWAY_TOKEN`    | API authentication token         | (generated)                  |
| `GOCLAW_HOST`             | Server bind address              | `0.0.0.0`                    |
| `GOCLAW_PORT`             | Server port                      | `18790`                      |
| `GOCLAW_PROVIDER`         | Default LLM provider             | `anthropic`                  |
| `GOCLAW_MODEL`            | Default model                    | `claude-sonnet-4-5-20250929` |
| `GOCLAW_WORKSPACE`        | Agent workspace directory        | `~/.goclaw/workspace`        |
| `GOCLAW_DATA_DIR`         | Data storage directory           | `~/.goclaw/data`             |
| `GOCLAW_SESSIONS_STORAGE` | Sessions storage path            | `~/.goclaw/sessions`         |
| `GOCLAW_SKILLS_DIR`       | Skills directory                 | `~/.goclaw/skills`           |
| `GOCLAW_OWNER_IDS`        | Admin user IDs (comma-separated) — owners can manage **all** agents regardless of ownership and are used as default owner for auto-seeded resources |                              |

</details>

<details>
<summary><strong>Database</strong></summary>

| Variable                | Description                            |
| ----------------------- | -------------------------------------- |
| `GOCLAW_POSTGRES_DSN`   | PostgreSQL connection string           |
| `GOCLAW_ENCRYPTION_KEY` | AES-256-GCM key for API key encryption |
| `GOCLAW_MIGRATIONS_DIR` | Path to migration files                |

</details>

<details>
<summary><strong>Messaging Channels</strong></summary>

| Variable                           | Description                   |
| ---------------------------------- | ----------------------------- |
| `GOCLAW_TELEGRAM_TOKEN`            | Telegram bot token            |
| `GOCLAW_ZALO_TOKEN`                | Zalo access token             |
| `GOCLAW_FEISHU_APP_ID`             | Feishu/Lark app ID            |
| `GOCLAW_FEISHU_APP_SECRET`         | Feishu/Lark app secret        |
| `GOCLAW_FEISHU_ENCRYPT_KEY`        | Feishu message encryption key |
| `GOCLAW_FEISHU_VERIFICATION_TOKEN` | Feishu verification token     |

</details>

<details>
<summary><strong>Scheduler Lanes</strong></summary>

| Variable               | Description                  | Default |
| ---------------------- | ---------------------------- | ------- |
| `GOCLAW_LANE_MAIN`     | Main lane concurrency        | `30`    |
| `GOCLAW_LANE_SUBAGENT` | Subagent lane concurrency    | `50`    |
| `GOCLAW_LANE_DELEGATE` | Delegation lane concurrency  | `100`   |
| `GOCLAW_LANE_CRON`     | Cron lane concurrency        | `30`    |

</details>

<details>
<summary><strong>Tailscale</strong> (requires build tag <code>tsnet</code>)</summary>

| Variable                | Description                                   | Default    |
| ----------------------- | --------------------------------------------- | ---------- |
| `GOCLAW_TSNET_HOSTNAME` | Tailscale device name (e.g. `goclaw-gateway`) | (disabled) |
| `GOCLAW_TSNET_AUTH_KEY` | Tailscale auth key                            |            |
| `GOCLAW_TSNET_DIR`      | Persistent state directory                    | OS default |

</details>

<details>
<summary><strong>Telemetry</strong> (requires build tag <code>otel</code>)</summary>

| Variable                        | Description                 | Default          |
| ------------------------------- | --------------------------- | ---------------- |
| `GOCLAW_TELEMETRY_ENABLED`      | Enable OTel export          | `false`          |
| `GOCLAW_TELEMETRY_ENDPOINT`     | OTLP endpoint               |                  |
| `GOCLAW_TELEMETRY_PROTOCOL`     | `grpc` or `http`            | `grpc`           |
| `GOCLAW_TELEMETRY_INSECURE`     | Skip TLS verification       | `false`          |
| `GOCLAW_TELEMETRY_SERVICE_NAME` | Service name in traces      | `goclaw-gateway` |
| `GOCLAW_TRACE_VERBOSE`          | Log full LLM input in spans | `0`              |

</details>

<details>
<summary><strong>TTS (Text-to-Speech)</strong></summary>

| Variable                        | Description         |
| ------------------------------- | ------------------- |
| `GOCLAW_TTS_OPENAI_API_KEY`     | OpenAI TTS API key  |
| `GOCLAW_TTS_ELEVENLABS_API_KEY` | ElevenLabs API key  |
| `GOCLAW_TTS_MINIMAX_API_KEY`    | MiniMax TTS API key |
| `GOCLAW_TTS_MINIMAX_GROUP_ID`   | MiniMax group ID    |

</details>

## CLI Commands

```
goclaw                    Start gateway (default command)
goclaw onboard            Interactive setup wizard
goclaw version            Print version and protocol info
goclaw doctor             System health check (includes schema status)

goclaw upgrade            Upgrade database schema and run data hooks
goclaw upgrade --status   Show current vs required schema version
goclaw upgrade --dry-run  Preview pending changes without applying

goclaw agent list         List configured agents
goclaw agent chat         Chat with an agent
goclaw agent add          Add a new agent
goclaw agent delete       Delete an agent

goclaw migrate up         Apply all pending migrations
goclaw migrate down       Roll back migrations
goclaw migrate version    Show current migration version
goclaw migrate force N    Force set migration version
goclaw migrate goto N     Migrate to specific version
goclaw migrate drop       Drop all tables (dangerous)

goclaw config show        Show current configuration
goclaw config path        Show config file path
goclaw config validate    Validate configuration

goclaw sessions list      List active sessions
goclaw sessions delete    Delete a session
goclaw sessions reset     Reset session history

goclaw cron list          List scheduled jobs
goclaw cron delete        Delete a job
goclaw cron toggle        Enable/disable a job

goclaw skills list        List available skills
goclaw skills show        Show skill details
```

**Adding core skills:**

Place a skill folder inside `skills/` (local dev) or `/app/bundled-skills/` (Docker image). Each folder must contain a `SKILL.md` with YAML frontmatter (`name`, `description`, `slug`). Folders prefixed with `_` are treated as shared code, not skills.

On server startup, the seeder automatically discovers all skill folders, upserts them into the database, and runs an async dependency check. No environment variable needed — the seeder falls back to `skills/` in local dev and `/app/bundled-skills` in Docker.

```

goclaw models             List AI models and providers
goclaw channels           List messaging channels

goclaw pairing approve    Approve a pairing code
goclaw pairing list       List paired devices
goclaw pairing revoke     Revoke a pairing
```

**Flags:**

```
--config, -c    Path to config file (default: config.json)
--verbose, -v   Enable debug logging
```

## API

Interactive API documentation is available at `/docs` (Swagger UI) when the gateway is running. The OpenAPI 3.0 spec is served at `/v1/openapi.json`.

| Documentation | Description |
|---------------|-------------|
| [HTTP REST API](docs/18-http-api.md) | 130+ HTTP endpoints — chat completions, agents, skills, providers, MCP, memory, knowledge graph, channels, traces, usage, storage, API keys |
| [WebSocket RPC](docs/19-websocket-rpc.md) | 64+ RPC methods — chat, agents, config, sessions, cron, teams, pairing, delegations, approvals |
| [API Keys & Auth](docs/20-api-keys-auth.md) | Authentication model, RBAC scopes, API key management, security design |
| [Gateway Protocol](docs/04-gateway-protocol.md) | WebSocket wire protocol (v3), frame format, connection lifecycle |

## Docker Compose

Composable files for different deployment scenarios:

| File                             | Purpose                                            |
| -------------------------------- | -------------------------------------------------- |
| `docker-compose.yml`             | Base service definition                            |
| `docker-compose.postgres.yml`    | PostgreSQL (pgvector/pgvector:pg18)                |
| `docker-compose.upgrade.yml`     | One-shot database upgrade service                  |
| `docker-compose.selfservice.yml` | Web dashboard UI (nginx + React SPA)               |
| `docker-compose.browser.yml`     | Headless Chrome for browser automation             |
| `docker-compose.sandbox.yml`     | Docker-based code execution sandbox                |
| `docker-compose.otel.yml`        | OpenTelemetry + Jaeger tracing                     |
| `docker-compose.redis.yml`       | Redis cache backend (build-tag gated)              |
| `docker-compose.tailscale.yml`   | Tailscale VPN mesh listener                        |

### Examples

```bash
# Prepare .env (auto-generates secrets, prompts for API key)
chmod +x prepare-env.sh && ./prepare-env.sh

# Managed (PostgreSQL)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d --build

# Managed + Web Dashboard (http://localhost:3000)
docker compose -f docker-compose.yml \
  -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d --build

# Managed + Web Dashboard + OpenTelemetry (Jaeger UI at http://localhost:16686)
docker compose -f docker-compose.yml \
  -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml \
  -f docker-compose.otel.yml up -d --build

# Managed + Tailscale (secure remote access via VPN mesh)
docker compose -f docker-compose.yml \
  -f docker-compose.postgres.yml \
  -f docker-compose.tailscale.yml up -d --build

# Check health
curl http://localhost:18790/health
```

### Upgrading (Docker Compose)

**Simple upgrade** — pull the latest code, rebuild, and restart. The entrypoint automatically runs `goclaw upgrade` (schema migrations + data hooks) before starting:

```bash
# Pull latest code
git pull

# Rebuild and restart (auto-upgrades database on start)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d --build
```

Replace the compose files with whichever overlays you use (e.g. add `-f docker-compose.otel.yml` for OTel).

**Explicit upgrade** — if you want to preview changes or run the upgrade separately before restarting:

```bash
# Check current schema status
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.upgrade.yml run --rm upgrade --status

# Preview pending changes (dry-run)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.upgrade.yml run --rm upgrade --dry-run

# Apply upgrade (schema migrations + data hooks), then remove container
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.upgrade.yml run --rm upgrade

# Then rebuild and restart the gateway with the new image
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d --build
```

### Environment File (.env)

Use the `prepare-env.sh` script to generate `.env` with auto-generated secrets:

```bash
./prepare-env.sh
```

This creates `.env` with `GOCLAW_ENCRYPTION_KEY` and `GOCLAW_GATEWAY_TOKEN` pre-filled. You only need to add your provider API key. See `.env.example` for all available variables.

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
| `delegate`         | orchestration | Delegate tasks to other agents (sync/async, cancel, list)    |
| `delegate_search`  | orchestration | Search delegation targets (hybrid FTS + semantic)            |
| `handoff`          | orchestration | Transfer conversation control to another agent               |
| `evaluate_loop`    | orchestration | Generate-evaluate-revise quality feedback loop               |
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

## Browser Pairing

Browser clients can authenticate without pre-shared tokens using a pairing code flow:

1. User opens the web dashboard and enters their User ID
2. Clicks "Request Access (Pairing)" — gateway generates an 8-character code
3. Code is displayed in the browser UI
4. An admin approves the code via CLI (`goclaw pairing approve XXXX`) or the web UI
5. Browser automatically detects approval and gains operator-level access
6. On subsequent visits, the browser reconnects automatically using the stored pairing (no re-approval needed)

**Revoking access:**

```bash
# List paired devices
goclaw pairing list

# Revoke a specific pairing
goclaw pairing revoke <sender_id>
```

After revocation, the browser falls back to the pairing flow on next visit.

## Tailscale (Remote Access)

GoClaw supports an optional [Tailscale](https://tailscale.com) listener for secure remote access via VPN mesh. The Tailscale listener runs alongside the main gateway, serving the same routes on both listeners.

**Build-tag gated:** The `tsnet` dependency (~20 MB) is only compiled when building with `-tags tsnet`. The default binary is unaffected.

```bash
# Build with Tailscale support
go build -tags tsnet -o goclaw .

# Configure via environment variables
export GOCLAW_TSNET_HOSTNAME=goclaw-gateway
export GOCLAW_TSNET_AUTH_KEY=tskey-auth-xxxxx

# Start — both localhost:18790 and Tailscale listener are active
./goclaw
```

When Tailscale is enabled and the gateway is still bound to `0.0.0.0`, a log suggestion recommends switching to `127.0.0.1` for localhost-only + Tailscale access:

```
GOCLAW_HOST=127.0.0.1 ./goclaw
```

This keeps the gateway inaccessible from the LAN while remaining reachable via Tailscale from any device on your tailnet.

**Docker:**

```bash
docker compose -f docker-compose.yml \
  -f docker-compose.postgres.yml \
  -f docker-compose.tailscale.yml up -d
```

Requires `GOCLAW_TSNET_AUTH_KEY` in your `.env` file. Tailscale state is persisted in a `tsnet-state` Docker volume.

## Security

- **Transport**: WebSocket CORS validation, 512KB message limit, 1MB HTTP body limit, timing-safe token auth
- **API key management**: Multi-key auth with 5 RBAC scopes, SHA-256 hashed storage, optional expiry, revocation, show-once pattern. See [API Keys & Auth](docs/20-api-keys-auth.md)
- **Rate limiting**: Token bucket per user/IP, configurable RPM
- **Prompt injection**: Input guard with 6 pattern detection (detection-only, never blocks)
- **Shell security**: Deny patterns for `curl|sh`, `wget|sh`, reverse shells, `eval`, `base64|sh`
- **Network**: SSRF protection with blocked hosts + private IP + DNS pinning
- **File system**: Path traversal prevention, workspace restriction
- **Encryption**: AES-256-GCM for provider API keys in database
- **Browser pairing**: Token-free browser auth with admin approval (pairing codes, auto-reconnect)
- **Tailscale**: Optional VPN mesh listener for secure remote access (build-tag gated)

## Testing

```bash
# Unit tests
go test ./...

# Integration tests (requires running gateway)
go test -v -run 'TestHealthHTTP|TestConnectHandshake' ./tests/integration/

# Full integration (requires API key)
GOCLAW_OPENROUTER_API_KEY=sk-or-xxx go test -v ./tests/integration/ -timeout 120s
```

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
- **Lane-based scheduler** — Main/subagent/delegate/cron lane isolation with concurrent execution tested. Group chats support up to 3 concurrent agent runs per session with adaptive throttle and deferred session writes for history isolation.
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
- **Knowledge graph** — LLM-powered entity extraction, graph traversal, force-directed visualization, and `knowledge_graph_search` agent tool.
- **Memory management** — Admin dashboard for memory documents (CRUD, semantic search, chunk/embedding details, bulk re-indexing).
- **Persistent pending messages** — Channel messages persisted to PostgreSQL with auto-compaction (LLM summarization) and monitoring dashboard.

### Implemented but Not Fully Tested

- **Agent handoff** — Conversation transfer between agents with routing overrides. Implementation complete, needs E2E testing.
- **Quality gates** — Hook-based output validation with command and agent evaluator types. Implementation complete, needs E2E testing.
- **Slack** — Channel integration implemented, not yet validated with real users.
- **Other messaging channels** — Discord, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp channel adapters are implemented but have not been tested end-to-end in production. Only Telegram has been validated with real users.
- **OpenTelemetry export** — OTLP gRPC/HTTP exporter implemented (build-tag gated). In-app tracing works; external OTel export not validated in production.
- **Tailscale integration** — tsnet listener implemented (build-tag gated). Not tested in a real deployment.
- **Redis cache** — Optional distributed cache backend (build-tag gated). Not tested in production.
- **Browser pairing** — Pairing code flow implemented with CLI and web UI approval. Basic flow tested but not validated at scale.

## Acknowledgments

GoClaw is built upon the original [OpenClaw](https://github.com/openclaw/openclaw) project. We are grateful for the architecture and vision that inspired this Go port.

## License

MIT
