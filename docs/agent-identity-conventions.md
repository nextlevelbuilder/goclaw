# Agent Identity Conventions — `agent_key` vs `UUID`

> Dual-identity rules for agents, teams, and tenants. Read this before touching any code path that handles identifier strings, DomainEvents, SQL `WHERE agent_id`, router cache, or system prompts.

## 1. Core rule

**Question to ask for every identifier:**

> *Does this identifier ever touch the DB layer or need to be unique system-wide?*
>
> - **Yes** → use `UUID`
> - **No** (display / path / template / log only) → use `agent_key`

Vietnamese phrasing used by the team:

> *"Cái identifier này có bao giờ đụng vào DB layer hoặc cần unique toàn hệ thống không? Có → UUID. Không → agent_key."*

There is no third option. Every site that handles an `AgentID`, `TenantID`, or `TeamID` string must answer this question.

## 2. Decision table

### 2.1. MUST use UUID

| Use case | Reason | Example |
|---|---|---|
| SQL `WHERE`/`JOIN`/`INSERT` on `agent_id` column | DB column type is `uuid` | `episodic_summaries.agent_id`, `kg_entities.agent_id` |
| Store model struct field | Compile-time type safety | `store.EpisodicSummary.AgentID uuid.UUID` |
| `store.WithAgentID(ctx, ...)` | Downstream SQL query context | `loop_context.go` |
| `eventbus.DomainEvent.AgentID` ⚠️ | Consumer workers parse and query DB with it | `episodic_worker`, `semantic_worker`, `dreaming_worker` |
| OpenTelemetry span attribute `agent_id` | Cross-session tracing correlation | `loop_tracing.go` |
| Tenant-scoped FK (team members, agent_links) | DB constraint enforcement | `TeamMember.AgentID`, `AgentLink.SourceAgentID` |
| Public HTTP/WS API `AgentID` params | Stable across renames | `params.AgentID` in heartbeat, config, teams methods |
| `SystemPromptConfig.AgentUUID` | Runtime identification, not exposed to LLM | Paired with `AgentID` (key) |

### 2.2. MUST use agent_key

| Use case | Reason | Example |
|---|---|---|
| System prompt template ("You are @X") | LLM/user reads it, must be human-readable | `SystemPromptConfig.AgentID` |
| Filesystem path segment `agents/{X}/...` | Path is user-visible | `workspace.ResolveParams.AgentID` → `sanitizeSegment()` |
| WS `AgentEvent` broadcast to UI | UI filters and displays by name | `AgentEvent.AgentID` |
| Slog log fields (`"agent"` key) | Human-readable when grep'ing logs | `slog.Info(..., "agent", l.id)` |
| `store.WithAgentKey(ctx, ...)` | Intentional key propagation | `loop_context.go` |
| `tools.WithToolAgentKey(ctx, ...)` | Tool access identifier | `loop_context.go` |
| Router cache lookup | Callers have `agent_key` readily available | `router.Get(ctx, agentKey)` |
| Channel bot identifier (Telegram `@X`) | Users type it in chat | Channels layer |
| `Loop.ID() string` method | Display API | `loop_tracing.go` |

## 3. Trap zones

Five known identifier trap zones. All are mitigated by the agent identity hardening work (commits `acbfb2e4..d3f37068`). Status per zone below.

### 🪤 Trap 1: `DomainEvent.AgentID` is a `string`

- **Problem:** field type is `string`; the compiler cannot enforce UUID vs key. Publishers nearby only see the type, not the semantic intent. If a publisher writes `AgentID: l.id` instead of `l.agentUUID.String()`, downstream consumers (which `uuid.Parse` and query DB) explode with `invalid input syntax for type uuid`.
- **Rule:** if ANY consumer calls `uuid.Parse(event.AgentID)` or passes the value down to SQL `WHERE agent_id = $1`, the publisher MUST set a UUID string. Today every production consumer in `internal/consolidation/*` does exactly that.
- **Status:** Mitigated by the publish-time observer in `internal/eventbus/validate_agent_id.go` — warns with `slog.Warn("eventbus.non_uuid_agent_id", ...)` on any non-UUID drift. Strict type lift to `uuid.UUID` is gated (Phase 5) behind wire-format audit + rolling-deploy coexistence tests.

### 🪤 Trap 2: Struct with both `AgentID` and `AgentUUID`

