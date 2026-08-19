# Scout: RBAC + Tenant Policies — Phase 7 Enterprise Gap Analysis

**Status:** DONE_WITH_CONCERNS
**Date:** 2026-08-19
**Scope:** Read-only scout of `internal/permissions`, `internal/edition`, gateway/HTTP auth gating, store tenant scoping.
**Repo ref:** `dev` @ e75a4c07

---

## 1. RBAC — current state

### 1.1 Roles (fixed, role-based, NOT fine-grained)

Four hard-coded roles in `internal/permissions/policy.go:25-35`:

| Role | Level | Can do |
|---|---|---|
| `owner` | 4 | Superset of admin. Bypasses everything (`IsMasterScope`, `IsOwnerRole`). Owner-gated RPCs (`config.*` full, `config.chat_behavior.preview`). |
| `admin` | 3 | Full access to all classified methods; tenant admin gate for tenant-scoped writes. |
| `operator` | 2 | All read + write methods; NOT admin methods. |
| `viewer` | 1 | Read-only (explicit read allowlist only). |
| `""` (RoleNone) | 0 | Sentinel for unclassified RPCs — **fail-closed deny for everyone** (`policy.go:32-35`, `router.go:80-85`). |

Plus a **5th role** in the tenant membership table: `member` (`internal/store/tenant_store.go:27`), mapped down to `RoleOperator` for gateway access (`policy.go:140-153` `RoleFromTenantRole`: `member → operator`).

Hierarchy: `roleLevel()` at `policy.go:525-538`; min-role comparison `CanAccess` at `policy.go:99-105`.

### 1.2 Role assignment & propagation

- **DB:** `tenant_users.role` column (`migrations/000027_tenant_foundation.up.sql:25-35`, default `'member'`). Roles: owner/admin/operator/member/viewer (`tenant_store.go:23-29`).
- **API-key scopes** (`policy.go:37-57`): `operator.admin/read/write/approvals/pairing/provision` — a second, independent permission dimension for API keys (`MethodScopes` at `policy.go:217-231`, `CanAccessWithScopes` at `policy.go:108-125`).
- **Propagation:** WS router injects `store.WithRole(ctx, client.Role())` (`internal/gateway/router.go:96-110`); HTTP `enrichContext` via `requireAuth` (`internal/http/auth.go:384-411`). `RoleFromTenantRole` is the tenant-role → gateway-role bridge (`policy.go:140-153`).

### 1.3 Where RBAC is enforced

- **WS method dispatch:** central gate in `internal/gateway/router.go:67-94` → `PolicyEngine.CanAccess`. Method classification is static allowlists: `isAdminMethod` (`policy.go:233-330`), `isWriteMethod` (`policy.go:332-398`), `isReadMethod` (`policy.go:403-518`), `isPublicMethod` (`policy.go:205-214`). **Fail-closed by construction** — unknown method = RoleNone = deny.
- **HTTP:** `requireAuth(minRole, next)` (`auth.go:384-411`) — auto role from HTTP verb (`httpMinRole`) or explicit. Handlers pass `RoleAdmin`/`RoleOperator`/`""`.
- **Scope guards:** `requireTenantAdmin` (`internal/http/tenant_auth_helpers.go:22-56`) for tenant-scoped tables (checks owner/admin membership in `tenant_users`); `requireMasterScope` (`tenant_auth_helpers.go:71-87`, shared predicate `store.IsMasterScope` at `internal/store/context.go:462-468`) for global (no-`tenant_id`) tables. WS mirrors: `requireMasterScope`/`requireOwner` middleware in `internal/gateway/methods/config.go:40-89`, `chat_behavior.go:26-44`. Decision table: `CONTRIBUTING.md:58-73`.
- **Owner-only file roots:** `internal/http/files.go:256-262` — tenant-scoped file roots only when `edition.Current().RBACEnabled`.

### 1.4 Per-resource / per-action permissions (NOT fine-grained)

**No per-tenant custom roles or resource-action policy matrix exists.** What does exist is **per-agent, per-user grants for channel config actions** — NOT dashboard RBAC:

- `agent_config_permissions` table (`migrations/000022_agent_heartbeats.up.sql:54-68`, `tenant_id` added in `000027:56`): `(agent_id, scope, config_type, user_id, permission allow|deny)`.
- Config types: `file_writer`, `heartbeat`, `cron`, `context_files`, `*` (`internal/store/config_permission_store.go:14-20`).
- Scopes: `agent`, `*`, `group:*`, `group:...`, `guild:...` (`config_permission_store.go:63-71`).
- Enforcement only in **group/guild channel context** for file-writes/context-file-writes/cron (`CheckFileWriterPermission` `config_permission_store.go:129-164`, `CheckContextFilePermission:168-208`, `CheckCronPermission:238-279`). RBAC admin/operator/owner roles bypass these grants (`isAdminRole` `config_permission_store.go:215-221`).
- RPC surface: `config.permissions.list/check/grant/revoke` (`internal/gateway/methods/config_permissions.go`), admin-classified (`policy.go:243-246`).

