# Agent Hooks

Lifecycle hooks let you intercept, observe, or inject behavior at defined points in the agent loop. Use cases: block unsafe tool calls, auto-lint after writes, inject session context, notify on stop, audit tool usage.

## Concepts

### Events
Seven lifecycle events fire during an agent session:

| Event | Blocking | When it fires |
|---|---|---|
| `session_start` | no | A new session is established. |
| `user_prompt_submit` | **yes** | Before the user's message enters the pipeline. |
| `pre_tool_use` | **yes** | Before any tool call executes. |
| `post_tool_use` | no | After a tool call completes. |
| `stop` | no | The agent session terminates normally. |
| `subagent_start` | **yes** | A sub-agent is spawned. |
| `subagent_stop` | no | A sub-agent finishes. |

Blocking events wait for the sync chain to return an allow/block decision. Non-blocking events fire hooks asynchronously for observation only.

### Handler types

| Handler | Editions | Notes |
|---|---|---|
| `command` | Lite only | Local shell command; exit 2 → block, exit 0 → allow. |
| `http` | Lite + Standard | POST to endpoint; JSON body → decision. SSRF-protected. |
| `prompt` | Lite + Standard | LLM-based evaluation with structured tool-call output. Prompt-injection-resistant, budget-bounded. Requires `matcher` or `if_expr`. |

### Scopes
- **global** — applies to all tenants. Master scope required to create.
- **tenant** — applies to one tenant (any agent).
- **agent** — applies to a specific agent within a tenant.

Hooks resolve in priority order, highest first. A single `block` decision short-circuits the chain.

## Security model

- **Edition gating**: `command` handler blocked on Standard at both config-time AND dispatch-time (defense in depth).
- **Tenant isolation**: all reads/writes scope by `tenant_id` unless caller is in master scope. Global hooks use a sentinel tenant id.
- **SSRF protection**: HTTP handler validates URLs before request, pins resolved IP, blocks loopback/link-local/private ranges.
- **PII redaction**: audit rows truncate error text to 256 chars; full error is encrypted (AES-256-GCM) in `error_detail`.
- **Fail-closed**: any unhandled error in a blocking event yields `block`. Timeouts respect `OnTimeout` (default `block` for blocking events).
- **Circuit breaker**: N consecutive blocks/timeouts in a rolling window auto-disables the hook and persists `enabled=false`.
- **Loop detection**: sub-agent hook chains bounded at depth 3 (`MaxLoopDepth`).

## Handler reference

### command

```json
{
  "handler_type": "command",
  "config": {
    "command": "bash /path/to/script.sh",
    "allowed_env_vars": ["MY_VAR"],
    "cwd": "/workspace"
  }
}
```

- Stdin: JSON-encoded event payload.
- Stdout (exit 0): optional `{"continue": false}` → block; anything else → allow.
- Exit 2: block.
- Other non-zero exits: error (fail-closed for blocking events).
- Env allowlist: only listed keys are passed through to prevent secret leakage.

### http

```json
{
  "handler_type": "http",
  "config": {
    "url": "https://example.com/webhook",
    "headers": {"Authorization": "<AES-encrypted>"}
  }
}
```

- Method: POST, body = event JSON.
- Authorization header values are stored AES-256-GCM encrypted; decrypted at dispatch.
- 1 MiB response cap.
- Retries once on 5xx with 1s backoff; 4xx fail-closed (no retry).
- Response body:
  ```json
  { "decision": "allow" | "block", "additionalContext": "...", "updatedInput": {}, "continue": true }
  ```
- Non-JSON 2xx → allow.

### prompt

```json
{
  "handler_type": "prompt",
  "matcher": "^(exec|shell|write_file)$",
  "config": {
    "prompt_template": "Evaluate safety of this tool call.",
    "model": "haiku",
    "max_invocations_per_turn": 5
  }
}
```

Required:
- `prompt_template` — system-level instruction the evaluator receives.
- `matcher` or `if_expr` — runaway-cost guard; prevents firing the LLM on every event.

Safeguards:
- **Structured output**: evaluator MUST call a `decide(decision, reason, injection_detected, updated_input)` tool. Free-text responses fail-closed.
- **Sanitized input**: only `tool_input` reaches the evaluator (sandwiched in a fenced `USER INPUT` block with anti-injection preamble); the raw user message is never included.
- **Decision cache**: 60s TTL keyed by `sha256(hook_id || version || tool_name || tool_input)`. Hook edits bump version → busts cache.
- **Per-turn cap**: default 5 invocations per user turn; configurable.
- **Per-tenant monthly budget**: atomic UPDATE deduct; warn at 20% remaining, block at 0.