- **Problem:** `SystemPromptConfig` has `AgentID string` (agent_key) + `AgentUUID string` (UUID). Two adjacent `string` fields are trivially swappable at a callsite.
- **Rule:** always check field comments or downstream consumer behavior before setting either field. `AgentID` is rendered into the system prompt ("You are @{AgentID}"); `AgentUUID` is for runtime identification and never reaches the LLM.
- **Status:** Mitigated. The one historical omission (`memoryflush.go` missing `AgentUUID`) is now set. See section 10 for why the omission worked by accident historically.

### 🪤 Trap 3: `mustParseUUID` silently returns `uuid.Nil`

- **Problem:** a store helper named `mustParseUUID` looked like `uuid.MustParse` from the stdlib (which panics), but actually behaved like `uuid.Parse` with the error swallowed — returning `uuid.Nil` on bad input. 88 callsites in `internal/store/pg/`. Passing an `agent_key` in would silently write `agent_id = '00000000-0000-0000-0000-000000000000'` or, for reads, return an empty result / zero-row update.
- **Rule:** two helpers, two contracts.
  - `parseUUID(s string) (uuid.UUID, error)` — use for **every** `INSERT`/`UPDATE`/`UPSERT`/`DELETE` and any `SELECT WHERE` where an empty result would hide a bug. Propagate the error. Callers fail fast with a clean Go error instead of a cryptic PG 23503 or a silently corrupted row.
  - `parseUUIDOrNil(s string) uuid.UUID` — honest rename of the former `mustParseUUID`. Intentionally silent. Only acceptable on read-only `SELECT WHERE` paths where a no-match on bad input is the correct semantics. Prefer `parseUUID` for any new code.
- **Status:** Mitigated by the `parseUUID` refactor across `internal/store/pg/`. 57 critical write sites migrated. The remaining `parseUUIDOrNil` sites are audited and intentional.

### 🪤 Trap 4: `Router.Get(ctx, X)` accepts both UUID and agent_key

- **Problem:** the router resolver parses either a UUID or an agent_key and returns the same `Loop`. Pre-hardening, the cache key was the raw input string — so the same agent could end up with two cache entries (one under UUID, one under agent_key), defeating invalidation. In addition, `HasSuffix`-based invalidation could collide with unrelated keys that happened to share a suffix.
- **Rule:** cache entries are **canonicalized** to `tenantID:agentKey` after resolution, regardless of whether the caller passed UUID or key. Invalidation uses exact-segment match, not substring. `IsRunning(ctx, agentKey)` is tenant-aware and requires `tenantID` from context.
- **Status:** Mitigated. See section 8 for the full cache strategy.

### 🪤 Trap 5: WS method params `AgentID` — inconsistent parsing

- **Problem:** WS handlers variously used bare `uuid.Parse(params.AgentID)`, `resolveAgentUUID(...)`, or passed straight to the router. From the client's perspective there was no single rule for whether to send UUID or agent_key.
- **Rule:** WS handlers accept **both** UUID and agent_key for `AgentID` params and resolve through a single helper. Hot-path handlers use `resolveAgentUUIDCached` which consults the router cache first and falls back to a DB lookup on miss. Cold-path handlers use `resolveAgentUUID` directly.
- **Status:** Mitigated. Standardized through `internal/gateway/methods/agent_links.go` helpers.

## 4. Code review sanity checklist

For every new `AgentID:` assignment or new `agent_id` read/write, walk through in order:

### Step 1 — Destination field type?

| Destination type | Required source |
|---|---|
| `uuid.UUID` | UUID source only: `l.agentUUID`, `ag.ID`, `store.AgentIDFromContext(ctx)` |
| `*uuid.UUID` | Same as above, nullable |
| `string` | → Step 2 |

### Step 2 — If destination is `string`, what does the consumer do with it?

| Consumer behavior | Required identifier |
|---|---|
| `uuid.Parse(...)` or passes to SQL `WHERE x = $1` on a uuid column | UUID string |
| Logs / displays / compares / renders template / builds path | `agent_key` string |
| Forwards to another consumer | Follow the terminal consumer |

### Step 3 — Double-check with grep

```bash
grep -rn "\.AgentID" <consumer files>
```

