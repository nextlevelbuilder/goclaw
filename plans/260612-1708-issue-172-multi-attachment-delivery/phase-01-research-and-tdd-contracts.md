---
phase: 1
title: Research and TDD Contracts
status: completed
priority: P1
effort: 1h
dependencies: []
---

# Phase 1: Research and TDD Contracts

## Overview

Lock current behavior and expected batch semantics before implementation. Tests should fail until the new batch API and Telegram grouping helpers exist.

## Requirements

- Functional: agents can request multiple existing files in one call.
- Functional: returned `Result.Media` preserves file order, filename, MIME type, and per-file captions when provided.
- Functional: duplicate delivery guard rejects already queued files in the same run.
- Non-functional: no file content reads in the tool path; only `stat` and path metadata.
- Non-functional: keep single-file `send_file` behavior backward compatible.

## Architecture

`send_file` stays the existing tool name and grows an optional `attachments` array. The array entries mirror the current single-file fields: `path` and optional `caption`. If `attachments` is present, it is the source of truth; otherwise current `path` behavior remains.

## Related Code Files

- Modify: `internal/tools/send_file.go`
- Modify: `internal/tools/send_file_test.go`
- Modify: `internal/channels/capabilities.go`
- Create: `internal/channels/telegram/send_media_group_test.go` if helper tests need a focused file.

## Implementation Steps

1. Add failing tests for `send_file` with `attachments: [{path, caption}, ...]`.
2. Add failing tests for duplicate handling: one duplicate in a batch rejects before marking new paths.
3. Add failing tests proving old `path` + `caption` still returns one media entry.
4. Add Telegram helper tests for chunk sizing and type grouping without calling the network.
5. Add Discord regression test coverage around multi-file `sendMediaMessage` if existing test hooks allow it without live API.

## Success Criteria

- [x] Tests describe batch tool contract and current single-file compatibility.
- [x] Telegram grouping constraints are encoded in pure helper tests.
- [x] No live channel credentials required.

## Risk Assessment

Main risk is over-scoping platform capability metadata. Keep tests focused on behavior needed by issue #172.
