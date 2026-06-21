---
title: "Human-Like Channel Chat Behavior MVP"
description: "TDD plan for digitopvn/goclaw#67: quick acknowledgement and safe final multi-message splitting for non-streaming channel delivery, with global gateway plus per-channel config only."
status: completed
priority: P2
branch: "codex/issue-67-human-like-chat-behavior"
tags: [issue-67, channels, chat-behavior, tdd, web-ui]
blockedBy: []
blocks: []
created: "2026-05-29T05:10:15.334Z"
createdBy: "ck:plan"
source: skill
---

# Human-Like Channel Chat Behavior MVP

## Overview

Implement the approved MVP for issue #67.

Scope:
- runtime config for human-like channel chat behavior
- quick acknowledgement before longer/tool work
- safe final multi-message splitting for channel delivery
- dashboard controls and preview API
- global gateway defaults plus per-channel overrides only

Explicitly out of scope:
- issue #76 archive/timeline storage or renderer
- per-agent overrides
- Web UI acknowledgement delivery
- streaming channel acknowledgement delivery
- public share/export surfaces

Recommended architecture: resolve behavior config into `RunContext` at run registration, emit ack from channel event handling for non-streaming runs, split final assistant content in the channel outbound path after final content sanitization.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Contract and Splitter Tests](./phase-01-contract-and-splitter-tests.md) | Completed |
| 2 | [Config Resolution and Preview API](./phase-02-config-resolution-and-preview-api.md) | Completed |
| 3 | [Runtime Acknowledgement and Final Splitting](./phase-03-runtime-acknowledgement-and-final-splitting.md) | Completed |
| 4 | [Dashboard Controls and Channel Overrides](./phase-04-dashboard-controls-and-channel-overrides.md) | Completed |
| 5 | [Validation and Handoff](./phase-05-validation-and-handoff.md) | Completed |

## Dependencies

- GitHub issue: `digitopvn/goclaw#67`
- Related non-overlap issue: `digitopvn/goclaw#76`
- Brainstorm report: `../reports/260529-1210-issue-67-human-like-chat-behavior-brainstorm.md`
- Existing event/run surfaces: `internal/channels/runs.go`, `internal/channels/events.go`, `internal/agent/loop_run.go`
- Existing block reply path: `internal/pipeline/think_stage.go`, `internal/agent/loop_pipeline_adapter.go`
- Existing channel chunking: `internal/channels/chunking.go`
- Existing config UI: `ui/web/src/pages/config/sections/behavior-section.tsx`, `ui/web/src/pages/channels/channel-schemas.ts`

## Acceptance Criteria

- [x] Ack sends only for non-streaming channel runs when enabled and gate passes.
- [x] Ack does not send for Web UI, streaming channel runs, disabled config, silent replies, or quick-complete runs.
- [x] Final split sends at most configured max messages, with configured delay between extra messages.
- [x] Splitter preserves fenced code, quotes, tables, lists, structured JSON/YAML/XML, links, and short messages.
- [x] Global gateway and per-channel config override resolution is deterministic and tested.
- [x] Preview API returns ack decision and split parts without sending messages.
- [x] Dashboard exposes global controls and per-channel overrides with i18n in en/vi/zh.
- [x] No schema/timeline persistence added; no issue #76 file overlap except references.

## Completion Evidence

- GitHub issue `digitopvn/goclaw#67` is closed.
- Beta implementation shipped via `digitopvn/goclaw#99`.
- Project changelog records the human-like channel delivery MVP.
- Follow-up channel behavior fixes passed GitHub CI in `digitopvn/goclaw#135` and `digitopvn/goclaw#139`.

## Validation Commands

```bash
go test ./internal/channels ./internal/config ./internal/gateway/methods
go test -tags sqliteonly ./internal/channels ./internal/config ./internal/gateway/methods
go build ./...
go build -tags sqliteonly ./...
go vet ./...
cd ui/web && pnpm test -- --run
cd ui/web && pnpm build
git diff --check
```
