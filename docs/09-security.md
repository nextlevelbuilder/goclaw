# 09 - Security

Defense-in-depth with five independent layers from transport to isolation. Each layer operates independently -- even if one layer is bypassed, the remaining layers continue to protect the system.

> AES-256-GCM encryption protects secrets stored in PostgreSQL (LLM provider API keys, MCP server API keys, custom tool environment variables). Agent-level access control uses the 4-step `CanAccess` pipeline (see [06-store-data-model.md](./06-store-data-model.md)).

---

## 1. Five Defense Layers

```mermaid
flowchart TD
    REQ["Request"] --> L1["Layer 1: Transport<br/>CORS, message size limits, timing-safe auth"]
    L1 --> L2["Layer 2: Input<br/>Injection detection (6 patterns), message truncation"]
    L2 --> L3["Layer 3: Tool<br/>Shell deny patterns, path traversal, SSRF, exec approval"]
    L3 --> L4["Layer 4: Output<br/>Credential scrubbing, content wrapping"]
    L4 --> L5["Layer 5: Isolation<br/>Workspace isolation, Docker sandbox, read-only FS"]
```

### Layer 1: Transport Security

| Mechanism | Detail |
|-----------|--------|
| CORS (WebSocket) | `checkOrigin()` validates against `allowed_origins` (empty = allow all for backward compatibility) |
| WS message limit | `SetReadLimit(512KB)` -- gorilla auto-closes connection on exceed |
| HTTP body limit | `MaxBytesReader(1MB)` -- error returned before JSON decode |
| Token auth | `crypto/subtle.ConstantTimeCompare` (timing-safe) |
| Rate limiting | Token bucket per user/IP, configurable via `rate_limit_rpm` |

### Layer 2: Input -- Injection Detection

The input guard scans for 6 injection patterns.

| Pattern | Detection Target | Regex Match |
|---------|-----------------|--------------|
| `ignore_instructions` | "ignore all previous instructions" | Case-insensitive: ignore + (all?)previous/prior/above/earlier/preceding + instructions/rules/prompts/directives/guidelines |
| `role_override` | "you are now...", "pretend you are..." | Case-insensitive: (you are now\|from now on you are\|pretend you are\|act as if you are\|imagine you are) |
| `system_tags` | `<system>`, `[SYSTEM]`, `[INST]`, `<<SYS>>` | Case-insensitive: `</?system>`, `[SYSTEM]`, `[INST]`, `<<SYS>>`, `<\|im_start\|>system` |
| `instruction_injection` | "new instructions:", "override:", "system prompt:" | Case-insensitive: (new instructions?\|override\|system prompt\|<\|system\|>) |
| `null_bytes` | Null characters `\x00` (obfuscation attempts) | Raw `\x00` byte detection |
| `delimiter_escape` | "end of system", "begin user input", `</instructions>`, `</prompt>` | Case-insensitive: (end of system\|begin user input\|`</?instructions?>`\|`</rules>`\|`</prompt>`\|`</context>`) |

**Configurable action** (`gateway.injection_action`):

| Value | Behavior |
|-------|----------|
| `"log"` | Log info level, continue processing |
| `"warn"` (default) | Log warning level, continue processing |
| `"block"` | Log warning, return error, stop processing |
| `"off"` | Disable detection entirely |

**Message truncation**: Messages exceeding `max_message_chars` (default 32K) are truncated (not rejected), and the LLM is notified of the truncation.

### Layer 3: Tool Security

**Shell deny patterns** -- 7 categories of blocked commands (see `internal/tools/shell.go`):

| Category | Examples |
|----------|----------|
| Destructive file ops | `rm -rf`, `del /f`, `rmdir /s` |
| Destructive disk ops | `mkfs`, `dd if=`, `> /dev/sd*` |
| System commands | `shutdown`, `reboot`, `poweroff` |
| Fork bombs | `:(){ ... };:` |
| Remote code execution | `curl \| sh`, `wget -O - \| sh` |
| Reverse shells | `/dev/tcp/`, `nc -e` |
| Eval injection | `eval $()`, `base64 -d \| sh` |

Commands are scanned at execution time via regex deny lists. Patterns can be configured per-binary via `exec_settings.deny_patterns` (default set hardened for destructive/exfil operations). Verbose flag blocking (deny_verbose list) prevents leakage of sensitive output.

**SSRF protection** -- 3-step validation:

