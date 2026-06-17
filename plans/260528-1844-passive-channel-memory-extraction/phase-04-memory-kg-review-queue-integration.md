---
phase: 4
title: "Memory KG Review Queue Integration"
status: complete
priority: P1
effort: "2d"
dependencies: [2, 3]
---

# Phase 4: Memory KG Review Queue Integration

## Context Links

- Episodic store: `internal/store/episodic_store.go`, `internal/store/pg/episodic_summaries.go`, `internal/store/sqlitestore/episodic.go`
- Semantic worker: `internal/consolidation/semantic_worker.go`
- KG store: `internal/store/knowledge_graph_store.go`
- Activity store: `internal/store/activity_store.go`

## Overview

Add approve/reject/delete semantics and connect approved channel extraction items to existing episodic Memory and KG ingestion. Keep review queue default-on.

## Requirements

- Functional: list pending items, approve, reject, delete, manual write, idempotent re-approve, source metadata preserved.
- Non-functional: approved summaries become searchable like episodic memory; KG receives only approved/redacted text; all writes are tenant/agent scoped.

## Architecture

Approval writes one episodic summary with:

- `SourceType`: `channel`
- `SourceID`: deterministic `channel:<run_id>:<item_id>`
- `SessionKey`: synthetic, e.g. `channel:<channel_name>:<history_key>`
- `Summary`: approved redacted memory text
- `KeyTopics`: extracted normalized topics/entities
- `UserID`: approving admin or configured channel agent owner user, decided in implementation after re-checking current auth model

Then publish `EventEpisodicCreated` so `semanticWorker` extracts KG facts through the existing path.

## Related Code Files

- Modify: `internal/consolidation/episodic_worker.go` only if helpers can be reused without changing session behavior.
- Create: `internal/consolidation/channel_memory_writer.go`
- Modify: `internal/eventbus/event_types.go` only if channel-specific payload is genuinely needed; prefer existing `EventEpisodicCreated`.
- Modify: new `ChannelMemoryExtractionStore` from Phase 2.
- Create: tests in `internal/consolidation/channel_memory_writer_test.go`.

## Implementation Steps

1. Write failing tests for approve idempotency:
   - first approve creates episodic and marks item written/approved
   - second approve no-ops
   - rejected/deleted item cannot be approved unless restored is explicitly implemented, which v1 does not need
2. Implement writer service that depends on:
   - `ChannelMemoryExtractionStore`
   - `EpisodicStore`
   - `DomainEventBus`
   - `ActivityStore`
3. Create episodic summary from approved item with deterministic source ID.
4. Publish existing `EventEpisodicCreated` with redacted summary.
5. Record activity:
   - `channel_memory.item.approved`
   - `channel_memory.item.rejected`
   - `channel_memory.item.deleted`
   - `channel_memory.item.written`
6. Add delete/forget behavior:
   - v1 deletes queued item if not written
   - if written, mark deleted and delete associated episodic summary when source ID matches
   - KG deletion for already-ingested facts may be best-effort by source metadata if existing KG store supports it; otherwise flag as v1 limitation in UI/docs

## Todo List

- [ ] Add writer service tests.
- [ ] Implement approve/reject/delete transitions.
- [ ] Implement episodic write.
- [ ] Publish existing semantic event.
- [ ] Add activity logs.
- [ ] Verify session episodic tests remain unchanged.

## Success Criteria

- [ ] Approved item appears in episodic search.
- [ ] KG extraction path is triggered through existing event bus.
- [ ] Re-approve cannot duplicate episodic/KG data.
- [ ] Reject/delete are auditable.
- [ ] Tenant mismatch tests fail closed.

## Risk Assessment

Deleting KG facts after approval may require source-level relation tracking not present today. If exact deletion is not supported, v1 must be honest: delete queued item + episodic source, and mark KG cleanup as best-effort/manual dedup.

## Security Considerations

Approval endpoint must require tenant admin for tenant-scoped channel rows. Master-scope owner is not required because rows are tenant-scoped.

## Next Steps

Expose the review queue and settings through API/UI.
