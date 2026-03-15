# GoClaw MCP Server Integration Guide

## Overview

After building an MCP server, register it in GoClaw using the `register_mcp_server` builtin tool. This stores the server config in the database, encrypts sensitive fields, and makes the server's tools available to agents.

---

## register_mcp_server Tool

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Unique server name (lowercase, hyphens allowed) |
| `transport` | string | Yes | `stdio`, `sse`, or `streamable-http` |
| `command` | string | stdio only | Command to run (e.g., `node`, `python`) |
| `args` | string[] | stdio only | Command arguments (e.g., `["dist/index.js"]`) |
| `url` | string | sse/http only | Server URL |
| `headers` | object | No | HTTP headers (encrypted at rest) |
| `env` | object | stdio only | Environment variables (encrypted at rest) |
| `api_key` | string | No | API key (encrypted at rest via AES-256-GCM) |
| `display_name` | string | No | Human-friendly display name |
| `tool_prefix` | string | No | Prefix added to all tool names (e.g., `myapi`) |
| `timeout_sec` | int | No | Connection timeout (default: 30) |
| `test_connection` | bool | No | Test connection before saving (default: false) |

### Transport Selection

| Transport | Use Case | Requirements |
|-----------|----------|--------------|
| `stdio` | Local servers, CLI tools | `command` + `args` |
| `sse` | Remote servers (legacy) | `url` |
| `streamable-http` | Remote servers (recommended) | `url` |

---

## Examples

### Local stdio Server (TypeScript)

```
register_mcp_server({
  "name": "github-mcp",
  "display_name": "GitHub MCP Server",
  "transport": "stdio",
  "command": "node",
  "args": ["path/to/dist/index.js"],
  "env": {"GITHUB_TOKEN": "ghp_..."},
  "tool_prefix": "github"
})
```

### Local stdio Server (Python)

```
register_mcp_server({
  "name": "slack-mcp",
  "display_name": "Slack MCP Server",
  "transport": "stdio",
  "command": "python",
  "args": ["-m", "slack_mcp"],
  "env": {"SLACK_TOKEN": "xoxb-..."}
})
```

### Remote Streamable HTTP Server

```
register_mcp_server({
  "name": "jira-mcp",
  "display_name": "Jira MCP Server",
  "transport": "streamable-http",
  "url": "https://mcp.example.com/jira",
  "headers": {"Authorization": "Bearer <token>"},
  "test_connection": true
})
```

---

## What Happens After Registration

1. **Database insert** — Server config stored in `mcp_servers` table
2. **Encryption** — API keys, headers, and env vars encrypted via AES-256-GCM
3. **Auto-grant** — Server automatically granted to the calling agent
4. **Available** — Server tools become available to the agent on the next turn
5. **UI visible** — Server appears in the web dashboard MCP Servers page

## Granting to Other Agents

After registering, grant access to other agents:

- **Web UI**: MCP Servers page > select server > Grants tab > Add agent
- **HTTP API**: `POST /v1/mcp/servers/{id}/grants/agent` with body `{"agent_id": "...", "enabled": true}`
- **Tool filtering**: Use `tool_allow` / `tool_deny` arrays in grants to restrict which tools an agent can use

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| "name already exists" | Server name conflict | Use a unique name or update existing |
| "connection failed" | Server not running or wrong config | Check command/url, verify server starts independently |
| "timeout" | Server startup too slow | Increase `timeout_sec` or check server performance |
| Tools not appearing | Grant not applied | Verify agent has a grant via MCP Servers > Grants |
| Permission denied | Missing API credentials | Check env/headers/api_key values |
