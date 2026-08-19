# Phase 7 — Enterprise (AgentKit Deep Integration Plan)

**Status:** IN PROGRESS
**Created:** 2026-08-19
**Branch:** dev
**Scope decision:** User chose "Toàn diện 8 khối" (all 8 Enterprise blocks).

## Context

Vision §105 Phase 7 (last phase). 4 scout reports in `reports/`:
- `scout-rbac-tenant-policies.md` — RBAC + tenant policies: 4 fixed roles exist (fail-closed); NO fine-grained/custom roles, NO per-tenant policies (quota/provider allowlist/resource limits/suspension).
- `scout-approval-audit.md` — Approval: exec-only, in-memory, NO `approval_requests` table, notif events declared never broadcast. Audit: `activity_logs` solid (PG+SQLite, tenant_id), ~120 sites; login not audited, WS lacks tenant_id, desktop drops audit, no retention/system UI.
- `scout-cost-observability.md` — Cost governance + observability largely BUILT (usage caps reserve/reconcile, pricing catalog, OTel); gaps: SQLite/desktop parity, no Prometheus, SLO alert unwired, dual cost paths, no budget-threshold alerts, OTel not CI-built.
- `scout-skill-signed.md` — Skill registry DB-backed exists (versions, grants, publish self-serve); NO approval/curation, NO package format/signing (only SHA-256 integrity).

## Confirmed schema baseline

- PG latest migration: `migrations/000102_missions.up.sql`, `RequiredSchemaVersion = 102`
- SQLite: `schema.go` `SchemaVersion = 65` + `schema.sql`

## Phases

| # | Phase | Blocks | Depends on |
|---|---|---|---|
| 1 | Approval queue persistence + Audit completeness | approval (persist/notif/RBAC/history) + audit (login, tenant_id, SQLite parity, retention, export) | none |
| 2 | Cost governance gaps + Observability gaps | SQLite UsageCapStore, budget overview, threshold alerts, session budget (optional), Prometheus, wire SLO alert, cost-in-OTel, OTel CI build | none |
| 3 | Skill registry approval/curation + Signed packages | skills.status lifecycle (draft→published), approve/reject methods, package format, ed25519 signing + `publisher_keys` trust anchor | none |
| 4 | Tenant policies + RBAC fine-grained | tenant_policies table (quota/provider-allowlist/limits/suspension), custom roles + role_permissions, per-tenant permission resolution | none |

Execution: dispatch phase groups in waves (01 then 02 then 03 then 04 per controller review). Controller owns Docker build/test + commit + PR + CI follow + merge to dev. Workstreams own their code + CI.

## Acceptance criteria (all phases)

- [ ] Every new table/column: PG migration + SQLite `schema.go` patch + `schema.sql` + version bumps (PG `RequiredSchemaVersion`, SQLite `SchemaVersion`).
- [ ] Every new user-facing string: `internal/i18n/keys.go` + `catalog_{en,vi,zh}.go` + web UI locale JSON (en/vi/zh) if UI-visible.
- [ ] All user input parameterized (`$N` PG / `?` SQLite). NO concatenation.
- [ ] NOT NULL columns never receive SQL NULL from store layer (audit nil helpers per `go-pro-max` skill).
- [ ] Tenant-scoped reads/writes fail-closed: `WHERE 1=0` when tenant absent; `WHERE tenant_id = $N` on writes.
- [ ] `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` pass (controller, in Docker).
- [ ] New store methods covered by unit tests (narrowest first); integration tests where public contract changed.
- [ ] Each phase report in `reports/` with Status protocol; plan ticked on merge.
- [ ] i18n key ordering: key + 3 catalogs added BEFORE handler code (runtime crash guard).
- [ ] Reverse-mapping check: every new WS method lands in correct `isReadMethod`/`isWriteMethod`/`isAdminMethod` slice.

## Migration number assignments (avoid collision)

| Phase | PG migration | SQLite patch |
|---|---|---|
| 1 W1 approval | `000103_approval_requests` | 66 (`approval_requests`) |
| 1 W2 audit | none (activity_logs exists) | none |
| 2 W1 cost | `000104_usage_cap_warn` (warn_at_percent col) | 67 (usage cap tables) |
| 2 W2 observability | none (code-only) | none |
| 3 W1 skill review | `000105_skill_review` | 68 (skill review cols) |
| 3 W2 signed | `000106_publisher_keys` (keys + signature cols) | 69 (publisher keys + signature) |
| 4 W1 tenant policies | `000107_tenant_policies` | 70 (tenant_policies) |
| 4 W2 RBAC | `000108_roles_permissions` | 71 (roles + role_permissions) |

Controller assigns final numbers if workstreams land out of order. Every workstream MUST bump `RequiredSchemaVersion` (PG) and `SchemaVersion` (SQLite) to match its own migration(s).

## Rollback

- All migrations additive-only (new tables/columns, no destructive change to existing rows).
- Down migrations provided for PG where table add; SQLite patches sequential (bump-down not supported — journal notes only).
- If a phase breaks CI: revert the phase's PR (previous verified commit), not silent partial.