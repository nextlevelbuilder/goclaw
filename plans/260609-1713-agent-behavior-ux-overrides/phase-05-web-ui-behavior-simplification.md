---
phase: 5
title: "Web UI Behavior Simplification"
status: completed
priority: P2
effort: "1d"
dependencies: [2, 3, 4]
---

# Phase 5: Web UI Behavior Simplification

## Overview

Update Web UI so users see one coherent Behavior model: Workspace defaults,
Agent overrides, and Channel overrides. Remove Tool Status Messages from
Config > Behavior.

## Requirements

- Functional: Config > Behavior no longer shows Tool Status Messages.
- Functional: Config > Behavior still controls Workspace Intermediate Replies
  and Quick Ack defaults.
- Functional: Agent advanced settings expose inherit/custom override for
  Intermediate Replies and Quick Ack.
- Functional: Channel settings expose the same override fields and provider/model
  options for Quick Ack and Intermediate Replies.
- Non-functional: all user-facing strings have en/vi/zh translations.

## Architecture

Use existing UI patterns:
- Workspace behavior card: `ui/web/src/pages/config/sections/behavior-*.tsx`.
- Agent advanced dialog: `ui/web/src/pages/agents/agent-detail/`.
- Channel config schema: `ui/web/src/pages/channels/channel-schemas.ts`.
- Existing channel schema already has `chat_behavior.quick_ack.*` override fields
  but lacks provider/model controls and still describes generated mode as
  "main LLM block reply".
- Channel create/edit forms flatten nested keys, delete `"inherit"`, and coerce
  string booleans in `channel-instance-form-dialog.tsx`; new nested delivery
  fields need serializer tests, not schema-only tests.

## Related Code Files

- Modify: `ui/web/src/pages/config/sections/behavior-ux-card.tsx`
- Modify: `ui/web/src/pages/config/sections/behavior-section.tsx`
- Modify: `ui/web/src/pages/config/sections/behavior-chat-card.tsx`
- Modify: `ui/web/src/components/layout/system-settings-modal.tsx`
- Modify: `ui/web/src/pages/agents/agent-detail/agent-advanced-dialog.tsx`
- Modify: `ui/web/src/pages/agents/agent-detail/agent-advanced-state-utils.ts`
- Modify: `ui/web/src/types/agent.ts`
- Modify: `ui/web/src/pages/channels/channel-schemas.ts`
- Modify: `ui/web/src/pages/channels/channel-instance-form-dialog.tsx` tests or
  extracted serializer helpers if implementation extracts them.
- Modify: `ui/web/src/i18n/locales/{en,vi,zh}/config.json`
- Modify: `ui/web/src/i18n/locales/{en,vi,zh}/agents.json`
- Modify: `ui/web/src/i18n/locales/{en,vi,zh}/channels.json`
- Inspect: `ui/desktop/frontend/src/components/channels/channel-schemas.ts`

## Implementation Steps

1. Remove `tool_status` from `BehaviorUxCard` UI state and save payload.
2. Remove or hide `gateway.tool_status` from any other user-facing config
   surface, including `system-settings-modal.tsx`.
3. Keep backend accepting `gateway.tool_status` silently for backward
   compatibility; do not expose it in Web UI.
4. Add Quick Ack and Intermediate Reply provider/model fields to Workspace
   behavior card with provider selectors where available.
5. Add Agent "Delivery Behavior" section with Inherit/Custom mode and controls
   for Intermediate Replies plus Quick Ack.
6. Persist Agent overrides into `other_config.delivery_behavior` without
   overwriting unrelated keys.
7. Extend channel schema fields with provider/model/timeout/max token settings
   for Quick Ack and Intermediate Replies.
8. Update preview copy so generated delivery is described as sidecar-generated,
   not "main LLM block reply".
9. Add or update UI tests for schema normalization, flatten/unflatten payloads,
   select coercion, and `other_config` merge behavior.
10. Scout desktop channel schema. Either add matching fields or document why
   desktop is out of scope for Standard channel settings.

## Success Criteria

- [x] Tool Status Messages label no longer appears in Config > Behavior.
- [x] `gateway.tool_status` is not exposed in any user-facing Web UI settings
      surface.
- [x] Agent UI can save inherit/custom behavior override without overwriting
      unrelated `other_config` keys.
- [x] Channel UI can save provider/model Quick Ack and Intermediate Reply
      overrides with correct nested JSON.
- [x] Workspace UI can save provider/model Quick Ack and Intermediate Reply
      defaults.
- [x] en/vi/zh locale keys are complete.
- [x] Mobile layout keeps inputs at `text-base md:text-sm` where applicable.

## Risk Assessment

Risk: agent advanced payloads can overwrite unrelated `other_config`. Reuse the
existing merge pattern used by inbound debounce and prompt settings; test it.
