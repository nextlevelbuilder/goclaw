<p align="center">
  <img src="_statics/goclaw.png" alt="GoClaw" />
</p>

# GoClaw

[![Go](https://img.shields.io/badge/Go_1.26-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/) [![PostgreSQL](https://img.shields.io/badge/PostgreSQL_18-316192?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/) [![Docker](https://img.shields.io/badge/Docker-2496ED?style=flat-square&logo=docker&logoColor=white)](https://www.docker.com/) [![WebSocket](https://img.shields.io/badge/WebSocket-010101?style=flat-square&logo=socket.io&logoColor=white)](https://developer.mozilla.org/en-US/docs/Web/API/WebSocket) [![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-000000?style=flat-square&logo=opentelemetry&logoColor=white)](https://opentelemetry.io/) [![Anthropic](https://img.shields.io/badge/Anthropic-191919?style=flat-square&logo=anthropic&logoColor=white)](https://www.anthropic.com/) [![OpenAI](https://img.shields.io/badge/OpenAI_Compatible-412991?style=flat-square&logo=openai&logoColor=white)](https://openai.com/) [![License: MIT](https://img.shields.io/badge/License-MIT-yellow?style=flat-square)](LICENSE)

**GoClaw** là một multi-agent AI gateway kết nối LLM với các công cụ, kênh và dữ liệu của bạn — được triển khai dưới dạng một binary Go duy nhất mà không cần runtime dependencies. Nó điều phối agent teams và inter-agent delegation trên 13+ LLM providers với multi-tenant isolation đầy đủ.

Phiên bản Go của [OpenClaw](https://github.com/openclaw/openclaw) với bảo mật nâng cao, multi-tenant PostgreSQL và observability cấp production.

## Điểm Nổi Bật

- **Agent Teams & Orchestration** — Teams với shared task boards, inter-agent delegation (sync/async), và hybrid agent discovery
- **Multi-Tenant PostgreSQL** — Per-user workspaces, per-user context files, encrypted API keys (AES-256-GCM), isolated sessions — Claw project duy nhất với DB-native multi-tenancy
- **Single Binary** — ~25 MB static Go binary, không cần Node.js runtime, <1s startup, chạy trên VPS $5
- **Production Security** — 5-layer defense: rate limiting, prompt injection detection, SSRF protection, shell deny patterns, AES-256-GCM encryption
- **13+ LLM Providers** — Anthropic (native HTTP+SSE với prompt caching), OpenAI, OpenRouter, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope (Qwen), Bailian Coding + Claude CLI (stdio + MCP bridge), Codex (gpt-5.3-codex qua OAuth)
- **7 Messaging Channels** — Telegram (forum topics, STT), Discord, Slack, Zalo OA, Zalo Personal (DM + groups), Feishu/Lark (streaming cards, media), WhatsApp với `/stop` và `/stopall` commands
- **Extended Thinking** — Per-provider thinking mode (Anthropic budget tokens, OpenAI reasoning effort, DashScope thinking budget) với streaming support

## Hệ Sinh Thái Claw

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
| MCP integration            | — (uses ACP)                         | —                                            | —                                     | ✅ (stdio/SSE/streamable-http) |
| Agent teams                | —                                    | —                                            | —                                     | ✅ Task board + mailbox        |
| Security hardening         | ✅ (SSRF, path traversal, injection) | ✅ (sandbox, rate limit, injection, pairing) | Basic (workspace restrict, exec deny) | ✅ 5-layer defense             |
| OTel observability         | ✅ (opt-in extension)                | ✅ (Prometheus + OTLP)                       | —                                     | ✅ OTLP (opt-in build tag)     |
| Prompt caching             | —                                    | —                                            | —                                     | ✅ Anthropic + OpenAI-compat   |
| Knowledge graph            | —                                    | —                                            | —                                     | ✅ LLM extraction + traversal  |
| Skill system               | ✅ Embeddings/semantic               | ✅ SKILL.md + TOML                           | ✅ Basic                              | ✅ BM25 + pgvector hybrid      |
| Lane-based scheduler       | ✅                                   | Bounded concurrency                          | —                                     | ✅ (main/subagent/team/cron + concurrent group runs) |
| Messaging channels         | 37+                                  | 15+                                          | 10+                                   | 7+                             |
| Companion apps             | macOS, iOS, Android                  | Python SDK                                   | —                                     | Web dashboard                  |
| Live Canvas / Voice        | ✅ (A2UI + TTS/STT)                  | —                                            | Voice transcription                   | TTS (4 providers)              |
| LLM providers              | 10+                                  | 8 native + 29 compat                         | 13+                                   | **13+**                        |
| Per-user workspaces        | ✅ (file-based)                      | —                                            | —                                     | ✅ (PostgreSQL)                |
| Encrypted secrets          | — (env vars only)                    | ✅ ChaCha20-Poly1305                         | — (plaintext JSON)                    | ✅ AES-256-GCM in DB           |

> **Điểm mạnh của GoClaw:** Project duy nhất với multi-tenant PostgreSQL, agent teams, hooks system, knowledge graph, và MCP protocol support.

## Kiến Trúc

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
        BUS --> SCHED["Lane-based Scheduler<br/>main · subagent · team · cron"]
        SCHED --> ROUTER["Agent Router"]
        ROUTER --> LOOP["Agent Loop<br/>think → act → observe"]
        LOOP --> TOOLS["Tool Registry<br/>fs · exec · web · memory · team · mcp · custom"]
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

GoClaw hỗ trợ bốn orchestration patterns cho agent collaboration, tất cả được quản lý thông qua permission links.

### Agent Delegation

> **Lưu ý:** Standalone `delegate` tool đã bị loại bỏ. Delegation hiện được quản lý thông qua agent teams — leads tạo tasks trên shared board và spawn members một cách rõ ràng. Các patterns dưới đây mô tả conceptual model; xem [Agent Teams](#agent-teams) cho tooling hiện tại.

Agent delegation cho phép các named agents giao việc cho agents khác — mỗi cái chạy với identity, tools, LLM provider, và context files riêng. Khác với subagents (anonymous clones của parent), delegation targets là fully independent agents.

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

| Mode | Cách hoạt động | Phù hợp cho |
|------|----------------|-------------|
| **Sync** | Agent A hỏi Agent B và **đợi** câu trả lời | Quick lookups, fact checks |
| **Async** | Agent A hỏi Agent B và **tiếp tục**. B thông báo kết quả sau | Long tasks, reports, deep analysis |

**Permission Links** — Agents giao tiếp thông qua **agent links** rõ ràng với access control:

```bash
# One-way: support-bot có thể delegate TO research-bot
agents.links.create {
  "sourceAgent": "support-bot",
  "targetAgent": "research-bot",
  "direction": "outbound",
  "maxConcurrent": 3
}

# Bidirectional: cả hai agents có thể delegate cho nhau
agents.links.create {
  "sourceAgent": "support-bot",
  "targetAgent": "content-writer",
  "direction": "bidirectional"
}
```

| Direction | Ý nghĩa |
|-----------|---------|
| `outbound` | Source có thể delegate TO target |
| `inbound` | Target có thể delegate TO source |
| `bidirectional` | Cả hai agents có thể delegate cho nhau |

**Concurrency Control** — Hai layers ngăn agent bị quá tải:

| Layer | Config | Example |
|-------|--------|---------|
| **Per-link** | `agent_links.max_concurrent` | support → research: max 3 |
| **Per-agent** | `agents.other_config.max_delegation_load` | research-bot: max 5 total |

**Per-User Restrictions** — JSONB `settings` trên agent links hỗ trợ per-user deny/allow lists.

**Agent Discovery** — Mỗi agent có `frontmatter` field cho discovery. Với ≤15 targets, auto-generated `AGENTS.md` được inject vào context. Delegation sử dụng subagent spawning cho target sets lớn hơn.

<details>
<summary>Delegation vs Subagents</summary>

| Aspect | Subagents | Agent Delegation |
|--------|-----------|-----------------|
| Target | Anonymous clone của parent | Named agent với identity riêng |
| Provider/Model | Kế thừa từ parent | Configuration riêng của target |
| Tools | Parent's tools trừ deny list | Target's own tool registry + policy |
| Context files | Simplified system prompt | Target's own SOUL.md, IDENTITY.md, etc. |
| Session | Shared với parent | Isolated (fresh per delegation) |
| Permission | Depth-based limits only | Explicit `agent_links` với direction |
| User control | None | Per-user deny/allow qua settings JSONB |
| Concurrency | Global + per-parent limits | Per-link + per-target-agent limits |

</details>

### Agent Teams

Teams cho phép coordinated multi-agent workflows với shared task board và peer-to-peer messaging.

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

- **Team roles** — Lead agent điều phối công việc, member agents thực thi tasks
- **Shared task board** — Create, claim, complete, search tasks với `blocked_by` dependencies. Atomic claiming ngăn double-assignment
- **Team mailbox** — Direct peer-to-peer messaging (send, broadcast, read unread)
- **Tools**: `team_tasks` cho task management, `team_message` cho mailbox

## Tính Năng

### LLM Providers
- **13+ providers** — OpenRouter, Anthropic, OpenAI, Groq, DeepSeek, Gemini, Mistral, xAI, MiniMax, Cohere, Perplexity, DashScope (Qwen), Bailian Coding, và bất kỳ OpenAI-compatible endpoint nào
- **Anthropic native** — Direct HTTP+SSE integration với prompt caching (`cache_control`) cho ~90% cost reduction trên repeated prefixes. Cũng hỗ trợ Claude CLI mode (stdio + MCP bridge với session management)
- **OpenAI-compatible** — Automatic prompt caching cho OpenAI, MiniMax, OpenRouter (cache metrics tracked in traces). Cũng hỗ trợ Codex mode (gpt-5.3-codex qua OAuth với "phase" metadata)
- **Extended thinking** — Per-provider thinking mode: Anthropic (budget tokens), OpenAI-compat (reasoning effort), DashScope (thinking budget) với streaming support

### Agent Orchestration
- **Agent loop** — Think-act-observe cycle với tool use, session history, và auto-summarization
- **Subagents** — Spawn child agents với models khác nhau cho parallel task execution
- **Agent delegation** — Sync/async inter-agent task delegation với permission links, concurrency limits, và per-user restrictions
- **Agent teams** — Shared task boards với dependencies, team mailbox, và coordinated multi-agent workflows
- **Delegation history** — Queryable audit trail của tất cả inter-agent delegations
- **Concurrent execution** — Lane-based scheduler (main/subagent/team/cron), adaptive throttle cho group chats

### Tools & Integrations
- **60+ built-in tools** — File system, shell exec, web search/fetch, memory, browser automation, TTS, và nhiều hơn nữa
- **MCP integration** — Kết nối external MCP servers qua stdio, SSE, hoặc streamable-http với per-agent/per-user grants
- **Hooks system** — Event-driven hooks với command evaluators (shell exit code) và agent evaluators (delegate to reviewer) cho output validation

### Messaging Channels
- **Telegram** — Full integration với streaming, rich formatting (HTML, tables, code blocks), reactions, media, forum topics (per-topic config và session isolation), speech-to-text, bot commands, group file writer restrictions
- **Slack** — Channel integration với bot commands
- **Feishu/Lark** — Streaming card updates, media attachments (images/files), mention resolution, topic session mode
- **Zalo OA** — Official Account integration cho DM conversations
- **Zalo Personal** — Unofficial reverse-engineered protocol hỗ trợ DM + group messages với restrictive default policies
- **Discord, WhatsApp** — Channel adapters với `/stop` và `/stopall` commands
- **Persistent pending messages** — Group chat messages persisted to PostgreSQL với auto-compaction (LLM summarization) khi queues vượt threshold

### Knowledge & Memory
- **Skills** — SKILL.md-based knowledge base với BM25 + embedding hybrid search (pgvector)
- **Long-term memory** — pgvector hybrid search (full-text + vector similarity) với admin dashboard cho CRUD, search, và bulk re-indexing
- **Knowledge graph** — LLM-powered entity/relationship extraction từ memory, graph traversal (recursive CTE, max depth 3), và force-directed visualization. Agent tool: `knowledge_graph_search`

### Infrastructure
- **Cron scheduling** — `at`, `every`, và cron expression syntax cho scheduled agent tasks
- **Browser automation** — Headless Chrome qua Rod cho web interaction
- **Text-to-Speech** — OpenAI, ElevenLabs, Edge, MiniMax providers
- **Docker sandbox** — Isolated code execution trong containers
- **Tracing** — LLM call tracing với cache metrics, span metadata, và optional OpenTelemetry OTLP export
- **Tailscale** — Optional VPN mesh listener cho secure remote access (build-tag gated)

### Security
- **Rate limiting** — Token bucket per user/IP, configurable RPM
- **API key management** — Multi-key auth với RBAC scopes (`admin`, `read`, `write`, `approvals`, `pairing`), SHA-256 hashed storage, optional expiry, revocation
- **Prompt injection detection** — 6-pattern regex scanner (detection-only, never blocks)
- **Credential scrubbing** — Auto-redact API keys, tokens, passwords từ tool outputs
- **Shell deny patterns** — Blocks `curl|sh`, reverse shells, `eval $()`, `base64|sh`
- **SSRF protection** — DNS pinning, blocked private IPs, blocked hosts
- **AES-256-GCM** — Encrypted provider API keys trong database
- **Browser pairing** — Token-free browser auth với admin-approved pairing codes

### Web Dashboard
- Agent management, traces & spans viewer, skills, teams, MCP servers, pairing approval, memory management (CRUD + search + chunking), knowledge graph (table + force-directed visualization), pending messages dashboard, API key management, và interactive API documentation (Swagger UI)

## Bắt Đầu Nhanh

```bash
git clone https://github.com/nextlevelbuilder/goclaw.git
cd goclaw
```

### Từ Source

```bash
# Build
make build

# Interactive setup wizard
./goclaw onboard

# Start the gateway
source .env.local && ./goclaw
```

### Với Docker

**1. Chuẩn bị environment:**

```bash
# Generate .env với auto-generated secrets (GOCLAW_ENCRYPTION_KEY, GOCLAW_GATEWAY_TOKEN)
chmod +x prepare-env.sh
./prepare-env.sh
```

Script tạo `.env` từ `.env.example`, auto-generates `GOCLAW_ENCRYPTION_KEY` và `GOCLAW_GATEWAY_TOKEN`, và kiểm tra provider API key. Thêm ít nhất một `GOCLAW_*_API_KEY` vào `.env` trước khi khởi động.

**2. Khởi động services:**

```bash
# Recommended: Gateway + Web Dashboard (http://localhost:3000)
# Pull pre-built images:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.selfservice.yml up -d

# Or build from source:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.selfservice.yml up -d --build

# Without dashboard
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d

# + OpenTelemetry tracing (Jaeger at http://localhost:16686)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.otel.yml up -d --build

# + Tailscale (secure remote access)
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.tailscale.yml up -d --build
```

Khi `GOCLAW_*_API_KEY` environment variables được set, gateway **auto-onboards** mà không cần interactive prompts — nó detects provider, generates gateway token, connects to Postgres, runs migrations, và seeds default data.

**Auto-onboard detects** API key đầu tiên theo thứ tự ưu tiên: OpenRouter → Anthropic → OpenAI → Groq → DeepSeek → Gemini → Mistral → xAI → MiniMax → Cohere → Perplexity. Override với `GOCLAW_PROVIDER` và `GOCLAW_MODEL`. Memory được auto-enabled với embedding support nếu OpenAI, OpenRouter, hoặc Gemini key được detect.

**Minimum `.env`:**

```bash
GOCLAW_OPENROUTER_API_KEY=sk-or-your-key    # Required: ít nhất một provider key
GOCLAW_GATEWAY_TOKEN=...                     # Auto-generated by prepare-env.sh
GOCLAW_ENCRYPTION_KEY=...                    # Auto-generated by prepare-env.sh
# GOCLAW_PROVIDER=openrouter                 # Optional: override default provider
# GOCLAW_MODEL=anthropic/claude-sonnet-4     # Optional: override default model
# POSTGRES_PASSWORD=your-secure-password     # Optional: defaults to "goclaw"
```

## Deployment

GoClaw yêu cầu PostgreSQL với pgvector. Thiết kế cho multi-user và multi-tenant deployments với **per-user isolation** — mỗi user có context files, session history, và workspace riêng.

```bash
# Set up database
export GOCLAW_POSTGRES_DSN="postgres://user:pass@localhost:5432/goclaw?sslmode=disable"
export GOCLAW_ENCRYPTION_KEY=$(openssl rand -hex 32)

# Run database upgrade (schema migrations + data hooks)
./goclaw upgrade

# Start gateway
./goclaw
```

**Tính năng:**

- Per-user context files và workspaces (`user_context_files` table)
- Agent types: `open` (per-user workspace) vs `predefined` (shared context)
- Agent teams, delegation
- LLM call tracing với spans và prompt cache metrics
- MCP server integration với per-agent và per-user access grants
- Embedding-based skill search (hybrid BM25 + pgvector)
- Web dashboard cho agents, traces, skills, teams, và MCP servers
- API key encryption (AES-256-GCM)

## Cài Đặt

### Prerequisites

- Go 1.26+
- PostgreSQL 18 với pgvector
- Docker (optional, cho sandbox và containerized deployment)

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

**So sánh binary size trong Claw ecosystem:**

| Build                    | Binary Size | Docker Image | Notes                                     |
| ------------------------ | ----------- | ------------ | ----------------------------------------- |
| **GoClaw** (base)        | ~25 MB      | ~50 MB       | `CGO_ENABLED=0 go build -ldflags="-s -w"` |
| **GoClaw** (+ OTel)      | ~36 MB      | ~60 MB       | Add `-tags otel` cho OTLP export          |
| **GoClaw** (+ Tailscale) | ~54 MB      | ~75 MB       | Add `-tags tsnet` cho Tailscale listener  |
| **GoClaw** (+ both)      | ~65 MB      | ~85 MB       | `-tags "otel,tsnet"`                      |
| PicoClaw                 | ~8 MB       | —            | Single Go binary                          |
| ZeroClaw                 | 3.4 MB      | —            | Minimal Rust binary                       |
| OpenClaw                 | 28 MB       | —            | + ~390 MB Node.js runtime required        |

> Optional features được gated behind build tags để tránh binary bloat. OTel thêm ~11 MB (gRPC + protobuf). Tailscale thêm ~20 MB (tsnet + WireGuard). Base build includes in-app tracing backed by PostgreSQL và localhost-only access.

### Docker Images (Pre-built)

Pre-built multi-arch images (linux/amd64 + linux/arm64) được publish đến **GHCR** và **Docker Hub** trên mỗi release:

```bash
# GHCR (recommended)
docker pull ghcr.io/nextlevelbuilder/goclaw:latest

# Docker Hub
docker pull digitop/goclaw:latest
```

**Available tags:**

| Tag        | Description                            |
| ---------- | -------------------------------------- |
| `latest`   | Base image (~50 MB Alpine)             |
| `node`     | + Node.js runtime cho JS tools         |
| `python`   | + Python runtime cho Python tools      |
| `full`     | + Node.js + Python + all bundled skills |
| `otel`     | + OpenTelemetry tracing support        |
| `tsnet`    | + Tailscale VPN mesh listener          |
| `redis`    | + Redis cache backend                  |

Semver tags cũng available: `1.0.0`, `1.0`, etc. (e.g. `ghcr.io/nextlevelbuilder/goclaw:1.0.0-python`).

**Web Dashboard:**

```bash
docker pull ghcr.io/nextlevelbuilder/goclaw-web:latest
docker pull digitop/goclaw-web:latest
```

### Docker Build (from source)

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

## Cấu Hình

### Setup Wizard

```bash
./goclaw onboard
```

Wizard configures: provider, model, gateway port, channels, memory, browser, TTS, và tracing. Nó generates `config.json` (no secrets) và `.env.local` (secrets only).

### Auto-Onboard (Docker / CI)

Khi `GOCLAW_*_API_KEY` environment variables được set, gateway tự động configure mà không cần interactive prompts. Nó retries Postgres connection (up to 5 attempts), runs migrations, và seeds default data.

### Environment Variables

<details>
<summary><strong>Provider API Keys</strong> (set ít nhất một)</summary>

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
| `GOCLAW_OWNER_IDS`        | Admin user IDs (comma-separated) — owners có thể manage **tất cả** agents regardless of ownership và được dùng làm default owner cho auto-seeded resources |                              |

</details>

<details>
<summary><strong>Database</strong></summary>

| Variable                | Description                            |
| ----------------------- | -------------------------------------- |
| `GOCLAW_POSTGRES_DSN`   | PostgreSQL connection string           |
| `GOCLAW_ENCRYPTION_KEY` | AES-256-GCM key cho API key encryption |
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
| `GOCLAW_LANE_TEAM`     | Team lane concurrency        | `100`   |
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
| `GOCLAW_TELEMETRY_PROTOCOL`     | `grpc` hoặc `http`          | `grpc`           |
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

Đặt skill folder trong `skills/` (local dev) hoặc `/app/bundled-skills/` (Docker image). Mỗi folder phải chứa `SKILL.md` với YAML frontmatter (`name`, `description`, `slug`). Folders có prefix `_` được xử lý như shared code, không phải skills.

Khi server startup, seeder tự động discovers tất cả skill folders, upserts chúng vào database, và chạy async dependency check. Không cần environment variable — seeder falls back to `skills/` trong local dev và `/app/bundled-skills` trong Docker.

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

Interactive API documentation available tại `/docs` (Swagger UI) khi gateway đang chạy. OpenAPI 3.0 spec được serve tại `/v1/openapi.json`.

| Documentation | Description |
|---------------|-------------|
| [HTTP REST API](docs/18-http-api.md) | 130+ HTTP endpoints — chat completions, agents, skills, providers, MCP, memory, knowledge graph, channels, traces, usage, storage, API keys |
| [WebSocket RPC](docs/19-websocket-rpc.md) | 64+ RPC methods — chat, agents, config, sessions, cron, teams, pairing, delegations, approvals |
| [API Keys & Auth](docs/20-api-keys-auth.md) | Authentication model, RBAC scopes, API key management, security design |
| [Gateway Protocol](docs/04-gateway-protocol.md) | WebSocket wire protocol (v3), frame format, connection lifecycle |

## Docker Compose

Composable files cho các deployment scenarios:

| File                             | Purpose                                            |
| -------------------------------- | -------------------------------------------------- |
| `docker-compose.yml`             | Base service definition                            |
| `docker-compose.postgres.yml`    | PostgreSQL (pgvector/pgvector:pg18)                |
| `docker-compose.upgrade.yml`     | One-shot database upgrade service                  |
| `docker-compose.selfservice.yml` | Web dashboard UI (nginx + React SPA)               |
| `docker-compose.browser.yml`     | Headless Chrome cho browser automation             |
| `docker-compose.sandbox.yml`     | Docker-based code execution sandbox                |
| `docker-compose.otel.yml`        | OpenTelemetry + Jaeger tracing                     |
| `docker-compose.redis.yml`       | Redis cache backend (build-tag gated)              |
| `docker-compose.tailscale.yml`   | Tailscale VPN mesh listener                        |

### Examples

```bash
# Prepare .env (auto-generates secrets, prompts for API key)
chmod +x prepare-env.sh && ./prepare-env.sh

# Using pre-built images (no --build flag):
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d

# Managed + Web Dashboard (http://localhost:3000)
docker compose -f docker-compose.yml \
  -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

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

> **Lưu ý:** Omit `--build` để sử dụng pre-built images từ GHCR. Add `--build` để build from source. Overlays yêu cầu build args (otel, tsnet, redis, sandbox) cần `--build`.

### Upgrading (Docker Compose)

**Simple upgrade** — pull latest images và restart. Entrypoint tự động runs `goclaw upgrade` (schema migrations + data hooks) trước khi start:

```bash
# Using pre-built images (recommended):
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml pull
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d

# Or build from source:
git pull
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.selfservice.yml up -d --build
```

Replace compose files với bất kỳ overlays bạn sử dụng (e.g. add `-f docker-compose.otel.yml` cho OTel).

**Explicit upgrade** — nếu bạn muốn preview changes hoặc run upgrade separately trước khi restart:

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

Sử dụng `prepare-env.sh` script để generate `.env` với auto-generated secrets:

```bash
./prepare-env.sh
```

Script này tạo `.env` với `GOCLAW_ENCRYPTION_KEY` và `GOCLAW_GATEWAY_TOKEN` pre-filled. Bạn chỉ cần add provider API key. Xem `.env.example` cho tất cả available variables.

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
| ~~`delegate`~~     | orchestration | ~~Delegate tasks to other agents~~ (removed — use `team_tasks`) |
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

Browser clients có thể authenticate mà không cần pre-shared tokens sử dụng pairing code flow:

1. User mở web dashboard và nhập User ID
2. Clicks "Request Access (Pairing)" — gateway generates an 8-character code
3. Code được hiển thị trong browser UI
4. Admin approves code qua CLI (`goclaw pairing approve XXXX`) hoặc web UI
5. Browser tự động detects approval và gains operator-level access
6. On subsequent visits, browser reconnects automatically using stored pairing (no re-approval needed)

**Revoking access:**

```bash
# List paired devices
goclaw pairing list

# Revoke a specific pairing
goclaw pairing revoke <sender_id>
```

After revocation, browser falls back to pairing flow on next visit.

## Tailscale (Remote Access)

GoClaw hỗ trợ optional [Tailscale](https://tailscale.com) listener cho secure remote access qua VPN mesh. Tailscale listener chạy alongside main gateway, serving same routes trên cả hai listeners.

**Build-tag gated:** `tsnet` dependency (~20 MB) chỉ được compile khi build với `-tags tsnet`. Default binary không bị ảnh hưởng.

```bash
# Build with Tailscale support
go build -tags tsnet -o goclaw .

# Configure via environment variables
export GOCLAW_TSNET_HOSTNAME=goclaw-gateway
export GOCLAW_TSNET_AUTH_KEY=tskey-auth-xxxxx

# Start — both localhost:18790 and Tailscale listener are active
./goclaw
```

Khi Tailscale enabled và gateway vẫn bound to `0.0.0.0`, một log suggestion recommends switching to `127.0.0.1` cho localhost-only + Tailscale access:

```
GOCLAW_HOST=127.0.0.1 ./goclaw
```

Điều này keeps gateway inaccessible from LAN trong khi vẫn reachable via Tailscale từ bất kỳ device nào trên tailnet của bạn.

**Docker:**

```bash
docker compose -f docker-compose.yml \
  -f docker-compose.postgres.yml \
  -f docker-compose.tailscale.yml up -d
```

Requires `GOCLAW_TSNET_AUTH_KEY` trong `.env` file. Tailscale state được persisted trong `tsnet-state` Docker volume.

## Security

- **Transport**: WebSocket CORS validation, 512KB message limit, 1MB HTTP body limit, timing-safe token auth
- **API key management**: Multi-key auth với 5 RBAC scopes, SHA-256 hashed storage, optional expiry, revocation, show-once pattern. Xem [API Keys & Auth](docs/20-api-keys-auth.md)
- **Rate limiting**: Token bucket per user/IP, configurable RPM
- **Prompt injection**: Input guard với 6 pattern detection (detection-only, never blocks)
- **Shell security**: Deny patterns cho `curl|sh`, `wget|sh`, reverse shells, `eval`, `base64|sh`
- **Network**: SSRF protection với blocked hosts + private IP + DNS pinning
- **File system**: Path traversal prevention, workspace restriction
- **Encryption**: AES-256-GCM cho provider API keys trong database
- **Browser pairing**: Token-free browser auth với admin approval (pairing codes, auto-reconnect)
- **Tailscale**: Optional VPN mesh listener cho secure remote access (build-tag gated)

## Testing

```bash
# Unit tests
go test ./...

# Integration tests (requires running gateway)
go test -v -run 'TestHealthHTTP|TestConnectHandshake' ./tests/integration/

# Full integration (requires API key)
GOCLAW_OPENROUTER_API_KEY=sk-or-xxx go test -v ./tests/integration/ -timeout 120s
```

## Tình Trạng Dự Án

### Implemented & Tested in Production

- **Agent management & configuration** — Create, update, delete agents via API và web dashboard. Agent types (`open` / `predefined`), agent routing, và lazy resolution tất cả đã tested.
- **Telegram channel** — Full integration tested: message handling, streaming responses, rich formatting (HTML, tables, code blocks), reactions, media, chunked long messages.
- **Seed data & bootstrapping** — Auto-onboard, DB seeding, migration pipeline tested end-to-end.
- **User-scope & content files** — Per-user context files (`user_context_files`), agent-level context files (`agent_context_files`), virtual FS interceptors, per-user seeding (`SeedUserFiles`), và user-agent profile tracking tất cả đã implemented và tested.
- **Core built-in tools** — File system tools (`read_file`, `write_file`, `edit_file`, `list_files`, `search`, `glob`), shell execution (`exec`), web tools (`web_search`, `web_fetch`), và session management tools tested trong real agent loops.
- **Memory system** — Long-term memory với pgvector hybrid search (FTS + vector) implemented và tested với real conversations.
- **Agent loop** — Think-act-observe cycle, tool use, session history, auto-summarization, và subagent spawning tested trong production.
- **WebSocket RPC protocol (v3)** — Connect handshake, chat streaming, event push tất cả tested với web dashboard và integration tests.
- **Store layer (PostgreSQL)** — All PG stores (sessions, agents, providers, skills, cron, pairing, tracing, memory, teams) implemented và running.
- **Browser automation** — Rod/CDP integration cho headless Chrome, tested trong production agent workflows.
- **Lane-based scheduler** — Main/subagent/team/cron lane isolation với concurrent execution tested. Group chats hỗ trợ up to 3 concurrent agent runs per session với adaptive throttle và deferred session writes cho history isolation.
- **Security hardening** — Rate limiting, prompt injection detection, CORS, shell deny patterns, SSRF protection, credential scrubbing tất cả implemented và verified.
- **Web dashboard** — Channel management, agent management, pairing approval, traces & spans viewer, skills, MCP, cron, sessions, teams, và config pages tất cả implemented và working.
- **Prompt caching** — Anthropic (explicit `cache_control`), OpenAI/MiniMax/OpenRouter (automatic). Cache metrics tracked trong trace spans và displayed trong web dashboard.
- **Agent delegation** — Inter-agent task delegation với permission links, sync/async modes, per-user restrictions, concurrency limits, và hybrid agent search. Tested trong production.
- **Agent teams** — Team creation với lead/member roles, shared task board (create, claim, complete, search, blocked_by dependencies), team mailbox (send, broadcast, read). Tested trong production.
- **Evaluate loop** — Generator-evaluator feedback cycles với configurable max rounds và pass criteria. Tested trong production.
- **Delegation history** — Queryable audit trail của inter-agent delegations. Tested trong production.
- **Skill system** — BM25 search, ZIP upload, SKILL.md parsing, và embedding hybrid search. Tested trong production.
- **MCP integration** — stdio, SSE, và streamable-http transports với per-agent/per-user grants. Tested trong production.
- **Cron scheduling** — `at`, `every`, và cron expression scheduling. Tested trong production.
- **Docker sandbox** — Isolated code execution trong containers. Tested trong production.
- **Text-to-Speech** — OpenAI, ElevenLabs, Edge, MiniMax providers. Tested trong production.
- **HTTP API** — `/v1/chat/completions`, `/v1/agents`, `/v1/skills`, etc. Tested trong production. Interactive Swagger UI at `/docs`.
- **API key management** — Multi-key auth với RBAC scopes, SHA-256 hashed storage, show-once pattern, optional expiry, revocation. HTTP + WebSocket CRUD. Web UI cho management.
- **Hooks system** — Event-driven hooks với command evaluators (shell exit code) và agent evaluators (delegate to reviewer). Blocking gates với auto-retry và recursion-safe evaluation.
- **Media tools** — `create_image` (DashScope, MiniMax), `create_audio` (OpenAI, ElevenLabs, MiniMax, Suno), `create_video` (MiniMax, Veo), `read_document` (Gemini File API), `read_image`, `read_audio`, `read_video`. Persistent media storage với lazy-loaded MediaRef.
- **Additional provider modes** — Claude CLI (Anthropic via stdio + MCP bridge), Codex (OpenAI gpt-5.3-codex via OAuth).
- **Knowledge graph** — LLM-powered entity extraction, graph traversal, force-directed visualization, và `knowledge_graph_search` agent tool.
- **Memory management** — Admin dashboard cho memory documents (CRUD, semantic search, chunk/embedding details, bulk re-indexing).
- **Persistent pending messages** — Channel messages persisted to PostgreSQL với auto-compaction (LLM summarization) và monitoring dashboard.

### Implemented but Not Fully Tested

- **Slack** — Channel integration implemented, chưa validated với real users.
- **Other messaging channels** — Discord, Zalo OA, Zalo Personal, Feishu/Lark, WhatsApp channel adapters đã implemented nhưng chưa được tested end-to-end trong production. Chỉ Telegram đã được validated với real users.
- **OpenTelemetry export** — OTLP gRPC/HTTP exporter implemented (build-tag gated). In-app tracing works; external OTel export chưa validated trong production.
- **Tailscale integration** — tsnet listener implemented (build-tag gated). Chưa tested trong real deployment.
- **Redis cache** — Optional distributed cache backend (build-tag gated). Chưa tested trong production.
- **Browser pairing** — Pairing code flow implemented với CLI và web UI approval. Basic flow tested nhưng chưa validated at scale.

## Acknowledgments

GoClaw được xây dựng dựa trên project gốc [OpenClaw](https://github.com/openclaw/openclaw). Chúng tôi biết ơn về architecture và vision đã inspire Go port này.

## License

MIT