Look for any `uuid.Parse(...)` or SQL `WHERE agent_id = $1` in the downstream path. If any consumer eventually touches DB, the publisher must supply a UUID.

### Step 4 — Run the regression tests

```bash
go test ./internal/consolidation/... ./internal/eventbus/... ./internal/store/pg/...
```

Relevant tests:

- `consolidation/workers_test.go::TestEpisodicWorkerHandle_NonUUIDAgentID` — rejects `"goctech-leader"` with a clean error.
- `consolidation/workers_test.go::TestEpisodicWorkerHandle_NonUUIDTenantID` — mirror for tenant.
- `eventbus/validate_agent_id_test.go` — publish-time observer coverage.
- `store/pg/parseuuid_test.go` — helper contract.
- `agent/router_cache_canonicalize_test.go`, `router_invalidate_collision_test.go`, `router_isrunning_test.go` — router cache strategy.
- `gateway/methods/agent_resolve_cached_test.go` — cache-aware resolver.

## 5. How this applies to teams and tenants

The dual-identity pattern is a **codebase-wide convention**, not agent-specific:

| Entity | UUID field | Key field | Notes |
|---|---|---|---|
| Agent | `agents.id uuid` | `agents.agent_key text` | Dual-tenant: same key across tenants is allowed |
| Team | `agent_teams.id uuid` | (no key field) | Teams use UUID only; no slug or key field |
| Tenant | `tenants.id uuid` | `tenants.tenant_slug text` | Slug used in URLs and onboarding wizard |
| Session | `sessions.session_key text` | (no separate UUID) | Session key is the only identifier — safe by construction |

The rules in section 2 apply to agents and tenants. For **teams**:

- `WithTenantID(ctx, ...)` takes a `uuid.UUID`. Never pass a slug.
- `TeamMember.AgentID` is the canonical team-membership FK and must be a UUID.
- Slog log fields `"tenant"`, `"team"` should receive the human-readable slug/key, not the UUID.

## 6. Post-hardening status

| Trap zone | Status | Guardrail |
|---|---|---|
| Trap 1 — `DomainEvent.AgentID` as string | Mitigated (strict type gated) | `eventbus/validate_agent_id.go` publish observer |
| Trap 2 — `SystemPromptConfig` dual field | Mitigated | `memoryflush.go` + `loop_history.go` set both fields |
| Trap 3 — `mustParseUUID` silent nil | Mitigated | `parseUUID` error-propagating helper in 57 critical sites |
| Trap 4 — Router cache fragmentation | Mitigated | Canonical `tenantID:agentKey` keys + exact-segment invalidation |
| Trap 5 — WS method inconsistency | Mitigated | `resolveAgentUUID` / `resolveAgentUUIDCached` helpers |

Phase 5 (DomainEvent strict-type lift) is **gated** — only starts once Phases 1-4 have been stable on `dev` for ≥72h with zero `eventbus.non_uuid_agent_id` observer warnings, and after a coexistence test covering rolling deploys. Without those signals, the runtime observer is sufficient defense.

## 7. Regression test coverage map

| Test | File | Guards |
|---|---|---|
| `TestEpisodicWorkerHandle_NonUUIDAgentID` | `internal/consolidation/workers_test.go` | Trap 1 — consumer fails cleanly on bad `AgentID` |
| `TestEpisodicWorkerHandle_NonUUIDTenantID` | `internal/consolidation/workers_test.go` | Trap 1 — same for tenant |
| `TestValidateAgentID_*` | `internal/eventbus/validate_agent_id_test.go` | Trap 1 — publish-time observer warns on drift |
| `TestParseUUID*` / `TestParseUUIDOrNil*` | `internal/store/pg/parseuuid_test.go` | Trap 3 — helper contract (error vs silent) |
| `TestRouterGet_Canonicalize*` | `internal/agent/router_cache_canonicalize_test.go` | Trap 4 — UUID and key both map to same canonical cache entry |
| `TestRouterInvalidate_SuffixCollision` | `internal/agent/router_invalidate_collision_test.go` | Trap 4 — exact-segment invalidation does not collide on substring |
| `TestRouterIsRunning_*` | `internal/agent/router_isrunning_test.go` | Trap 4 — `IsRunning` is tenant-scoped, not tenant-blind |
| `TestRouterGet_DualTenantSameAgentKey` | `internal/agent/router_cache_canonicalize_test.go` | Dual-tenant agent_key isolation (see section 13) |
| `TestAgentResolveCached_*` | `internal/gateway/methods/agent_resolve_cached_test.go` | Trap 5 — cache-aware fast path plus DB fallback |
| `TestVaultHandlerUpload_*` | `internal/http/vault_handler_upload_test.go` | HTTP form `agent_id` entry point (bypass gap for owner / lite edition) |
| `TestMemoryFlush_*` | `internal/agent/memoryflush_test.go` | Trap 2 — `SystemPromptConfig.AgentUUID` is set |

