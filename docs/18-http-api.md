# 18 — HTTP REST API

GoClaw exposes a comprehensive HTTP REST API alongside the WebSocket RPC protocol. All endpoints are served from the same gateway server and share authentication, rate limiting, and i18n infrastructure.

Interactive documentation is available at `/docs` (Swagger UI) and the raw OpenAPI 3.0 spec at `/v1/openapi.json`.

---

## 1. Authentication

All HTTP endpoints (except `/health`) require authentication via Bearer token in the `Authorization` header:

```
Authorization: Bearer <TOKEN>
```

Two token types are accepted:

| Type | Format | Scope |
|------|--------|-------|
| Gateway token | Configured in `config.json` | Full admin access |
| API key | `goclaw_` + 32 hex chars | Scoped by key permissions |

API keys are hashed with SHA-256 before lookup — the raw key is never stored. See [20 — API Keys & Auth](20-api-keys-auth.md) for details.

> Some endpoints accept the token as a query parameter `?token=<TOKEN>` for use in `<img>` and `<audio>` tags (e.g., `/v1/files/`, `/v1/media/`).

### Common Headers

| Header | Purpose |
|--------|---------|
| `Authorization` | Bearer token for authentication |
| `X-GoClaw-User-Id` | External user ID for multi-tenant context |
| `X-GoClaw-Agent-Id` | Agent identifier for scoped operations |
| `X-GoClaw-Tenant-Id` | Tenant scope — UUID or slug (gateway token / cross-tenant API keys) |
| `Accept-Language` | Locale (`en`, `vi`, `zh`) for i18n error messages |
| `Content-Type` | `application/json` for request bodies |

---

## 2. Chat Completions

OpenAI-compatible chat API for programmatic access to agents.

### `POST /v1/chat/completions`

```json
{
  "model": "goclaw:agent-id-or-key",
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "stream": false,
  "user": "optional-user-id"
}
```

**Response** (non-streaming):

```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "..."},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
}
```

**Streaming:** Set `"stream": true` to receive Server-Sent Events (SSE) with `data: {...}` chunks, terminated by `data: [DONE]`.

**Rate limiting:** Per-IP when `rate_limit_rpm` is configured.

---

## 3. OpenResponses Protocol

### `POST /v1/responses`

Alternative response-based protocol (compatible with OpenAI Responses API). Accepts the same auth and returns structured response objects.

---

## 4. Agents

CRUD operations for agent management. Requires `X-GoClaw-User-Id` header for multi-tenant context.

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| `GET` | `/v1/agents` | List agents accessible by user | Bearer |
| `POST` | `/v1/agents` | Create new agent | Bearer |
| `GET` | `/v1/agents/{id}` | Get agent by ID or key | Bearer |
| `PUT` | `/v1/agents/{id}` | Update agent (owner only) | Bearer |
| `DELETE` | `/v1/agents/{id}` | Delete agent (owner only) | Bearer |
| `POST` | `/v1/agents/sync-workspace` | Sync agent workspace files | Admin |

### Shares

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/agents/{id}/shares` | List agent shares |
| `POST` | `/v1/agents/{id}/shares` | Share agent with user |
| `DELETE` | `/v1/agents/{id}/shares/{userID}` | Revoke share |

### Agent Actions

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/agents/{id}/regenerate` | Regenerate agent config with custom prompt |
| `POST` | `/v1/agents/{id}/resummon` | Retry initial LLM summoning |
| `POST` | `/v1/agents/{id}/cancel-summon` | Cancel an in-progress summon |
| `GET` | `/v1/agents/{id}/system-prompt-preview` | Preview rendered system prompt |