```mermaid
flowchart TD
    URL["URL to fetch"] --> S1["Step 1: Check blocked hostnames<br/>localhost, *.local, *.internal,<br/>metadata.google.internal"]
    S1 --> S2["Step 2: Check private IP ranges<br/>10.0.0.0/8, 172.16.0.0/12,<br/>192.168.0.0/16, 127.0.0.0/8,<br/>169.254.0.0/16, IPv6 loopback/link-local"]
    S2 --> S3["Step 3: DNS Pinning<br/>Resolve domain, check every resolved IP.<br/>Also applied to redirect targets."]
    S3 --> ALLOW["Allow request"]
```

**MCP stdio validation** -- Admin-created, imported, tested, and on-demand-discovered MCP server configs pass `internal/mcp.ValidateServerConfig()` before any temporary or persistent client process is created. `stdio` configs are restricted to bare allowlisted runtime names resolved from `PATH`; path-bearing commands such as `./node`, `tools/node`, `/tmp/node`, or `.\\node.exe` are rejected to prevent wrapper substitution. Arguments reject shell metacharacters and eval/import flags, and block remote loader/script/package execution modes such as `node --loader`, `python -m`, `deno`/`bun` remote refs, `npx`/`uvx`/`pipx` package targets, `uv --with`, `npm exec`, `go run`, `cargo install`, and `dotnet tool install`. SSE and streamable-HTTP transports use the SSRF validator above.

**Path traversal**: `resolvePath()` applies `filepath.Clean()` then `HasPrefix()` to ensure all paths stay within the workspace. With `restrict = true`, any path outside the workspace is blocked.

**PathDenyable** -- An interface that lets filesystem tools reject specific path prefixes:

```go
type PathDenyable interface {
    DenyPaths(...string)
}
```

All four filesystem tools (`read_file`, `write_file`, `list_files`, `edit`) implement `PathDenyable`. The agent loop calls `DenyPaths(".goclaw")` at startup to prevent agents from accessing internal data directories. `list_files` additionally filters denied directories from output entirely -- the agent does not see denied paths in directory listings.

#### Credentialed Exec Security

**Direct Exec Mode** for credentialed CLI tools implements defense-in-depth with 4 independent layers:

| Layer | Mechanism | Protects Against |
|-------|-----------|------------------|
| **No shell** | `exec.CommandContext(binary, args...)` (never `sh -c`) | Shell command injection, credential leakage via env var expansion |
| **Path verify** | `exec.LookPath()` + config match check | Binary spoofing (e.g., `./gh` in workspace) |
| **Deny patterns** | Per-binary regex deny lists on arguments + verbose flags | Sensitive operations per CLI (e.g., `auth`, `ssh-key`) |
| **Output scrub** | Credential values registered for dynamic scrubbing | Credentials in stdout/stderr |

| Attack Surface | Mitigation |
|----------------|------------|
| Shell operator injection, argument injection via spaces | Early regex scan + shell-word parsing (no shell evaluation) |
| Binary PATH manipulation, symlink attacks | `exec.LookPath()` + absolute path required + config match |
| Env var exfiltration, output parsing tricks | No-shell exec (env vars never expand) + dynamic credential scrubbing |
| Timeout abuse | Configurable per-binary timeout with context deadline |
| Sandbox escape | Docker container isolation when sandbox enabled |
| Verbose flag leakage | Separate deny_verbose list blocks verbose/debug output |

**Agent-level grant enforcement** -- The gate runs **before** any process spawn, blocking ungranted agents from executing registered binaries:

| Control | Implementation |
|---------|-----------------|
| **Grant lookup** | `store.SecureCLIStore.IsRegisteredBinary(ctx, binaryName)` checks `secure_cli_agent_grants` table. Non-global binaries require a row for the calling agent. |
| **Fail-CLOSED** | If the grant lookup errors (DB down, timeout), exec is denied with retry message. Per-lookup timeout: 2 seconds. |
| **Env scrubbing** | When a command escapes the credentialed path (e.g., via adversarial `exec` tool), child process env is scrubbed of all credential keys (static deny list + dynamic keys from every registered binary in the tenant) before spawn. Prevents credential leakage into non-credentialed commands. |
| **Wrapper unwrap** | Blocks shell wrappers (`sh -c`, `bash -c`, etc.) that attempt to evade binary path matching. Checks up to 3 levels of nesting; deeper chains are rejected as adversarial. |
| **Logging** | Three security events: `security.credentialed_binary_denied` (ungranted agent), `security.credentialed_binary_gate_error` (lookup failure), `security.credentialed_binary_wrapper_too_deep` (nested wrapper attack). All include: binary, wrapper, agent_id, tenant_id, command prefix. |
| **Subagent wiring** | Subagent `ExecTool`s use the same `SecureCLIStore` via `cmd/gateway_agents.go` → `buildSubagentToolsRegistry`. Parent agents cannot bypass the gate by delegating exec to spawned subagents. |