## 8. Router cache canonicalization strategy

Cache invariants after hardening:

1. **Key format:** `{tenantID}:{agentKey}`. Computed AFTER the resolver returns an `Agent`. Inputs that arrived as UUID are translated to `agentKey` via the resolved record. No more dual entries.
2. **Tenant scoping:** all router operations take `ctx` and extract tenant via `store.TenantIDFromContext`. `Router.Get`, `Router.GetCached`, `Router.IsRunning`, and `InvalidateAgent` all require a tenant.
3. **Invalidation:** exact-segment match on the full canonical key. The previous `strings.HasSuffix(entry, agentKey)` approach could collide when one agent's key was a suffix of another's. Now replaced with exact match against `{tenantID}:{agentKey}`.
4. **`IsRunning(ctx, agentKey)`:** tenant-scoped. Pre-hardening, the check was tenant-blind and always returned `false` for any tenant whose agent_key collided with another tenant's loop (see section 13).

## 9. Batch fail-fast contract

Pre-hardening: store methods that accepted a batch (vault bulk upload, KG ingest, memory doc batch) called `mustParseUUID` on each row. Bad UUIDs resolved to `uuid.Nil`, the `INSERT` went through, and the DB rejected it via FK code 23503 — **for the first bad row only**. Successfully inserted rows stayed. Callers saw a partial success.

Post-hardening: `parseUUID` errors at parse time. The entire batch fails fast at the first invalid UUID, **before any write happens**. Net contract change:

- Error is raised earlier, not silent-to-loud.
- Callers that relied on partial success must validate UUIDs up front or split the batch themselves.
- Clients using bulk operations cannot mix valid and invalid UUIDs in the same call.

**Affected operations:** vault bulk upload, knowledge graph batch ingest, memory documents batch upsert.

This is an intentional contract change and is documented here rather than hidden in a release note, because callers outside this repo (if any) will see the new error type.

## 10. memoryflush historical accident

`memoryflush.go` historically set only `SystemPromptConfig.AgentID` and not `AgentUUID`. This did not cause a bug because `buildRuntimeSection` — the function that consumes `AgentUUID` — runs **after** the `CacheBoundaryMarker` in the assembled system prompt. Anything below the marker is rendered fresh every turn and never feeds into the stable prefix that gets cached by the LLM provider. Missing `AgentUUID` simply meant an empty runtime field in the dynamic suffix. No prompt cache bust, no functional consequence.

The field is now populated unconditionally for consistency with `loop_history.go`, which has always set both fields. Mentioned here because anyone reading the fix is likely to wonder what broke — nothing broke, it was a latent gap that would have become a real bug if `AgentUUID` ever moved above the cache boundary.

## 11. FK as safety net

PostgreSQL and SQLite both enforce FK constraints on `agent_id` → `agents.id`. This is the **primary correctness guarantee** against silent data corruption, not the `parseUUID` refactor.

**PostgreSQL:** all primary phase 4 target tables have an FK with `CASCADE` (or `SET NULL` for nullable columns). Verified against staging on 2026-04-11 across `episodic_summaries`, `kg_entities`, `kg_relations`, `memory_documents`, `memory_chunks`, `vault_documents` — plus 25 other agent-scoped tables. Bad UUIDs are rejected with PG error code 23503.

**SQLite:** `PRAGMA foreign_keys = ON` is applied per-connection via `sqlitestore/pool.go` and `schema.sql`. All `agent_id TEXT` columns declare `REFERENCES agents(id) ON DELETE CASCADE`. Bad input → SQLite constraint error.

**Orphan scan:** staging MCP scan on 2026-04-11 found **zero** rows with `agent_id = '00000000-0000-0000-0000-000000000000'` across 5 NOT-NULL target tables. Self-hosted deployments that predate the FK migrations can run `scripts/scan_orphan_agent_rows.sh` as a pre-flight check before upgrading.

