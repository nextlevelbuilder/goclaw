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

**Edge case mitigations** (13 scenarios analyzed):
- Shell operators in command string → Blocked by early regex scan
- Argument injection via spaces → Protected by shell-word parsing (not shell evaluation)
- Binary PATH manipulation → Absolute path required + config match
- Symlink attacks → Verified by `exec.LookPath()` + config match
- Env var exfiltration → Command runs without shell, env vars never expand
- Output parsing tricks → Dynamic scrubbing catches all registered credential values
- Timeout abuse → Configurable per-binary timeout with context deadline
- Sandbox escape → Docker container isolation if sandbox enabled
- Verbose flag leakage → Separate deny_verbose list blocks verbose/debug output

### Layer 4: Output Security

| Mechanism | Detail |
|-----------|--------|
| Static credential scrubbing | Regex patterns detect: OpenAI (`sk-[a-zA-Z0-9]{20,}`), Anthropic (`sk-ant-[a-zA-Z0-9-]{20,}`), GitHub tokens (`ghp_/gho_/ghu_/ghs_/ghr_` + 36 chars), AWS (`AKIA[A-Z0-9]{16}`), generic patterns (API/token/secret/password/bearer/authorization + 8+ chars), connection strings (postgres/mysql/mongodb/redis/amqp URLs), env vars (KEY/SECRET/CREDENTIAL/PRIVATE + 8+ chars, DSN/DATABASE_URL/REDIS_URL/MONGO_URI), VIRTUAL_* vars (4+ chars), long hex strings (64+ chars). All replaced with `[REDACTED]`. |
| Dynamic credential scrubbing | Runtime-registered credential values (min 6 chars) scrubbed via `AddCredentialScrubValues()` and replaced with `[REDACTED]` |
| Dynamic value scrubbing (SSRF) | Server IPs and other runtime-discovered values registered via `AddDynamicScrubValues()` and replaced with `[SERVER_IP]` |
| Web content wrapping | Fetched content wrapped in `<<<EXTERNAL_UNTRUSTED_CONTENT>>>` tags with security warning |

### Layer 5: Isolation

**Per-user workspace isolation** -- Two levels prevent cross-user file access:

| Level | Scope | Directory Pattern |
|-------|-------|------------------|
| Per-agent | Each agent gets its own base directory | `~/.goclaw/{agent-key}-workspace/` |
| Per-user | Each user gets a subdirectory within the agent workspace | `{agent-workspace}/user_{sanitized_id}/` |

The workspace is injected into tools via `WithToolWorkspace(ctx)` context injection. Tools read the workspace from context at execution time (fallback to the struct field for backward compatibility). User IDs are sanitized: anything outside `[a-zA-Z0-9_-]` becomes an underscore (`group:telegram:-1001234` → `group_telegram_-1001234`).

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

## 2. Encryption

AES-256-GCM encryption for secrets stored in PostgreSQL. Key provided via `GOCLAW_ENCRYPTION_KEY` environment variable.

| What's Encrypted | Table | Column |
|-----------------|-------|--------|
| LLM provider API keys | `llm_providers` | `api_key` |
| MCP server API keys | `mcp_servers` | `api_key` |
| Custom tool env vars | `custom_tools` | `env` |

**Format**: `"aes-gcm:" + base64(12-byte nonce + ciphertext + GCM tag)`

Backward compatible: values without the `aes-gcm:` prefix are returned as plaintext (for migration from unencrypted data).

---

## 3. Rate Limiting -- Gateway + Tool

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

## 4. RBAC -- 3 Roles

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

## 5. Sandbox -- Container Lifecycle

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

## 6. API Key Security

API keys are generated and stored securely.

| Mechanism | Detail |
|-----------|--------|
| Format | `goclaw_<32 hex chars>` (48 chars total) |
| Key generation | 16 random bytes → hex-encoded, generated via `crypto.GenerateAPIKey()` |
| Storage | SHA-256 hash stored in database (`api_keys.hash`), never the raw key. Raw key shown once at creation. |
| Comparison | Timing-safe comparison via `crypto/subtle.ConstantTimeCompare` (not standard `==`) prevents timing attacks. Display prefix: first 8 hex chars of random part (e.g., `1a2b3c4d...`) |
| API auth | HTTP header `Authorization: Bearer {token}` or WebSocket param. Validated via constant-time hash comparison. |