### Layer 4: Output Security

| Mechanism | Detail |
|-----------|--------|
| Static credential scrubbing | Regex patterns detect: OpenAI (`sk-[a-zA-Z0-9]{20,}`), Anthropic (`sk-ant-[a-zA-Z0-9-]{20,}`), GitHub tokens (`ghp_/gho_/ghu_/ghs_/ghr_` + 36 chars), AWS (`AKIA[A-Z0-9]{16}`), generic patterns (API/token/secret/password/bearer/authorization + 8+ chars), connection strings (postgres/mysql/mongodb/redis/amqp URLs), env vars (KEY/SECRET/CREDENTIAL/PRIVATE + 8+ chars, DSN/DATABASE_URL/REDIS_URL/MONGO_URI), VIRTUAL_* vars (4+ chars), long hex strings (64+ chars). All replaced with `[REDACTED]`. |
| Dynamic credential scrubbing | Runtime-registered credential values (min 6 chars) scrubbed via `AddCredentialScrubValues()` and replaced with `[REDACTED]` |
| Dynamic value scrubbing (SSRF) | Server IPs and other runtime-discovered values registered via `AddDynamicScrubValues()` and replaced with `[SERVER_IP]` |
| Web content wrapping | Fetched content wrapped in `<<<EXTERNAL_UNTRUSTED_CONTENT>>>` tags with security warning |

### Passive Channel Memory Redaction

Passive channel memory extraction runs an additional pre-LLM redaction pass over
pending group messages before any durable candidate is created. The pass removes
common secret/key assignments, bearer/provider tokens, connection strings,
payment-like digit sequences, phone numbers, email addresses, configured
excluded sender IDs, and configured regex exclusion patterns. The new extraction
tables store redaction counts/types and extracted summaries only, not raw message
bodies.

### Layer 5: Isolation

**Per-user workspace isolation** -- Two levels prevent cross-user file access:

| Level | Scope | Directory Pattern |
|-------|-------|------------------|
| Per-agent | Each agent gets its own base directory | `~/.goclaw/{agent-key}-workspace/` |
| Per-user | Each user gets a subdirectory within the agent workspace | `{agent-workspace}/user_{sanitized_id}/` |

The workspace is injected into tools via `WithToolWorkspace(ctx)` context injection. Tools read the workspace from context at execution time (fallback to the struct field for backward compatibility). User IDs are sanitized: anything outside `[a-zA-Z0-9_-]` becomes an underscore (`group:telegram:-1001234` → `group_telegram_-1001234`).

**Privilege separation for package management** -- System packages (apk) are installed via root-privileged helper:

| Component | User | Scope | Socket |
|-----------|------|-------|--------|
| Main app | goclaw (1000) | All operations except system packages | N/A |
| pkg-helper | root | System package (apk) install/uninstall only | `/tmp/pkg.sock` (0660 root:goclaw) |

The pkg-helper is started in `docker-entrypoint.sh` *before* privileges are dropped to goclaw. The main app connects to the Unix socket to request apk operations. System packages are persisted to `/app/data/.runtime/apk-packages` so they survive container recreation. Python and npm packages are installed directly by the goclaw user to writable runtime directories (`$PIP_TARGET`, `$NPM_CONFIG_PREFIX`).

**Docker sandbox** -- Container-based isolation for shell command execution:

| Hardening | Configuration |
|-----------|---------------|
| Read-only root FS | `--read-only` |
| Drop all capabilities | `--cap-drop ALL` |
| No new privileges | `--security-opt no-new-privileges` |
| Memory limit | 512 MB |
| CPU limit | 1.0 |
| PID limit | Enabled |
| Network disabled | `--network none` |
| Tmpfs mounts | `/tmp`, `/var/tmp`, `/run` |
| Output limit | 1 MB |
| Timeout | 300 seconds |

---

## 2. Docker Entrypoint & Runtime Configuration

GoClaw runs in a non-root container with three privilege levels:

