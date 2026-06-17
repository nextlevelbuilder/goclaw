---
phase: 1
title: "Research and Contracts"
status: complete
effort: "M"
---

# Phase 1: Research and Contracts

## Context Links

- Issue: https://github.com/digitopvn/goclaw/issues/66
- Current page: `ui/web/src/pages/channels/channel-detail/channel-detail-page.tsx`
- Current groups tab: `ui/web/src/pages/channels/channel-detail/channel-groups-tab.tsx`
- Current credentials tab: `ui/web/src/pages/channels/channel-detail/channel-credentials-tab.tsx`
- Channel routes: `internal/http/channel_instances.go`
- MCP store/routes: `internal/store/mcp_store.go`, `internal/http/mcp_grants.go`, `internal/http/mcp_user_credentials.go`
- Secure CLI store/routes: `internal/store/secure_cli_store.go`, `internal/http/secure_cli_agent_grants.go`, `internal/http/secure_cli_user_credentials.go`
- Docs: `docs/05-channels-messaging.md`, `docs/23-ai-agent-permission-matrix.md`, `docs/09-security.md`

## Overview

Lock the API contracts and threat model before implementation. This phase should produce failing tests or contract snapshots for the expected DTOs, plus a concise scope note that separates channel login credentials from MCP/CLI scoped credentials.

## Key Insights

- Existing `groups` tab is Telegram config overrides, not a general group/member view.
- Existing channel credentials are encrypted channel-login data; they must not become MCP/CLI runtime credentials.
- MCP access currently resolves server defaults + per-user credentials only.
- Secure CLI access currently resolves global binary + agent grant + user credential only.
- `agent_config_permissions` is for config/file-writer grants and should not be stretched into MCP/CLI grant storage.
- Discord currently captures guild/channel metadata but does not expose full live members/roles without extra intent work.

## Requirements

- Define canonical context scope model for `channel`, `group`, and `member` rows.
- Define masked credential DTOs that never include raw secret values.
- Define source labels for effective capability and credential rows: global, agent, channel, group, user.
- Define permission requirements for read vs write endpoints.
- Document resolver precedence: user > group/member-role > channel > agent > global.

## Architecture

- Add HTTP DTOs near existing channel/admin route files; keep store structs separate from UI response structs.
- Keep MCP and Secure CLI resolver changes behind explicit scope inputs instead of reading mutable process-global state.
- Represent channel context with stable fields: `channelInstanceID`, `channelType`, `scopeType`, `scopeKey`, `displayName`, `source`, `lastSeenAt`.
- Treat Discord role/member support as a provider capability flag until verified.

## Related Code Files

- Modify later: `internal/http/channel_instances.go`
- Modify later: `internal/store/channel_contact_store.go`
- Modify later: `internal/store/mcp_store.go`
- Modify later: `internal/store/secure_cli_store.go`
- Modify later: `ui/web/src/types/channel.ts`
- Modify later: `ui/web/src/pages/channels/channel-detail/*`
- Create later only if needed: focused DTO/test files with kebab-case or existing Go naming convention.

## Implementation Steps

1. Write failing backend tests for channel context DTO masking and tenant-scoped route authorization.
2. Write failing resolver contract tests that encode credential precedence without creating new tables yet.
3. Add a short issue-scope note in this plan directory if implementation discovers a contract gap.
4. Verify all planned endpoint names against existing route structure before coding handlers.
5. Verify current UI route/i18n conventions before adding visible strings.

## Todo List

- [ ] Add backend contract tests for context list shape and permission checks.
- [ ] Add resolver precedence characterization tests for MCP and Secure CLI.
- [ ] Confirm UI i18n key placement for new channel detail tabs.
- [ ] Confirm Discord provider capability and permissions needed for live member/role sync.

## Success Criteria

- [ ] Tests fail for the intended missing behavior before implementation.
- [ ] Contract fields and precedence rules are explicit and cited in later phase code.
- [ ] No plan step relies on raw credential reveal.
- [ ] No plan step reuses `channel_instances.credentials` for MCP/CLI secrets.

## Risk Assessment

- Risk: resolver scope leaks across tenants. Mitigation: tenant ID on every new table/query and route-level tenant checks.
- Risk: UI implies Discord support that backend cannot provide. Mitigation: capability flags and documented fallback.
- Risk: scope strings become ad hoc. Mitigation: typed scope model plus tests.

## Security Considerations

- Never serialize raw secrets.
- Audit all write operations in later phases.
- Use existing encrypted storage patterns and parameterized SQL only.

## Next Steps

- Proceed to Phase 2 after route/DTO tests are committed or queued in the implementation branch.

## Unresolved Questions

- None for planning; Discord depth remains a phase gate.
