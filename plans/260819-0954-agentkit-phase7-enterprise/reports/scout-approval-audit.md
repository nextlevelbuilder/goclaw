# Scout Report: Approval Queue + Audit Logging (Phase 7 Enterprise Gap Analysis)

Status: DONE_WITH_CONCERNS
Date: 2026-08-19
Scope: Read-only scout of approval queue and audit logging in GoClaw gateway (WS + HTTP + MCP + store + UI).

---

## 1. Approval Queue — Current State

### What triggers approval today
Only **shell `exec`** gated by approval. Two gates inside `internal/tools/shell.go`:

1. **Deny-pattern override for package installs** (`shell.go:349-362`): if a normalized command matches `pkgInstallPatterns` (npm/pip/apt/etc. install), `RequestApproval()` is called directly (2-min timeout) and the command is blocked pending resolution. Not routed through `CheckCommand`.
2. **Exec approval policy** (`shell.go:412-426`): calls `approvalMgr.CheckCommand(command)`; `"deny"` → error, `"ask"` → `RequestApproval(command, agentID, 2*time.Minute)`. `RequestApproval` blocks the exec tool goroutine on a channel until resolved or timeout.

No other tools (filesystem write, web_fetch, browser, subagent, delegate, MCP, deploy) are approval-gated. **Team tools** have their own separate `approve`/`reject`/`review` task actions (`internal/tools/team_task_policy.go:17-21`) — these are team-task lifecycle transitions, not a human-approval-before-sensitive-action mechanism against the exec approval queue.

### Policy evaluation (`internal/tools/exec_approval.go`)
- `ExecApprovalConfig`: `security` ∈ {deny, allowlist, full} (default full), `ask` ∈ {off, on-miss, always} (default off), `allowlist` glob patterns (`exec_approval.go:41-45`, config `internal/config/config_channels.go:527-532`).
- `CheckCommand` (`exec_approval.go:117-149`): deny → return deny; allowlist → allow/miss/ask per ask mode; full → off=allow, always=ask, on-miss=ask unless allowlist match or safe bin.
- `safeBins` (`exec_approval.go:60-77`): read-only/dev tools only; infra/network binaries (`docker`, `kubectl`, `terraform`, `curl`, `wget`, `ssh`, `rsync`) are **excluded** — they require approval when ask=on-miss.
- **Default config is fully permissive** (`exec_approval.go:48-53`): security=full + ask=off → everything auto-allowed unless a deny pattern matches. Approval queue is opt-in via `GOCLAW_CONFIG` `tools.execApproval.*`.

### Workflow / manager (`exec_approval.go:97-208`)
- `ExecApprovalManager` is **in-memory only**: `pending map[string]*PendingApproval`, dynamic `alwaysAllow` map. **No `approval_requests` table exists in any migration** (no match for `approval_requests` in `migrations/`, SQLite `schema.sql`, or store layer).
- Flow: exec tool → `RequestApproval(command, agentID, 2min)` → creates pending entry `exec-N` with a `resultCh` → blocks → WS/MCP handler calls `Resolve(id, decision)` → decision pushed onto channel → exec resumes or returns "denied". 2-min timeout auto-denies (`exec_approval.go:169-193`).
- Decisions: `AllowOnce | AllowAlways | Deny` (`exec_approval.go:82-86`). AllowAlways adds the binary to a **runtime-only** dynamic allowlist (lost on restart, `exec_approval.go:175-184`).

### Who can approve (RBAC)
- WS methods registered with no per-method middleware (`internal/gateway/methods/exec_approval.go:25-29`); auth is enforced globally by `MethodRouter` via `permissions.MethodRole` + `MethodScopes` (`internal/gateway/router.go:67-95`).
- `exec.approval.list` → RoleViewer; `exec.approval.approve/deny` → RoleOperator (`internal/permissions/policy_test.go:337-345`; scopes `ScopeApprovals` in `policy.go:221-223`). So **any operator+ can approve/deny — not restricted to owner/admin, not restricted per-agent/tenant beyond the caller's tenant scope.**

### Surfaces
- **WS**: `exec.approval.list/approve/deny` (`pkg/protocol/methods.go:108-110`; handlers `internal/gateway/methods/exec_approval.go`).
- **MCP**: `goclaw_exec_approval_{list,approve,deny}` mirror the WS methods, registered when `deps.ExecApproval` non-nil (`internal/mcp/crud_exec_approval.go:15-31`, `internal/mcp/crud_server.go:207`).
- **HTTP**: no REST endpoints for approval.
- **Web UI**: approvals page + `useApprovals` hook (`ui/web/src/pages/approvals/`), polls on load and re-loads on WS events `exec.approval.requested` / `exec.approval.resolved` (`use-approvals.ts:42-45`).
- **Desktop UI**: no approval surface found (search for exec.approval/approval in `ui/desktop/frontend/src/` returned only chat-activity store files — chat activity, not approvals).

