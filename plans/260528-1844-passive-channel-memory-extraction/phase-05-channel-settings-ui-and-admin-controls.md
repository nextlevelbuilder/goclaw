---
phase: 5
title: "Channel Settings UI and Admin Controls"
status: complete
priority: P2
effort: "2d"
dependencies: [2, 3, 4]
---

# Phase 5: Channel Settings UI and Admin Controls

## Context Links

- HTTP channel handler: `internal/http/channel_instances.go`
- WS channel methods: `internal/gateway/methods/channel_instances.go`
- Channel UI: `ui/web/src/pages/channels/channels-page.tsx`, `ui/web/src/pages/channels/channel-detail/`
- UI types: `ui/web/src/types/channel.ts`
- i18n: `ui/web/src/i18n/locales/{en,vi,zh}/channels.json`

## Overview

Expose passive memory settings and review queue in channel detail. Keep the UI operator-focused: no global dashboard in v1.

## Requirements

- Functional: configure, manually run extraction, show last run, show counts, approve/reject/delete pending items.
- Non-functional: mobile-safe controls, no raw secret/PII display beyond already-redacted item text, i18n in EN/VI/ZH.

## Architecture

HTTP endpoints under channel instance scope:

- `GET /v1/channels/instances/{id}/memory-extraction`
- `PUT /v1/channels/instances/{id}/memory-extraction/settings`
- `POST /v1/channels/instances/{id}/memory-extraction/run`
- `GET /v1/channels/instances/{id}/memory-extraction/items`
- `POST /v1/channels/instances/{id}/memory-extraction/items/{itemID}/approve`
- `POST /v1/channels/instances/{id}/memory-extraction/items/{itemID}/reject`
- `DELETE /v1/channels/instances/{id}/memory-extraction/items/{itemID}`

WS methods are optional in v1. Prefer HTTP because current web hooks already use HTTP for channel instances.

## Related Code Files

- Modify: `internal/http/channel_instances.go`
- Create: `internal/http/channel_memory_extraction_handlers.go`
- Create: `internal/http/channel_memory_extraction_handlers_test.go`
- Modify: `ui/web/src/types/channel.ts`
- Create: `ui/web/src/pages/channels/channel-detail/passive-memory-section.tsx`
- Create: `ui/web/src/pages/channels/hooks/use-channel-memory-extraction.ts`
- Modify: `ui/web/src/pages/channels/channel-detail/channel-detail-page.tsx`
- Modify: locale files in `ui/web/src/i18n/locales/en/`, `vi/`, `zh/`

## Implementation Steps

1. Add HTTP handler tests first:
   - viewer can read status/items
   - admin required for settings/run/approve/reject/delete
   - tenant mismatch returns not found/forbidden without leaking row existence
2. Add settings validation:
   - interval min/max
   - message cap min/max
   - retention min/max
   - allowed types from enum
   - exclude patterns bounded length/count
3. Add routes to `ChannelInstancesHandler.RegisterRoutes`.
4. Implement UI hook with TanStack Query and invalidation on mutation.
5. Build `PassiveMemorySection`:
   - toggle
   - interval and cap numeric inputs
   - review mode toggle
   - allowed type checkboxes
   - last run summary
   - manual run button
   - pending extracted items list
   - approve/reject/delete buttons
6. Add i18n keys to EN/VI/ZH channel namespace.
7. Ensure mobile rules:
   - inputs use `text-base md:text-sm`
   - tables/lists horizontally safe
   - buttons have sufficient hit area

## Todo List

- [ ] Add HTTP route tests.
- [ ] Implement handler validation.
- [ ] Add UI hook.
- [ ] Add channel detail section.
- [ ] Add i18n strings.
- [ ] Add basic UI tests if existing setup supports it; otherwise rely on build + manual browser verification.

## Success Criteria

- [ ] Admin can enable passive memory for a channel.
- [ ] Admin can force a run.
- [ ] Pending items appear after extraction.
- [ ] Admin can approve/reject/delete items.
- [ ] Viewer cannot mutate settings/items.
- [ ] Web build passes.

## Risk Assessment

UI can balloon. Keep it as one compact channel-detail section. Do not add global review inbox until v1 proves useful.

## Security Considerations

UI must show redaction status and avoid displaying raw source messages. If source preview is needed later, it must be separate permissioned work.

## Next Steps

Run full verification, docs, and issue handoff.
