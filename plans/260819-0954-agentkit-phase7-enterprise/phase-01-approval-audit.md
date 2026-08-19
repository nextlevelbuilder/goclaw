# Phase 1 — Approval Queue Persistence + Audit Completeness

**Depends on:** none
**Files in this phase:** approval (`internal/tools/exec_approval.go`, `internal/gateway/methods/exec_approval.go`, `internal/store/`), audit (`internal/gateway/methods/audit.go`, `internal/http/audit.go`, `cmd/gateway_events.go`, `internal/store/`)
**Ownership:** W1 = approval, W2 = audit. Distinct files, no shared edits (except `cmd/gateway_events.go` — approval notif may emit there; keep audit wiring in W2, approval notif in W1). Controller verifies merge.

## Context / verified baseline

- `approval_requests` table does NOT exist; `ExecApprovalManager` (`internal/tools/exec_approval.go:97-113`) is in-memory: `pending map[string]*PendingApproval`, lost on restart.
- Notif gap: `EventExecApprovalReq/Res` declared (`pkg/protocol/events.go:10-11`) but never broadcast. Web UI listens (`use-approvals.ts:42-45`) — dead listener.
- Audit: `activity_logs` solid (PG `000015`, SQLite `schema.sql:1357`, tenant_id in `000027`); WS `emitAudit` (`methods/audit.go:10-25`) does NOT pass TenantID; persister `wireAuditSubscriber` (`cmd/gateway_events.go:262-299`) only when `pgStores.Activity != nil` — desktop/SQLite drops audit.
- Migration baseline: PG `000102`, `RequiredSchemaVersion=102`; SQLite `SchemaVersion=65`. New PG migration = `000103`. New SQLite = patch 66.

## Scope (from scout gaps)

### W1 — Approval queue persistence (scout A1, A3, A4, A6)
1. **PG migration `000103_approval_requests.up.sql`** (additive):
   - `approval_requests(id UUID PK default gen_random_uuid(), tenant_id UUID NOT NULL, agent_id UUID NULL, requester_id UUID, requester_type TEXT, action_type TEXT NOT NULL, payload JSONB NOT NULL DEFAULT '{}', command TEXT, status TEXT NOT NULL, decision TEXT NULL, decided_by UUID NULL, allow_once BOOLEAN NULL, allow_always BOOLEAN NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), decided_at TIMESTAMPTZ NULL, expired_at TIMESTAMPTZ NULL, timeout_seconds INT NOT NULL DEFAULT 120)`
   - Indexes: `(tenant_id, status)`, `(agent_id)`. Down migration drops table.
2. **SQLite parity:** patch 66 in `schema.go` + `schema.sql` table (mirror columns, `?` placeholders). Bump `SchemaVersion` → 66.
3. **`internal/store/approval_store.go`** interface + **`internal/store/pg/approval_store.go`** + **`internal/store/sqlitestore/approval.go`** implementations: `CreateRequest`, `ListPending(ctx, tenantID)`, `Resolve(id, decision, decidedBy, allowOnce, allowAlways)`, `GetByID`, `MarkExpired`, `ListHistory(ctx, tenantID, limit, offset)`.
4. **`ExecApprovalManager` refactor:** keep in-memory fast path; on `RequestApproval` also persist CreateRequest (best-effort, non-blocking); on `Resolve` persist Resolve; on timeout call `MarkExpired` + status `denied(timeout)`. Store `expired_at = now + timeout_seconds`.
5. **Broadcast notif:** emit `EventExecApprovalReq` (broadcast-to-tenant) in `RequestApproval`; `EventExecApprovalRes` in `Resolve` — via msgBus. Web UI already listens; verify no double load needed.
6. **Approver RBAC hardening:** `exec.approval.approve/deny` require caller tenant == request tenant (already via WS tenant scope) + require RoleOperator (exists). Add decided_by=caller. Deny if request already resolved (idempotent).
7. **Approval history surface:** extend `GET /v1/activity` filter for `exec.approved/denied` OR add `GET /v1/approvals` (history, tenant-scoped, admin). Prefer reusing activity list (KISS) — add `entity_type='approval_request'` rows? Decide: if adding dedicated endpoint, keep it thin (limit/offset/status filter).

### W2 — Audit completeness (scout B1, B3, B4, B5)
1. **Login/logout audit:** emit `auth.login` / `auth.logout` (success+failure, method used) in WS `connect` handler + HTTP `requireAuth` path (`internal/http/auth.go`). Failure events: action `auth.login_failed`.
2. **WS audit tenant_id:** fix `methods/audit.go:10-25` to pass `TenantID: store.TenantIDFromContext(ctx)` (mirror HTTP helper `http/audit.go:32`).
3. **SQLite audit persistence:** wire `wireAuditSubscriber` to also use `sqliteStores.Activity` when PG store nil (`cmd/gateway_events.go:263`). SQLiteActivityStore already exists.
4. **Retention:** config `audit.retentionDays` (default 0 = keep forever). Add `Prune(ctx, before)` on ActivityStore + a daily background sweep modeled on `workstation_activity` Prune worker (`internal/tracing/snapshot_worker.go` pattern). Delete rows older than retention.
5. **Audit export:** `GET /v1/activity/export?format=csv|jsonl` tenant-scoped, admin-gated, streamed (CSV via csv.Writer / JSONL lines). Add to `internal/http/activity.go`.
6. Tests: store CRUD (approval), audit persist via SQLite path, approval round-trip (request→resolve→history), auth audit events.

## Verification steps
- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` (controller Docker).
- Unit: approval_store PG+SQLite, activity Prune, WS audit tenant scoping, login audit.
- Integration (if controller provides DB): approval request→resolve→exec resumes; audit row has tenant_id.
- Report in `reports/phase-01-approval-audit.md`.

## Risks / rollback
- New table additive; rollback = drop `approval_requests` via down migration (PG). SQLite patch additive, bump-down not supported (journal note).
- Persisting approval every request could slow exec if DB down — keep persistence best-effort (non-blocking, log + continue in-memory path). Do NOT block exec on DB write.
- Audit retention delete is destructive — only deletes rows older than configured retention, default keeps everything.