**Phase 1: Root (docker-entrypoint.sh)**
- Re-install persisted system packages from `/app/data/.runtime/apk-packages`
- Start `pkg-helper` (root-privileged service listening on `/tmp/pkg.sock`)
- Set up Python and Node.js runtime directories with proper env vars

**Phase 2: Drop to goclaw user (su-exec)**
- Main app runs as `goclaw` (UID 1000) via `su-exec goclaw /app/goclaw`
- All agent operations execute in this context
- System package requests are delegated to pkg-helper via Unix socket

**Phase 3: Optional sandbox (per-agent)**
- Exec operations can be sandboxed in Docker containers (configurable)
- Sandbox containers inherit resource limits and security options

### Docker Compose Security

| Config | Purpose |
|--------|---------|
| `cap_drop: ALL` | Remove all Linux capabilities |
| `cap_add: [SETUID, SETGID, CHOWN, DAC_OVERRIDE]` | Minimum required for su-exec and pkg-helper socket |
| `security_opt: no-new-privileges:true` | Prevent privilege escalation |
| `tmpfs: /tmp` | Writable /tmp (256MB, noexec, nosuid) |

Docker-compose.yml mounts data volume at `/app/data`, which contains:
- config.json (runtime configuration)
- .runtime/apk-packages (persisted system packages list)
- .runtime/pip (pip install target directory)
- .runtime/npm-global (npm install prefix)
- skills/ (uploaded skills)

### Runtime Directory Structure

| Path | Owner | Purpose |
|------|-------|---------|
| `/app/data/.runtime/apk-packages` | 0666 (rw-rw-rw-) | Persisted apk package list, written by pkg-helper |
| `/app/data/.runtime/pip` | goclaw | Python packages installed via pip install --target |
| `/app/data/.runtime/npm-global` | goclaw | npm packages installed globally to prefix |
| `/app/data/.runtime/pip-cache` | goclaw | pip cache directory |
| `/tmp/pkg.sock` | 0660 (rw-rw----) | Unix socket: owner root, group goclaw |

---

## 3. Encryption

AES-256-GCM encryption for secrets stored in PostgreSQL. Key provided via `GOCLAW_ENCRYPTION_KEY` environment variable.

| What's Encrypted | Table | Column |
|-----------------|-------|--------|
| LLM provider API keys | `llm_providers` | `api_key` |
| MCP server API keys | `mcp_servers` | `api_key` |
| Custom tool env vars | `custom_tools` | `env` |
| Credentialed CLI env vars | `secure_cli_binaries`, `secure_cli_agent_grants`, `secure_cli_user_credentials`, `secure_cli_agent_credentials` | `encrypted_env` |

**Format**: `"aes-gcm:" + base64(12-byte nonce + ciphertext + GCM tag)`

Backward compatible: values without the `aes-gcm:` prefix are returned as plaintext (for migration from unencrypted data).

Credentialed CLI env entries have a separate visibility kind inside the encrypted JSON blob when `GOCLAW_ENCRYPTION_KEY` is configured. `sensitive` entries are masked in normal API/UI responses and never returned raw except through the explicit audited grant reveal flow. `value` entries use the same at-rest storage path but are returned to authorized admins for operational review.

---

## 4. Rate Limiting -- Gateway + Tool

Protection at two levels: gateway-wide (per user/IP) and tool-level (per session).

```mermaid
flowchart TD
    subgraph "Gateway Level"
        GW_REQ["Request"] --> GW_CHECK{"rate_limit_rpm > 0?"}
        GW_CHECK -->|No| GW_PASS["Allow all"]
        GW_CHECK -->|Yes| GW_BUCKET{"Token bucket<br/>has capacity?"}
        GW_BUCKET -->|Available| GW_ALLOW["Allow + consume token"]
        GW_BUCKET -->|Exhausted| GW_REJECT["WS: INVALID_REQUEST error<br/>HTTP: 429 + Retry-After header"]
    end

    subgraph "Tool Level"
        TL_REQ["Tool call"] --> TL_CHECK{"Entries in<br/>last 1 hour?"}
        TL_CHECK -->|">= maxPerHour"| TL_REJECT["Error: rate limit exceeded"]
        TL_CHECK -->|"< maxPerHour"| TL_ALLOW["Record + allow"]
    end
```

| Level | Algorithm | Key | Burst | Cleanup |
|-------|-----------|-----|:-----:|---------|
| Gateway | Token bucket | user/IP | 5 | Every 5 min (inactive > 10 min) |
| Tool | Sliding window | `agent:userID` | N/A | Manual `Cleanup()` |

