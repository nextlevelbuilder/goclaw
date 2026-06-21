---
phase: 3
title: "Extractor Worker and Redaction"
status: complete
priority: P1
effort: "2d"
dependencies: [1, 2]
---

# Phase 3: Extractor Worker and Redaction

## Context Links

- Existing compaction: `internal/channels/history_compaction.go`
- Existing KG extractor: `internal/knowledgegraph/extractor.go`
- Background provider resolver: `internal/providerresolve`
- Usage caps: `internal/usage/caps`
- Runtime wiring: `cmd/gateway.go`

## Overview

Build the passive extraction worker. It scans enabled channel configs, selects eligible message batches, redacts sensitive content, calls LLM extraction/summarization once per batch, and stores review items.

## Requirements

- Functional: interval/message-cap/manual trigger, group-only v1, minimum useful content gate, redaction before durable writes, retry-safe run status.
- Non-functional: bounded worker concurrency, cost controlled by batching, no panic on provider failure, graceful shutdown.

## Architecture

Add `internal/consolidation/channel_memory_worker.go` or a focused `internal/channelmemory/` package if the file would exceed 200 LOC.

Data flow:

1. scan `ChannelInstanceStore` for enabled channels with `config.passive_memory.enabled=true`
2. inspect `PendingMessageStore.ListGroups`
3. choose groups where interval elapsed or message cap reached
4. load `PendingMessageStore.ListByKey`
5. redact
6. create/claim extraction run
7. call extractor prompt for durable facts only
8. persist `pending_review` items
9. log activity + `slog.Info/Warn`

## Related Code Files

- Create: `internal/consolidation/channel_memory_config.go`
- Create: `internal/consolidation/channel_memory_redactor.go`
- Create: `internal/consolidation/channel_memory_worker.go`
- Create: `internal/consolidation/channel_memory_prompt.go`
- Modify: `internal/consolidation/workers.go` or gateway wiring if worker stays separate from existing worker bundle
- Modify: `cmd/gateway.go`
- Modify: `cmd/gateway_managed.go` if managed gateway has separate wiring

## Implementation Steps

1. Implement `PassiveMemoryConfig` parser with defaults:
   - disabled
   - review mode true
   - interval 360 minutes
   - cap 100 messages
   - retention 168 hours
   - allowed types default set
2. Implement redactor with tests for:
   - API keys/tokens/passwords
   - env-like secrets
   - connection strings
   - payment/banking numbers
   - phone/address-like PII
   - configured exclude user IDs and regex patterns
3. Implement candidate selector with tests:
   - skip disabled
   - skip DM/private keys by v1 policy
   - skip too few useful messages
   - trigger on interval or message cap
4. Implement LLM prompt and parser for durable facts:
   - output JSON array of items
   - allowed types only
   - confidence score
   - concise memory text
   - optional entity/relation hints
5. Use `usageCaps.Chat` with purpose `channel-memory-extraction`.
6. Store run/item status transitions.
7. Emit audit activity for run started/completed/failed.
8. Wire worker start/stop in gateway with conservative ticker and concurrency.

## Todo List

- [ ] Config parser tests pass.
- [ ] Redactor tests pass.
- [ ] Candidate selector tests pass.
- [ ] Worker run lifecycle tests pass.
- [ ] Provider failure leaves run `failed` and does not block gateway.
- [ ] Usage cap purpose covered.
- [ ] Gateway wiring starts worker only when channels + stores are available.

## Success Criteria

- [ ] Worker creates review items from eligible group messages.
- [ ] No raw unredacted message text stored in new tables.
- [ ] Duplicate scans do not create duplicate runs/items.
- [ ] Provider failures are non-fatal and auditable.

## Risk Assessment

The redactor can false-positive and reduce extraction quality. Accept for v1. Better to drop sensitive content than store it.

## Security Considerations

Prompt must explicitly reject secrets and low-confidence guesses. Logs must include counts and IDs, not raw message body.

## Next Steps

Approved review items can write to Memory/KG after queue semantics are stable.
