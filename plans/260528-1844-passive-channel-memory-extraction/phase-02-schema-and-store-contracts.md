---
phase: 2
title: "Schema and Store Contracts"
status: complete
priority: P1
effort: "2d"
dependencies: [1]
---

# Phase 2: Schema and Store Contracts

## Context Links

- Phase 1 tests and scout report
- PG migration latest during planning: `000073_secure_cli_credential_type.up.sql`
- PG schema version: `internal/upgrade/version.go`
- SQLite schema: `internal/store/sqlitestore/schema.sql`, `internal/store/sqlitestore/schema.go`
- Store registry: `internal/store/stores.go`, `internal/store/pg/factory.go`, `internal/store/sqlitestore/factory.go`

## Overview

Add durable run and review-item stores for passive extraction. Keep channel settings inside `channel_instances.config` to avoid another settings subsystem.

## Requirements

- Functional: persist extraction runs, pending extracted items, approval/rejection/deletion state, source ranges, redaction stats, and write metadata.
- Non-functional: tenant-scoped SQL, PG + SQLite parity, deterministic source IDs, no raw message body in extraction item after redaction.

## Architecture

Use two new store concepts:

1. `ChannelMemoryExtractionRun`: one batch attempt per channel/history key.
2. `ChannelMemoryExtractionItem`: one extracted candidate fact or KG relation bundle pending review.

`channel_instances.config.passive_memory` carries settings. Store layer exposes typed config helpers so HTTP/WS/UI do not mutate arbitrary JSON blindly.

## Related Code Files

- Create: `migrations/000074_channel_memory_extraction.up.sql`
- Create: `migrations/000074_channel_memory_extraction.down.sql`
- Modify: `internal/upgrade/version.go`
- Modify: `internal/store/sqlitestore/schema.sql`
- Modify: `internal/store/sqlitestore/schema.go`
- Create: `internal/store/channel_memory_extraction_store.go`
- Create: `internal/store/pg/channel_memory_extraction.go`
- Create: `internal/store/sqlitestore/channel_memory_extraction.go`
- Modify: `internal/store/stores.go`
- Modify: `internal/store/pg/factory.go`
- Modify: `internal/store/sqlitestore/factory.go`

## Implementation Steps

1. Add PG migration `000074` with tenant-scoped tables and indexes:
   - `channel_memory_extraction_runs`
   - `channel_memory_extraction_items`
2. Add unique constraints for idempotency:
   - run: `(tenant_id, channel_instance_id, history_key, source_start_id, source_end_id)`
   - item: `(tenant_id, run_id, item_hash)`
3. Add statuses:
   - run: `pending`, `running`, `completed`, `failed`, `cancelled`
   - item: `pending_review`, `approved`, `rejected`, `written`, `deleted`
4. Add source metadata fields: channel instance ID/name, history key, platform message ID range, time range, extractor run ID.
5. Add redaction summary fields: count, categories, skipped reason.
6. Add SQLite schema and migration map patch; bump `SchemaVersion` from 42 to 43.
7. Bump PG `RequiredSchemaVersion` from 73 to 74.
8. Implement PG and SQLite stores using existing scope patterns.
9. Wire stores into `store.Stores` and factories.
10. Make Phase 1 store tests pass.

## Todo List

- [ ] Add PG migration + version bump.
- [ ] Add SQLite schema + migration map + version bump.
- [ ] Add store interface types.
- [ ] Add PG store implementation.
- [ ] Add SQLite store implementation.
- [ ] Wire factories.
- [ ] Add idempotency tests.
- [ ] Add tenant isolation tests.

## Success Criteria

- [ ] Fresh PG migration applies and rolls back.
- [ ] Fresh SQLite DB contains tables.
- [ ] Existing SQLite DB migrates 42 to 43.
- [ ] Store tests pass for create/list/update/claim/status transitions.
- [ ] No SQL string concatenation with user input.

## Risk Assessment

Migration conflict risk is high on active `dev`. Before implementation, re-run `ls migrations/*.up.sql | sort | tail` and renumber if another migration claims `000074`.

## Security Considerations

All read/write methods require tenant context unless explicitly cross-tenant. Review item payload must store redacted text only; raw body stays in existing pending-message table subject to retention.

## Next Steps

Worker can claim runs only after store contracts exist.
