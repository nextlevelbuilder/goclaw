---
phase: 4
title: "Dashboard Controls and Channel Overrides"
status: pending
priority: P2
effort: "1d"
dependencies: [2, 3]
---

# Phase 4: Dashboard Controls and Channel Overrides

## Overview

Expose global gateway controls in the Behavior config tab and per-channel overrides in existing channel forms.

## Requirements

- Functional: admins can enable/disable behavior, ack, and final splitting globally.
- Functional: admins can set max split messages, min chars, delay, ack threshold, and templates.
- Functional: each channel can inherit, enable, or disable the behavior fields.
- Non-functional: use existing React config patterns and i18n in en/vi/zh.

## Architecture

Extend existing config UI instead of adding a new page.

Global:
- `ui/web/src/pages/config/sections/behavior-section.tsx`
- new focused sub-card if file growth risks >200 lines.

Per-channel:
- `ui/web/src/pages/channels/channel-schemas.ts`
- add `chat_behavior.*` controls or grouped advanced fields if the form helper supports nested paths.

Preview:
- add a small dashboard preview panel that calls `chat_behavior.preview` and displays ack decision plus split message count/content.

## Related Code Files

- Modify: `ui/web/src/pages/config/sections/behavior-section.tsx`
- Modify/Create: `ui/web/src/pages/config/sections/behavior-chat-card.tsx`
- Modify/Create: `ui/web/src/pages/config/sections/behavior-chat-preview.tsx`
- Modify: `ui/web/src/pages/channels/channel-schemas.ts`
- Modify: `ui/web/src/i18n/locales/en/config.json`
- Modify: `ui/web/src/i18n/locales/vi/config.json`
- Modify: `ui/web/src/i18n/locales/zh/config.json`
- Tests if existing UI test pattern exists for config sections.

## Implementation Steps

1. Add/extend frontend types for config values.
2. Add global behavior card with existing switch/input components.
3. Add dashboard preview panel wired to `chat_behavior.preview`.
4. Add per-channel override schema entries under Advanced behavior.
5. Add i18n keys for all labels/help text.
6. Keep components under 200 LOC or split into focused modules.
7. Run `pnpm test -- --run` and `pnpm build`.

## Success Criteria

- [ ] Global config patch writes the `gateway.chat_behavior` shape.
- [ ] Dashboard preview calls `chat_behavior.preview` and displays ack/split results without sending messages.
- [ ] Channel forms can write per-channel `chat_behavior` override.
- [ ] No hardcoded new user-facing English strings in JSX.
- [ ] Mobile controls use existing accessible input/select patterns.

## Risk Assessment

Risk: nested channel schema support may be limited. Mitigation: if nested paths are not supported, add a focused custom advanced panel rather than flattening backend config names.