---

## 7. Security Logging Convention

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

## 8. Hook Recursion Prevention

The hook system (quality gates) can trigger infinite recursion: an agent evaluator delegates to a reviewer → delegation completes → fires quality gate → delegates to reviewer again → infinite loop.

A context flag `hooks.WithSkipHooks(ctx, true)` prevents this. Three injection points set the flag:

| Injection Point | Why |
|----------------|-----|
| Agent evaluator | Delegating to the reviewer for quality checks must not re-trigger gates |
| Evaluate-optimize loop | All internal generator/evaluator delegations skip gates |
| Agent eval callback (cmd layer) | When the hook engine itself triggers delegation |

`DelegateManager.Delegate()` checks `hooks.SkipHooksFromContext(ctx)` before applying quality gates. If the flag is set, gates are skipped entirely.

---

## 9. Group File Writer Restrictions

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

## 10. Browser Pairing Security

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

## 11. Delegation Security

Agent delegation is protected through delegation history tracking and concurrency controls.

| Control | Scope | Description |
|---------|-------|-------------|
| Per-agent load cap | B (all sources) | `other_config.max_delegation_load` limits total concurrent delegations targeting B |

When concurrency limits are hit, the error message is written for LLM reasoning: *"Agent at capacity (5/5). Try a different agent or handle it yourself."*

---

## File Reference

| File | Description |
|------|-------------|
| `internal/agent/input_guard.go` | Injection pattern detection (6 patterns) |
| `internal/tools/scrub.go` | Credential scrubbing (regex-based redaction), dynamic scrub values |
| `internal/tools/shell.go` | Shell deny patterns, command validation |
| `internal/tools/web_fetch.go` | Web content wrapping, SSRF protection |
| `internal/permissions/policy.go` | RBAC (3 roles, scope-based access), method routing |
| `internal/gateway/ratelimit.go` | Gateway-level token bucket rate limiter (per user/IP) |
| `internal/sandbox/sandbox.go` | Docker sandbox configuration and modes |
| `internal/sandbox/docker.go` | Docker sandbox creation, execution, pruning |
| `internal/sandbox/fsbridge.go` | File operations in sandbox (read/write/list) |
| `internal/crypto/aes.go` | AES-256-GCM encrypt/decrypt |
| `internal/crypto/apikey.go` | API key generation (format, hash, display prefix) |
| `internal/tools/types.go` | PathDenyable interface definition |
| `internal/tools/filesystem.go` | Denied path checking (`checkDeniedPath` helper) |
| `internal/tools/filesystem_list.go` | Denied path support + directory filtering |
| `internal/hooks/context.go` | WithSkipHooks / SkipHooksFromContext (recursion prevention) |
| `internal/hooks/engine.go` | Hook engine, evaluator registry |
| `internal/gateway/methods/pairing.go` | Pairing RPC methods (request, approve, deny, list, revoke) |
| `internal/store/pg/pairing.go` | Pairing store implementation (code generation, TTLs) |
| `internal/store/pairing_store.go` | Pairing store interface definition |

---

## Cross-References

| Document | Relevant Content |
|----------|-----------------|
| [03-tools-system.md](./03-tools-system.md) | Shell deny patterns, exec approval, PathDenyable, delegation system, quality gates |
| [04-gateway-protocol.md](./04-gateway-protocol.md) | WebSocket auth, RBAC, rate limiting |
| [06-store-data-model.md](./06-store-data-model.md) | API key encryption, agent access control pipeline |
| [07-bootstrap-skills-memory.md](./07-bootstrap-skills-memory.md) | Context file merging, virtual files |
| [08-scheduling-cron.md](./08-scheduling-cron.md) | Scheduler lanes, cron lifecycle, /stop and /stopall |
| [10-tracing-observability.md](./10-tracing-observability.md) | Tracing and OTel export |
