---
phase: 2
title: "Config Resolution and Storage Contract"
status: completed
priority: P1
effort: "0.75d"
dependencies: [1]
---

# Phase 2: Config Resolution and Storage Contract

## Overview

Define the durable config contract for Workspace, Agent, and Channel behavior.
Keep existing channel and workspace keys backward-compatible while adding a
typed agent override envelope that can live in `agents.other_config`.

## Requirements

- Functional: resolve Intermediate Replies and Quick Ack independently by
  `Channel > Agent > Workspace`.
- Functional: support provider/model settings for generated Quick Ack and
  generated Intermediate Replies.
- Functional: keep existing `gateway.block_reply`, `gateway.chat_behavior`,
  channel `block_reply`, and channel `chat_behavior` readable.
- Non-functional: avoid DB migration unless tests prove `other_config` is not
  enough.

## Architecture

Add a small delivery behavior resolver instead of widening unrelated channel
code. Do not hang Intermediate Replies under `quick_ack`: current
`ShouldDeliverGeneratedProgress()` is quick-ack-gated, so the new contract must
make the two features independent. Recommended agent `other_config` contract:

```json
{
  "delivery_behavior": {
    "intermediate_replies": {
      "enabled": true,
      "mode": "sidecar_generated",
      "provider": "groq",
      "model": "llama-3.1-8b-instant",
      "timeout_ms": 2500,
      "max_tokens": 60,
      "max_chars": 180
    },
    "quick_ack": {
      "enabled": true,
      "mode": "sidecar_generated",
      "provider": "groq",
      "model": "llama-3.1-8b-instant",
      "min_delay_ms": 1000,
      "timeout_ms": 2500,
      "max_tokens": 40,
      "max_chars": 120,
      "templates": ["Got it. Working on it..."]
    }
  }
}
```

Backward compatibility mapping:
- `gateway.block_reply` and channel `block_reply` become the inherited
  `intermediate_replies.enabled` default.
- `gateway.chat_behavior.quick_ack` and channel `chat_behavior.quick_ack`
  become the inherited `quick_ack` default.
- `gateway.chat_behavior.final_split` remains final-answer splitting and should
  not be bundled into the new Agent override unless implementation tests require
  it.

Agent DB rows should parse this from `AgentData.OtherConfig`. Config-file agents
may use `AgentSpec.DeliveryBehavior` only if the config loader path needs
first-class support.

## Related Code Files

- Modify: `internal/config/config_channels.go`
- Modify: `internal/config/config.go`
- Modify: `internal/channels/chat_behavior.go`
- Modify: `internal/channels/runs.go`
- Modify: `internal/store/agent_store.go`
- Modify: `cmd/gateway_consumer_normal.go`
- Modify: `docs/05-channels-messaging.md`

## Implementation Steps

1. Add `DeliveryModeSidecarGenerated` for generated delivery, while keeping
   legacy quick-ack modes `llm_generated`, `fixed_template`, and `off` readable.
2. Add `IntermediateRepliesConfig` and `QuickAckConfig` provider/model/timeout
   fields or a shared nested generator object; choose the smaller shape during
   implementation.
3. Add `DeliveryBehaviorConfig` or equivalent resolver input for
   `IntermediateReplies + QuickAck`.
4. Add `ParseDeliveryBehaviorConfig()` on `AgentData` for
   `other_config.delivery_behavior`.
5. Add resolver function that accepts workspace, agent, and channel configs and
   applies them in that exact order.
6. Update channel manager APIs so callers can pass the already-resolved result
   instead of letting channel manager know only workspace/channel.
7. Remove/generated-progress coupling to `quick_ack.enabled`; tests must prove
   the two feature toggles are independent.
8. Document backward compatibility and the new agent override JSON shape.

## Success Criteria

- [x] Unit tests prove Channel > Agent > Workspace for both `block_reply` and
      `quick_ack`.
- [x] Unit tests prove Intermediate Replies can be on while Quick Ack is off.
- [x] Missing agent config has no behavior change.
- [x] Existing channel `chat_behavior` configs still parse and resolve.
- [x] Invalid/unknown Quick Ack mode normalizes to a safe default.
- [x] No migration is added unless an implementation blocker is documented.

## Risk Assessment

Risk: adding a generic config type can sprawl into final split, reasoning, or
other channel settings. Keep this plan scoped to Intermediate Replies and Quick
Ack; final split can keep existing behavior unless a regression test forces it.
