# Optional Sem and Weave MCP servers

[Sem](https://github.com/Ataraxy-Labs/sem) provides repository entity lookup;
[Weave](https://github.com/Ataraxy-Labs/weave) provides entity edits and merge
previews. They can use GoClaw's existing stdio MCP connection path.

## Operator-owned, trusted single-workspace setup

Install `sem` and `weave-mcp` in the gateway's execution environment. In Docker,
that means inside the gateway container, with the checkout mounted there. These
subprocesses run on the gateway side, not automatically inside a delegated
agent's sandbox. Use the same intended checkout for both servers.

Merge the entries below into `tools.mcp_servers` in your existing config; do not
replace the rest of the config. Replace every absolute-path placeholder first.
Both entries start disabled:

```json
{
  "tools": {
    "mcp_servers": {
      "sem": {
        "transport": "stdio",
        "command": "/absolute/path/to/sem",
        "args": [
          "mcp"
        ],
        "env": {
          "SEM_REPO": "/absolute/path/to/checkout",
          "SEM_LOCAL": "1",
          "SEM_MCP_NO_SHARED": "1",
          "SEM_NO_NETWORK": "1",
          "SEM_NO_TELEMETRY": "1"
        },
        "enabled": false
      },
      "weave": {
        "transport": "stdio",
        "command": "/absolute/path/to/weave-mcp",
        "args": [],
        "env": {
          "WEAVE_REPO": "/absolute/path/to/checkout"
        },
        "enabled": false
      }
    }
  }
}
```

Set only the intended server's `enabled` to `true` and restart the gateway.
Confirm connection in the gateway logs/tool discovery, then ask the agent to use
the advertised Sem find tool to locate a known symbol and report its location.
GoClaw may prefix advertised tool names; use the names it reports.

This config-file path is shared across agents and does not apply database-backed
per-agent grant filtering. It is for an operator-owned trusted workspace, not a
recipe for tenant isolation. On multi-tenant deployments, use database-backed
server grants and OS/container access controls for each checkout instead of
these global entries. `SEM_REPO`/`WEAVE_REPO` select a repository; they do not
create a security boundary.

## Capabilities and data

Sem stdio advertises more than read-only lookup, including command-running tools.
Its local/no-network flags disable its cloud client and set CLI controls, not OS
network isolation. Local index/cache files may be created. Returned source enters
the configured model context and may be recorded in session traces.

Weave is independently opt-in. `weave_edit` writes source, and its optional `then`
argument can execute a shell command. `dry_run: true` previews edits;
`weave_preview` previews a merge between two branches. Neither guarantees tests
pass. Use a disposable checkout first, inspect `git diff`, and run project tests.
Do not assume GoClaw's built-in exec approval policy automatically guards an
external MCP tool's internal subprocesses.

To disconnect, set the corresponding entry back to `enabled: false` and restart.
This recipe does not install packages, expose a public HTTP service, or change
GoClaw defaults.
