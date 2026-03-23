# 18 — HTTP REST API

GoClaw exposes a comprehensive HTTP REST API alongside the WebSocket RPC protocol. All endpoints are served from the same gateway server and share authentication, rate limiting, and i18n infrastructure.

Interactive documentation is available at `/docs` (Swagger UI) and the raw OpenAPI 3.0 spec at `/v1/openapi.json`.

---

## 1. Authentication

All HTTP endpoints (except `/health`) require authentication via Bearer token in the `Authorization` header:

```
Authorization: Bearer <token>
```

Two token types are accepted:

| Type | Format | Scope |
|------|--------|-------|
| Gateway token | Configured in `config.json` | Full admin access |
| API key | `goclaw_` + 32 hex chars | Scoped by key permissions |

API keys are hashed with SHA-256 before lookup — the raw key is never stored. See [20 — API Keys & Auth](20-api-keys-auth.md) for details.

> Some endpoints accept the token as a query parameter `?token=<token>` for use in `<img>` and `<audio>` tags (e.g., `/v1/files/`, `/v1/media/`).

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

### Predefined Agent Instances

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/agents/{id}/instances` | List user instances |
| `GET` | `/v1/agents/{id}/instances/{userID}/files` | List user context files |
| `GET` | `/v1/agents/{id}/instances/{userID}/files/{fileName}` | Get specific user context file |
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
| `GET` | `/v1/providers/{id}/models` | Proxy to upstream provider model list |
| `GET` | `/v1/providers/claude-cli/auth-status` | Check Claude CLI login status |

**Supported types:** `anthropic_native`, `openai_compat`, `chatgpt_oauth`, `gemini_native`, `dashscope`, `bailian`, `minimax`, `claude_cli`, `acp`

---

## 7. Sessions

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/sessions` | List sessions (paginated) |
| `GET` | `/v1/sessions/{key}` | Get session with messages |
| `DELETE` | `/v1/sessions/{key}` | Delete session |
| `POST` | `/v1/sessions/{key}/reset` | Clear session messages |
| `PATCH` | `/v1/sessions/{key}` | Update label, model, metadata |

---

## 8. MCP Servers

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

---

## 9. Tools

### Built-in Tools

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/tools/builtin` | List all built-in tools |
| `GET` | `/v1/tools/builtin/{name}` | Get tool definition |
| `PUT` | `/v1/tools/builtin/{name}` | Update enabled/settings |

### Custom Tools

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/tools/custom` | List custom tools (paginated) |
| `POST` | `/v1/tools/custom` | Create custom tool |
| `GET` | `/v1/tools/custom/{id}` | Get tool details |
| `PUT` | `/v1/tools/custom/{id}` | Update tool |
| `DELETE` | `/v1/tools/custom/{id}` | Delete tool |

Query parameters for list: `agent_id`, `search`, `limit`, `offset`

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

## 11. Knowledge Graph

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

---

## 12. Channels

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

## 13. Pending Messages

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/pending-messages` | List all groups with titles |
| `GET` | `/v1/pending-messages/messages` | List messages by channel+key |
| `DELETE` | `/v1/pending-messages` | Delete message group |
| `POST` | `/v1/pending-messages/compact` | LLM-based summarization (async, 202) |

Compaction runs in the background. Falls back to hard delete if no LLM provider is available.

---

## 14. Delegations

Agent task delegation and authorization history.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/delegations` | List delegations (paginated, filterable) |
| `GET` | `/v1/delegations/{id}` | Get delegation record |

**Filters:** `source_agent_id`, `target_agent_id`, `team_id`, `user_id`, `status`, `limit`, `offset`

---

## 15. Team Events

Team activity and audit trail.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/teams/{id}/events` | List team events (paginated) |

---

## 16. Secure CLI Credentials

CLI authentication credentials for secure command execution. Requires **admin role** (full gateway token or empty gateway token in dev/single-user mode).

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/cli-credentials` | List all credentials |
| `POST` | `/v1/cli-credentials` | Create new credential |
| `GET` | `/v1/cli-credentials/{id}` | Get credential details |
| `PUT` | `/v1/cli-credentials/{id}` | Update credential |
| `DELETE` | `/v1/cli-credentials/{id}` | Delete credential |
| `GET` | `/v1/cli-credentials/presets` | Get preset credential templates |
| `POST` | `/v1/cli-credentials/{id}/test` | Test credential connection (dry-run) |

---

## 17. Runtime & Packages Management

