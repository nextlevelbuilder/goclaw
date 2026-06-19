---
phase: 5
title: Runtime git adapter validation
status: completed
effort: ''
---

# Phase 5: Runtime git adapter validation

## Context Links

- Adapter prepare call: `internal/tools/credentialed_exec.go:466`
- Synthetic user credential helper: `internal/tools/credentialed_exec.go:511`
- Git adapter: `internal/tools/credential_adapter_git.go`
- Git adapter tests: `internal/tools/credential_adapter_git_test.go`
- SSH adapter tests: `internal/tools/credential_adapter_git_ssh_test.go`
- Current docs mention User Credentials: `docs/git-credential-adapter.md:29`

## Overview

Wire the effective credential source into runtime execution and validate the git adapter still injects PAT/SSH credentials only for remote git operations.

Priority: P1.

Status: pending.

## Key Insights

- `credentialed_exec.go` currently synthesizes a `SecureCLIUserCredential` from `UserEnv`, `UserCredentialType`, and `UserHostScope`.
- After Phase 2, the adapter should receive a source-neutral credential object, or the helper should be renamed so non-user sources are not misrepresented.
- Git operations must be tested with same-agent, different-channel contexts.

## Requirements

- Keep `git status`, `git log`, and other local-only commands uncredentialed.
- Continue denying sandbox mode for non-passthrough adapters unless sandbox support is explicitly added later.
- Add audit source metadata so operators can tell whether `user`, `context`, or `agent` credential was used.
- Validate PAT header behavior against a GitHub-like HTTP endpoint or fixture.
- Validate SSH key injection still scrubs temp paths and key bytes.

## Architecture

Runtime should deal with a neutral credential payload:

```go
type SecureCLIEffectiveCredential struct {
  BinaryID uuid.UUID
  SubjectID string
  Source string // user, context, agent
  EncryptedEnv []byte
  CredentialType *string
  HostScope *string
}
```

If implementation keeps `SecureCLIUserCredential` as the adapter input for minimal change, add comments/tests that prove `UserID` is metadata-only and do not expose it as the credential source.

## Related Code Files

- Update `internal/tools/credential_adapter.go` if a neutral type is introduced.
- Update `internal/tools/credentialed_exec.go`.
- Update `internal/tools/credential_audit_log_test.go`.
- Update `internal/tools/shell_credentialed_gate_test.go` fake store.
- Update git adapter tests.

## Implementation Steps

1. Decide minimal runtime shape:
   - preferred: introduce neutral `SecureCLIEffectiveCredential`
   - fallback: keep `SecureCLIUserCredential` but add source metadata elsewhere
2. Update `userCredFromBinary` or replace it with `effectiveCredentialFromBinary`.
3. Ensure adapters receive credential data from user/context/agent source.
4. Add audit source to `emitSystemEnvInjectionAudit`.
5. Add tests:
   - no `userID`, agent credential present, git clone injects PAT
   - two different `CredentialUserID` values use same agent credential
   - user credential overrides agent credential
   - context credential overrides agent credential
   - agent credential does not bypass grant for non-global binary
   - PAT and SSH paths scrub secrets and temp paths
6. Validate PAT transport:
   - create a local HTTP test server that captures git extra header behavior, or unit-test the generated Git config/env args
   - reconcile docs and code on Basic vs Bearer if mismatch is found
7. Run targeted Go tests for tools/store/http.

## Todo List

- [ ] Runtime uses effective credential from agent source.
- [ ] Audit includes credential source without raw host or secret value.
- [ ] Cross-channel runtime tests pass.
- [ ] Git PAT and SSH adapter tests pass.
- [ ] Grant boundary tests pass.

## Success Criteria

- [ ] Git clone/fetch/pull/push can use an agent credential without a matching user credential.
- [ ] Same agent uses the same credential from Discord, Telegram, HTTP, and cron contexts.
- [ ] Per-user overrides remain backward compatible.
- [ ] Local git operations remain uncredentialed.
- [ ] Secrets remain scrubbed from output, logs, and errors.

## Risk Assessment

- Risk: adapter API churn touches many tests. Mitigation: start with a small neutral adapter type and update fake stores in one pass.
- Risk: PAT auth behavior is wrong for GitHub. Mitigation: add characterization test and update docs/code together.
- Risk: audit source reveals too much host info. Mitigation: keep host hashed or omit host value, matching current audit style.

## Security Considerations

- Host scope validation remains exact host or host:port, no wildcards.
- Deny patterns from binary/grant/context still apply after credential resolution.
- Temporary SSH files must be removed and scrubbed from errors.

## Next Steps

- Phase 6 updates docs and performs final plan/implementation validation.