### Notification gap (CONCERN — confirmed)
`EventExecApprovalReq = "exec.approval.requested"` and `EventExecApprovalRes = "exec.approval.resolved"` are declared in `pkg/protocol/events.go:10-11` and the Web UI listens for them, but **no Go code ever broadcasts them** (repo-wide grep: only the declarations and the UI hook). The UI's approval list therefore only refreshes on manual load/poll; no push notification when a new approval is requested. Also no channel/Telegram notification on approval request.

### Approval audit trail
Approving/denying emits audit events `exec.approved` / `exec.denied` (`internal/gateway/methods/exec_approval.go:96,127`) — the only evidence the approval happened. `RequestApproval` itself logs only `slog.Info` (`exec_approval.go:166`). No persisted approval-request record (who requested, who decided, when, allow-once vs always) beyond those audit rows.

---

## 2. Audit — Current State

### Storage
- **`activity_logs` table** exists in PG (`migrations/000015_agent_budget.up.sql:3-18`) and SQLite (`internal/store/sqlitestore/schema.sql:1357`). Columns: `id, actor_type, actor_id, action, entity_type, entity_id, details JSONB, ip_address, created_at, tenant_id`.
- **`tenant_id` added** in migration `000027_tenant_foundation` (`migrations/000027_tenant_foundation.up.sql:75,140,188`); index `idx_activity_logs_tenant`. **Not a NOT NULL FK going forward** (`ALTER COLUMN tenant_id DROP DEFAULT` then added as a nullable column — row tenant-scoped via `store.WithTenantID` at write).
- Structured store interface `store.ActivityStore` (`internal/store/activity_store.go:51-56`): `Log`, `List`, `Count`, `Aggregate`. PG + SQLite implementations both exist (`internal/store/pg/activity_store.go`, `internal/store/sqlitestore/activity.go`).
- **No retention**: no `Prune`/`DELETE FROM activity_logs` anywhere; table grows unbounded. (Contrast: `workstation_activity` has a nightly-style `Prune` in PG `workstation_activity.go:114` and SQLite `workstation_activity.go:119`.)

### Write path
- Emitters call `emitAudit` — two helpers:
  - WS: `internal/gateway/methods/audit.go:10-25` — builds `bus.AuditEventPayload` with ActorType="user", actor=client.UserID(), IP=client.RemoteAddr(); **no TenantID passed** (activity via WS is not tenant-tagged at emit; persisted tenant comes from `payload.TenantID` which is empty → `store.WithTenantID(ctx, "")` at `cmd/gateway_events.go:283`).
  - HTTP: `internal/http/audit.go:12-35` — includes `TenantID: store.TenantIDFromContext`.
- Persister: `wireAuditSubscriber` (`cmd/gateway_events.go:262-299`) subscribes to `bus.TopicAudit`, pushes to a 256-capacity channel, single worker calls `pgStores.Activity.Log()` (tenant from payload); on full queue logs `audit.queue_full` and **drops the entry**. **PG-only**: if `d.pgStores.Activity == nil` (SQLite/desktop path) the subscriber is not wired (`gateway_events.go:263`) — audit events are still emitted/broadcast but **never persisted** in desktop edition. SQLite `SQLiteActivityStore` exists (`sqlitestore/activity.go`) but is not connected to the subscriber in `cmd/`.
- **Login is NOT audited.** No `emitAudit` in any auth handler (`internal/http/auth.go` / oauth — grep found only `oauth.login_started` in `internal/http/oauth.go:250`). OAuth `logout` (`oauth.go:383`). No `auth.login`/`auth.logout` row for API-key/cookie auth.

