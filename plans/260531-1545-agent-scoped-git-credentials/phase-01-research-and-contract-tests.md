---
phase: 1
title: Research and contract tests
status: completed
effort: ''
---

# Phase 1: Research and contract tests

## Context Links

- Issue: https://github.com/digitopvn/goclaw/issues/117
- Current user ID resolver: `internal/agent/user_identity_resolver.go:40`
- Tool execution context injection: `internal/agent/loop_pipeline_tool_callbacks.go:47`
- Secure CLI store contract: `internal/store/secure_cli_store.go:120`
- Current per-user HTTP API: `internal/http/secure_cli_user_credentials.go:13`
- Current git UI entry point: `ui/web/src/pages/cli-credentials/cli-credentials-table.tsx:80`

## Overview

Write characterization and contract tests before schema or UI changes. The goal is to pin the current failure mode: git typed credentials depend on a user credential row, but cross-channel usage often cannot map to the same credential user ID.

Priority: P1.

Status: pending.

## Key Insights

- Agent identity is stable for the runtime path; external user identity is not stable across channels.
- User credentials should stay supported, but they should not be the primary git credential setup path.
- Existing context credentials already show that credential resolution is not purely per-user; agent credentials should join that explicit precedence chain.

## Requirements

- Preserve existing per-user credential behavior.
- Add tests that fail under the current implementation when no matching `userID` exists but an agent credential exists.
- Make API and UI requirements explicit before implementation.

## Architecture

Effective credential source should become a small explicit enum in tests and later code:

1. `user` for explicit per-user override.
2. `context` for group/member/channel scoped credential.
3. `agent` for the new agent-scoped credential.
4. `binary` for legacy/global binary env.
5. `none` when no typed credential is available.

## Related Code Files

- Modify tests under `internal/store/pg/`, `internal/store/sqlitestore/`, `internal/tools/`, and `internal/http/`.
- Add or extend UI tests under `ui/web/src/pages/cli-credentials/__tests__/`.
- No production code changes in this phase except test fixtures if needed.

## Implementation Steps

1. Add store contract tests proving the intended precedence: user > context > agent > binary.
2. Add a runtime test where the same agent executes `git clone` from two different credential user IDs and resolves the same agent credential.
3. Add an HTTP route contract test for planned agent credential endpoints:
   - `GET /v1/cli-credentials/{id}/agent-credentials`
   - `GET /v1/cli-credentials/{id}/agent-credentials/{agentId}`
   - `PUT /v1/cli-credentials/{id}/agent-credentials/{agentId}`
   - `DELETE /v1/cli-credentials/{id}/agent-credentials/{agentId}`
4. Add negative API tests:
   - invalid `binaryID`
   - invalid `agentID`
   - missing `host_scope` for `pat` and `ssh_key`
   - unsupported `credential_type`
   - response never includes raw token/key/blob
5. Add Web UI tests that the git credential action defaults to Agent Credentials, with User Credentials shown as advanced/personal override.
6. Document which tests fail before implementation.

## Todo List

- [ ] Store precedence tests written.
- [ ] Runtime cross-channel agent credential test written.
- [ ] HTTP endpoint contract tests written.
- [ ] UI default-flow test written.
- [ ] Initial failing test set documented in the phase notes.

## Success Criteria

- [ ] Tests prove the current user-id keyed design cannot satisfy the target behavior.
- [ ] Tests define the exact endpoint contract and response masking.
- [ ] No implementation-only code is added before the contract tests exist.

## Risk Assessment

- Risk: tests could encode a wrong precedence order. Mitigation: keep user override highest for compatibility, but make agent credential the default UI path.
- Risk: agent credential could accidentally grant binary execution access. Mitigation: test that credential rows do not bypass non-global agent grants.

## Security Considerations

- Contract tests must assert no plaintext credential values appear in list/detail responses, audit labels, logs, or errors.
- Tests must assert tenant isolation for every new endpoint.

## Next Steps

- Phase 2 adds schema and store implementation until Phase 1 tests pass.
