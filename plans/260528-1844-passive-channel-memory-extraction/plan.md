---
title: "Passive Channel Memory Extraction"
description: "Add disabled-by-default, reviewable passive extraction from group channel buffers into episodic Memory and Knowledge Graph."
status: complete
priority: P1
issue: 64
branch: "codex/issue-64-passive-channel-memory-extraction"
tags: [channels, memory, knowledge-graph, privacy, tdd, issue-64]
blockedBy: []
blocks: []
created: "2026-05-28T11:45:20.128Z"
createdBy: "ck:plan"
source: skill
---

# Passive Channel Memory Extraction

## Overview

Implement issue #64 as a privacy-first v1. Agents may learn durable context from group/channel conversations they already receive, but only after a channel admin enables the feature. Extraction is batched, redacted, reviewable, tenant-scoped, and writes to Memory/KG only after approval or explicit high-confidence auto-approve.

Hard boundary: no realtime per-message extraction, no default enablement, no permanent raw full-history store, and no cross-channel memory sharing.

## Current Codebase Anchors

| Area | Existing anchor |
|------|-----------------|
| Channel instances | `channel_instances.config`, `internal/store/channel_instance_store.go`, `internal/http/channel_instances.go`, `internal/gateway/methods/channel_instances.go` |
| Group buffer | `channel_pending_messages`, `internal/store/pending_message_store.go`, `internal/channels/history.go`, `internal/channels/history_compaction.go` |
| Memory/KG pipeline | `internal/consolidation/episodic_worker.go`, `internal/consolidation/semantic_worker.go`, `internal/knowledgegraph/extractor.go` |
| Activity audit | `internal/store/activity_store.go`, `internal/http/activity.go`, `emitAudit(...)` call sites |
| Runtime wiring | `cmd/gateway.go`, `cmd/gateway_http_handlers.go`, `cmd/gateway_http_wiring.go`, `cmd/gateway_channels_setup.go` |
| Web UI | `ui/web/src/pages/channels/`, `ui/web/src/pages/memory/`, `ui/web/src/types/channel.ts` |
| Schema versions | PG migration `000075`; `internal/upgrade/version.go` is 75. SQLite schema version is 44. |

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Research and Characterization Tests](./phase-01-research-and-characterization-tests.md) | Complete |
| 2 | [Schema and Store Contracts](./phase-02-schema-and-store-contracts.md) | Complete |
| 3 | [Extractor Worker and Redaction](./phase-03-extractor-worker-and-redaction.md) | Complete |
| 4 | [Memory KG Review Queue Integration](./phase-04-memory-kg-review-queue-integration.md) | Complete |
| 5 | [Channel Settings UI and Admin Controls](./phase-05-channel-settings-ui-and-admin-controls.md) | Complete |
| 6 | [Verification Documentation and Ship Readiness](./phase-06-verification-documentation-and-ship-readiness.md) | Complete |

## Dependencies

- GitHub issue: `digitopvn/goclaw#64`
- Existing group pending-message persistence must stay intact for channel context.
- Existing Memory/KG workers must keep session behavior unchanged.
- Standard edition only for channels. Lite has no channels, so SQLite migrations still must compile/apply, but UI should naturally hide channel surfaces through existing edition gates.

## Recommended Defaults

| Setting | Default | Reason |
|---------|---------|--------|
| `enabled` | `false` | Sensitive feature; explicit opt-in only |
| `review_mode` | `true` | Prevent accidental durable PII/secret storage |
| `interval_minutes` | `360` | Cheap daily-ish cadence without long wait |
| `message_cap` | `100` | Batching per issue request |
| `retention_hours` | `168` | 7-day raw buffer cap, same spirit as pending-message TTL |
| `allowed_types` | people, projects, decisions, todos, preferences, events | Durable work context only |
| `group_only` | `true` for v1 | Avoid DM privacy ambiguity |

## Success Criteria

- [x] Passive extraction is disabled by default and opt-in per channel instance.
- [x] Extraction runs only by interval, message cap, or manual trigger.
- [x] Raw channel messages are read from tenant-scoped pending buffers and are not stored permanently outside existing retention.
- [x] Redaction blocks common secrets, tokens, credentials, payment/banking data, phone/address patterns, and configurable excluded users/patterns before durable writes.
- [x] Extracted items go to a review queue by default.
- [x] Approved items write to `episodic_summaries` with `source_type='channel'` and deterministic `source_id`.
- [x] KG ingestion receives only approved/redacted summaries and records source metadata.
- [x] Review reject/delete removes queued items and prevents later writes.
- [x] Tenant/channel/agent boundaries covered by focused tests and store scoping.
- [x] PG and SQLite schemas both migrate and compile.
- [x] UI exposes settings, last run status, pending items, approve/reject/delete, and manual run.

## Key Risks

| Risk | Mitigation |
|------|------------|
| Durable sensitive-data leak | Redaction before queue, review default, no raw full-history durable copy |
| Duplicate facts | Deterministic run/item source IDs, store-level uniqueness, reuse KG dedup worker |
| Cross-tenant leak | Tenant-scoped queries and tests before implementation |
| Cost spike | Batched triggers, minimum content gate, usage caps purpose string |
| UX overbuild | Channel detail section only in v1; no separate global console |

## Open Questions

- None for v1. Assumptions: review queue required, group channels first, UI in channel detail.