**There is NO way to express "agent:deploy" only for operator.** Every method maps to exactly one role tier. A tenant cannot define its own roles or override per-resource/action access.

### 1.5 Edition gating

`internal/edition/edition.go` — two presets:
- `Standard` (`edition.go:28-36`): all features on, no limits, `RBACEnabled: true`.
- `Lite` (`edition.go:39-53`): `RBACEnabled: false`, 5 agents / 1 team / 5 members, 2 concurrent subagents, depth 1, `TeamFullMode: false` (lite task actions only), no KG/vector/channels.

**Limit enforcement gap:** `MaxAgents`/`MaxTeams`/`MaxTeamMembers` are stored on the Edition struct but **only consumed indirectly** — `IsLimited()` (`edition.go:78-80`) and `ChildRunLimit()` (`edition.go:100-105`) are used for feature toggles; a repo-wide grep found **no count-check enforcing MaxAgents at agent-create time** (only `edition_test.go` references `MaxAgents`). Same for `MaxSubagentConcurrent`/`MaxSubagentDepth` outside tests. `RBACEnabled` is read only in `files.go:256` (tenant-scoped file roots) and `edition_test.go`. So the Lite limits are mostly descriptive; actual caps are partial.

---

## 2. Tenant policies — current state

### 2.1 What exists (mostly NOT per-tenant policy)

- **`tenants` table** with a `settings JSONB` column (`migrations/000027_tenant_foundation.up.sql:8-16`) and `tenants.settings` updateable via `tenants.update` RPC (`internal/gateway/methods/tenants.go:155-205`, `Settings map[string]any`). **The JSONB is free-form** — no schema, no typed policy fields, nothing reads policy keys from it today (grep for policy keys on tenant settings: none).
- **`tenant_users`** role/membership (`000027:25-35`), `metadata JSONB`.
- **Per-tenant tool/skill toggles** (the closest thing to tenant policy):
  - `builtin_tool_tenant_configs(tool_name, tenant_id, enabled, settings)` — `000027:239-248`. Handlers: `internal/http/builtin_tools.go:329,370,438` (gated `requireTenantAdmin`).
  - `skill_tenant_configs(skill_id, tenant_id, enabled)` — `000027:254-262`.
- **Quota/rate limits — global config only, not per-tenant:**
  - `config.QuotaConfig` (global) at `internal/config/config_channels.go:408-425`: `Default`, `Providers` map, `Channels` map, `Groups` map. Enforced by `internal/channels/quota.go:33-145` (`QuotaChecker`). Keys are **provider/channel/group-id, NOT tenant_id**.
  - `config.ToolPolicySpec` global/per-agent/per-provider allow/deny (`config_channels.go:553-560`, `tools/policy.go`). No tenant dimension.
- **No per-tenant rate limit, provider/model allowlist, or quota config exists.**

### 2.2 Edition vs tenant

Edition is **global/process-wide** (`edition.SetCurrent` once at startup, `cmd/gateway.go:254`, `cmd/gateway_stores_sqliteonly.go:43`, `cmd/gateway_stores_sqlite.go:54`). There is **no per-tenant edition or per-tenant feature tier**. All tenants on a server share one edition.

### 2.3 Where per-tenant policy would naturally live

- **Existing seam:** `tenants.settings` JSONB (already mutable via `tenants.update`, tenant-scoped).
- **Existing pattern:** typed per-tenant config tables mirroring `builtin_tool_tenant_configs` / `skill_tenant_configs`.
- **New table candidate:** `tenant_policies(tenant_id, policy_type, payload JSONB, updated_at)` — no such table exists today.

---

## 3. Gaps vs "tenant policies + RBAC" as an Enterprise feature

### 3.1 RBAC gaps (build-needed)

