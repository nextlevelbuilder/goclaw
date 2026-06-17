---
phase: 2
title: Config Contract
status: completed
priority: P1
effort: ''
dependencies:
  - 1
---

# Phase 2: Config Contract

## Overview

Add an explicit quick acknowledgement mode contract so fixed templates stop being the default behavior while old configurations remain representable.

## Requirements

- Functional: default `chat_behavior.quick_ack` mode is generated-first with template fallback.
- Functional: explicit fixed-template mode preserves old behavior.
- Functional: existing `templates` field remains the fallback template list.
- Functional: config preview returns enough metadata for UI to explain generated vs fallback decisions.
- Non-functional: additive JSON config only; no migration.

## Architecture

Proposed additive shape:

```json
{
  "gateway": {
    "chat_behavior": {
      "enabled": true,
      "quick_ack": {
        "enabled": true,
        "mode": "llm_generated",
        "min_delay_ms": 1000,
        "templates": ["Got it. Working on it..."]
      }
    }
  }
}
```

Mode semantics:
- `llm_generated`: generated `block.reply` is preferred; `templates` are fallback if no generated progress arrives before `min_delay_ms`.
- `fixed_template`: old behavior; send template after `min_delay_ms` when eligible.
- `off`: disables chat-behavior quick acknowledgement only; explicit `gateway.block_reply=true` must still preserve existing block reply delivery.

Implementation detail: use string constants in `internal/channels/chat_behavior.go`; keep config structs string-based to avoid custom JSON code.

## Related Code Files

- Modify: `internal/config/config_channels.go`
- Modify: `internal/channels/chat_behavior.go`
- Modify: `internal/gateway/methods/chat_behavior.go`
- Modify: `internal/channels/chat_behavior_test.go`
- Read: `cmd/gateway_system_config_sync.go`

## Implementation Steps

1. Add `Mode *string json:"mode,omitempty"` to `QuickAckConfig`.
2. Add `Mode string` to `ResolvedQuickAckConfig`.
3. Define accepted modes near existing quick ack defaults.
4. Update `ResolveChatBehavior` to default quick ack mode to `llm_generated` while retaining fallback templates.
5. Treat nil/empty mode as `llm_generated`, even when legacy configs have `templates`; this is the requested default change. Treat unknown strings as `llm_generated` and test the fallback.
6. Update `ShouldSendQuickAck` or split it into clearer helpers if needed:
   - fallback timer eligibility
   - fixed template delivery eligibility
   - generated progress delivery eligibility
7. Update preview response so UI can show whether acknowledgement is generated-first, fixed-template, or off.
8. Keep `templates` cleaning behavior but document it as fallback templates.

## Success Criteria

- [ ] Existing JSON config remains backward compatible.
- [ ] Default resolver no longer treats fixed template as the primary quick ack.
- [ ] Unknown/empty templates still resolve to the fallback default string.
- [ ] Preview can distinguish generated-first from fixed-template behavior.

## Risk Assessment

Risk: changing default mode breaks users expecting old fixed-template quick ack. Mitigation: expose explicit `fixed_template` mode and keep `templates` field semantics intact.
