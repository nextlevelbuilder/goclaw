---
title: "Archived Interleaved Run Timeline"
description: "TDD plan for issue #76: persist safe run timeline previews and render a Claude-like session archive, without implementing issue #67 chat delivery heuristics."
status: complete
priority: P2
branch: "codex/issue-76-run-timeline-plan"
tags: [timeline, archive, traces, web-ui, tdd, issue-76]
blockedBy: []
blocks: []
created: "2026-05-28T11:04:29.547Z"
createdBy: "ck:plan"
source: skill
---

# Archived Interleaved Run Timeline

## Overview

Implement `digitopvn/goclaw#76`: persist assistant intermediate messages, tool calls, tool results, activity markers, and final run status as an ordered archive timeline. Phase 1 exposes both HTTP and WS RPC fetch APIs, stores safe previews only, and links to traces/spans for admin debug.

Recommended Phase 1 UI: add the archive inside session detail first, with a per-run timeline panel/drawer and trace links for admin/debug. This is closest to user intent ("what happened in this run?") and avoids building a public share surface before the archive contract is proven.

Hard boundary with related issue `#67`: this plan does not add quick acknowledgement, multi-message final splitting, channel spam controls, or delivery heuristics. It archives existing structured events; #67 can later reuse the same timeline model for live/channel behavior.

## Current Code Context

- Runtime event enrichment: `internal/agent/loop_run.go`
- Tool events: `internal/agent/loop_pipeline_tool_callbacks.go`
- Intermediate assistant content: `internal/pipeline/think_stage.go`, `internal/pipeline/observe_stage.go`
- Existing trace product data: `internal/store/tracing_store.go`, `internal/http/traces.go`
- Existing session messages: `sessions.messages` JSONB in `migrations/000001_init_schema.up.sql`
- Existing live UI event handling: `ui/web/src/pages/chat/hooks/use-chat-messages.ts`
- Existing tool card UI: `ui/web/src/components/chat/tool-call-card.tsx`

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [TDD Contract and Schema](./phase-01-tdd-contract-and-schema.md) | Complete |
| 2 | [Timeline Persistence](./phase-02-timeline-persistence.md) | Complete |
| 3 | [HTTP and WS Timeline APIs](./phase-03-http-and-ws-timeline-apis.md) | Complete |
| 4 | [Session Archive UI](./phase-04-session-archive-ui.md) | Complete |
| 5 | [Validation and Issue Handoff](./phase-05-validation-and-issue-handoff.md) | Complete |

## Dependencies

- GitHub issue: `digitopvn/goclaw#76`
- Related non-overlap issue: `digitopvn/goclaw#67`
- No blocking plan found. Existing pending issue #80 plan targets skills export UI and does not overlap this feature.

## Data Contract Target

Timeline item kinds:
- `activity`
- `assistant.message`
- `tool.call`
- `tool.result`
- `run.status`

Storage rule:
- Store previews only for tool args/results in Phase 1.
- Store assistant `block.reply` and final answer content because they are user-visible.
- Store `thinking` as redacted marker only; do not persist raw reasoning.
- Link to trace/span IDs when available for admin debug.

## Validation Commands

Run focused tests first, then full compile gates:

```bash
go test ./internal/store/pg ./internal/store/sqlitestore ./internal/http ./internal/gateway/methods ./internal/agent ./internal/pipeline
go test -tags sqliteonly ./internal/store/sqlitestore ./internal/gateway/methods
go build ./...
go build -tags sqliteonly ./...
go vet ./...
cd ui/web && pnpm test -- --run
cd ui/web && pnpm build
git diff --check
```