| # | Gap | Evidence | Suggested approach |
|---|---|---|---|
| G1 | **No fine-grained resource/action permissions** (e.g. `agent:deploy` for operator only). Only 4 static role tiers. | `policy.go:25-35`, static allowlists `policy.go:233-518` | Introduce permission catalog (`resource:action` pairs) + `role_permissions` mapping table; keep tier fallback for compat. New `PolicyEngine.EffectivePermissions(role, tenantID)`. |
| G2 | **No custom roles** — tenants cannot define roles or bind permissions. | `tenant_users.role` is a free VARCHAR but only 5 constants read (`tenant_store.go:23-29`); `RoleFromTenantRole` fixed switch `policy.go:140-153` | New `roles` + `role_permissions` tables (per-tenant), resolver fallback to builtin roles. |
| G3 | **No per-tenant permission overrides** — RBAC is global for all tenants sharing a server. | `permissions.PolicyEngine` built from global ownerIDs (`policy.go:71-79`); no tenant_id in policy tables | Add `tenant_id` to permission tables; tenant-scope resolution in `CanAccess`. |
| G4 | **API-key scopes and roles are two parallel systems** with no unified policy surface. | `Scope` vs `Role` at `policy.go:37-47` vs `:25-35`; `RoleFromScopes` `policy.go:156-169` | Leave as-is (back-compat) or add scope↔permission mapping for admin visibility. Low priority. |
| G5 | **Mixed enforcement layers** — WS router role gate + per-handler `requireMasterScope/requireOwner` + HTTP `requireTenantAdmin`. Drift risk. | `router.go:67-94`, `config.go:40-89`, `tenant_auth_helpers.go:22-56` | Consolidate on one decision function `Authorize(ctx, method/resource, action)` shared by WS+HTTP. |
| G6 | **`RBACEnabled` edition flag barely enforced** — only file roots. | `files.go:256`; grep found no other consumer | Define what RBAC-off means (role ignored? no tenant users?) and enforce consistently. |

### 3.2 Tenant-policy gaps (build-needed)

| # | Gap | Evidence | Suggested approach |
|---|---|---|---|
| T1 | **No per-tenant quotas/rate limits.** Global `QuotaConfig` only; keys are provider/channel/group. | `config_channels.go:408-425`, `quota.go` | Add tenant-aware key to `QuotaChecker` (`tenant_id` dimension) + `tenants.settings` or `tenant_policies` payload. |
| T2 | **No per-tenant provider/model allowlist.** `llm_providers` is per-tenant storage but no gate on which providers/models a tenant may use. | `000027:94` (tenant_id on llm_providers); no provider-gating config | Add `allowed_providers`/`allowed_models` policy; enforce at provider resolution/chat entry. |
| T3 | **No per-tenant resource limits** (max agents/sessions). Edition `MaxAgents` is global and unenforced. | `edition.go:11-16`; no count-check at create | `tenant_policies` limits + count queries at create (agents, teams, sessions). |
| T4 | **`tenants.settings` JSONB is unstructured** — no schema or readers. | `000027:13`, `tenants.go:155-205` | Add typed policy struct + validation; versioned migration of `settings` keys. |
| T5 | **No suspension/freeze policy enforcement** — `tenants.status` exists (`tenant_store.go:16-20`) but no check blocks agent runs for suspended tenants. | `TenantStatusSuspended` const only | Enforce `status != active` at auth/connect + run entry. |

### 3.3 What already exists (reuse, don't rebuild)

- Multi-tenant scoping: `tenant_id` on ~30 tables (`000027`), `store.WithTenantID`/`TenantIDFromContext`/`IsMasterScope` (`internal/store/context.go:414-468`), `base.TenantIDForInsert`/`RequireTenantID` (`internal/store/base/tenant.go`).
- Role plumbing: `tenant_users.role` → `RoleFromTenantRole` → `WithRole` ctx → `CanAccess` (whole chain is wired and tested).
- Fail-closed default-deny method classification (G5's foundation is solid).
- Per-tenant typed config pattern: `builtin_tool_tenant_configs`, `skill_tenant_configs` + their `requireTenantAdmin` handlers.
- Tenant membership + role cache: `cache.PermissionCache` (`router.go:30,46`).
- Tenant CRUD RPCs incl. `settings` update (`methods/tenants.go`).

---

## 4. Unresolved questions

1. Does "Enterprise" scope mean per-tenant **quota/limits** (T1-T3) AND fine-grained RBAC (G1-G2), or only RBAC? Affects build size roughly 2x.
2. Should custom roles be additive to the 4 builtin tiers or replace them? Back-compat of `tenant_users.role` values matters.
3. Is per-tenant provider/model gating acceptable to enforce at chat entry (latency cost) or at provider-creation only?
4. Does Lite (SQLite desktop) need any of this, or is Phase 7 PG-only? `agent_config_permissions` already has SQLite parity; new policy tables would need dual-DB migrations.

---

Status: DONE_WITH_CONCERNS
Summary: RBAC is solid but coarse (4 fixed roles, fail-closed method classification, tenant role propagation), with a separate per-agent channel-config grant table; there is NO fine-grained/custom-role permission system and NO per-tenant policy config — quotas/providers/limits are all global edition or global config, and the `tenants.settings` JSONB is unstructured and unread.
Concerns/Blockers: Edition `MaxAgents`/`MaxTeams` limits are defined but unenforced (no count-check at create); `tenants.status` suspension is not enforced at run entry; scope of "Enterprise" (limits vs RBAC vs both) needs a decision before planning build phases.