Gateway rate limiting applies to both WebSocket (`chat.send`) and HTTP (`/v1/chat/completions`) chat endpoints. Config: `gateway.rate_limit_rpm` (0 = disabled, any positive value = enabled).

---

## 5. RBAC -- 3 Roles

Role-based access control for WebSocket RPC methods and HTTP API endpoints. Roles are hierarchical: higher levels include all permissions of lower levels.

```mermaid
flowchart LR
    V["Viewer (level 1)<br/>Read-only access"] --> O["Operator (level 2)<br/>Read + Write"]
    O --> A["Admin (level 3)<br/>Full control"]
```

| Role | Key Permissions |
|------|----------------|
| Viewer | agents.list, config.get, sessions.list, health, status, skills.list |
| Operator | + chat.send, chat.abort, sessions.delete/reset, cron.*, skills.update |
| Admin | + config.apply/patch, agents.create/update/delete, channels.toggle, device.pair.approve/revoke |

### Access Check Flow

```mermaid
flowchart TD
    REQ["Method call"] --> S1["Step 1: MethodRole(method)<br/>Determine minimum required role"]
    S1 --> S2{"Step 2: roleLevel(user) >= roleLevel(required)?"}
    S2 -->|Yes| ALLOW["Allow"]
    S2 -->|No| DENY["Deny"]
    S2 --> S3["Step 3 (optional):<br/>CanAccessWithScopes() for tokens<br/>with narrow scope restrictions"]
```

Token-based role assignment happens during the WebSocket `connect` handshake. Scopes include: `operator.admin`, `operator.read`, `operator.write`, `operator.approvals`, `operator.pairing`.

---

## 6. Sandbox -- Container Lifecycle

Docker-based code isolation for shell command execution.

```mermaid
flowchart TD
    REQ["Exec request"] --> CHECK{"ShouldSandbox?"}
    CHECK -->|off| HOST["Execute on host<br/>timeout: 60s"]
    CHECK -->|non-main / all| SCOPE["ResolveScopeKey()"]
    SCOPE --> GET["DockerManager.Get(scopeKey)"]
    GET --> EXISTS{"Container exists?"}
    EXISTS -->|Yes| REUSE["Reuse existing container"]
    EXISTS -->|No| CREATE["docker run -d<br/>+ security flags<br/>+ resource limits<br/>+ workspace mount"]
    REUSE --> EXEC["docker exec sh -c [cmd]<br/>timeout: 300s"]
    CREATE --> EXEC
    EXEC --> RESULT["ExecResult{ExitCode, Stdout, Stderr}"]
```

### Sandbox Modes

| Mode | Behavior |
|------|----------|
| `off` (default) | Execute directly on host |
| `non-main` | Sandbox all agents except main/default |
| `all` | Sandbox every agent |

### Container Scope

| Scope | Reuse Level | Scope Key |
|-------|-------------|-----------|
| `session` (default) | One container per session | sessionKey |
| `agent` | Shared across sessions for the same agent | `"agent:" + agentID` |
| `shared` | One container for all agents | `"shared"` |

### Workspace Access

| Mode | Mount |
|------|-------|
| `none` | No workspace access |
| `ro` | Read-only mount |
| `rw` | Read-write mount |

### Auto-Pruning

| Parameter | Default | Action |
|-----------|---------|--------|
| `idle_hours` | 24 | Remove containers idle for more than 24 hours |
| `max_age_days` | 7 | Remove containers older than 7 days |
| `prune_interval_min` | 5 | Check every 5 minutes |

### FsBridge -- File Operations in Sandbox

| Operation | Docker Command |
|-----------|---------------|
| ReadFile | `docker exec [id] cat -- [path]` |
| WriteFile | `docker exec -i [id] sh -c 'cat > [path]'` |
| ListDir | `docker exec [id] ls -la -- [path]` |
| Stat | `docker exec [id] stat -- [path]` |

---

## 7. API Key Security

API keys are generated and stored securely.

| Mechanism | Detail |
|-----------|--------|
| Format | `goclaw_<32 hex chars>` (48 chars total) |
| Key generation | 16 random bytes → hex-encoded, generated via `crypto.GenerateAPIKey()` |
| Storage | SHA-256 hash stored in database (`api_keys.hash`), never the raw key. Raw key shown once at creation. |
| Comparison | Timing-safe comparison via `crypto/subtle.ConstantTimeCompare` (not standard `==`) prevents timing attacks. Display prefix: first 8 hex chars of random part (e.g., `1a2b3c4d...`) |
| API auth | HTTP header `Authorization: Bearer {token}` or WebSocket param. Validated via constant-time hash comparison. |

