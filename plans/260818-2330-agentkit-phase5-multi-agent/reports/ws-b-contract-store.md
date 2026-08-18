# WS-B Report — Contract Store (Durable Multi-Agent Records)

## Scope

Implemented the durable persistence layer for multi-agent collaboration records
(handoff, jury, competition, negotiation) with dual-DB lockstep: PostgreSQL
migrations + SQLite schema. The store treats `ContractRecord.Body` as an opaque
JSON string, so it never imports `internal/contract` (no import cycle).

## Files Modified/Created

| File | Action | Notes |
|------|--------|-------|
| `internal/store/contract_store.go` | created | Shared `ContractStore` interface, `ContractRecord` struct, kind/status constants, `ValidContractRecordStatus` |
| `internal/store/pg/contract.go` | created | PostgreSQL implementation (JSONB body) |
| `internal/store/pg/contract_test.go` | created | 5 PG tests |
| `internal/store/sqlitestore/contract.go` | created | SQLite implementation (TEXT body) |
| `internal/store/sqlitestore/contract_test.go` | created | 5 SQLite tests |
| `migrations/000100_multi_agent_records.up.sql` | created | PG migration |
| `migrations/000100_multi_agent_records.down.sql` | created | PG rollback |
| `internal/upgrade/version.go` | modified | `RequiredSchemaVersion` 99 → 100 |
| `internal/store/sqlitestore/schema.go` | modified | `SchemaVersion` 62 → 63, migration patch `62:` |
| `internal/store/sqlitestore/schema.sql` | modified | Appended `multi_agent_records` table + indexes (lines 2458-2477) |

## Design

- **Tenant-scope fail-closed:** every read/write resolves the tenant from context
  via `tenantIDForInsert` / `requireTenantID`; list/single-read builders return
  ` WHERE 1=0` when a tenant is required but absent from the context, matching
  the `pg/artifact.go` convention. `IsCrossTenant(ctx)` bypasses the tenant
  predicate for master-scope reads.
- **Opaque body:** `ContractRecord.Body` is a JSON-encoded string. PG stores it
  as JSONB (`$5::jsonb` on write, `body::text AS body` on read); SQLite stores
  TEXT. Empty body normalizes to `{}`. The store never parses the payload, so
  `internal/contract` is never imported.
- **Validation:** kind is required and constrained to the four known kinds;
  status defaults to `draft` and must be a known status on create/update.
- **Ordering:** list reads are newest-first (`created_at DESC, id DESC`) with
  `LIMIT` (default 100, max 500) and `OFFSET`.
- **JSONB normalization:** PG `jsonb::text` output normalizes whitespace and key
  ordering, so the PG roundtrip tests compare bodies semantically
  (`json.Unmarshal` + `reflect.DeepEqual`). SQLite tests use exact string
  comparison since TEXT preserves bytes.

## Tests (5 per backend)

PG (`internal/store/pg/contract_test.go`, uses `hooksTestDB` + `seedTenantAndAgent`):
1. `TestPGContractStoreCreateGetRoundtrip` — create handoff, ID assigned, status defaults to draft, tenant matches, get roundtrip.
2. `TestPGContractStoreListFilters` — 3 records across runs/kinds; byRun (2), byKind (1 jury), byStatus (1 closed).
3. `TestPGContractStoreUpdateStatus` — update to active verified; invalid status rejected.
4. `TestPGContractStoreTenantScope` — tenant B get fails, no-tenant get fails closed, tenant B list empty.
5. `TestPGContractStoreBodyJSONRoundtrip` — nested JSON roundtrip + empty body → `{}`.

SQLite (`internal/store/sqlitestore/contract_test.go`, uses `openTestDB` + `EnsureSchema` + `seedSQLiteRunTimelineTenant`):
1. `TestSQLiteContractStoreCreateGetRoundtrip` — mirror of PG #1 (master tenant).
2. `TestSQLiteContractStoreListFilters` — mirror of PG #2.
3. `TestSQLiteContractStoreUpdateStatus` — mirror of PG #3.
4. `TestSQLiteContractStoreTenantScope` — tenant A/B isolation + no-tenant fail-closed.
5. `TestSQLiteContractStoreBodyJSONRoundtrip` — nested JSON roundtrip + empty body → `{}`.

No benchmark/load tests were added.

## Dual-DB Lockstep Verification

| Check | Status |
|-------|--------|
| `migrations/000100_multi_agent_records.up.sql` + `.down.sql` exist | PASS |
| `internal/upgrade/version.go`: `RequiredSchemaVersion uint = 100` | PASS |
| `internal/store/sqlitestore/schema.go`: `SchemaVersion = 63`, migration patch `62:` added after patch `61:` | PASS |
| `internal/store/sqlitestore/schema.sql`: `multi_agent_records` table appended (lines 2465-2477) | PASS |
| Migration name carries no phase ID (`000100_multi_agent_records`) | PASS |

## Safety Constraints

- Did NOT touch `internal/contract/**`, `internal/orchestration/**`, `internal/workflow/**`, `internal/store/artifact_store.go`, `internal/store/subagent_store.go`, `internal/store/agent_store.go`, or existing migrations 000098/000099.
- No import cycle with `internal/contract` — body is opaque, no contract types referenced.
- Factories (`pg/factory.go`, `sqlitestore/factory.go`, `stores.go`) untouched — wiring is WS-C's Stage 2 responsibility.
- No phase IDs in code comments; comments describe behavior.
- All params parameterized (`$N` PG, `?` SQLite) — no string concatenation of user input.
- No background processes started; no shell/git/gh run (code-only, per task constraint).

## Validation Pending

No local Go toolchain available — the Docker gate (controller) must run:
`go build ./...`, `go vet ./...`, `go build -tags sqliteonly ./...`,
`go test ./internal/store/pg/...` and `go test ./internal/store/sqlitestore/...`.

## Files Not Owned (Read-Only Reference)

Read-only references used for patterns only: `pg/artifact.go`,
`sqlitestore/artifact.go`, `pg/hooks_test.go`,
`sqlitestore/run_timeline_test.go`, `sqlitestore/schema_migration_test.go`,
`pg/helpers.go`, `sqlitestore/helpers.go`, `base/helpers.go`,
`sqlitestore/scan_time.go`, `migrations/000097/000098/000099`.

Status: DONE
Summary: Contract store implemented for dual-DB (PG + SQLite) with 5 tests per backend; migration 000100 + RequiredSchemaVersion=100 + SQLite schema patch 62→63 in lockstep.
Concerns/Blockers: None. Docker gate verification pending — no local Go available; factories unwired by design (WS-C Stage 2).
