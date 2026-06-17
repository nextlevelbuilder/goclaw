---
phase: 2
title: Tool API Batch Attachments
status: completed
priority: P1
effort: 2h
dependencies:
  - 1
---

# Phase 2: Tool API Batch Attachments

## Overview

Implement the backward-compatible tool API extension in `send_file`.

## Requirements

- Functional: `send_file({attachments:[...]})` queues multiple media files in one result.
- Functional: path resolution, allow/deny guards, regular-file checks, MIME detection, and duplicate detection match single-file behavior.
- Functional: optional captions are copied onto result media for downstream channel senders.
- Non-functional: batch validation is all-or-nothing; do not mark any path delivered until every entry validates.

## Architecture

Refactor `SendFileTool.Execute` into small helpers:
- Parse single vs batch arguments.
- Resolve and validate each requested attachment.
- Check duplicate state against `DeliveredMedia` and within the batch.
- Build `[]bus.MediaFile`, then mark delivered after success.

No new tool name unless tests show the model schema becomes unclear; existing `send_file` is the least disruptive contract.

## Related Code Files

- Modify: `internal/tools/send_file.go`
- Modify: `internal/tools/send_file_test.go`
- Modify: `internal/bus/types.go`
- Modify: `internal/agent/loop_types.go`
- Modify: `internal/agent/loop_tools.go`
- Modify: `cmd/gateway_consumer.go`

## Implementation Steps

1. Extend `SendFileTool.Parameters()` with optional `attachments` array.
2. Add caption support to `bus.MediaFile`, `agent.MediaResult`, and `appendMediaToOutbound` so tool-level captions reach channel senders.
3. Implement parsing helpers with clear validation errors for missing path, non-array entries, duplicate paths, and denied paths.
4. Keep existing single-file code path behavior and messages stable.
5. Run `go test ./internal/tools -run 'TestSendFile|TestMessage|TestWriteFile|TestDeliveredMedia'`.

## Success Criteria

- [x] Batch call returns N media entries in request order.
- [x] Duplicate paths in same batch or already delivered paths fail cleanly.
- [x] Single-path `send_file` tests still pass.

## Risk Assessment

Changing media structs has a broad compile surface. Keep new fields additive with `omitempty` JSON tags and update only conversion points that need captions.