**What the `parseUUID` refactor adds on top of FK:**

- Earlier error detection (before the SQL round-trip).
- A clean Go error with a descriptive message instead of a driver-layer PG 23503.
- Coverage for `UPDATE WHERE agent_id = uuid.Nil` and `SELECT WHERE agent_id = uuid.Nil` — FK cannot help on reads or no-op updates. This is where the real silent-bug risk lived post-FK, and the refactor closes it.

**If a future developer misses a `parseUUID` site:** the result is a cryptic PG error at the driver layer, not corruption. The FK safety net still holds.

## 12. Telemetry tables without FK

`spans`, `traces`, `usage_snapshots` do **not** have FK on `agent_id`. Reason: these are append-only, high-volume telemetry tables — FK validation overhead at insert rate would be prohibitive.

**Type-layer defense instead of FK:**

- All struct `AgentID` fields in `store/pg/tracing.go`, `store/pg/snapshot.go`, etc. are `*uuid.UUID`, not `string`.
- Inserts go through `store/base/helpers.go::nilUUID()` which converts `uuid.Nil → SQL NULL`.
- There is no `mustParseUUID` call site in telemetry store code — callers cannot pass bare strings. The compile-time contract blocks the trap entirely.

**Residual risk:** if a caller bypasses `nilUUID()` and writes `uuid.Nil` raw, the row lands with `agent_id = '00000000-0000-0000-0000-000000000000'`. Impact is scoped to analytics pollution (these tables feed dashboards, not downstream pipelines). Check with:

```sql
SELECT count(*) FROM spans WHERE agent_id = '00000000-0000-0000-0000-000000000000'::uuid;
```

If the count ever rises above zero on production, it is a caller bug in the telemetry emission path — not a store-layer regression.

## 13. Dual-tenant `agent_key`

`agent_key` is unique **only within a tenant**. The same `agent_key` can exist in multiple tenants with different UUIDs. Example from staging (2026-04-11): `tieu-ho` exists in both the Master tenant and the Việt Org tenant under different `agents.id` values.

The unique index is:

```sql
CREATE UNIQUE INDEX agents_tenant_key_unique
  ON agents (tenant_id, agent_key)
  WHERE deleted_at IS NULL;
```

Consequences for code:

- **Never** use bare `agent_key` as a global identifier. It is only unique inside a tenant.
- `Router.Get(ctx, "tieu-ho")` requires `tenantID` from ctx. Without tenant scoping, the lookup is ambiguous across tenants.
- Cache entries are keyed as `{tenantID}:{agentKey}` (section 8).
- Log formatting: prefer `{"agent": "tieu-ho", "tenant": "master"}` together, or just the UUID. Never log `agent_key` alone without a tenant.
- When designing new tables with agent_key columns, the unique index must include `tenant_id`. Scoping to tenant_id prevents the same error class in future features.

**Note on teams:** Unlike agents, `agent_teams` table uses **UUID only**. There is no `team_key` or slug field. Always reference teams by `id` (UUID). Teams are not dual-identity.

## Appendix — TL;DR one-liner

- Use **`l.agentUUID.String()`** when setting `DomainEvent.AgentID`, store model fields, SQL query parameters, OTel span attributes, FK constraints, and any tenant-scoped unique key.
- Use **`l.id`** when logging, emitting `AgentEvent` for the UI, rendering the system prompt, building filesystem paths, setting the key context (`WithAgentKey`), and doing router lookups.

---

## ActorID vs UserID in Group Chats

A second identity pattern complements agent identity: **who is acting** vs
**which scope the action belongs to**. The pattern mirrors agent UUID/key
but applies to end users in group contexts (Telegram, Discord, Feishu, Zalo).

### The two identities

| Identity | Context key | Helper | Meaning |
|---|---|---|---|
| **SCOPE** — namespace / memory | `UserIDKey` | `store.UserIDFromContext(ctx)` | `group:telegram:<chatID>` in Telegram groups; `guild:<guildID>:user:<senderID>` in Discord guilds; sender ID in DMs |
| **ACTOR** — acting principal | `SenderIDKey` | `store.SenderIDFromContext(ctx)` | Always the individual sender's numeric ID (never group-scoped) |
| (combined) | — | `store.ActorIDFromContext(ctx)` | `SenderID` if set, else falls back to `UserID` |