## Matchers

- **matcher** — POSIX-ish regex applied to `tool_name`. Example: `^(exec|shell|write_file)$`.
- **if_expr** — [cel-go](https://github.com/google/cel-go) expression evaluated against `{tool_name, tool_input, depth}`. Example: `tool_name == "exec" && size(tool_input.cmd) > 80`.

Both are optional for `command` / `http`. At least one is required for `prompt`.

## Safeguards summary

| Safeguard | Default | Overridable per hook |
|---|---|---|
| Per-hook timeout | 5s | yes (`timeout_ms`, max 10s) |
| Chain budget | 10s | no (dispatcher constant) |
| Circuit threshold | 5 blocks in 1 minute | no (dispatcher constant) |
| Prompt per-turn cap | 5 | yes (`max_invocations_per_turn`) |
| Prompt decision cache TTL | 60s | no |
| Tenant monthly token budget | 1,000,000 | seeded per tenant |

## Web UI walkthrough

Navigate to **Hooks** in the sidebar. The list view shows all hooks visible under the current role + tenant scope.

1. **Create** → pick event, handler type (command disabled on Standard), scope, matcher, then fill the handler-specific sub-form.
2. **Test** panel → fires the hook with a sample event (`dryRun=true`, no audit row written). Shows decision badge, duration, stdout/stderr (command), status code (http), reason (prompt). If the response includes `updatedInput`, a side-by-side JSON diff is rendered.
3. **History** tab → paginated executions from `hook_executions` (full implementation lands with operational pagination; current release shows the note state).
4. **Overview** tab → summary card with event, type, scope, matcher.

## Observability

### Audit table (`hook_executions`)

| Column | Notes |
|---|---|
| `hook_id` | `ON DELETE SET NULL` → executions preserved after hook deletion. |
| `dedup_key` | Unique index prevents double rows on retry (H6). |
| `error` | Truncated to 256 chars; full detail in `error_detail` (encrypted). |
| `metadata` | JSONB: `matcher_matched`, `cel_eval_result`, `stdout_len`, `http_status`, `prompt_model`, `prompt_tokens`, `trace_id`. |

### Tracing spans

Every hook execution emits a span named `hook.<handler_type>.<event>` (e.g. `hook.prompt.pre_tool_use`) with:

- `status` — `completed` or `error`.
- `duration_ms`.
- `metadata.decision` — `allow`/`block`/`error`/`timeout`.
- `parent_span_id` — nested under the current pipeline span when one exists.

No-op when no tracing collector is attached to ctx (safe in tests and for tenants without tracing enabled).

### Metrics (slog keys)

- `security.hook.circuit_breaker` — tripped breaker.
- `security.hook.audit_write_failed` — audit row write error.
- `security.hook.resolve_error` — store resolve error (blocking event → fail-closed).
- `security.hook.loop_depth_exceeded` — `MaxLoopDepth` violation.
- `security.hook.prompt_parse_error` — evaluator returned malformed structured output.
- `security.hook.budget_deduct_failed` / `budget_precheck_failed` — budget store error.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| HTTP hook always returns `error` | SSRF block on loopback (test). | Call `security.SetAllowLoopbackForTest(true)` in tests. |
| Prompt hook blocks everything | Evaluator returning free-text (no tool call). | Review `prompt_template`; keep it short + imperative. |
| Hook stopped firing | Circuit breaker tripped (5 blocks/min). | `UPDATE agent_hooks SET enabled=true WHERE id=...` after fixing upstream cause. |
| UI `command` radio greyed out | Standard edition. | Use HTTP or prompt, or upgrade to Lite. |
| Per-turn cap hit | `max_invocations_per_turn` too low. | Raise in hook config; review matcher tightness. |
| Budget exceeded | Tenant spent monthly token budget. | Raise `tenant_hook_budget.budget_total` or wait for rollover. |

## Migration notes

No data migration required when upgrading from a pre-hooks release. Migration `000052_agent_hooks` creates `agent_hooks`, `hook_executions`, and `tenant_hook_budget`. SQLite users must also have schema version 20 or later.

## Known limitations

- Prompt decision cache is per-process (no cluster-wide Redis yet).
- No skill-frontmatter hooks (planned post-MVP).
- No `agent` handler type (inter-agent delegation via prompt instead).
- Desktop Wails UI has list/detail parity but no dedicated mobile-optimized form.
