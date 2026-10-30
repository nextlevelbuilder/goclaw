# deed.page MCP Server

Connect GoClaw agents to [deed.page](https://deed.page), a free static host with a
keyless remote MCP endpoint. A granted agent can publish HTML (or other static
files) as a web page and reply with the URL. No API key, account or new code is
needed: this uses the existing streamable-HTTP MCP support.

Endpoint: `https://deed.page/mcp` (streamable HTTP)

> **Published pages are public.** Anyone with the URL can open them. Grant this
> server only to agents that should be able to publish, and don't let them publish
> secrets, credentials or private conversation content.

## Add the server

**Web UI:** MCP Servers → **Add Server**

| Field | Value |
|-------|-------|
| Name | `deed-page` |
| Transport | Streamable HTTP |
| URL | `https://deed.page/mcp` |
| Tool Prefix | leave empty (auto-derived as `mcp_deed_page`) |

Click **Test Connection**; it should list the server's tools. Then **Create**.

**HTTP API:** the same server as a `POST /v1/mcp/servers` body:

```json
{
  "name": "deed-page",
  "transport": "streamable-http",
  "url": "https://deed.page/mcp",
  "timeout_sec": 60,
  "enabled": true
}
```

The URL passes the normal SSRF validation for SSE/streamable-HTTP transports
(see [09-security.md](./09-security.md)); no host allowlist entry is needed.

## Grant it to an agent

The server exposes roughly 27 tools, most of them account and domain management,
including ones that change or delete existing pages. Most agents only need to
publish. Limit the grant with `tool_allow`, which takes original MCP tool names:

```json
{
  "agent_id": "<agent uuid>",
  "tool_allow": ["publish_site"]
}
```

Send it to `POST /v1/mcp/servers/{id}/grants/agent`, or use **Manage grants** in
the UI. Add `get_site` or `delete_site` only if the agent should inspect or
remove pages. The agent then sees `mcp_deed_page__publish_site`.

## Using it

Ask the agent something like *"Turn this report into a one-page HTML summary and
publish it for one hour"*. The agent calls `publish_site` with a `files` map
(for example `{"index.html": "<!doctype html>..."}`) and an optional
`ttl_seconds`. The result contains:

- `url`: the public page address
- `slug`: the page identifier
- `token`: a management token for updating or deleting that page

The token is part of the tool result, so it appears in the session. Keep it if
you plan to update or delete the page later. Use `ttl_seconds` for throwaway pages
so they expire on their own. If that owner token leaks and you still have it,
call `rotate_token` (add it to `tool_allow` when the agent should be able to).
It requires the current token, returns a new one once for the same owner, and
every site carries over. Omit `overlap_seconds`, or pass 0, and the previous
token stops working immediately; set it from 1 to 300 to keep both working for
that many seconds, after which the old token returns 401 `token_revoked`.
`rotate_token` does not restore a token you no longer have. With no recovery
email, a lost token stays unrecoverable and the pages stay online.

Limits, the full tool list and the HTTP API are in the
[deed.page docs](https://deed.page/docs). An optional agent skill is available at
[f-o-x11/deed-page-skill](https://github.com/f-o-x11/deed-page-skill).