### Coverage (actions that emit audit)
~120 call sites across HTTP + WS. Categories observed (all `emitAudit(...)`):
- Agents: created/updated/deleted/workspace_synced/share_revoked/regenerated (`internal/http/agents.go:353,561,654,713`; `agents_sharing.go`; `agents_update.go:267`, `agents_delete.go:80`, `agents_create.go:195`).
- Channels: created/updated/deleted, memory extraction run/approve/reject, context/mcp/cli grants (`channel_instances.go`, `channel_memory_extraction.go`, `channel_instance_context_capability_admin.go`).
- Providers: created/updated/deleted/reconnected (`providers.go:676,745,900,933`).
- Skills: updated/deleted/toggled/grant changed/uploaded/tenant config (`skills.go`, `skills_grants.go`, `skills_versions.go`, `skills_upload.go`).
- MCP servers/grants/requests (`mcp.go`, `mcp_grants.go`, `mcp_requests.go`).
- Tenants/users (`tenants.go:113-286`), config applied/patched (`config.go:149,212`), cron CRUD (`cron.go`), sessions patched/deleted/reset/compacted (`sessions.go`), heartbeat (`heartbeat.go`), pairing approved/denied/revoked (`pairing.go`), teams CRUD/members (`teams_crud.go`, `teams_members.go`, `teams.go`), agent_links (`agent_links.go`), secure_cli (`secure_cli.go`).
- **Not covered**: exec tool execution itself (only approval decisions `exec.approved/denied`), LLM/agent message turns, tool invocations by agents, file writes, login.

### Read surface
- **HTTP**: `GET /v1/activity` (list) and `GET /v1/activity/aggregate?group_by=...` (`internal/http/activity.go:25-28`). `actor_id` grouping requires RoleAdmin; viewer scoping enforced via `resolveAuth` (see tests `activity_aggregate_test.go:31-134`). Tenant scope enforced by `requireAuth` + store `WHERE tenant_id = $N`.
- **WS**: no `activity.*` methods. Only `workstations.activity.list` (`pkg/protocol/methods.go:263`) — workstation-scoped exec audit, `adminOnly` (`internal/gateway/methods/workstations.go:60,512`), backend `workstation_activity` table (`migrations/000064_workstation_activity.up.sql:5-21`), separate store.
- **MCP**: no audit CRUD tools.
- **Web UI**: `GET /v1/activity` list + aggregate surfaced via team audit logs modal (`ui/web/src/pages/teams/team-audit-logs-modal.tsx`); workstation activity tab. No dedicated System Audit Logs page listing all actions.
- **Desktop UI**: no activity/audit page.

---

## 3. Gaps vs "Approval Queue + Audit" as an Enterprise Feature

### Approval — build-needed
| # | Gap | Evidence | Suggested approach |
|---|---|---|---|
| A1 | **Persistent approval_requests table** (submit → approve/reject → notify → execute stored) | No `approval_requests` table in any migration; manager is in-memory (`exec_approval.go:100-113`) | New dual-DB migration (PG `migrations/` + SQLite `schema.sql`/`schema.go` patches) `approval_requests(id, tenant_id, agent_id, requester, action_type, payload JSONB, status, decision, decided_by, decided_at, allow_once/always, created_at)`. Persist on request + on resolve. |
| A2 | **Broaden approval to more action types** (filesystem destructive ops, web_fetch external, browser, subagent spawn, credential access, deploy) | Only exec tool gated (`shell.go:349-362,412-426`); no `deploy` tool approval | Add an `ApprovalGate` interface on sensitive tools; PolicyEngine step or interceptor checks gate per tool. Phase-in: filesystem write + credential/bin grants first. |
| A3 | **Push notification when approval requested** | `EventExecApprovalReq/Res` declared (`events.go:10-11`) but never broadcast; UI refreshes on events that never fire `use-approvals.ts:42-45` | Emit `EventExecApprovalReq` on `RequestApproval` and `EventExecApprovalRes` on `Resolve` via msgBus (broadcast-to-tenant); also a channel/Telegram notify hook for operators. |
| A4 | **Approver identity/RBAC hardening** | Any operator+ can approve/deny (`policy_test.go:341-345`); no per-agent or per-request approval-role scoping | Add config policy for who may approve per action type (owner/admin by default for infra commands); record `decided_by`; reject approval by a tenant that doesn't own the requester's agent. |
| A5 | **Approval history / audit UI** | Only `exec.approved/denied` audit rows (`exec_approval.go:96,127`); approvals page shows only live pending `use-approvals.ts:28` | Extend `GET /v1/activity`-style list filtered by action `exec.approved/denied`, or a dedicated `GET /v1/approvals` history endpoint + UI tab. |
| A6 | **Approval timeout/expiry surface** | 2-min hard timeout auto-denies silently (`exec_approval.go:188-193`) | Persist timed-out decisions as `denied(timeout)`; expose in history; make timeout configurable. |
| A7 | **Web UI parity** — desktop UI has no approval page | No match in `ui/desktop/frontend/` | Add desktop approval panel mirroring `ui/web` approvals page. |