### Predefined Agent Instances

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/agents/{id}/instances` | List user instances |
| `GET` | `/v1/agents/{id}/instances/{userID}/files` | List user context files |
| `PUT` | `/v1/agents/{id}/instances/{userID}/files/{fileName}` | Update user file (USER.md only) |
| `PATCH` | `/v1/agents/{id}/instances/{userID}/metadata` | Update instance metadata |

### Wake (External Trigger)

```
POST /v1/agents/{id}/wake
```

```json
{
  "message": "Process new data",
  "session_key": "optional-session",
  "user_id": "optional-user",
  "metadata": {}
}
```

Response: `{content, run_id, usage?}`. Used by orchestrators (n8n, Paperclip) to trigger agent runs.

### Codex/OpenAI OAuth Routing in `other_config`

For agents whose main `provider` is a `chatgpt_oauth` provider, `other_config.chatgpt_oauth_routing`
can override or inherit routing behavior while keeping the main `provider` field as the preferred/default account alias.

```json
{
  "provider": "openai-codex",
  "model": "gpt-5.4",
  "other_config": {
    "chatgpt_oauth_routing": {
      "override_mode": "custom",
      "strategy": "round_robin"
    }
  }
}
```

Rules:
- Provider settings may define reusable `settings.codex_pool` defaults for a primary alias.
- `settings.codex_pool.extra_provider_names` is the authoritative membership list for that pool owner.
- A provider listed in another pool cannot also manage its own pool.
- `override_mode: "inherit"` tells the agent to follow those provider defaults.
- `override_mode: "custom"` stores an agent-local routing override for that provider-owned pool.
- `strategy: "primary_first"` keeps the main `provider` as the preferred account. When saved as a custom override with no extra names, it disables pooling for that agent.
- Provider aliases are arbitrary. `openai-codex`, `codex-work`, and `codex-team` are examples, not required prefixes.
- `strategy: "round_robin"` rotates requests across the main provider plus the provider-owned extra authenticated OpenAI Codex OAuth providers.
- `strategy: "priority_order"` tries the main provider first, then drains the provider-owned extra providers in order.
- Retryable upstream failures can fall through to the next eligible OpenAI Codex OAuth provider in the same request.
- Only enabled and authenticated `chatgpt_oauth` providers participate.
- Provider-scoped auth remains unchanged: `cmd/auth` and `/v1/auth/chatgpt/{provider}/*` still operate on explicit providers.

Provider-level defaults example:

```json
{
  "name": "openai-codex",
  "provider_type": "chatgpt_oauth",
  "settings": {
    "codex_pool": {
      "strategy": "round_robin",
      "extra_provider_names": ["codex-work", "codex-team"]
    }
  }
}
```

### Provider reasoning defaults in `settings`

Providers can store the reusable default reasoning policy in `settings.reasoning_defaults`.

```json
{
  "name": "openai-codex",
  "provider_type": "chatgpt_oauth",
  "settings": {
    "reasoning_defaults": {
      "effort": "high",
      "fallback": "provider_default"
    }
  }
}
```

Rules:
- these defaults are provider-owned and apply to any agent that inherits reasoning from this provider
- the final runtime effort is still normalized against the agent's selected model capabilities
- if no provider default is saved, inherit mode resolves to reasoning `off`

### Agent reasoning policy in `other_config`

Agents can now store capability-aware GPT-5/Codex reasoning intent under `other_config.reasoning`.

```json
{
  "provider": "openai-codex",
  "model": "gpt-5.4",
  "other_config": {
    "reasoning": {
      "override_mode": "inherit"
    }
  }
}
```

Rules:
- `reasoning.override_mode` supports `inherit|custom`
- `override_mode: "inherit"` tells the agent to follow `settings.reasoning_defaults`
- `override_mode: "custom"` stores an agent-local override; the dashboard also writes a derived `thinking_level` shim for rollback safety
- `thinking_level` remains the coarse compatibility shim: `off|low|medium|high`
- `reasoning.effort` supports `off|auto|none|minimal|low|medium|high|xhigh`
- `reasoning.fallback` supports `downgrade|off|provider_default`
- existing `reasoning` payloads without `override_mode` continue to behave as custom overrides
- unset reasoning resolves to `off`
- the runtime may normalize unsupported efforts, and the actual decision is surfaced in trace span metadata

### Export & Import

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/agents/{id}/export/preview` | Preview agent export |
| `GET` | `/v1/agents/{id}/export` | Export agent config |
| `GET` | `/v1/agents/{id}/export/download/{token}` | Download export file |
| `GET` | `/v1/export/download/{token}` | Global export download |
| `POST` | `/v1/agents/import/preview` | Preview agent import |
| `POST` | `/v1/agents/import` | Import agent |
| `POST` | `/v1/agents/{id}/import` | Merge import into existing agent |

### Team Export & Import

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/teams/{id}/export/preview` | Preview team export |
| `GET` | `/v1/teams/{id}/export` | Export team config |
| `POST` | `/v1/teams/import` | Import team |

### Codex Pool Activity

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/agents/{id}/codex-pool-activity` | Summarize recent Codex/OpenAI OAuth pool usage for one agent |

Query parameters:
- `limit` optional, defaults to `18`, max `50`

Response fields:
- `strategy`: effective routing strategy (`primary_first`, `round_robin`, or `priority_order`)
- `pool_providers`: configured primary + extra provider aliases in pool order
- `stats_sample_size`: number of recent routed `llm_call` spans used to derive runtime health. The server derives health from `max(limit, 120)` recent spans even when `recent_requests` is still capped by the requested `limit`.
- `provider_counts`: per-alias routing evidence:
  - `request_count`: backward-compatible count of direct selections
  - `direct_selection_count`: times the router selected that alias first
  - `failover_serve_count`: times that alias only served as failover
  - `success_count`, `failure_count`: trace-backed runtime outcomes attributed to that alias. Success is attributed to the alias that actually served the request. On successful failover, earlier attempted aliases receive failures. On terminal error, every attempted alias receives a failure.
  - `consecutive_failures`: current newest-first failure streak from recent trace evidence
  - `success_rate`, `health_score`, `health_state`: additive runtime health summary. `health_score` is heuristic, but the stable bands are `idle` when there are no recent outcomes, `critical` at 3+ consecutive failures or score `< 40`, `degraded` below `80`, otherwise `healthy`
  - `last_selected_at`, `last_failover_at`, `last_used_at`, `last_success_at`, `last_failure_at`: latest timestamps for each evidence type
- `recent_requests`: recent routed Codex calls:
  - `span_id`, `trace_id`, `started_at`, `status`, `duration_ms`, `model`
  - `selected_provider`: alias chosen first by the router
  - `provider_name`: alias that actually served the request. This can be empty on terminal failures where no alias completed the call.
  - `attempt_count`, `used_failover`, `failover_providers`

Use `direct_selection_count` plus the `selected_provider` sequence to verify real round-robin behavior. A provider with `failover_serve_count > 0` and `direct_selection_count = 0` was only observed as a rescue target, not as a confirmed round-robin selection.

---

## 5. Skills

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/skills` | List all skills |
| `POST` | `/v1/skills/upload` | Upload ZIP with SKILL.md (20 MB limit) |
| `GET` | `/v1/skills/{id}` | Get skill details |
| `PUT` | `/v1/skills/{id}` | Update skill metadata |
| `DELETE` | `/v1/skills/{id}` | Delete skill (not system skills) |
| `POST` | `/v1/skills/{id}/toggle` | Toggle skill enabled/disabled state |
| `PUT` | `/v1/skills/{id}/tenant-config` | Set tenant-level skill config |
| `DELETE` | `/v1/skills/{id}/tenant-config` | Delete tenant-level skill config |

### Skill Grants

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/skills/{id}/grants/agent` | Grant skill to agent |
| `DELETE` | `/v1/skills/{id}/grants/agent/{agentID}` | Revoke from agent |
| `POST` | `/v1/skills/{id}/grants/user` | Grant skill to user |
| `DELETE` | `/v1/skills/{id}/grants/user/{userID}` | Revoke from user |

### Agent Skills

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/agents/{agentID}/skills` | List skills with grant status for agent |

### Skill Files & Dependencies

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/skills/{id}/versions` | List available versions |
| `GET` | `/v1/skills/{id}/files` | List files in skill |
| `GET` | `/v1/skills/{id}/files/{path...}` | Read file content |
| `POST` | `/v1/skills/rescan-deps` | Rescan runtime dependencies |
| `POST` | `/v1/skills/install-deps` | Install all missing deps |
| `POST` | `/v1/skills/install-dep` | Install single dependency |
| `GET` | `/v1/skills/runtimes` | Check runtime availability |

### Export & Import

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/skills/export/preview` | Preview skills export bundle |
| `GET` | `/v1/skills/export` | Export skills bundle |
| `POST` | `/v1/skills/import` | Import skills bundle |

---

## 6. Providers

LLM provider management. API keys are encrypted with AES-256-GCM in the database and masked in responses.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/providers` | List providers (keys masked) |
| `POST` | `/v1/providers` | Create provider |
| `GET` | `/v1/providers/{id}` | Get provider |
| `PUT` | `/v1/providers/{id}` | Update provider |
| `DELETE` | `/v1/providers/{id}` | Delete provider |

### Provider Verification & Models

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/providers/{id}/verify` | Test provider+model with minimal LLM call |
| `GET` | `/v1/providers/{id}/models` | List models plus any known reasoning capability metadata |
| `POST` | `/v1/providers/{id}/verify-embedding` | Verify embedding model configuration |
| `GET` | `/v1/providers/{id}/codex-pool-activity` | Provider-level Codex pool activity |
| `GET` | `/v1/embedding/status` | Check global embedding availability |
| `GET` | `/v1/providers/claude-cli/auth-status` | Check Claude CLI login status |

**Supported types:** `anthropic_native`, `openai_compat`, `chatgpt_oauth`, `gemini_native`, `dashscope`, `bailian`, `minimax`, `claude_cli`, `acp`

Example response:

```json
{
  "models": [
    {
      "id": "gpt-5.4",
      "name": "GPT-5.4",
      "reasoning": {
        "levels": ["none", "low", "medium", "high", "xhigh"],
        "default_effort": "none"
      }
    },
    {
      "id": "custom-model",
      "name": "custom-model"
    }
  ],
  "reasoning_defaults": {
    "effort": "high",
    "fallback": "provider_default"
  }
}
```

Notes:
- `chatgpt_oauth` providers return a backend-owned model list because OAuth tokens cannot rely on `/v1/models`
- `reasoning_defaults` is returned only when the provider has saved defaults and at least one returned model exposes reasoning capability metadata
- unknown models remain usable and simply omit the `reasoning` field
- the web UI uses this endpoint as the source of truth for provider-first reasoning controls
- when upstream model discovery fails, the endpoint returns an empty `models` array instead of a hard error

---

## 7. MCP Servers

Model Context Protocol server management.

### Server CRUD

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/mcp/servers` | List servers with agent grant counts |
| `POST` | `/v1/mcp/servers` | Create MCP server |
| `GET` | `/v1/mcp/servers/{id}` | Get server details |
| `PUT` | `/v1/mcp/servers/{id}` | Update server |
| `DELETE` | `/v1/mcp/servers/{id}` | Delete server |
| `POST` | `/v1/mcp/servers/test` | Test connection (no save) |
| `POST` | `/v1/mcp/servers/{id}/reconnect` | Reconnect MCP server |
| `GET` | `/v1/mcp/servers/{id}/tools` | List runtime-discovered tools |

### Agent Grants

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/mcp/servers/{id}/grants` | List grants for server |
| `POST` | `/v1/mcp/servers/{id}/grants/agent` | Grant to agent |
| `DELETE` | `/v1/mcp/servers/{id}/grants/agent/{agentID}` | Revoke from agent |
| `GET` | `/v1/mcp/grants/agent/{agentID}` | List agent's server grants |

### User Grants

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/mcp/servers/{id}/grants/user` | Grant to user |
| `DELETE` | `/v1/mcp/servers/{id}/grants/user/{userID}` | Revoke from user |

### Access Requests

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/mcp/requests` | Create access request |
| `GET` | `/v1/mcp/requests` | List pending requests |
| `POST` | `/v1/mcp/requests/{id}/review` | Approve/deny request |

Grants support `tool_allow` and `tool_deny` JSON arrays for fine-grained tool filtering.

### User Credentials

Per-user credential storage for MCP servers (e.g., API keys users provide for external services).

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/v1/mcp/servers/{id}/user-credentials` | Set user credentials |
| `GET` | `/v1/mcp/servers/{id}/user-credentials` | Get user credentials |
| `DELETE` | `/v1/mcp/servers/{id}/user-credentials` | Delete user credentials |

### Export & Import

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/mcp/export/preview` | Preview MCP export bundle |
| `GET` | `/v1/mcp/export` | Export MCP servers + grants |
| `POST` | `/v1/mcp/import` | Import MCP config bundle |

---

## 8. Tools

### Built-in Tools

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/tools/builtin` | List all built-in tools |
| `GET` | `/v1/tools/builtin/{name}` | Get tool definition |
| `PUT` | `/v1/tools/builtin/{name}` | Update enabled/settings |
| `GET` | `/v1/tools/builtin/{name}/tenant-config` | Get tenant-level tool config |
| `PUT` | `/v1/tools/builtin/{name}/tenant-config` | Set tenant-level tool config |
| `DELETE` | `/v1/tools/builtin/{name}/tenant-config` | Delete tenant-level tool config |

### Direct Invocation

```
POST /v1/tools/invoke
```

```json
{
  "tool": "web_fetch",
  "action": "fetch",
  "args": {"url": "https://example.com"},
  "dryRun": false,
  "agentId": "optional",
  "channel": "optional",
  "chatId": "optional",
  "peerKind": "direct"
}
```

Set `"dryRun": true` to return tool schema without execution.

---

## 9. TTS

Text-to-speech synthesis and configuration.

### Synthesis & Connection

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/tts/synthesize` | Synthesize speech from text (operator role) |
| `POST` | `/v1/tts/test-connection` | Test TTS provider connection (operator role) |

### Configuration

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/tts/config` | Get tenant TTS configuration (admin) |
| `POST` | `/v1/tts/config` | Save tenant TTS configuration (admin) |

### Provider Capabilities

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/tts/capabilities` | Get `ProviderCapabilities` schema for all registered providers (operator role) |

The capabilities endpoint returns the dynamic param schema for each provider — param types, ranges, defaults, `dependsOn` conditions, and custom feature flags. Used by the UI to render per-provider param editors without hard-coded field lists. See [`docs/tts-provider-capabilities.md`](tts-provider-capabilities.md) for schema details.

---

## 10. Memory

Per-agent vector memory using pgvector.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/memory/documents` | List all documents globally |
| `GET` | `/v1/agents/{agentID}/memory/documents` | List documents for agent |
| `GET` | `/v1/agents/{agentID}/memory/documents/{path...}` | Get document details |
| `PUT` | `/v1/agents/{agentID}/memory/documents/{path...}` | Put/update document |
| `DELETE` | `/v1/agents/{agentID}/memory/documents/{path...}` | Delete document |
| `GET` | `/v1/agents/{agentID}/memory/chunks` | List chunks for document |
| `POST` | `/v1/agents/{agentID}/memory/index` | Index single document |
| `POST` | `/v1/agents/{agentID}/memory/index-all` | Index all documents |
| `POST` | `/v1/agents/{agentID}/memory/search` | Semantic search |

Optional query parameter `?user_id=` for per-user scoping.

---

## 11. Episodic Memory

Episodic memory captures conversation summaries per user session for long-term context continuity.

### List Episodic Summaries

```
GET /v1/agents/{agentID}/episodic
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `user_id` | string | Filter by user ID (optional). |
| `limit` | integer | Max results (default: 20, max: 500). |
| `offset` | integer | Pagination offset (default: 0). |

**Response:**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "user_id": "user-123",
    "summary": "User asked about deployment pipeline optimization. Discussed GitHub Actions, Docker layers, caching strategies. User implemented multi-stage builds.",
    "key_entities": ["GitHub Actions", "Docker", "CI/CD"],
    "sentiment": "positive",
    "interaction_count": 5,
    "tokens_exchanged": 4200,
    "created_at": "2026-04-05T10:00:00Z",
    "updated_at": "2026-04-05T11:30:00Z"
  }
]
```

### Search Episodic Summaries

```
POST /v1/agents/{agentID}/episodic/search
```

**Request:**

```json
{
  "query": "Docker optimization strategies",
  "user_id": "optional-user-id",
  "max_results": 10,
  "min_score": 0.5
}
```

Runs hybrid search combining BM25 (keyword) and vector (semantic) matching.

**Response:**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "user_id": "user-123",
    "summary": "User asked about deployment pipeline optimization...",
    "similarity_score": 0.92,
    "created_at": "2026-04-05T10:00:00Z"
  }
]
```

---

## 12. Knowledge Graph

Per-agent entity-relation graph.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/agents/{agentID}/kg/entities` | List/search entities (BM25) |
| `GET` | `/v1/agents/{agentID}/kg/entities/{entityID}` | Get entity with relations |
| `POST` | `/v1/agents/{agentID}/kg/entities` | Upsert entity |
| `DELETE` | `/v1/agents/{agentID}/kg/entities/{entityID}` | Delete entity |
| `POST` | `/v1/agents/{agentID}/kg/traverse` | Traverse graph (max depth 3) |
| `POST` | `/v1/agents/{agentID}/kg/extract` | LLM-powered entity extraction |
| `GET` | `/v1/agents/{agentID}/kg/stats` | Knowledge graph statistics |
| `GET` | `/v1/agents/{agentID}/kg/graph` | Full graph for visualization |
| `GET` | `/v1/agents/{agentID}/kg/graph/compact` | Compact graph optimized for 10k+ nodes |
| `POST` | `/v1/agents/{agentID}/kg/dedup/scan` | Scan for duplicate entities |
| `GET` | `/v1/agents/{agentID}/kg/dedup` | List dedup candidates |
| `POST` | `/v1/agents/{agentID}/kg/merge` | Merge duplicate entities |
| `POST` | `/v1/agents/{agentID}/kg/dedup/dismiss` | Dismiss dedup candidate |

---

## 13. Knowledge Vault

Persistent knowledge vault stores documents with vector embeddings and outbound/backlink graph connections.

### Global Vault Operations

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/vault/documents` | List all vault documents (cross-agent) |
| `POST` | `/v1/vault/documents` | Create vault document |
| `GET` | `/v1/vault/documents/{docID}` | Get document |
| `PUT` | `/v1/vault/documents/{docID}` | Update document |
| `DELETE` | `/v1/vault/documents/{docID}` | Delete document |
| `GET` | `/v1/vault/documents/{docID}/links` | Get document outlinks and backlinks |
| `POST` | `/v1/vault/links` | Create link between documents |
| `DELETE` | `/v1/vault/links/{linkID}` | Delete link |
| `POST` | `/v1/vault/links/batch` | Batch get links |
| `POST` | `/v1/vault/upload` | Upload document file |
| `POST` | `/v1/vault/rescan` | Rescan vault for FS sync |
| `GET` | `/v1/vault/tree` | Get vault file tree |
| `POST` | `/v1/vault/search` | Hybrid FTS+vector search (cross-agent) |
| `GET` | `/v1/vault/enrichment/status` | Get enrichment pipeline status |
| `POST` | `/v1/vault/enrichment/stop` | Stop enrichment pipeline |
| `GET` | `/v1/vault/graph` | Get vault document relationship graph |

### Per-Agent Vault Operations

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/agents/{agentID}/vault/documents` | List agent vault documents |
| `GET` | `/v1/agents/{agentID}/vault/documents/{docID}` | Get agent document |
| `POST` | `/v1/agents/{agentID}/vault/documents` | Create agent document |
| `PUT` | `/v1/agents/{agentID}/vault/documents/{docID}` | Update agent document |
| `DELETE` | `/v1/agents/{agentID}/vault/documents/{docID}` | Delete agent document |
| `GET` | `/v1/agents/{agentID}/vault/documents/{docID}/links` | Get agent document links |
| `POST` | `/v1/agents/{agentID}/vault/search` | Search agent vault documents |
| `POST` | `/v1/agents/{agentID}/vault/links` | Create link for agent document |
| `DELETE` | `/v1/agents/{agentID}/vault/links/{linkID}` | Delete agent document link |

**Query Parameters (list):**

| Parameter | Type | Description |
|-----------|------|-------------|
| `scope` | string | Filter by scope (e.g., `team`, `user`, `global`). |
| `doc_type` | string | Comma-separated doc types (e.g., `guide,reference,note`). |
| `limit` | integer | Max results (default: 20, max: 500). |
| `offset` | integer | Pagination offset. |
| `agent_id` | string | (Cross-agent only) Filter by specific agent. |

---

## 14. Evolution Metrics & Suggestions

Per-agent evolution metrics track tool usage, retrieval performance, and user feedback to drive automated agent improvements.

### Get Evolution Metrics

```
GET /v1/agents/{agentID}/evolution/metrics
```

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | no | Filter by metric type: `tool`, `retrieval`, `feedback`. Omit for all types. |
| `aggregate` | boolean | no | Return aggregated metrics (grouped by tool/metric). Default: `false` (raw metrics). |
| `since` | ISO 8601 | no | Start timestamp (default: 7 days ago). Example: `2026-04-01T00:00:00Z` |
| `limit` | integer | no | Max results (default: 100, max: 500). |

**Response (raw metrics):**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "metric_type": "tool",
    "tool_name": "web_fetch",
    "metric_key": "invocation_count",
    "metric_value": 15,
    "metadata": {"status": "success"},
    "recorded_at": "2026-04-06T10:30:00Z"
  }
]
```

**Response (aggregated metrics):**

```json
{
  "tool_aggregates": [
    {
      "tool_name": "web_fetch",
      "invocation_count": 15,
      "success_count": 14,
      "failure_count": 1,
      "avg_duration_ms": 2340
    }
  ],
  "retrieval_aggregates": [
    {
      "query_count": 42,
      "avg_recall": 0.78,
      "avg_precision": 0.85,
      "avg_relevance_score": 0.81
    }
  ]
}
```

### List Evolution Suggestions

```
GET /v1/agents/{agentID}/evolution/suggestions
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `status` | string | Filter: `pending`, `approved`, `applied`, `rejected`, `rolled_back`. Omit for all. |
| `limit` | integer | Max results (default: 50, max: 500). |

**Response:**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "suggestion_type": "low_retrieval_usage",
    "status": "pending",
    "title": "Improve retrieval threshold",
    "description": "Recent queries show low recall. Consider lowering retrieval_threshold from 0.5 to 0.4.",
    "parameters": {
      "current_threshold": 0.5,
      "proposed_threshold": 0.4,
      "confidence": 0.85
    },
    "created_at": "2026-04-06T09:00:00Z",
    "reviewed_by": null,
    "reviewed_at": null
  }
]
```

**Suggestion Types:**
- `low_retrieval_usage` — Retrieval recall is below threshold for recent queries.
- `tool_failure` — High failure rate detected for a tool.
- `repeated_tool` — Tool called repeatedly without context change; candidate for skill.

### Update Suggestion Status

```
PATCH /v1/agents/{agentID}/evolution/suggestions/{suggestionID}
```

**Request:**

```json
{
  "status": "approved",
  "reviewed_by": "optional-user-id"
}
```

**Valid status transitions:** `pending` → `approved`, `rejected`, `rolled_back`.

**Response:**

```json
{
  "status": "ok"
}
```

---

## 15. Orchestration Mode

Determines how an agent routes requests (standalone, delegation, team-based).

### Get Agent Orchestration Mode

```
GET /v1/agents/{agentID}/orchestration
```

**Response:**

```json
{
  "mode": "delegate",
  "delegate_targets": [
    {
      "agent_key": "research-agent",
      "display_name": "Research Specialist"
    }
  ],
  "team": null
}
```

Or in team mode:

```json
{
  "mode": "team",
  "delegate_targets": [],
  "team": {
    "id": "uuid",
    "name": "Platform Team"
  }
}
```

**Mode values:**
- `standalone` — No delegation. Agent handles all requests directly.
- `delegate` — Routes complex requests to specialized agents (via agent links).
- `team` — Routes to team members via task system.

---

## 16. V3 Feature Flags

Per-agent feature flags control v3 system capabilities (evolution, episodic memory, vault, etc.).

### Get V3 Flags

```
GET /v1/agents/{agentID}/v3-flags
```

**Response:**

```json
{
  "evolution_enabled": true,
  "episodic_enabled": true,
  "vault_enabled": true,
  "orchestration_enabled": false,
  "skill_evolve": true,
  "self_evolve": false
}
```

### Update V3 Flags

```
PATCH /v1/agents/{agentID}/v3-flags
```

Accepts partial updates. Flag keys are validated against recognized v3 flags.

**Request:**

```json
{
  "evolution_enabled": true,
  "episodic_enabled": false,
  "vault_enabled": true
}
```

**Response:**

```json
{
  "status": "ok"
}
```

---

## 17. Channels

### Channel Instances

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/channels/instances` | List instances (paginated) |
| `POST` | `/v1/channels/instances` | Create instance |
| `GET` | `/v1/channels/instances/{id}` | Get instance |
| `PUT` | `/v1/channels/instances/{id}` | Update instance |
| `DELETE` | `/v1/channels/instances/{id}` | Delete instance (not default) |

### Contacts

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/contacts` | List contacts (paginated) |
| `GET` | `/v1/contacts/resolve?ids=...` | Resolve contacts by IDs (max 100) |
| `POST` | `/v1/contacts/merge` | Merge contacts into unified identity |
| `POST` | `/v1/contacts/unmerge` | Unmerge previously merged contacts |
| `GET` | `/v1/contacts/merged/{tenantUserId}` | List merged contacts for tenant user |

### Tenant Users

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/tenant-users` | List tenant users |
| `GET` | `/v1/users/search` | Search users by query |

### Group Writers

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/channels/instances/{id}/writers/groups` | List group file writers |
| `GET` | `/v1/channels/instances/{id}/writers` | List writers for group |
| `POST` | `/v1/channels/instances/{id}/writers` | Add writer to group |
| `DELETE` | `/v1/channels/instances/{id}/writers/{userId}` | Remove writer |

**Supported channels:** `telegram`, `discord`, `slack`, `whatsapp`, `zalo_oa`, `zalo_personal`, `feishu`

Credentials are masked in HTTP responses.

---

## 18. Pending Messages

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/pending-messages` | List all groups with titles |
| `GET` | `/v1/pending-messages/messages` | List messages by channel+key |
| `DELETE` | `/v1/pending-messages` | Delete message group |
| `POST` | `/v1/pending-messages/compact` | LLM-based summarization (async, 202) |

Compaction runs in the background. Falls back to hard delete if no LLM provider is available.

---

## 19. Team Events

Team activity and audit trail.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/teams/{id}/events` | List team events (paginated) |

---

## 20. Secure CLI Credentials

CLI authentication credentials for secure command execution. Requires **admin role** (full gateway token or empty gateway token in dev/single-user mode).

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/cli-credentials` | List all credentials |
| `POST` | `/v1/cli-credentials` | Create new credential |
| `GET` | `/v1/cli-credentials/presets` | Get preset credential templates |
| `POST` | `/v1/cli-credentials/check-binary` | Check binary availability |
| `GET` | `/v1/cli-credentials/{id}` | Get credential details |
| `PUT` | `/v1/cli-credentials/{id}` | Update credential |
| `DELETE` | `/v1/cli-credentials/{id}` | Delete credential |
| `POST` | `/v1/cli-credentials/{id}/test` | Test credential connection (dry-run) |

### Per-User Credentials

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/cli-credentials/{id}/user-credentials` | List user credentials for CLI cred |
| `GET` | `/v1/cli-credentials/{id}/user-credentials/{userId}` | Get user credential |
| `PUT` | `/v1/cli-credentials/{id}/user-credentials/{userId}` | Set user credential |
| `DELETE` | `/v1/cli-credentials/{id}/user-credentials/{userId}` | Delete user credential |

### Agent Grants

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/cli-credentials/{id}/agent-grants` | List agent grants for CLI cred |
| `POST` | `/v1/cli-credentials/{id}/agent-grants` | Create agent grant |
| `GET` | `/v1/cli-credentials/{id}/agent-grants/{grantId}` | Get agent grant |
| `PUT` | `/v1/cli-credentials/{id}/agent-grants/{grantId}` | Update agent grant |
| `DELETE` | `/v1/cli-credentials/{id}/agent-grants/{grantId}` | Delete agent grant |

---

## 21. Runtime & Packages Management

Manage system (apk), Python (pip), and Node (npm) package installation in the GoClaw runtime container. These endpoints do not inspect host-level runtimes. Requires authentication. When `GOCLAW_GATEWAY_TOKEN` is empty (dev/single-user mode), all users get admin role and can manage packages.

### List Installed Packages

```
GET /v1/packages
```

Returns all installed packages grouped by category.

**Response:**

```json
{
  "system": [
    {"name": "github-cli", "version": "2.72.0-r6"},
    {"name": "curl", "version": "8.9.1-r1"}
  ],
  "pip": [
    {"name": "pandas", "version": "2.0.0"},
    {"name": "requests", "version": "2.31.0"}
  ],
  "npm": [
    {"name": "typescript", "version": "5.1.0"},
    {"name": "docx", "version": "8.12.0"}
  ]
}
```

### Install Package

```
POST /v1/packages/install
```

**Request:**

```json
{
  "package": "github-cli"
}
```

Package name can optionally include prefix: `"pip:pandas"` or `"npm:typescript"`. Without prefix, defaults to system (apk).

**Validation:** Package names must match `^[a-zA-Z0-9@][a-zA-Z0-9._+\-/@]*$` (max 4096 bytes). Names starting with `-` are rejected to prevent argument injection.

**Response:**

```json
{
  "ok": true,
  "error": ""
}
```

| Category | Manager | Behavior |
|----------|---------|----------|
| System (apk) | root-privileged pkg-helper | Sent to `/tmp/pkg.sock`, persisted to `/app/data/.runtime/apk-packages` for container recreates |
| Python (pip) | direct install | Installs to `$PIP_TARGET` (writable runtime dir) with `PIP_BREAK_SYSTEM_PACKAGES=1` |
| Node (npm) | direct install | Installs globally to `$NPM_CONFIG_PREFIX` (writable runtime dir) |

### Uninstall Package

```
POST /v1/packages/uninstall
```

Same format as install. System packages are removed from persist file and container state.

### Check Runtime Availability

```
GET /v1/packages/runtimes
```

Check which prerequisite runtimes are available inside the active GoClaw runtime container. Host-installed runtimes and shell-profile-managed binaries (for example `nvm`) are not included in this result.

**Response:**

```json
{
  "runtimes": [
    {"name": "python3", "available": false},
    {"name": "pip3", "available": false},
    {"name": "node", "available": false},
    {"name": "npm", "available": false},
    {"name": "pkg-helper", "available": true, "version": "socket"}
  ],
  "ready": false
}
```

### GitHub Releases

```
GET /v1/packages/github-releases
```

Lists available GoClaw GitHub release versions for the update checker.

### Shell Deny Groups

```
GET /v1/shell-deny-groups
```

List shell command deny group patterns configured for the gateway.

---

## 22. Traces & Costs

LLM call tracing and cost analysis.

### Traces

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/traces` | List traces (paginated, filterable) |
| `GET` | `/v1/traces/{traceID}` | Get trace with spans |
| `GET` | `/v1/traces/{traceID}/export` | Export trace tree (gzipped JSON) |

**Filters:** `agent_id`, `user_id`, `session_key`, `status`, `channel`

### Costs

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/costs/summary` | Cost summary by agent/time range |

---

## 23. Usage & Analytics

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/usage/timeseries` | Time-series usage points |
| `GET` | `/v1/usage/breakdown` | Breakdown by provider/model/channel |
| `GET` | `/v1/usage/summary` | Summary with period comparison |

**Query params:** `from`, `to` (RFC 3339), `agent_id`, `provider`, `model`, `channel`, `group_by`

**Periods:** `24h`, `today`, `7d`, `30d`

---

## 24. Activity & Audit

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/activity` | List activity audit logs (filterable) |

---

## 25. Storage

Workspace file management.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/storage/files` | List files with depth limiting |
| `GET` | `/v1/storage/files/{path...}` | Read file (JSON or raw) |
| `POST` | `/v1/storage/files` | Upload file (admin) |
| `DELETE` | `/v1/storage/files/{path...}` | Delete file/directory |
| `PUT` | `/v1/storage/move` | Move/rename file (admin) |
| `GET` | `/v1/storage/size` | Stream storage size (Server-Sent Events, cached 60 min) |

**Query parameters:**
- `?raw=true` — Serve native MIME type instead of JSON
- `?depth=N` — Limit directory traversal depth

**Security:** Protected directories `skills/` and `skills-store/` cannot be deleted. Path traversal and symlink attacks are blocked.

---

## 26. Voices & Audio

Voice discovery for TTS providers (ElevenLabs). All endpoints are tenant-scoped and require tenant admin or operator role.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/voices` | List available voices (in-memory cached, TTL 1h) |
| `POST` | `/v1/voices/refresh` | Force refresh voice cache (admin-only) |

### `GET /v1/voices`

**Response** (200 OK):
```json
[
  {
    "voice_id": "pMsXgVXv3BLzUgSXRplE",
    "name": "Alice",
    "preview_url": "https://...",
    "category": "premade",
    "labels": {
      "use_case": "conversational",
      "accent": "american"
    }
  }
]
```

**Caching:** In-memory cached per tenant with TTL 1h. Cache miss triggers immediate fetch from provider.

### `POST /v1/voices/refresh`

Invalidate the voice cache for the current tenant, forcing a fresh fetch on the next request.

**Response** (202 Accepted):
```json
{ "message": "voice cache invalidated" }
```

---

## 27. Media

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/media/upload` | Upload file (multipart, 50 MB limit) |
| `GET` | `/v1/media/{id}` | Serve media by ID with caching |

---

## 28. Files

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/files/{path...}` | Serve workspace file by path |
| `POST` | `/v1/files/sign` | Generate signed URL for token-based file access |

Auth via Bearer token or `?token=` query param (for `<img>` tags). MIME type auto-detected. Path traversal blocked.

---

## 29. API Keys

Admin-only endpoints for managing gateway API keys. See [20 — API Keys & Auth](20-api-keys-auth.md) for the full authentication and authorization model.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/api-keys` | List all API keys (masked) |
| `POST` | `/v1/api-keys` | Create API key (returns raw key once) |
| `POST` | `/v1/api-keys/{id}/revoke` | Revoke API key |

### Create Request

```json
{
  "name": "ci-deploy",
  "scopes": ["operator.read", "operator.write"],
  "expires_in": 2592000
}
```

### Create Response

```json
{
  "id": "01961234-...",
  "name": "ci-deploy",
  "prefix": "goclaw_a1b2c3d4",
  "key": "goclaw_a1b2c3d4e5f6...full-key",
  "scopes": ["operator.read", "operator.write"],
  "expires_at": "2026-04-14T12:00:00Z",
  "created_at": "2026-03-15T12:00:00Z"
}
```

> The `key` field is only returned in the create response. Subsequent list/get calls show only the `prefix`.

---

## 30. OAuth

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/auth/chatgpt/{provider}/status` | Check ChatGPT OAuth status for a provider |
| `GET` | `/v1/auth/chatgpt/{provider}/quota` | Fetch Codex/OpenAI quota state for a provider |
| `POST` | `/v1/auth/chatgpt/{provider}/start` | Start ChatGPT OAuth flow for a provider |
| `POST` | `/v1/auth/chatgpt/{provider}/callback` | Manual callback handler for a provider |
| `POST` | `/v1/auth/chatgpt/{provider}/logout` | Revoke ChatGPT OAuth token for a provider |
| `GET` | `/v1/auth/openai/status` | Check OpenAI auth status |
| `GET` | `/v1/auth/openai/quota` | Fetch quota state for the default `openai-codex` provider |
| `POST` | `/v1/auth/openai/start` | Start OAuth flow |
| `POST` | `/v1/auth/openai/callback` | Manual callback handler |
| `POST` | `/v1/auth/openai/logout` | Revoke token |

Legacy `/v1/auth/openai/*` routes remain as compatibility aliases for the default `openai-codex` OpenAI Codex OAuth provider.

### Provider Quota Response

`GET /v1/auth/chatgpt/{provider}/quota` and `GET /v1/auth/openai/quota` always return a provider-scoped quota envelope.

Success payload:

```json
{
  "provider_name": "openai-codex",
  "success": true,
  "plan_type": "team",
  "windows": [
    {
      "label": "Primary",
      "used_percent": 24,
      "remaining_percent": 76,
      "reset_after_seconds": 3600,
      "reset_at": "2026-03-24T20:15:00Z"
    }
  ],
  "core_usage": {
    "five_hour": {
      "label": "Primary",
      "remaining_percent": 76,
      "reset_after_seconds": 3600,
      "reset_at": "2026-03-24T20:15:00Z"
    },
    "weekly": {
      "label": "Secondary",
      "remaining_percent": 62,
      "reset_after_seconds": 604800,
      "reset_at": "2026-03-31T19:15:00Z"
    }
  },
  "last_updated": "2026-03-24T19:15:00Z"
}
```

Notes:
- Invalid provider slugs return `400`.
- Missing provider returns `404`, provider type conflicts return `409`.
- Missing quota metadata, expired workspace access, upstream `402`/`403`/`429` return `200` with a structured failure payload.
- `error_code` can be `missing_account_id`, `reauth_required`, `payment_required`, `quota_api_forbidden`, `quota_endpoint_not_found`, `rate_limited`, `provider_unavailable`, `network_timeout`, `network_error`, `quota_request_failed`, or `unknown_upstream_error`.

---

## 31. Edition

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/edition` | Get current edition info (lite vs standard) |

---

## 32. Tenants

Multi-tenant management (admin only).

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/tenants` | List tenants |
| `POST` | `/v1/tenants` | Create tenant |
| `GET` | `/v1/tenants/{id}` | Get tenant |
| `PATCH` | `/v1/tenants/{id}` | Update tenant |
| `GET` | `/v1/tenants/{id}/users` | List tenant users |
| `POST` | `/v1/tenants/{id}/users` | Add user to tenant |
| `DELETE` | `/v1/tenants/{id}/users/{userId}` | Remove user from tenant |

---

## 33. System Configs

Key-value system configuration store.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/system-configs` | List all system configs |
| `GET` | `/v1/system-configs/{key}` | Get config by key |
| `PUT` | `/v1/system-configs/{key}` | Set config value (admin) |
| `DELETE` | `/v1/system-configs/{key}` | Delete config (admin) |

---

## 34. Team Workspace & Attachments

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/teams/{teamId}/workspace/upload` | Upload file to team workspace |
| `PUT` | `/v1/teams/{teamId}/workspace/move` | Move workspace item |
| `GET` | `/v1/teams/{teamId}/attachments/{attachmentId}/download` | Download task attachment |

---

## 35. Backup & Restore

System-level backup/restore endpoints (admin only). Stream progress via SSE.

### System Backup

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/system/backup` | Create system backup (streams SSE progress) |
| `GET` | `/v1/system/backup/preflight` | Check backup prerequisites |
| `GET` | `/v1/system/backup/download/{token}` | Download backup archive |
| `POST` | `/v1/system/restore` | Restore from backup archive (streams SSE progress) |

**Query params for restore:** `skip_db=true`, `skip_files=true`, `dry_run=true`

### S3 Backup Integration

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/system/backup/s3/config` | Get S3 backup configuration |
| `PUT` | `/v1/system/backup/s3/config` | Set S3 backup configuration |
| `GET` | `/v1/system/backup/s3/list` | List S3 backup objects |
| `POST` | `/v1/system/backup/s3/upload` | Upload backup to S3 |
| `POST` | `/v1/system/backup/s3/backup` | Trigger S3 backup |

### Tenant Backup

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/tenant/backup` | Create tenant-scoped backup |
| `GET` | `/v1/tenant/backup/preflight` | Check tenant backup prerequisites |
| `GET` | `/v1/tenant/backup/download/{token}` | Download tenant backup archive |
| `POST` | `/v1/tenant/restore` | Restore tenant from backup |

---

## 36. System

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check (no auth) |
| `GET` | `/v1/openapi.json` | OpenAPI 3.0 spec |
| `GET` | `/docs` | Swagger UI |

### Health Response

```json
{
  "status": "ok",
  "protocol": 3
}
```

---

## 37. MCP Bridge

Exposes GoClaw tools to Claude CLI via streamable HTTP at `/mcp/bridge`. Only listens on localhost. Protected by gateway token with HMAC-signed context headers.

| Header | Purpose |
|--------|---------|
| `X-Agent-ID` | Agent context for tool execution |
| `X-User-ID` | User context |
| `X-Channel` | Channel routing |
| `X-Chat-ID` | Chat routing |
| `X-Peer-Kind` | `direct` or `group` |
| `X-Bridge-Sig` | HMAC signature over all context fields |

---

## Error Responses

All endpoints return errors in a consistent JSON format:

```json
{
  "error": "human-readable error message"
}
```

Error messages are localized based on the `Accept-Language` header. HTTP status codes follow standard conventions:

| Code | Meaning |
|------|---------|
| `400` | Bad request (invalid JSON, missing fields) |
| `401` | Unauthorized (missing or invalid token) |
| `403` | Forbidden (insufficient permissions) |
| `404` | Not found |
| `409` | Conflict (duplicate name, version mismatch) |
| `429` | Rate limited |
| `500` | Internal server error |

---

## Notes on WebSocket-Only Endpoints

The following operations are **only available via WebSocket RPC**, not HTTP:

- **Sessions:** List, preview, patch, delete, reset (use WebSocket method `sessions.*`)
- **Cron jobs:** List, create, update, delete, logs (use WebSocket method `cron.*`)
- **Send messages:** Send to channels (use WebSocket method `send.*`)
- **Config management:** Get, apply, patch (use WebSocket method `config.*`)

These endpoints require an active WebSocket connection to the `/ws` endpoint with proper authentication and agent context.

---

## Module Reference

| Module | Path | Purpose |
|--------|------|---------|
| Auth & Routing | `internal/http/`, `internal/gateway/` | Auth middleware, RBAC, request routing, MCP bridge |
| Chat, Agents & Skills | `internal/http/chat_completions.go`, `agents*.go`, `skills*.go`, `wake.go` | Conversational endpoints, agent CRUD, skill management |
| Memory, Vault & Knowledge | `internal/http/memory.go`, `vault_handlers*.go`, `knowledge_graph.go`, `episodic_handlers.go` | Persistent knowledge, vector memory, document graph |
| Evolution, Orchestration & Flags | `internal/http/evolution_handlers.go`, `orchestration_handlers.go`, `v3_flags_handlers.go` | v3 runtime control, metrics, suggestions, feature flags |
