# API Reference

## HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/ws` | WebSocket upgrade |
| POST | `/v1/chat/completions` | OpenAI-compatible chat API |
| POST | `/v1/responses` | Responses protocol |
| POST | `/v1/tools/invoke` | Tool invocation |
| GET/POST | `/v1/agents/*` | Agent management |
| GET/POST | `/v1/skills/*` | Skills management |
| GET/POST/PUT/DELETE | `/v1/tools/custom/*` | Custom tool CRUD |
| GET/POST/PUT/DELETE | `/v1/mcp/*` | MCP server + grants management |
| GET | `/v1/traces/*` | Trace viewer |

## Custom Tools

Define shell-based tools at runtime via HTTP API — no recompile or restart needed. The LLM can invoke custom tools identically to built-in tools.

**How it works:**
1. Admin creates a tool via `POST /v1/tools/custom` with a shell command template
2. LLM generates a tool call with the custom tool name
3. GoClaw renders the command template with shell-escaped arguments, checks deny patterns, and executes with timeout

**Capabilities:**
- **Scope** — Global (all agents) or per-agent (`agent_id` field)
- **Parameters** — JSON Schema definition for LLM arguments
- **Security** — All arguments auto shell-escaped, deny pattern filtering (blocks `curl|sh`, reverse shells, etc.), configurable timeout (default 60s)
- **Encrypted env vars** — Environment variables stored with AES-256-GCM encryption in the database
- **Cache invalidation** — Mutations broadcast events for hot-reload without restart

**API:**

| Method | Path | Description |
|---|---|---|
| GET | `/v1/tools/custom` | List tools (filter by `?agent_id=`) |
| POST | `/v1/tools/custom` | Create a custom tool |
| GET | `/v1/tools/custom/{id}` | Get tool details |
| PUT | `/v1/tools/custom/{id}` | Update a tool (JSON patch) |
| DELETE | `/v1/tools/custom/{id}` | Delete a tool |

**Example — create a tool that checks DNS records:**

```json
{
  "name": "dns_lookup",
  "description": "Look up DNS records for a domain",
  "parameters": {
    "type": "object",
    "properties": {
      "domain": { "type": "string", "description": "Domain name to look up" },
      "record_type": { "type": "string", "enum": ["A", "AAAA", "MX", "CNAME", "TXT"] }
    },
    "required": ["domain"]
  },
  "command": "dig +short {{.record_type}} {{.domain}}",
  "timeout_seconds": 10,
  "enabled": true
}
```

## MCP Integration

Connect external [Model Context Protocol](https://modelcontextprotocol.io) servers to extend agent capabilities. MCP tools are registered transparently into GoClaw's tool registry and invoked like any built-in tool.

**Supported transports:** `stdio`, `sse`, `streamable-http`

**Static config** — configure in `config.json` (deprecated; use HTTP API for dynamic management):

```json
{
  "mcp": {
    "servers": {
      "filesystem": {
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
      },
      "remote-tools": {
        "transport": "streamable-http",
        "url": "https://mcp.example.com/tools"
      }
    }
  }
}
```

**HTTP API** — full CRUD with per-agent and per-user access grants:

| Method | Path | Description |
|---|---|---|
| GET | `/v1/mcp/servers` | List registered MCP servers |
| POST | `/v1/mcp/servers` | Register a new MCP server |
| GET | `/v1/mcp/servers/{id}` | Get server details |
| PUT | `/v1/mcp/servers/{id}` | Update server config |
| DELETE | `/v1/mcp/servers/{id}` | Remove MCP server |
| POST | `/v1/mcp/servers/{id}/grants/agent` | Grant access to an agent |
| DELETE | `/v1/mcp/servers/{id}/grants/agent/{agentID}` | Revoke agent access |
| GET | `/v1/mcp/grants/agent/{agentID}` | List agent's MCP grants |
| POST | `/v1/mcp/servers/{id}/grants/user` | Grant access to a user |
| DELETE | `/v1/mcp/servers/{id}/grants/user/{userID}` | Revoke user access |
| POST | `/v1/mcp/requests` | Request access (user self-service) |
| GET | `/v1/mcp/requests` | List pending access requests |
| POST | `/v1/mcp/requests/{id}/review` | Approve or reject a request |

**Features:**
- **Multi-server** — Connect multiple MCP servers simultaneously
- **Tool name prefixing** — Optional `{prefix}__{toolName}` to avoid collisions
- **Per-agent grants** — Control which agents can access which MCP servers, with tool allow/deny lists
- **Per-user grants** — Fine-grained user-level access control
- **Access requests** — Users can request access; admins approve or reject
