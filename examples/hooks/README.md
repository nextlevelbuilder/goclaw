# Example Hooks Library

Copy-paste-ready hook configurations for common use cases. Each JSON is a valid `hooks.create` WS payload — send it via the Web UI → **Hooks → Create** (paste into the relevant fields) or POST directly to the RPC endpoint.

## Safety reminders

- **Review regex matchers** before enabling. A too-broad matcher on a prompt hook drains token budget. Prefer anchored patterns like `^(exec|shell)$`.
- **`command` hooks require Lite edition.** On Standard the UI greys them out and dispatch fails closed.
- **SSRF-safe HTTP hooks**: URLs are resolved + pinned before the request. Loopback/private ranges are blocked in production.
- **Authorization headers** get AES-256-GCM encrypted at rest. Never commit plaintext secrets to these JSON files.
- **Test in staging first.** Use the `Test` tab in the UI — it runs with `dryRun=true` and does NOT write to `hook_executions`.

## Catalog

| File | Event | Handler | Scope | Purpose |
|---|---|---|---|---|
| `block-rm-rf.json` | `pre_tool_use` | command | agent | Block dangerous `rm -rf /` via local shell script. Lite only. |
| `auto-lint-after-write.json` | `post_tool_use` | http | agent | Fire-and-forget lint request after file writes. |
| `audit-tool-usage.json` | `post_tool_use` | http | tenant | Stream every tool invocation to an external audit sink. |
| `session-context-injector.json` | `session_start` | http | agent | Injects project metadata into the agent context at start. |
| `notify-discord-on-stop.json` | `stop` | http | tenant | Discord webhook notification when a session ends. |

## Usage

### Via Web UI

1. Open `/hooks` → click **Create hook**.
2. Copy fields from the example JSON into the form.
3. For `http.config.headers`, paste your secret in the Authorization field — the server encrypts it before storing.
4. Click **Save**, then **Test** with a sample event before enabling in production.

### Via WS RPC

```bash
wscat -c ws://localhost:18790/ws
# After connect:
> {"id":"1","method":"hooks.create","params": <paste JSON here> }
```

## Conventions

- `tenant_id` omitted → current tenant from WS session. Set `scope: "global"` to apply cross-tenant (master required).
- `agent_id` required for `scope: "agent"`; otherwise leave null.
- `priority: 10` is the recommended default. Higher priority hooks run first; first `block` wins the chain.
- `on_timeout: "block"` for anything security-sensitive; `"allow"` for observation-only.
