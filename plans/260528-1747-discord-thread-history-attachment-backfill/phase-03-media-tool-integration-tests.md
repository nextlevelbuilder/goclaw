---
phase: 3
title: "Media Tool Integration Tests"
status: complete
priority: P1
effort: "3h"
dependencies: [2]
---

# Phase 3: Media Tool Integration Tests

## Overview

Prove the backfilled attachments are not just shown as text tags; they must flow into persisted media refs so `read_image` and `read_document` can access them in the agent run.

## Requirements

- Functional: backfilled image attachments become image refs available to `read_image`.
- Functional: backfilled document attachments become document refs available to `read_document`.
- Non-functional: reuse existing media persistence and tool context logic, do not create a Discord-specific media store.

## Architecture

The Discord handler publishes `bus.MediaFile`. Agent loop persists those via `persistMedia`, adds `MediaRefs` to the current user message, and then tools resolve refs from context/history. Tests should validate at the lowest reliable layer without live LLM calls.

## Related Code Files

- Modify: `internal/channels/discord/thread-history-backfill_test.go`
- Modify or add targeted agent test: `internal/agent/loop_input_media_test.go` or existing media tests
- Read: `internal/agent/loop_input_media.go`
- Read: `internal/agent/media_tool_routing.go`
- Read: `internal/tools/read_image.go`
- Read: `internal/tools/read_document.go`

## Implementation Steps

1. Add a focused test that feeds backfilled `bus.MediaFile` image/doc into `enrichInputMedia` and asserts resulting `MediaRefs` contain image/document kinds.
2. For image path mode, assert `loadHistoricalImagesForTool` can include persisted image refs in tool context.
3. For document path mode, assert `tools.MediaDocRefsFromCtx(ctx)` contains backfilled document refs.
4. Ensure content tags include media IDs/paths where existing enrichment functions already provide them.
5. Avoid actual provider calls; do not invoke LLM/vision/document providers.

## Success Criteria

- [x] Tests prove `read_image` has image context or path access for prior thread image.
- [x] Tests prove `read_document` can resolve prior thread document ref.
- [x] No fake string-only acceptance; actual files are persisted/resolved.

## Risk Assessment

Risk: tool integration test may overreach into private loop internals. Mitigation: prefer existing agent media tests and helper-level assertions over full LLM runs.