### Audit — build-needed
| # | Gap | Evidence | Suggested approach |
|---|---|---|---|
| B1 | **Audit login/logout events** | No `emitAudit` in auth handlers; only `oauth.login_started`/`oauth.logout` (`oauth.go:250,383`) | Emit `auth.login`/`auth.logout` (success/failure, method used) in the auth middleware/`connect` handler and HTTP auth. |
| B2 | **Audit agent/tool activity (LLM turns, tool calls, file writes)** | No coverage in pipeline/tools for tool invocations; exec itself not audited (only approval decisions) | Hook the pipeline activity phase + tool registry to emit `agent.tool_used` / `agent.run` (with entity=run_id/agent_id). Consider sampling or only sensitive tools first. |
| B3 | **WS audit entries lack tenant_id** | `audit.go:10-25` doesn't populate TenantID; persist path uses empty tenant (`gateway_events.go:283`) | Populate `TenantID` in WS `emitAudit` from client tenant scope (mirror HTTP helper `http/audit.go:32`). |
| B4 | **Desktop/SQLite audit not persisted** | Subscriber only wired when `pgStores.Activity != nil` (`gateway_events.go:263`) | Wire SQLite path too (gateway wiring in `cmd/` for sqliteStores.Activity), so Lite edition persists audit. |
| B5 | **Retention / lifecycle** | No `Prune` on `activity_logs` (only `workstation_activity` has it) | Add configurable retention (e.g. `audit.retentionDays`, nightly sweep) modeled on `workstation_activity` Prune workers. Consider partition-by-month. |
| B6 | **Audit export / forward** | No export endpoint; only HTTP list/aggregate | Add `GET /v1/activity/export` (CSV/JSONL, tenant-scoped, admin), and optional webhook/otel forward for SIEM. |
| B7 | **Audit UI (system-wide)** | No dedicated system audit page; only team task audit modal + workstation activity tab | Add a "System Audit Logs" admin page in `ui/web` surfacing `/v1/activity` + aggregate; optional desktop side. |
| B8 | **Queue drop silently loses audit** | `audit.queue_full` → drop (`gateway_events.go:278`) | Spill to disk or backpressure; at minimum metric + retry buffer. |
| B9 | **MCP audit tools** | No audit CRUD MCP tools (only exec.approval) | Add `goclaw_audit_list` / `goclaw_audit_aggregate` read-only MCP tools if operators want programmatic access. |

### Already exists (no build needed)
- Persistent `activity_logs` table (PG + SQLite) with tenant_id, indexed (`000015`/`000027`, `schema.sql:1357-1374`).
- Structured query store (`ActivityStore` Log/List/Count/Aggregate) with RBAC-scoped HTTP read surface (`/v1/activity`, `/v1/activity/aggregate`).
- Exec approval policy engine (security modes, ask modes, allowlist, safe-bins) + in-memory submit→resolve→execute flow with 3 decision types.
- WS + MCP approval methods; Web UI approvals page.
- Workstation exec audit (`workstation_activity`) with retention prune — usable as a template for B5.

---

## Suggested Phase 7 sequencing (concise)
1. **A1 + A3 + A4** (persisted approval_requests, real notification events, approver scoping) — core approval queue as an Enterprise feature. Blocking.
2. **B1 + B3 + B4** (login audit, WS tenant_id, SQLite persistence) — audit correctness/parity, low effort.
3. **A2** (broader tool gating) — design decision; start with filesystem + credential-grant tools.
4. **B5 + B6 + B7** (retention, export, audit UI) — admin-facing value; medium effort.
5. A5/A7/B8/B9 — follow-ups.

---

Status: DONE_WITH_CONCERNS
Summary: Approval is exec-only, in-memory (no `approval_requests` table), opt-in, and the UI's approval push events (`exec.approval.requested/resolved`) are declared but never broadcast — real gaps. Audit has a solid persistent `activity_logs` table (PG+SQLite, tenant_id, HTTP read surface) but login is not audited, WS entries lack tenant_id, desktop/SQLite never persists audit, and there is no retention or system-wide audit UI.
Concerns/Blockers: The WS approval methods and approval manager are not tenant-owner scoped beyond role; the approval manager is process-local so multi-process/gateway-restart loses pending requests. Desktop (SQLite) edition currently drops audit entirely. Confirm Phase 7 intent on scope of "sensitive actions" (exec only, or also filesystem/web_fetch/deploy) before building A2.