---

## 8. Security Logging Convention

All security events use `slog.Warn` with a `security.*` prefix for consistent filtering and alerting.

| Event | Meaning |
|-------|---------|
| `security.injection_detected` | Prompt injection pattern detected |
| `security.injection_blocked` | Message blocked due to injection (when action = block) |
| `security.rate_limited` | Request rejected due to rate limit |
| `security.cors_rejected` | WebSocket connection rejected due to CORS policy |
| `security.message_truncated` | Message truncated because it exceeded the size limit |

Filter all security events by grepping for the `security.` prefix in log output.

---

## 9. Hook Recursion Prevention

The hook system (quality gates) can trigger infinite recursion: an agent evaluator delegates to a reviewer → delegation completes → fires quality gate → delegates to reviewer again → infinite loop.

A context flag `hooks.WithSkipHooks(ctx, true)` prevents this. Three injection points set the flag:

| Injection Point | Why |
|----------------|-----|
| Agent evaluator | Delegating to the reviewer for quality checks must not re-trigger gates |
| Evaluate-optimize loop | All internal generator/evaluator delegations skip gates |
| Agent eval callback (cmd layer) | When the hook engine itself triggers delegation |

`DelegateManager.Delegate()` checks `hooks.SkipHooksFromContext(ctx)` before applying quality gates. If the flag is set, gates are skipped entirely.

---

## 10. Group File Writer Restrictions

In group chats (Telegram), write-sensitive operations are restricted to designated writers. This prevents unauthorized users from modifying agent files or resetting sessions in shared groups.

```mermaid
flowchart TD
    CMD["Write-sensitive command<br/>(/reset, /addwriter, file writes)"] --> GROUP{"In group chat?"}
    GROUP -->|No| ALLOW["Allow (DM = no restriction)"]
    GROUP -->|Yes| CHECK["Check IsGroupFileWriter()<br/>(agentID, groupID, senderID)"]
    CHECK -->|Writer| ALLOW
    CHECK -->|Not writer| DENY["Deny operation"]
    CHECK -->|DB error| FALLBACK["Fail-open: Allow<br/>(log security.reset_writer_check_failed)"]
```

### Group ID Format

`group:{channel}:{chatID}` — for example, `group:telegram:-1001234567`.

### Managed Commands

| Command | Restriction |
|---------|-------------|
| `/reset` | Writers only in groups |
| `/addwriter` | Writers only (reply to target user to add) |
| `/removewriter` | Writers only |
| `/writers` | No restriction (informational) |
| File writes (exec) | Writers only in groups |

