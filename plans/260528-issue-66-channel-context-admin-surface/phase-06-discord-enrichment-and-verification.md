---
phase: 6
title: "Discord Enrichment and Verification"
status: complete
effort: "M"
---

# Phase 6: Discord Enrichment and Verification

## Context Links

- Discord channel: `internal/channels/discord/discord.go`, `internal/channels/discord/handler.go`
- Channel docs: `docs/05-channels-messaging.md`
- Permission matrix docs: `docs/23-ai-agent-permission-matrix.md`
- Web channel detail UI: `ui/web/src/pages/channels/channel-detail/`

## Overview

Validate Discord-specific group/member behavior after the generic channel context surface works. Ship either real Discord enrichment or a documented follow-up, depending on verified Discord intents and permission requirements.

## Requirements

- Support Discord guild/channel concepts in the channel detail page.
- If live member/role listing is available, implement it behind capability checks.
- If required intents are not available, show stored contacts and document the limitation.
- Run final backend, SQLite, and web validation.

## Architecture

- Reuse the Phase 2 context/member endpoints.
- Add Discord provider capability flags rather than hardcoding UI assumptions.
- Use stored contact metadata for baseline Discord user rows.
- Only add live role/member sync after verifying gateway intents, bot permissions, and API rate limits.

## Related Code Files

- `internal/channels/discord/discord.go`
- `internal/channels/discord/handler.go`
- `internal/channels/channel.go`
- `internal/channels/manager.go`
- `ui/web/src/pages/channels/channel-detail/`
- `docs/05-channels-messaging.md`
- `docs/23-ai-agent-permission-matrix.md`

## Implementation Steps

1. TDD: add Discord capability tests for stored-contact fallback and unsupported live members.
2. Verify required Discord intents and permissions against the current channel setup.
3. If safe, implement Discord `GroupMemberProvider` support for guild/channel/role views.
4. If not safe, keep fallback UI and add a follow-up issue/note with exact missing permissions.
5. Add docs for channel scoped grant and credential precedence.
6. Run focused tests:
   - `go test ./internal/http ./internal/channels ./internal/store/pg`
   - `go test -tags sqliteonly ./internal/store/sqlitestore`
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
   - relevant `pnpm` checks in `ui/web/`
7. Perform manual UI smoke for channel detail tabs.

## Todo List

- [ ] Add Discord fallback tests before behavior change.
- [ ] Verify intents/permissions before live sync.
- [ ] Implement or document Discord live member/role support.
- [ ] Update docs for final shipped behavior.
- [ ] Run backend, SQLite, and web validation.

## Success Criteria

- [ ] Discord channel details show guild/channel/user context without cross-tenant leakage.
- [ ] Live Discord member/role support is either implemented with verified permissions or documented as a precise follow-up.
- [ ] Final docs describe precedence: user > group/member-role > channel > agent > global.
- [ ] Final verification commands pass or failures are documented with root cause.

## Risk Assessment

- Risk: Discord member intent requires privileged configuration. Mitigation: capability gate and documented follow-up.
- Risk: Discord API rate limits from live member fetch. Mitigation: cache/stored contacts first; live fetch only where safe.

## Security Considerations

- Do not expose Discord tokens or raw provider credentials.
- Keep all context/member rows tenant and channel-instance scoped.

## Next Steps

- Open implementation PR after all phases pass and issue acceptance criteria are checked.

## Unresolved Questions

- Discord live role/member support depends on verified bot intents and permissions.
