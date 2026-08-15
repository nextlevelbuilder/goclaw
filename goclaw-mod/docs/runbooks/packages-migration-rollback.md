# Rollback Runbook: packages-cli-credentials-unified-ui (migration 000058)

## Scope

Migration `000058_agent_grants_env_override` adds `encrypted_env BYTEA` to `secure_cli_agent_grants`.

Phase 2 store code (`Get`, `ListByBinary`) SELECTs this column. If the schema is rolled
back while Phase 2 code is still running, every query against that table will 500.


> **WARNING — DESTRUCTIVE ROLLBACK**
> Running `000058` down **permanently discards** all per-grant env override data.
> Every row in `secure_cli_agent_grants` where `encrypted_env IS NOT NULL` will lose
> its encrypted values. **There is no undo after the column is dropped.**
> 
> **Mandatory before running down:**
> ```bash
> pg_dump --table=secure_cli_agent_grants "$DATABASE_URL" > grants_env_backup_$(date +%Y%m%d_%H%M%S).sql
> ```
> The down migration emits a RAISE NOTICE with the count of affected rows before dropping.
> Review the count and abort if non-zero unless you have confirmed data loss is acceptable.

**Critical rule: revert app code FIRST, then migrate the schema down.**

---

## PostgreSQL Rollback

### Step 1 — Revert app binary (FIRST)

Deploy previous binary (the one without Phase 2 store changes) to all pods/instances.
Wait for health checks to pass before proceeding.

```bash
# Verify old binary is live and no Phase-2 store queries are executing
kubectl rollout status deployment/goclaw
```

### Step 2 — Migrate schema down

```bash
# Against production database (use your DSN)
./goclaw migrate down 1
# or with explicit DSN:
migrate -database "$DATABASE_URL" -path migrations down 1
```

### Step 3 — Verify

```bash
psql "$DATABASE_URL" -c "\d secure_cli_agent_grants"
# encrypted_env column should be absent
```

---

## SQLite / Desktop (Lite edition) Rollback

SQLite 3.35+ (bundled via modernc.org/sqlite ≥ v1.18) supports `ALTER TABLE … DROP COLUMN`.
The v27 → v26 downgrade path is **not implemented** in `schema.go` migrations map because
golang-migrate is PostgreSQL-only; SQLite versioning is upgrade-only.

### Option A — Clean reinstall (recommended for desktop users)

1. Back up `~/.goclaw/data/goclaw.db`.
2. Install older version of goclaw-lite.
3. Delete `~/.goclaw/data/goclaw.db`.
4. Restart — fresh DB at v24 schema.

### Option B — Manual column drop (advanced)

```bash
sqlite3 ~/.goclaw/data/goclaw.db \
  "ALTER TABLE secure_cli_agent_grants DROP COLUMN encrypted_env;"
# Then manually update schema_version row:
sqlite3 ~/.goclaw/data/goclaw.db \
  "UPDATE schema_version SET version = 26;"
```

Requires SQLite ≥ 3.35 (check with `sqlite3 --version`).

---

## Phase 2 Guard

Do NOT roll back the schema while Phase 2 or later code is deployed.
The store method `ListByBinary` hardcodes `encrypted_env` in its SELECT.
Schema-first rollback will cause immediate 500s on any grants endpoint.
