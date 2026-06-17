---
phase: 4
title: Dashboard Controls
status: completed
priority: P2
effort: ''
dependencies:
  - 2
  - 3
---

# Phase 4: Dashboard Controls

## Overview

Update the Behavior UI so the default is generated progress, and fallback templates are presented as fallback, not the main acknowledgement content.

## Requirements

- Functional: global Behavior card can select quick ack mode.
- Functional: per-channel override schema can override quick ack mode where current quick ack enabled override exists.
- Functional: UI preview explains generated-first vs fallback/fixed behavior.
- Non-functional: all new user-facing strings use existing i18n namespace and en/vi/zh locale files.
- Non-functional: keep component files under the repo's 200-line guidance or split a focused child component.

## Architecture

Current UI:
- `normalizeChatBehavior` defaults templates to `"Got it. Working on it..."` in `ui/web/src/pages/config/sections/behavior-section.tsx:128`.
- `BehaviorChatCard` labels templates as quick ack templates in `ui/web/src/pages/config/sections/behavior-chat-card.tsx:115`.
- Per-channel schema only overrides `chat_behavior.quick_ack.enabled` in `ui/web/src/pages/channels/channel-schemas.ts:31`.

Target UI:
- Add a mode control with values generated-first, fixed-template, off.
- Rename template textarea copy to fallback templates.
- Keep delay field as fallback/fixed delay.
- Preserve mobile input font size rule: `text-base md:text-sm` on inputs/selects if custom components require class names.

## Related Code Files

- Modify: `ui/web/src/pages/config/sections/behavior-section.tsx`
- Modify: `ui/web/src/pages/config/sections/behavior-chat-card.tsx`
- Modify: `ui/web/src/pages/channels/channel-schemas.ts`
- Modify: `ui/web/src/i18n/locales/en/config.json`
- Modify: `ui/web/src/i18n/locales/vi/config.json`
- Modify: `ui/web/src/i18n/locales/zh/config.json`
- Optional create: a focused child component under `ui/web/src/pages/config/sections/` if `behavior-chat-card.tsx` would exceed 200 lines
- Modify: `docs/05-channels-messaging.md`

## Implementation Steps

1. Add mode to `ChatBehaviorValues`.
2. Normalize default mode to `llm_generated`.
3. Add mode control and copy that explains no extra LLM call.
4. Rename template labels/hints to fallback template language.
5. Update preview rendering for generated-first/fallback/fixed states.
6. Add per-channel schema override for `chat_behavior.quick_ack.mode`.
7. Add i18n keys to all three config locale files.
8. Update channel messaging docs with new semantics and explicit no-storage note.

## Success Criteria

- [ ] Dashboard no longer presents fixed template as default immediate response.
- [ ] Config payload includes `quick_ack.mode` when changed.
- [ ] Per-channel override can inherit/generated/fixed/off.
- [ ] All new UI strings are localized.
- [ ] Component size guidance is respected or consciously documented.

## Risk Assessment

Risk: adding controls could bloat the existing card. Mitigation: split a small quick-ack settings component if the file crosses 200 lines.
