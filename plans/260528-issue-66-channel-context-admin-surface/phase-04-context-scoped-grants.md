---
phase: 4
title: "Context-scoped Grants"
status: complete
effort: "XL"
---

# Phase 4: Context-scoped Grants

## Context Links

- MCP grant schema: `migrations/000001_init_schema.up.sql`, later MCP migrations, `internal/store/pg/mcp_servers_access.go`
- Secure CLI grant schema: `migrations/000058_secure_cli_binaries.up.sql`, `migrations/000073_secure_cli_typed_credentials.up.sql`
- SQLite schema: `internal/store/sqlitestore/schema.sql`, `internal/store/sqlitestore/schema.go`
- Upgrade version: `internal/upgrade/version.go`
- Grant APIs: `internal/http/mcp_grants.go`, `internal/http/secure_cli_agent_grants.go`

## Overview

Add channel/group scoped grants for MCP and Secure CLI. This is the first write phase and requires dual database migrations, resolver changes, audit logs, and UI mutation controls.

## Requirements

- Add dedicated context grant tables; do not overload `agent_config_permissions`.
- Scope every row by tenant, channel instance, scope type, and scope key.
- Support grant sources: channel and group/member-role.
- Preserve existing agent/user grant behavior.
- Audit create/update/delete operations.

## Architecture

- Proposed tables:
  - `mcp_context_grants`
  - `secure_cli_context_grants`
- Common columns: `id`, `tenant_id`, `channel_instance_id`, `scope_type`, `scope_key`, resource ID, allow/deny or config overrides, `granted_by`, timestamps.
- Add PG migrations and SQLite full schema + incremental migrations in the same implementation PR.
- Extend resolver input with channel context. Prefer explicit params where call sites already carry channel metadata; use context values only when explicit threading would create broad churn.
- Runtime grant precedence: user > group/member-role > channel > agent > global.

## Related Code Files

- `migrations/*`
- `internal/upgrade/version.go`
- `internal/store/sqlitestore/schema.sql`
- `internal/store/sqlitestore/schema.go`
- `internal/store/mcp_store.go`
- `internal/store/secure_cli_store.go`
- `internal/store/pg/mcp_servers_access.go`
- `internal/store/pg/secure_cli.go`
- `internal/http/mcp_grants.go`
- `internal/http/secure_cli_agent_grants.go`
- `ui/web/src/pages/channels/channel-detail/`

## Implementation Steps

1. TDD: add migration tests or store tests that fail until context grant tables exist in PG and SQLite.
2. TDD: add resolver tests proving group grant overrides channel grant, and user grant overrides both.
3. Add PG up/down migrations and bump `RequiredSchemaVersion`.
4. Add SQLite schema and incremental migration map updates with `SchemaVersion` bump.
5. Add store interfaces and PG/SQLite implementations for context grants.
6. Extend MCP and Secure CLI resolvers to include context grants without breaking existing call sites.
7. Add admin-only HTTP endpoints for create/update/delete/list context grants.
8. Add UI controls to grant/revoke MCP/CLI rows for selected channel context.
9. Emit security/audit logs on every write.
10. Run dual DB compile and focused store/resolver tests.

## Todo List

- [ ] Add failing PG and SQLite tests for context grant persistence.
- [ ] Add failing resolver precedence tests.
- [ ] Add dual DB migrations and version bumps.
- [ ] Add audited admin endpoints.
- [ ] Add UI mutation controls with disabled/loading/error states.

## Success Criteria

- [ ] Channel/group scoped grants can be created, listed, updated, and revoked.
- [ ] Effective capability matrix shows channel/group grant sources.
- [ ] Existing agent/user MCP and CLI flows keep passing.
- [ ] PG and SQLite schema versions are both updated.
- [ ] Audit logs include actor, tenant, channel instance, scope, resource, and action.

## Risk Assessment

- Risk: resolver signature churn touches many runtime paths. Mitigation: enumerate all callers before editing and add compile-time failures as guide.
- Risk: partial migration coverage breaks desktop. Mitigation: SQLite schema update is mandatory in the same phase.
- Risk: broad grants accidentally cross tenant. Mitigation: tenant in primary queries and route authorization tests.

## Security Considerations

- Admin writes must use tenant admin or master-scope checks according to table ownership.
- SQL must be parameterized and indexed on tenant/channel/scope/resource columns.
- Never log secret material from overrides.

## Next Steps

- Phase 5 adds scoped credential storage after grant resolver behavior is stable.

## Unresolved Questions

- Decide exact DB constraint names during implementation after checking existing migration naming style.
