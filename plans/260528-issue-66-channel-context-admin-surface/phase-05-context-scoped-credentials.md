---
phase: 5
title: "Context-scoped Credentials"
status: complete
effort: "XL"
---

# Phase 5: Context-scoped Credentials

## Context Links

- MCP credentials: `internal/http/mcp_user_credentials.go`, `internal/store/pg/mcp_user_credentials.go`, `internal/mcp/manager.go`
- Secure CLI credentials: `internal/http/secure_cli_user_credentials.go`, `internal/store/pg/secure_cli.go`, `internal/tools/secure_cli*`
- Encryption patterns: `internal/crypto/`, existing encrypted store columns
- Security docs: `docs/09-security.md`, `docs/20-api-keys-auth.md`

## Overview

Add channel/group scoped credential overrides for MCP and Secure CLI. This phase must be stricter than grants because it stores encrypted secrets and changes runtime credential resolution.

## Requirements

- Store MCP and CLI context credentials in dedicated encrypted tables.
- Never return raw secret values through list/detail endpoints.
- Support create/update/delete with audit logs.
- Apply precedence: user > group/member-role > channel > agent > global.
- Clearly label whether a capability is using global, agent, channel, group, or user credentials.

## Architecture

- Proposed tables:
  - `mcp_context_credentials`
  - `secure_cli_context_credentials`
- Scope columns match Phase 4 grant tables.
- MCP credential payload follows current per-user model: API key, headers, env.
- Secure CLI credential payload follows current typed credential model and encrypted env behavior.
- Runtime resolvers should merge defaults first, then progressively apply agent/channel/group/user overrides.
- UI uses masked fields, "configured" flags, and explicit reset/delete actions.

## Related Code Files

- `migrations/*`
- `internal/upgrade/version.go`
- `internal/store/sqlitestore/schema.sql`
- `internal/store/sqlitestore/schema.go`
- `internal/store/mcp_store.go`
- `internal/store/secure_cli_store.go`
- `internal/mcp/manager.go`
- `internal/http/mcp_user_credentials.go`
- `internal/http/secure_cli_user_credentials.go`
- `ui/web/src/pages/channels/channel-detail/`

## Implementation Steps

1. TDD: add credential masking tests for HTTP list/detail responses.
2. TDD: add resolver precedence tests for global, agent, channel, group, and user credentials.
3. TDD: add audit tests or log assertions for credential create/update/delete where existing test patterns allow.
4. Add PG and SQLite encrypted credential tables and version bumps.
5. Add store interfaces and implementations using existing encryption helpers.
6. Add admin endpoints for scoped MCP credentials and scoped Secure CLI credentials.
7. Update MCP manager credential resolution with context scope input.
8. Update Secure CLI lookup/credential resolution with context scope input.
9. Add UI credential forms with masked values, configured flags, and delete/reset flows.
10. Update docs to document precedence and masking behavior.

## Todo List

- [ ] Add failing tests proving no raw secrets are serialized.
- [ ] Add failing runtime resolver precedence tests.
- [ ] Add PG and SQLite schema changes together.
- [ ] Add audited credential write endpoints.
- [ ] Add UI forms with masked values only.
- [ ] Update security/API docs.

## Success Criteria

- [ ] Admin can configure channel/group scoped MCP credentials.
- [ ] Admin can configure channel/group scoped Secure CLI credentials.
- [ ] Runtime uses scoped credentials according to documented precedence.
- [ ] UI labels credential source and override state.
- [ ] All list/detail endpoints return masked metadata only.

## Risk Assessment

- Risk: raw secret leakage in UI/API/logs. Mitigation: masking tests and no reveal endpoint in this phase.
- Risk: agent/global config accidentally overwritten by channel edits. Mitigation: dedicated tables and explicit scope keys.
- Risk: resolver behavior differs between MCP and CLI. Mitigation: shared precedence test cases.

## Security Considerations

- Encrypt all stored secret payloads using existing crypto patterns.
- Audit create/update/delete, but never audit payload values.
- Rate-limit or omit reveal flows; default is no raw reveal.

## Next Steps

- Phase 6 verifies Discord-specific behavior and closes docs gaps.

## Unresolved Questions

- Whether to support one-time reveal for newly-created secrets is intentionally out of scope unless explicitly approved.