### When to use each

| Purpose | Helper | Why |
|---|---|---|
| Memory / KG / session key | `UserIDFromContext` / `MemoryUserID` / `KGUserID` | Memory is per-group by design — all members share conversational context |
| File / path scope | `UserIDFromContext` | Per-group workspace; group members share files |
| **Permission check** | `SenderIDFromContext` | Checks attribute to the real user, not the group |
| **Audit trail** (`initiated_by`, event `UserID`) | `ActorIDFromContext` | Traces the real user action across wrappers |
| **Ownership** (`OwnerID`, creator fields) | `ActorIDFromContext` | A skill published in a group belongs to the individual user |
| Role / RBAC | `ActorIDFromContext` | Role applies to the human, not the group |

### Group behavior table

| Context | `UserID` | `SenderID` | `ActorID` |
|---|---|---|---|
| Telegram DM | sender numeric | sender numeric | sender numeric |
| Telegram group | `group:telegram:<chatID>` | sender numeric | sender numeric |
| Discord DM | sender snowflake | sender snowflake | sender snowflake |
| Discord guild | `guild:<guildID>:user:<senderID>` | sender snowflake | sender snowflake |
| HTTP API | HTTP user ID | (empty) | HTTP user ID |
| Cron / subagent system ctx | (inherited / empty) | (empty or propagated) | same as SenderID, else UserID |

### Propagation through wrappers (#915)

When a tool wrapper synthesizes an inbound message (subagent announce,
delegate announce, teammate dispatch, session_send), the synthetic
`SenderID` carries routing identity (e.g. `subagent:<taskID>`) — NOT a
real user. The real acting sender must travel through `InboundMessage.Metadata`
under `tools.MetaOriginSenderID`, and the next-turn builder copies it to
`RunRequest.SenderID`. Without this propagation:

- Permission checks against `bus.IsInternalSender(...)` prefixes → **denied**
  (synthetic senders never match a grant)
- Empty sender → **denied** (`CheckFileWriterPermission` group-context fail-closed
  policy)

Code path:

```
SubagentTask.OriginSenderID
  → AnnounceMetadata.OriginSenderID
  → InboundMessage.Metadata[MetaOriginSenderID]
  → subagentAnnounceRouting.SenderID
  → RunRequest.SenderID
  → loop_context.go:WithSenderID(ctx, …)
  → SenderIDFromContext(ctx) in permission checks
```

Same chain for delegate (`delegate_tool.announceToParent`) and teammate
dispatch (`team_tool_dispatch.go`).

### Group permission check policy

`store.CheckFileWriterPermission` / `store.CheckCronPermission` in group
or guild context (`UserID` starts with `group:` or `guild:`):

- empty `SenderID` → **DENY** (system context must not gain write access silently)
- synthetic-prefix `SenderID` → **DENY** (`subagent:`, `notification:`, `teammate:`,
  `system:`, `ticker:`, `session_send_tool`)
- real numeric `SenderID` → DB lookup; DENY if no `file_writer` allow grant for this sender
- DB error → fail-open (availability over strictness)

In DM / HTTP / cron-direct context (no group/guild prefix): always allow.
No per-user writer gate applies.

### Legacy-data tolerance for skill ownership

Skills / cron / delegate audit trails created before #915 migration store
`UserIDFromContext` values (possibly `group:*` prefixed) in `owner_id` /
`user_id` columns. Ownership checks at `skill_manage.go` (patch/delete)
accept either `ActorIDFromContext` (new) or `UserIDFromContext` (legacy).
This lets existing group-scoped rows remain accessible without a
destructive backfill. When a user re-publishes a skill, the new row uses
actor ownership (tighter by default).

### Related code

- `internal/store/context.go` — helper definitions
- `internal/store/config_permission_store.go` — group permission policy
- `internal/tools/subagent_spawn.go`, `subagent_exec.go` — subagent propagation
- `internal/tools/delegate_tool.go` — delegate propagation
- `cmd/gateway_subagent_announce_queue.go`, `gateway_consumer_handlers.go` — re-ingress reconstruction
- `tests/integration/telegram_group_write_file_permission_test.go` — regression fixtures

When in doubt, walk the four-step checklist in section 4.
