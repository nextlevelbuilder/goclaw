# Phase 4 — Tenant Policies + RBAC Fine-Grained

**Depends on:** none (largest phase; dispatch as 2 workstreams with clear file ownership)
**Files in this phase:** tenant policies (`internal/store/`, `internal/http/`, `internal/config/`, `internal/channels/quota.go`), RBAC (`internal/permissions/`, `internal/gateway/router.go`, `internal/http/tenant_auth_helpers.go`)
**Ownership:** W1 = tenant policies, W2 = RBAC fine-grained. `internal/permissions/` is W2-only. Store table dir split: W1 owns `tenant_policies` tables + quota, W2 owns `roles`/`role_permissions` tables. Controller verifies.

## Context / verified baseline

- RBAC: 4 fixed roles owner/admin/operator/viewer (`internal/permissions/policy.go:25-35`), fail-closed method classification (isAdminMethod/isWriteMethod/isReadMethod `policy.go:233-518`), tenant role propagation via `RoleFromTenantRole` (`policy.go:140-153`), role level `roleLevel` (`policy.go:525-538`). NO custom roles, NO per-resource/action policy matrix, NO per-tenant overrides. `tenant_users.role` VARCHAR free-form but only 5 constants read.
- Tenant policies: `tenants.settings` JSONB (`000027:8-16`) free-form, mutable via `tenants.update` RPC, NOTHING reads policy keys today. Global quota (`config.QuotaConfig` `config_channels.go:408-425`) keyed by provider/channel/group, NOT tenant. No per-tenant provider/model allowlist, no resource limits enforcement (edition `MaxAgents` etc. defined but only indirect consumption), `tenants.status` suspension not enforced at run entry.
- Migration baseline: PG `000102`, SQLite `SchemaVersion=65`.

## Scope (from scout gaps)

### W1 — Tenant policies (scout T1-T5)
1. **`tenant_policies` table** — PG migration (new number, controller assigns, e.g. `000105_tenant_policies.up.sql`): `(id UUID PK, tenant_id UUID NOT NULL UNIQUE, quota JSONB NOT NULL DEFAULT '{}', allowed_providers TEXT[] NOT NULL DEFAULT '{}', allowed_models TEXT[] NOT NULL DEFAULT '{}', max_agents INT NULL, max_sessions INT NULL, max_teams INT NULL, status TEXT NOT NULL default 'active', updated_at TIMESTAMPTZ NOT NULL default now())`. Down migration drops. SQLite parity + SchemaVersion bump.
2. **Typed policy struct** in `internal/store/tenant_policy.go`: `TenantPolicy{Quota QuotaSpec, AllowedProviders []string, AllowedModels []string, MaxAgents/MaxSessions/MaxTeams *int, Status string}`. `QuotaSpec` mirrors global `QuotaConfig` shape (providers/channels/groups with per-window limits) — reuse type if compatible.
3. **Store CRUD**: `internal/store/tenant_policy_store.go` interface + PG + SQLite impls: `GetPolicy(ctx, tenantID)`, `UpsertPolicy`, `DeletePolicy`, `ListPolicyLimits`. Reads fail-closed (`WHERE tenant_id=$N`).
4. **Enforcement points:**
   - Quota: extend `internal/channels/quota.go` `QuotaChecker` to merge tenant policy quota on top of global (tenant overrides global for same key). Inject `TenantPolicyStore` into quota checker.
   - Provider/model allowlist: gate at chat entry / provider resolution — `internal/providerresolve` resolver + `internal/gateway` chat method: if tenant policy `allowed_providers` non-empty and requested provider not in list → error `policy.provider_denied`. Same for models.
   - Resource limits: at `agents.create` / `teams.create` / session creation, count existing per-tenant and block if over `max_agents`/`max_teams`/`max_sessions`. Add count queries to respective stores.
   - Suspension: at auth/connect + run entry, if `tenants.status` != active OR policy `status` != active → reject (reuse `TenantStatusSuspended` const, currently unused).
5. **HTTP surface**: `GET/PUT /v1/tenants/{id}/policies` (tenant-admin gated) + WS method `tenant.policies.get/update`. Emit audit `tenant.policy.updated`.
6. i18n keys for policy error messages (`policy.provider_denied`, `policy.limit_reached`, `policy.tenant_suspended`) + 3 catalogs.

### W2 — RBAC fine-grained (scout G1, G2, G3, G6)
1. **Permission catalog**: define resource:action pairs (e.g. `agent:create`, `agent:deploy`, `channel:create`, `skill:publish`, `provider:create`, `config:write`, `user:invite`) in `internal/permissions/catalog.go` — a `map[string]Permission` with default min-role tier (back-compat: tier fallback). Keep existing method classification working (no behavior change when no custom role overrides exist).
2. **`roles` table** — PG migration (new number): `(id UUID PK, tenant_id UUID NOT NULL, name TEXT NOT NULL, description TEXT, builtin BOOLEAN NOT NULL DEFAULT false, created_at TIMESTAMPTZ NOT NULL default now(), UNIQUE(tenant_id, name))`. SQLite parity.
3. **`role_permissions` table**: `(id UUID PK, role_id UUID NOT NULL, permission TEXT NOT NULL, effect TEXT NOT NULL default 'allow', UNIQUE(role_id, permission))`. SQLite parity.
4. **`tenant_users.role`** remains the back-compat anchor: builtin roles (owner/admin/operator/viewer/member) resolve to their existing tier; custom roles defined per-tenant via `roles`+`role_permissions` override tier behavior.
5. **PolicyEngine.EffectivePermissions(ctx, tenantID, role)**: merge builtin tier + per-tenant role_permissions. `CanAccess` consults: (a) if tenant has custom role for user, use its permission set; (b) else tier fallback. Keep fail-closed.
6. **Admin surface**: WS `role.list/create/update/delete`, `role.permissions.set` (master-scope for builtin, tenant-admin for custom). HTTP equivalents. Audit `role.created/updated/deleted`.
7. **`RBACEnabled` flag** (G6): define semantics — when false (Lite), roles ignored (current behavior), tenants share default. Document in edition.go. No enforcement change for Lite.
8. Tests: permission catalog default tiers unchanged (back-compat), custom role allow/deny, tier fallback, per-tenant isolation (tenant A custom role can't affect tenant B), fail-closed unknown permission.

## Verification steps
- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` (controller Docker).
- Unit: tenant policy store, quota merge precedence, provider/model gate, resource limit counts, suspension block; RBAC catalog back-compat, custom role resolution, isolation.
- Integration: tenant policy blocks over-limit agent create; custom role allows `agent:deploy` for operator.
- Report in `reports/phase-04-tenant-policies-rbac.md`.

## Risks / rollback
- Largest phase — split W1/W2 cleanly on store table ownership to avoid dual-write conflicts. Controller does NOT run both on same files.
- Adding fine-grained RBAC must NOT break existing tier behavior when no custom roles exist (additive permission catalog, default tiers preserved).
- `tenants.settings` JSONB stays free-form (back-compat); new `tenant_policies` table is the typed home — no migration of existing settings.
- Provider/model allowlist enforcement at chat entry adds latency — only check when policy non-empty (fast path unchanged).
- Suspension enforcement could block active tenants if `tenants.status` semantics shift — default policy status `active`; only block when explicitly non-active.