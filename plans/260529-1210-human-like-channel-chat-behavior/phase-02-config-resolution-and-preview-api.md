---
phase: 2
title: "Config Resolution and Preview API"
status: pending
priority: P1
effort: "0.5d"
dependencies: [1]
---

# Phase 2: Config Resolution and Preview API

## Overview

Wire global gateway config plus per-channel override resolution, then expose a no-side-effect preview API for ack and final split behavior.

## Requirements

- Functional: global config defaults apply when channel override omitted.
- Functional: per-channel override can enable/disable ack and split independently.
- Functional: preview API returns resolved config, ack decision, and split parts without dispatching.
- Non-functional: master-scope/owner gate follows existing config mutation pattern.

## Architecture

Use config structs with pointer fields for inheritance. Add resolver in `internal/channels` that takes gateway config, channel name/type config, and run traits.

Preview uses a new read-only WS method:
- method name: `chat_behavior.preview`
- params: channel name/type, `isStreaming`, `isGroup`, `content`, optional `hasToolCalls`, optional `estimatedLongWork`
- response: `resolved`, `ack`, `split`

The method is master-scoped and owner-gated because it exposes resolved config details. It never dispatches outbound messages and never mutates config.

## Related Code Files

- Modify: `internal/config/config_channels.go`
- Modify: `internal/config/config_system.go`
- Modify/Create: `internal/channels/chat_behavior.go`
- Modify/Create: `internal/gateway/methods/chat_behavior.go`
- Modify: `pkg/protocol/methods.go` or method constants file
- Tests: `internal/channels/chat_behavior_test.go`, `internal/gateway/methods/*chat_behavior*_test.go`

## Implementation Steps

1. Add resolver tests: nil global, disabled global, enabled global, channel override true/false, partial override inheritance.
2. Add preview method tests: no dispatch, validated payload, deterministic split output.
3. Register `chat_behavior.preview` as a read-only owner/master-scope method.
4. Add i18n errors only if user-facing backend errors are introduced.
5. Run focused method/config tests.

## Success Criteria

- [ ] Resolver is fully table-tested.
- [ ] Preview API returns safe output and does not touch message bus.
- [ ] Config patch compatibility remains unchanged.

## Risk Assessment

Risk: config shape becomes hard to evolve. Mitigation: nest under `chat_behavior`, use pointer fields for overrides, and avoid per-agent storage in MVP.
