---
phase: 2
title: Schema and store resolver
status: completed
effort: ''
---

# Phase 2: Schema and store resolver

## Context Links

- User credential struct: `internal/store/secure_cli_store.go:79`
- Agent grant struct: `internal/store/secure_cli_store.go:97`
- PostgreSQL lookup path: `internal/store/pg/secure_cli.go:337`
- Context credential overlay: `internal/store/pg/secure_cli.go:505`
- SQLite schema version map: `internal/store/sqlitestore/schema.go`
- PostgreSQL latest migration at planning time: `migrations/000076_channel_memory_extraction.up.sql`

## Overview

Create durable agent-scoped typed credential storage and make `LookupByBinary` return an effective credential source without depending on channel-specific user IDs.

Priority: P1.

Status: pending.

## Key Insights

- `secure_cli_agent_grants.encrypted_env` is a policy override payload, not a typed credential identity. It lacks `credential_type` and `host_scope`.
- A dedicated table keeps grant authorization separate from credential material.
- SQLite must be updated in both fresh schema and incremental migrations.

## Requirements

- Add `secure_cli_agent_credentials` for both PostgreSQL and SQLite.
- Encrypt secret blob with the same AES-256-GCM pattern as existing SecureCLI credentials.
- Preserve per-user and context credentials.
- Return source metadata for audit and UI.
- Do not let an agent credential row grant access to a non-global CLI binary by itself.

## Architecture

New table shape:

```sql
secure_cli_agent_credentials (
  id uuid/text primary key,
  tenant_id uuid/text not null,
  binary_id uuid/text not null,
  agent_id uuid/text not null,
  encrypted_env bytea/blob not null,
  metadata jsonb/text not null default '{}',
  credential_type text null,
  host_scope text null,
  created_by text not null default '',
  created_at timestamptz/text not null,
  updated_at timestamptz/text not null,
  unique (tenant_id, binary_id, agent_id)
)
```

Effective precedence:

1. Per-user credential when `userID` maps to a row.
2. Context scoped credential from the channel scope chain.
3. Agent credential for `(tenant_id, binary_id, agent_id)`.
4. Binary/global env.

Authorization rule:

- If `secure_cli_binaries.is_global = false`, `secure_cli_agent_grants` must still allow the agent before runtime uses the binary or its credential.
- If `is_global = true`, an agent credential can specialize the otherwise global binary for that agent.

## Related Code Files

- Add migration: `migrations/000077_secure_cli_agent_credentials.up.sql` and `.down.sql`, if `000077` is still the next number.
- Update `internal/upgrade/version.go`.
- Update `internal/store/secure_cli_store.go`.
- Add `internal/store/pg/secure_cli_agent_credentials.go`.
- Add `internal/store/sqlitestore/secure-cli-agent-credentials.go`.
- Update `internal/store/pg/secure_cli.go`.
- Update `internal/store/sqlitestore/secure-cli.go`.
- Update `internal/store/sqlitestore/schema.sql` and `internal/store/sqlitestore/schema.go`.

## Implementation Steps

1. Re-run `find migrations -name '*.up.sql' | sort | tail` and choose the next migration number.
2. Add PG migration with foreign keys to `secure_cli_binaries`, `agents`, and tenant scope. Add indexes on `(tenant_id, binary_id)`, `(tenant_id, agent_id)`, and unique `(tenant_id, binary_id, agent_id)`.
3. Add SQLite fresh schema and incremental migration. Bump `SchemaVersion`.
4. Add `SecureCLIAgentCredential` struct and store methods:
   - `GetAgentCredentials(ctx, binaryID, agentID)`
   - `SetAgentCredentialsTyped(ctx, binaryID, agentID, encryptedEnv, credentialType, hostScope)`
   - `SetAgentCredentials(ctx, binaryID, agentID, encryptedEnv)` for legacy env
   - `DeleteAgentCredentials(ctx, binaryID, agentID)`
   - `ListAgentCredentials(ctx, binaryID)`
5. Extend lookup result with effective credential source fields. Prefer a neutral name such as `CredentialEnv`, `CredentialType`, `CredentialHostScope`, `CredentialSource`, and `CredentialSubjectID` instead of reusing `User*` fields for non-user sources.
6. Update PG and SQLite lookup:
   - join user credential only when `userID` exists
   - join agent credential when `agentID` exists
   - apply context credentials before agent credential if context credential exists
   - preserve grant authorization check before returning a non-global binary
7. Update fake stores in tests to implement the new interface.
8. Run targeted store tests for PG and SQLite.

## Todo List

- [ ] PG migration added and down migration removes table/indexes.
- [ ] SQLite schema and version migration added.
- [ ] Store interface and concrete PG/SQLite methods added.
- [ ] Effective source metadata added without breaking JSON responses.
- [ ] Phase 1 store tests pass.

## Success Criteria

- [ ] `LookupByBinary` can resolve typed git credentials for an agent even when `userID == ""`.
- [ ] Per-user credential still wins when present.
- [ ] Context credential still wins over agent credential.
- [ ] Non-global binaries still require enabled grants.
- [ ] PG and SQLite tests cover fresh and migrated schemas.

## Risk Assessment

- Risk: reusing `UserEnv` for agent credentials hides source semantics. Mitigation: introduce source-neutral fields and keep `User*` only for backward compatibility during refactor.
- Risk: migration number collision. Mitigation: verify immediately before implementation.
- Risk: SQLite desktop startup breaks. Mitigation: update both schema.sql and incremental migration map.

## Security Considerations

- Secret values remain encrypted at rest.
- Store methods must scope by tenant in every query.
- Delete binary or agent should cascade or fail predictably; use foreign keys consistent with existing store behavior.

## Next Steps

- Phase 3 exposes API CRUD over the new store methods.
