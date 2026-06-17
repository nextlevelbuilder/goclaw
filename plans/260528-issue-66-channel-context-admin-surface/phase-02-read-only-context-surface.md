---
phase: 2
title: "Read-only Context Surface"
status: complete
effort: "L"
---

# Phase 2: Read-only Context Surface

## Context Links

- UI shell: `ui/web/src/pages/channels/channel-detail/channel-detail-page.tsx`
- Current hooks: `ui/web/src/pages/channels/hooks/use-channel-detail.ts`
- Channel contacts: `internal/store/channel_contact_store.go`, `internal/store/pg/channel_contacts.go`
- Live members abstraction: `internal/channels/channel.go`, `internal/channels/manager.go`
- Feishu member implementation: `internal/channels/feishu/feishu.go`
- Discord metadata source: `internal/channels/discord/handler.go`

## Overview

Add a read-only admin view for channel contexts and members/users. This should make the channel detail page useful before introducing new grant or credential mutations.

## Requirements

- Show channel contexts/groups discovered from contacts and supported provider APIs.
- Show members/users for a selected context when available.
- Show source and freshness: stored contact, live provider, config writer scope, or unsupported.
- Preserve existing Telegram override editing until it is intentionally replaced or renamed.
- Enforce tenant isolation and admin/operator read permissions.

## Architecture

- Add a channel context service that aggregates stored contacts plus provider-supported group/member APIs.
- Add HTTP endpoints under existing channel instance routes:
  - `GET /v1/channels/instances/{id}/contexts`
  - `GET /v1/channels/instances/{id}/contexts/{scopeType}/{scopeKey}/members`
- Extend channel contact store filters to support channel instance and context filters.
- UI adds a general `Contexts` tab for all channel types and keeps Telegram overrides as a separate advanced section if still needed.

## Related Code Files

- `internal/http/channel_instances.go`
- `internal/store/channel_contact_store.go`
- `internal/store/pg/channel_contacts.go`
- `internal/store/sqlitestore/*channel*`
- `internal/channels/manager.go`
- `ui/web/src/pages/channels/channel-detail/channel-detail-page.tsx`
- `ui/web/src/pages/channels/hooks/use-channel-detail.ts`
- `ui/web/src/types/channel.ts`

## Implementation Steps

1. TDD: add backend tests for context listing, member listing, tenant mismatch, and unsupported provider responses.
2. Add channel-instance filtering to contact store interfaces and PG/SQLite implementations.
3. Implement read-only context/member HTTP handlers using existing auth middleware and masked DTOs.
4. Add UI query hooks and type definitions for contexts and members.
5. Add a general channel detail tab with loading, empty, error, and unsupported states.
6. Add i18n keys to all web locale files before rendering new user-facing strings.
7. Keep Telegram override editing intact and avoid changing channel config behavior in this phase.

## Todo List

- [ ] Backend tests fail first for `/contexts` and `/members`.
- [ ] Store filters support `channel_instance` without full scans.
- [ ] UI tests cover tab visibility, empty state, and unsupported provider state.
- [ ] Existing channel detail behavior remains unchanged for general/credentials/managers tabs.

## Success Criteria

- [ ] Admin can see groups/contexts and members/users where data exists.
- [ ] Unsupported live member sync is explicit in the UI instead of silently blank.
- [ ] Tenant isolation tests pass for both PG and SQLite paths where applicable.
- [ ] No mutation endpoints are added in this phase.

## Risk Assessment

- Risk: `channel_contacts` has incomplete rows for some providers. Mitigation: label source/freshness and use provider capability flags.
- Risk: adding filters breaks contact search. Mitigation: preserve existing default list/search behavior with regression tests.

## Security Considerations

- Read endpoints must scope by authenticated tenant and selected channel instance.
- Do not include provider tokens or channel credentials in responses.

## Next Steps

- Phase 3 can consume the selected context model to display effective MCP/CLI capability rows.

## Unresolved Questions

- None.
