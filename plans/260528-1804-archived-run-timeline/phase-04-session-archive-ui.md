---
phase: 4
title: "Session Archive UI"
status: complete
priority: P2
effort: "1.5d"
dependencies: [3]
---

# Phase 4: Session Archive UI

## Context Links

- Live chat event handling: `ui/web/src/pages/chat/hooks/use-chat-messages.ts`
- Tool card reference: `ui/web/src/components/chat/tool-call-card.tsx`
- Session detail page: `ui/web/src/pages/sessions/session-detail-page.tsx`
- Trace detail page: `ui/web/src/pages/traces/trace-detail-dialog.tsx`

## Overview

Add the Phase 1 archive UI under session detail as the recommended first surface. Trace pages link out for admin/debug, but the primary user journey is reviewing a session run archive.

## Key Insights

- Session detail is the right Phase 1 home because users review archived conversations there.
- Trace detail is a secondary admin/debug path, not the primary archive UX.
- Current `ToolCallCard` can leak full args/results on expand, so archive UI needs preview-only rendering.

## Requirements

- Functional: session detail shows available runs for the session and opens a Claude-like timeline.
- Functional: render assistant messages, activity markers, combined tool cards, and final status in strict order.
- Functional: tool cards collapsed by default and show preview only.
- Functional: admin/debug users see trace/span links when available.
- Non-functional: mobile-friendly, no nested cards, no raw args/results expansion in Phase 1.
- Non-functional: preserve current live chat UI behavior.

## Architecture

Recommended UI placement:
- Primary: `ui/web/src/pages/sessions/session-detail-page.tsx`
- Add a run archive drawer/panel or route-scoped section from session detail.
- Use current `ToolCallCard` patterns but create archive-safe component that never displays full raw JSON in Phase 1.
- Trace detail may include a "View archive timeline" link if `run_id` exists.

Why this recommendation:
- Users looking at old work start from sessions, not traces.
- Trace detail is developer/admin diagnostic UI.
- Public share page belongs to later phase after privacy model is proven.

## Related Code Files

- Create: `ui/web/src/types/run-timeline.ts`
- Create: `ui/web/src/pages/sessions/run-timeline-panel.tsx`
- Create: `ui/web/src/pages/sessions/run-timeline-item.tsx`
- Create: `ui/web/src/pages/sessions/hooks/use-run-timeline.ts`
- Create: `ui/web/src/pages/sessions/__tests__/run-timeline-panel.test.tsx`
- Modify: `ui/web/src/pages/sessions/session-detail-page.tsx`
- Modify: `ui/web/src/pages/traces/trace-detail-dialog.tsx`
- Modify: `ui/web/src/api/protocol.ts`
- Modify: `ui/web/src/i18n/locales/{en,vi,zh}/sessions.json`

## Implementation Steps

1. Write component tests first:
   - assistant intermediate and final messages render in order.
   - tool call/result combine into one collapsed card by `tool_call_id`.
   - preview appears; raw args/results do not.
   - trace link appears only when item has trace/span IDs and user can see debug affordance.
   - mobile layout does not depend on fixed wide columns.
2. Add API hook using HTTP fetch first; use WS RPC only where current page pattern prefers WS.
3. Add run selector/list in session detail using existing session/run data where possible.
4. Add timeline panel/drawer.
5. Add trace detail link to timeline when `run_id` and `session_key` exist.
6. Add i18n keys in all three locale files before wiring UI text.
7. Verify no current chat live event rendering regresses.

## Success Criteria

- [ ] Session detail can open an archive timeline for a run.
- [ ] Timeline order matches backend `seq`.
- [ ] Tool previews are collapsed and safe by default.
- [ ] Trace/span links are available for admin/debug users.
- [ ] UI works on mobile widths without horizontal overflow.
- [ ] Existing live chat `ActiveRunZone` still behaves unchanged.

## Todo List

- [ ] Add timeline TS types.
- [ ] Add timeline fetch hook.
- [ ] Add archive-safe timeline item component.
- [ ] Add session detail panel/drawer entry.
- [ ] Add trace detail link.
- [ ] Add i18n keys for en/vi/zh.
- [ ] Add component tests.

## Risk Assessment

Main risk: reusing live `ToolCallCard` leaks raw arguments/results. Mitigation: introduce archive-safe rendering that accepts preview fields only.

## Security Considerations

Do not render raw payload fields in Phase 1. Treat trace links as debug affordances and hide them when permission data is unavailable.

## Next Steps

Proceed to Phase 5 after UI tests and manual mobile review pass.
