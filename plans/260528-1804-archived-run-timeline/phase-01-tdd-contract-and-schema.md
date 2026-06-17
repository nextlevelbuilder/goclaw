---
phase: 1
title: "TDD Contract and Schema"
status: complete
priority: P1
effort: "1d"
dependencies: []
---

# Phase 1: TDD Contract and Schema

## Context Links

- GitHub issue: `digitopvn/goclaw#76`
- Related issue boundary: `digitopvn/goclaw#67`
- Current schema: `migrations/000001_init_schema.up.sql`
- SQLite schema: `internal/store/sqlitestore/schema.sql`
- Trace store reference: `internal/store/tracing_store.go`

## Overview

Define the durable run timeline contract and add tests before schema/store code. This phase locks the archive semantics so later implementation does not drift into issue #67 delivery behavior.

## Key Insights

- `sessions.messages` is useful for conversation history but not reliable enough for exact live event rhythm.
- `traces` and `spans` are diagnostic product data with different retention/debug semantics.
- Archive timeline needs its own append-only table because ordering and privacy rules differ.

## Requirements

- Functional: define timeline item DTOs for activity, assistant message, tool call, tool result, and run status.
- Functional: expose Phase 1 storage as previews only for tool args/results.
- Functional: include `run_id`, `session_key`, `agent_id`, `tenant_id`, `seq`, `type`, `status`, timestamps, optional `trace_id`, optional `span_id`.
- Non-functional: preserve strict sequence ordering under parallel tool calls.
- Non-functional: support both PostgreSQL and SQLite/Lite.
- Non-functional: no raw hidden thinking persistence.

## Architecture

Use a dedicated append-only product table instead of reconstructing from `sessions.messages` or `spans`.

Proposed table:

```text
run_timeline_items
- id uuid/text primary key
- tenant_id uuid/text not null
- run_id text not null
- session_key text not null
- agent_id uuid/text nullable
- user_id text nullable
- channel text nullable
- chat_id text nullable
- seq bigint not null
- item_type text not null
- status text nullable
- title text nullable
- preview text nullable
- content text nullable
- tool_name text nullable
- tool_call_id text nullable
- trace_id uuid/text nullable
- span_id uuid/text nullable
- metadata json/jsonb nullable
- created_at timestamp not null
```

Index:
- `(tenant_id, run_id, seq)`
- `(tenant_id, session_key, created_at desc)`
- optional `(trace_id)` where supported

## Related Code Files

- Create: `internal/store/run_timeline_store.go`
- Create: `internal/store/pg/run_timeline.go`
- Create: `internal/store/sqlitestore/run_timeline.go`
- Create: `internal/store/pg/run_timeline_test.go`
- Create: `internal/store/sqlitestore/run_timeline_test.go`
- Create: `migrations/0000XX_run_timeline_items.up.sql`
- Create: `migrations/0000XX_run_timeline_items.down.sql`
- Modify: `internal/store/sqlitestore/schema.sql`
- Modify: `internal/store/sqlitestore/schema.go`
- Modify: `internal/upgrade/version.go`

## Implementation Steps

1. Write store contract tests first:
   - append items with explicit sequence.
   - list by `tenant_id + run_id` ordered by `seq`.
   - reject cross-tenant reads.
   - persist preview fields but not full tool JSON.
2. Write schema migration tests or startup schema assertions for PostgreSQL and SQLite.
3. Add store interface and DTO structs.
4. Add PostgreSQL migration and implementation.
5. Add SQLite full schema and incremental migration. Bump `SchemaVersion`.
6. Bump `RequiredSchemaVersion` for PostgreSQL.
7. Confirm migration numbering against current `origin/dev` before implementation.

## Success Criteria

- [ ] Store tests fail before implementation and pass after.
- [ ] Both DB backends expose the same timeline item fields.
- [ ] Sequence order is deterministic and not timestamp-dependent.
- [ ] Tool args/results have preview fields only in Phase 1.
- [ ] SQLite and PostgreSQL schema versions are both updated.

## Todo List

- [ ] Add failing PostgreSQL store tests.
- [ ] Add failing SQLite store tests.
- [ ] Define store interface and DTOs.
- [ ] Add PostgreSQL migration.
- [ ] Add SQLite full schema and incremental migration.
- [ ] Bump both schema version choke points.

## Risk Assessment

Main risk: schema drift between Standard and Lite. Mitigation: add both migrations in the same phase and compile/test `-tags sqliteonly`.

## Security Considerations

Timeline rows are tenant-scoped product data. All store list queries must include tenant scope. Phase 1 must not persist full tool argument/result JSON.

## Next Steps

Proceed to Phase 2 only after both DB backends pass contract tests.
