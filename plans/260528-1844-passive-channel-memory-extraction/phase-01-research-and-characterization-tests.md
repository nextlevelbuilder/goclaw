---
phase: 1
title: "Research and Characterization Tests"
status: complete
priority: P1
effort: "1d"
dependencies: []
---

# Phase 1: Research and Characterization Tests

## Context Links

- Issue: `digitopvn/goclaw#64`
- Existing docs: `docs/06-store-data-model.md`, `docs/07-bootstrap-skills-memory.md`, `docs/09-security.md`, `docs/18-http-api.md`
- Channel buffer: `internal/channels/history.go`, `internal/store/pending_message_store.go`
- Memory/KG: `internal/consolidation/episodic_worker.go`, `internal/consolidation/semantic_worker.go`

## Overview

Lock current behavior with tests before changing schemas or workers. This phase proves the feature can reuse existing channel pending-message buffers and Memory/KG pipeline without breaking session memory.

## Requirements

- Functional: document exact current data flow for group pending messages, channel instance config update, episodic creation, KG extraction, and activity logging.
- Non-functional: tests first, no implementation until expected failures describe the desired behavior.

## Architecture

No runtime architecture added yet. The output is a verified map and failing tests for v1 contracts:

1. channel instance config can carry `passive_memory`
2. pending messages are tenant/channel/history-key scoped
3. extraction candidates exclude DMs/private history in v1
4. session-created episodic flow remains unchanged

## Related Code Files

- Read: `internal/channels/history.go`
- Read: `internal/channels/history_compaction.go`
- Read: `internal/store/pending_message_store.go`
- Read: `internal/store/channel_instance_store.go`
- Read: `internal/consolidation/episodic_worker.go`
- Read: `internal/consolidation/semantic_worker.go`
- Read: `internal/http/channel_instances.go`
- Create: `internal/consolidation/channel_memory_config_test.go`
- Create: `internal/consolidation/channel_memory_worker_test.go`
- Create: `internal/http/channel_memory_handlers_test.go`
- Create or extend: store tests under `internal/store/pg/` and `internal/store/sqlitestore/`

## Implementation Steps

1. Re-scout channel adapters to list which currently call `PendingHistory.Record`.
2. Re-scout `channel_pending_messages` PG/SQLite implementations and confirm tenant predicates.
3. Write failing config parser tests for default-disabled `passive_memory`.
4. Write failing candidate-selection tests: group-only, enabled-only, interval-or-cap trigger, minimum useful content gate.
5. Write failing redaction tests using representative secret/PII strings.
6. Write failing API tests for unauthorized settings/run/review actions.
7. Write a short `plans/.../reports/scout-report.md` with verified file/line anchors.

## Todo List

- [ ] Enumerate all pending-history recording call sites.
- [ ] Enumerate HTTP + WS channel instance update surfaces.
- [ ] Add failing config parser tests.
- [ ] Add failing worker trigger tests.
- [ ] Add failing redaction tests.
- [ ] Add failing admin API permission tests.
- [ ] Save scout report under this plan.

## Success Criteria

- [ ] Tests fail for missing passive-memory config, worker, queue, and handlers.
- [ ] Scout report lists exact files to modify and no fabricated APIs.
- [ ] Plan assumptions match live code after re-grep.

## Risk Assessment

Main risk is planning from stale scout notes. Mitigation: every claim in later phases must cite paths found in this phase, especially schema versions and route wiring.

## Security Considerations

Tests must include cross-tenant pending-message isolation and reject non-admin write access before any handler implementation exists.

## Next Steps

Proceed to schema/store only after tests describe the contracts.
