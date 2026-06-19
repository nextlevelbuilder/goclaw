---
phase: 3
title: "Effective Capability Matrix"
status: complete
effort: "L"
---

# Phase 3: Effective Capability Matrix

## Context Links

- MCP grants API: `internal/http/mcp_grants.go`
- MCP store: `internal/store/mcp_store.go`, `internal/store/pg/mcp_servers.go`, `internal/store/pg/mcp_servers_access.go`
- Secure CLI grants API: `internal/http/secure_cli_agent_grants.go`
- Secure CLI store: `internal/store/secure_cli_store.go`, `internal/store/pg/secure_cli.go`
- Channel detail UI: `ui/web/src/pages/channels/channel-detail/`

## Overview

Show the effective MCP servers/tools and Secure CLI packages for a channel context before allowing edits. This phase is read-only and should expose why access exists, not just whether access exists.

## Requirements

- Display MCP servers with tool allow/deny summaries and source labels.
- Display Secure CLI packages/binaries with source labels and masked env availability.
- Show future channel/group override slots as "not configured" until Phase 4/5 tables exist.
- Keep read endpoint tenant-scoped and usable by admins/operators with appropriate channel access.

## Architecture

- Add an effective capability aggregation service that composes existing MCP and Secure CLI store calls.
- Add endpoint under channel instance routes:
  - `GET /v1/channels/instances/{id}/contexts/{scopeType}/{scopeKey}/capabilities`
- Response groups rows by capability type: `mcp_server`, `mcp_tool`, `secure_cli`.
- Avoid resolver mutation in this phase; this is visibility over existing agent/user/global grants plus planned context placeholder metadata.

## Related Code Files

- `internal/http/channel_instances.go`
- `internal/http/mcp_grants.go`
- `internal/http/secure_cli_agent_grants.go`
- `internal/store/mcp_store.go`
- `internal/store/secure_cli_store.go`
- `ui/web/src/pages/channels/channel-detail/`
- `ui/web/src/types/channel.ts`

## Implementation Steps

1. TDD: add backend tests for effective capability response shape using existing agent/user grants.
2. TDD: add UI tests for capability matrix source labels and masked secret indicators.
3. Implement a read-only aggregator that calls existing MCP and Secure CLI stores.
4. Add handler under channel instance route group with tenant/channel authorization.
5. Add UI matrix with filters for MCP, CLI, and source.
6. Add i18n keys in all web locale files.
7. Document that edit controls remain disabled until Phase 4/5.

## Todo List

- [ ] Backend tests prove source labels for global, agent, and user-derived access.
- [ ] UI tests cover empty, partial, and mixed MCP/CLI rows.
- [ ] No new schema is introduced in this phase.
- [ ] No raw credential values appear in capability responses.

## Success Criteria

- [ ] Admin can see granted MCP servers/tools for a selected channel context.
- [ ] Admin can see granted Secure CLI packages/binaries for a selected channel context.
- [ ] Source labels are visible and consistent with resolver precedence.
- [ ] Read-only endpoint has tenant isolation tests.

## Risk Assessment

- Risk: read model diverges from actual runtime resolver. Mitigation: use existing store access methods where possible and add characterization tests before later resolver edits.
- Risk: UI becomes noisy. Mitigation: default grouped display and simple filters.

## Security Considerations

- Mask all environment and credential indicators.
- Return only metadata required to administer access.

## Next Steps

- Phase 4 can add mutation tables and update the matrix source labels to include channel/group grants.

## Unresolved Questions

- None.
