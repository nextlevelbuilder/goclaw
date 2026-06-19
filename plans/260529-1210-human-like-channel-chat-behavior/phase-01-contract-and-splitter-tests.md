---
phase: 1
title: "Contract and Splitter Tests"
status: pending
priority: P1
effort: "0.5d"
dependencies: []
---

# Phase 1: Contract and Splitter Tests

## Overview

Define the behavior contract before runtime changes. Add tests for safe final-message splitting and config structures, then implement only the minimal splitter helpers needed to make those tests pass.

## Requirements

- Functional: split final content only when enabled, over minimum length, and safe to split.
- Functional: return one message when splitting would damage formatting.
- Non-functional: deterministic output, no platform-specific network behavior, no sleeps in unit tests.

## Architecture

Create a small channel-level behavior file under `internal/channels/` so channel delivery can reuse it without importing agent or gateway internals.

Proposed types:
- `ChatBehaviorConfig`
- `QuickAckConfig`
- `FinalSplitConfig`
- `ResolvedChatBehavior`
- `SplitFinalMessage(content string, cfg FinalSplitConfig) []string`

The splitter may reuse `ChunkMarkdown` for max-length mechanics, but #67 splitting is semantic: max N human-like messages, not platform hard-limit chunks.

## Related Code Files

- Modify: `internal/channels/chunking.go` or create adjacent `internal/channels/chat_behavior.go`
- Modify/Create tests: `internal/channels/chat_behavior_test.go`
- Modify: `internal/config/config_channels.go`

## Implementation Steps

1. Add tests for disabled config, short content, max message cap, min chars.
2. Add tests for safe structures: fenced code blocks, markdown tables, bullet/numbered lists, block quotes, JSON/YAML/XML-looking blocks, URLs.
3. Implement conservative splitter: split on double-newline paragraphs only when all resulting parts are safe and within max count.
4. Add fallback: return original content unchanged on any unsafe structure or cap breach.
5. Add config structs in `internal/config` with JSON tags but no runtime wiring yet.
6. Run focused tests.

## Success Criteria

- [ ] Splitter tests prove safe split and no-split edge cases.
- [ ] Config structs compile in both PG and sqliteonly builds.
- [ ] No existing channel hard-limit chunking behavior changes.

## Risk Assessment

Risk: existing `ChunkMarkdown` already force-splits code for hard platform limits, while #67 wants semantic split. Mitigation: keep semantic splitter separate and use hard chunking only after semantic split if needed by adapters.
