# Redteam Report: Agent-scoped Git Credentials

Date: 2026-05-31

Scope: plan review for issue #117 before implementation.

## Findings

### R1 - Credential rows could accidentally bypass grants

Risk: If runtime joins `secure_cli_agent_credentials` without preserving the existing non-global grant gate, creating a credential row becomes an implicit grant.

Fix in plan: Phase 2 and Phase 5 require tests that non-global binaries still need `secure_cli_agent_grants`. Credential rows store secrets only.

Status: fixed in plan.

### R2 - Agent grants are the wrong place for typed secrets

Risk: `secure_cli_agent_grants.encrypted_env` already exists and is tempting to reuse, but it is policy override state and lacks `credential_type` and `host_scope`.

Fix in plan: Phase 2 uses dedicated `secure_cli_agent_credentials`.

Status: fixed in plan.

### R3 - User credential precedence could preserve the confusing default

Risk: Keeping user credentials as the visible default would not solve cross-channel identity confusion.

Fix in plan: Phase 4 makes Agent Credentials the primary git path and moves User Credentials to advanced overrides.

Status: fixed in plan.

### R4 - API support could lag behind UI

Risk: A UI-only feature would block automation and contradict the user requirement.

Fix in plan: Phase 3 defines full CRUD endpoints, request bodies, response masking, audit events, docs, and tests.

Status: fixed in plan.

### R5 - Typed validation might fork and drift

Risk: Copying PAT/SSH validation into a new handler can create inconsistent behavior between user and agent credentials.

Fix in plan: Phase 3 requires reusing `prepareTypedCredentialEnv` or a shared equivalent.

Status: fixed in plan.

### R6 - SQLite migration can be missed

Risk: Desktop edition uses SQLite and can break if only PostgreSQL migrations are added.

Fix in plan: Phase 2 requires PG migration, SQLite fresh schema, SQLite incremental migration, and version bump.

Status: fixed in plan.

### R7 - Runtime audit source can become misleading

Risk: Existing audit uses credential user ID. Agent credentials would make that label inaccurate.

Fix in plan: Phase 5 requires source-neutral credential metadata and audit source coverage.

Status: fixed in plan.

### R8 - PAT transport behavior needs proof

Risk: Docs and adapter assumptions around GitHub HTTPS auth can silently diverge.

Fix in plan: Phase 5 requires a GitHub-like PAT transport characterization test and code/docs reconciliation.

Status: fixed in plan.

## Unresolved Questions

None.