Writers are managed via `/addwriter` (reply to a user's message) and `/removewriter` commands. The writer list is stored per-agent per-group in the agent store.

---

## 11. Browser Pairing Security

Browser pairing allows web UI clients to authenticate without full admin credentials.

| Mechanism | Detail |
|-----------|--------|
| Pairing code | 8-character alphanumeric code (A-Z, 2-9, excludes I/O/L for clarity), generated via `generatePairingCode()` in `internal/store/pg/pairing.go` |
| Code TTL | 60 minutes; expired codes are auto-pruned from database |
| Paired device TTL | 30 days; provides defense-in-depth expiry (paired devices auto-cleaned if unused) |
| Pending limit | Max 3 pending pairing requests per account; prevents spam/enumeration |
| HTTP access | Paired browsers access HTTP APIs via `X-GoClaw-Sender-Id` header (requires `channel=browser`). Fail-closed: `IsPaired()` check blocks unpaired sessions. Logs failed HTTP pairing auth attempts for security monitoring. |
| Approval flow | Requires WebSocket `device.pair.approve` method from authenticated admin session, triggered by `pairing.approve` command. Admin approval adds sender to `paired_devices` table with `paired_by` audit field. |
| Stale session fix | Uses `useRef` (not `useState`) for senderID in browser pairing form to prevent stale closure. Auto-kick after pairing: `RequireAuth` now accepts senderID for paired browser sessions (skips logout). |

---

## 12. Delegation Security

Agent delegation is protected through delegation history tracking and concurrency controls.

| Control | Scope | Description |
|---------|-------|-------------|
| Per-agent load cap | B (all sources) | `other_config.max_delegation_load` limits total concurrent delegations targeting B |

When concurrency limits are hit, the error message is written for LLM reasoning: *"Agent at capacity (5/5). Try a different agent or handle it yourself."*

---

## 13. Package Management Security

### pkg-helper privilege model (v1 / v2)

The `pkg-helper` sidecar is the only root-privileged component of the gateway.

| Boundary | Detail |
|----------|--------|
| Socket path | `/tmp/pkg.sock` |
| Permissions | 0600 — owner `root`, accessible only to `goclaw` uid 1000 |
| Gateway process | Runs as uid 1000 (goclaw); never calls `apk` directly |
| Helper process | Runs as root inside the container; started by `docker-entrypoint.sh` before privilege drop |

Package name validation is defense-in-depth at three layers:
1. HTTP handler (`ValidateApkPackageName` — strict `^[a-z0-9][a-z0-9._+-]*$` regex)
2. `ApkUpdateExecutor.Update()` — same validator before socket dial
3. pkg-helper itself — validates again server-side before exec

### pkg-helper v2 (Phase 2b)

- **Trust boundary unchanged from v1:** `/tmp/pkg.sock` 0600 owned by `root`,
  group-readable by `goclaw`.
- **New actions** (`upgrade`, `update-index`, `list-outdated`) run under the same
  root privilege as v1 `install`/`uninstall`. No privilege escalation; same exec
  path, new action names.
- **`code` field** on error responses enables HTTP handlers to map errors to
  appropriate 4xx/5xx statuses without stderr parsing — eliminates the string-grep
  anti-pattern that risked misclassification.
- **apk invocation serialization** via process-wide `sync.Mutex` (`apkMutex`)
  prevents TOCTOU races between concurrent `install` + `upgrade` operations on the
  `/var/lib/apk/db` lock file.
- **No new network surface:** pkg-helper has no HTTP listener; it uses the same
  Unix socket as v1. The socket path (`/tmp/pkg.sock`) is unchanged.
- **Stderr truncation:** helper stderr captured by the gateway is truncated to
  500 chars (ANSI-stripped) before logging — prevents path leakage and PII in logs.

---

## 14. CLI Credential Adapters

The CLI credential adapter framework is the system-trusted path for injecting
auth material into spawned CLI subprocesses (`git clone`, `kubectl apply`,
`psql`, …). It is the second of two distinct trust boundaries for env-var
injection — distinct from, not a replacement for, the user-paste denylist.

User guide: [git-credential-adapter.md](./git-credential-adapter.md).
Implementer guide: [credential-adapter-playbook.md](./credential-adapter-playbook.md).

### Trust-boundary diagram

```
┌──────────────────────────────┐      ┌──────────────────────────────────┐
│  User-pasted env vars        │      │  System-injected adapter Env     │
│  (CLI Credentials → "env")   │      │  (CredentialAdapter.Prepare)     │
│                              │      │                                  │
│  ValidateGrantEnvVars         │      │  bypasses denylist               │
│  rejects GIT_SSH_COMMAND,    │      │  emits security.system_env_      │
│  LD_PRELOAD, PATH, …          │      │  injection slog.Warn per call    │
└──────────────────────────────┘      └──────────────────────────────────┘
        first line of defense                     second, audit-trailed line
```

Both paths coexist. A typo in `adapter_name` falls back to passthrough, which
restores the legacy denylist-only behavior — no silent bypass.

Runtime credential precedence is explicit: user override, then channel/context
credential, then agent credential, then binary-level env defaults. Agent
credentials are the default git trust boundary; granting access to an agent
also grants the ability to make that agent use its stored git credential.

### Audit log: `security.system_env_injection`

Every adapter injection emits **exactly one** structured slog line. Field
schema is pinned by `TestEmitSystemEnvInjectionAudit_*` in
`internal/tools/credential_audit_log_test.go` — changes here must update both
the test and operator-facing log-search recipes.

| Field | Type | Notes |
| ----- | ---- | ----- |
| `msg` | string | always `security.system_env_injection` |
| `adapter` | string | e.g. `git`, `psql`, `passthrough` |
| `binary` | string | binary name (`git`, `kubectl`, …) |
| `user_id` | string | tenant user UUID (empty for global-only contexts) |
| `credential_source` | string | `user`, `context`, `agent`, or empty when no scoped credential row was selected |
| `env_keys` | []string | sorted env-var NAMES (never values) |
| `argv_prefix_len` | int | number of argv elements prepended (NOT their content) |
| `host_scope_hash` | string | SHA-256 first 8 hex chars of normalized host_scope, or `"none"` |

**Plaintext hostname is intentionally omitted** to keep audit logs PII-safe
when goclaw is deployed inside a regulated tenant. Operators wanting to grep
for activity against a specific host pre-compute the hash:

```sh
echo -n "github.com" | sha256sum | cut -c1-8
```

Routing: `slog.Warn` writes to whatever the host runtime captures — for the
default goclaw deployment that's stderr → systemd/journald or Docker logs.
There is **no dedicated audit table** in v1 (see future work below).

### SSH TOFU MITM caveat

The git adapter's SSH path sets `StrictHostKeyChecking=accept-new`, which
accepts unknown host keys on first contact. A network attacker positioned
between goclaw and the git host CAN capture the SSH session on the first
connection.

Operators should pre-seed `~/.ssh/known_hosts` at deployment time:

```sh
ssh-keyscan github.com >> ~/.ssh/known_hosts
ssh-keyscan -p 22 gitea.internal >> ~/.ssh/known_hosts
```

Once a host key is in `known_hosts`, `accept-new` enforces match-or-fail on
subsequent connections — the TOFU window is one connection per host.

v2 will support per-credential pinned host keys (removes TOFU entirely).

### SIGKILL residual material

Ephemeral filesystem credentials (SSH key tmpfiles, `.pgpass` tmpfiles, future
KUBECONFIG/DOCKER_CONFIG tmpfiles) rely on `defer cleanup()` to remove
themselves after exec returns.

`SIGKILL` of the goclaw process leaves these 0600 files in `os.TempDir()`. On
POSIX, `os.TempDir()` is per-user, so exposure is limited to the goclaw uid.

High-security deployments should run a periodic sweep:

```sh
find "$TMPDIR" -name 'goclaw-gitkey-*' -mmin +60 -delete
find "$TMPDIR" -name 'goclaw-pgpass-*' -mmin +60 -delete
```

### Open future work

- Dedicated `audit_log` table for `security.system_env_injection` events
  (operator-grade query surface; v1 only writes slog).
- Multi-credential per user with host-routing logic (v1: one per
  user+binary+host_scope).
- Sandbox/Docker exec path support (v1 adapter is incompatible with the
  bind-mount-based sandbox path).
- Pinned SSH host keys per credential (replace TOFU).
- Credential-refresh primitive for `aws sts assume-role`-style short-lived
  STS credentials.

---

## File Reference

| Module | Path | Purpose |
|---|---|---|
| Input & output protection | `internal/agent/input_guard.go`, `internal/tools/scrub.go`, `internal/tools/shell.go`, `internal/tools/web_fetch.go` | Injection detection, credential scrubbing, shell deny patterns, SSRF protection |
| Crypto, RBAC & rate limiting | `internal/crypto/`, `internal/permissions/policy.go`, `internal/gateway/ratelimit.go` | AES-256-GCM, API key generation, 3-role RBAC, token bucket |
| Sandbox & filesystem isolation | `internal/sandbox/`, `internal/tools/filesystem*.go`, `internal/tools/types.go` | Docker sandbox lifecycle, FsBridge, PathDenyable interface |
| Pairing, packages & container init | `internal/gateway/methods/pairing.go`, `internal/store/pg/pairing.go`, `cmd/pkg-helper/`, `docker-entrypoint.sh` | Browser pairing, pkg-helper Unix socket, container privilege drop |

Use `grep` or your editor's symbol search for specific files.

---

## Cross-References

| Document | Relevant Content |
|----------|-----------------|
| [03-tools-system.md](./03-tools-system.md) | Shell deny patterns, exec approval, PathDenyable, delegation system |
| [04-gateway-protocol.md](./04-gateway-protocol.md) | WebSocket auth, RBAC, rate limiting |
| [06-store-data-model.md](./06-store-data-model.md) | API key encryption, agent access control pipeline |
| [07-bootstrap-skills-memory.md](./07-bootstrap-skills-memory.md) | Context file merging, virtual files |
| [08-scheduling-cron.md](./08-scheduling-cron.md) | Scheduler lanes, cron lifecycle, /stop and /stopall |
| [10-tracing-observability.md](./10-tracing-observability.md) | Tracing and OTel export |