Manage system (apk), Python (pip), and Node (npm) package installation in the runtime container. Requires authentication. When `GOCLAW_GATEWAY_TOKEN` is empty (dev/single-user mode), all users get admin role and can manage packages.

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

**Response:**

```json
{
  "ok": true,
  "error": ""
}
```

### Check Runtime Availability

```
GET /v1/packages/runtimes
```

Check if Python and Node runtimes are available in the container.

**Response:**

```json
{
  "python": true,
  "node": true
}
```

---

## 18. Traces & Costs

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

## 19. Usage & Analytics

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/usage/timeseries` | Time-series usage points |
| `GET` | `/v1/usage/breakdown` | Breakdown by provider/model/channel |
| `GET` | `/v1/usage/summary` | Summary with period comparison |

**Query params:** `from`, `to` (RFC 3339), `agent_id`, `provider`, `model`, `channel`, `group_by`

**Periods:** `24h`, `today`, `7d`, `30d`

---

## 20. Activity & Audit

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/activity` | List activity audit logs (filterable) |

---

## 21. Storage

Workspace file management.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/storage/files` | List files with depth limiting |
| `GET` | `/v1/storage/files/{path...}` | Read file (JSON or raw) |
| `DELETE` | `/v1/storage/files/{path...}` | Delete file/directory |
| `GET` | `/v1/storage/size` | Stream storage size (Server-Sent Events, cached 60 min) |

**Query parameters:**
- `?raw=true` — Serve native MIME type instead of JSON
- `?depth=N` — Limit directory traversal depth

**Security:** Protected directories `skills/` and `skills-store/` cannot be deleted. Path traversal and symlink attacks are blocked.

---

## 22. Media

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/media/upload` | Upload file (multipart, 50 MB limit) |
| `GET` | `/v1/media/{id}` | Serve media by ID with caching |

---

## 23. Files

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/files/{path...}` | Serve workspace file by path |

Auth via Bearer token or `?token=` query param (for `<img>` tags). MIME type auto-detected. Path traversal blocked.

---

## 24. API Keys

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

## 25. OAuth

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/auth/openai/status` | Check OpenAI auth status |
| `POST` | `/v1/auth/openai/start` | Start OAuth flow |
| `POST` | `/v1/auth/openai/callback` | Manual callback handler |
| `POST` | `/v1/auth/openai/logout` | Revoke token |

---

## 26. System

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

## 27. MCP Bridge

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

## File Reference

| File | Purpose |
|------|---------|
| `internal/http/chat_completions.go` | OpenAI-compatible chat API |
| `internal/http/responses.go` | OpenResponses protocol |
| `internal/http/agents.go` | Agent CRUD + shares + instances + files |
| `internal/http/skills.go` | Skill management + grants + versions |
| `internal/http/providers.go` | Provider CRUD + verification + models |
| `internal/http/mcp.go` | MCP server management + grants + requests |
| `internal/http/custom_tools.go` | Custom tool CRUD |
| `internal/http/builtin_tools.go` | Built-in tool management |
| `internal/http/tools_invoke.go` | Direct tool invocation |
| `internal/http/channel_instances.go` | Channel instance management + contacts |
| `internal/http/memory_handlers.go` | Memory document management + search + indexing |
| `internal/http/knowledge_graph.go` | Knowledge graph API (entities, relations, traversal) |
| `internal/http/traces.go` | LLM trace listing + export |
| `internal/http/usage.go` | Usage analytics + costs |
| `internal/http/activity.go` | Activity audit log |
| `internal/http/storage.go` | Workspace file management + size calculation |
| `internal/http/media_upload.go` | Media file upload |
| `internal/http/media_serve.go` | Media file serving |
| `internal/http/files.go` | Workspace file serving |
| `internal/http/api_keys.go` | API key management + revoke |
| `internal/http/delegations.go` | Delegation history API |
| `internal/http/team_events.go` | Team event history API |
| `internal/http/secure_cli.go` | CLI credential management |
| `internal/http/packages.go` | Runtime package management (apk/pip/npm) |
| `internal/http/pending_messages.go` | Pending message groups + compaction |
| `internal/http/oauth.go` | OAuth authentication flows |
| `internal/http/openapi.go` | OpenAPI spec + Swagger UI |
| `internal/http/auth.go` | Authentication helpers |
| `internal/gateway/server.go` | HTTP mux and route wiring |
| `cmd/gateway.go` | Handler instantiation and wiring |
| `cmd/pkg-helper/main.go` | Root-privileged system package helper (apk add/del) |
| `internal/skills/package_lister.go` | Query installed packages from apk/pip3/npm |
