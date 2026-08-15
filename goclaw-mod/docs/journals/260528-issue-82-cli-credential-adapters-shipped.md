# CLI Credential Adapters — git PAT + SSH, Framework Complete

**Date**: 2026-05-28 17:29
**Severity**: Medium
**Component**: CLI credential injection, git adapter, audit logging
**Status**: Resolved

## What Happened

Issue #82 (CLI Credential Adapters) completed across 6 commits spanning phases 1–6. The core problem: legacy CLI credential flow asked users to paste arbitrary env vars (e.g., `GIT_TOKEN`), but `git` does not read auth from any stable env var, so every clone failed silently. The solution: generic `CredentialAdapter` interface with typed `git` adapter supporting PAT (via GIT_CONFIG_COUNT env, never argv) and SSH (0600 tmpfile + GIT_SSH_COMMAND).

## The Brutal Truth

We shipped with the understanding that we had **no production git-auth flow at all**. Users copy-pasted a token into the wrong place and watched clones fail with "access denied" because goclaw was silently running `git clone` unauthenticated. The framework we built is the *first* correct solution; it's not a refactor of something that worked.

The frustrating part: we validated this by catching the auth failure *during testing* — the legacy passthrough adapter with no host scope is a safety valve, not a feature. If a user later tries to use it anyway, they get the exact same silent-failure behavior they had before, but at least now they have a path to fix it.

## Technical Details

### PAT Path (Phase 3, commit b0dccbe3)
- **Mechanism**: GIT_CONFIG_COUNT + GIT_CONFIG_KEY_0 + GIT_CONFIG_VALUE_0 environment variables (git 2.31+). Token never touches argv, .git/config, or remote URL.
- **Host-scope enforcement**: IDN normalization (golang.org/x/net/idna), embedded-userinfo rejection in URL parsing.
- **CVE-2018-17456 mitigation**: resolve remote URLs via `git config --get` (not `git remote get-url`) to dodge ext::sh protocol handler injection.
- **DenyArgs blocking**: case-insensitive rejection of `-c http.`, `-c credential.`, `-c core.sshcommand`, `config --global/--system`, `credential-helper`, bare `daemon`.
- **Tests**: 14 unit tests (subcommand routing, host normalization, scp-form parsing, userinfo rejection, CRLF token rejection, CVE-2018-17456 regression, DenyArgs coverage) + 3 integration tests against TLS git-http-backend proving zero token leakage into cloned .git/config.

### SSH Path (Phase 4, commit b0dccbe3)
- **Mechanism**: Per-call 0600 tmpfile materialized via Phase 2b `materializeEphemeral()` helper. GIT_SSH_COMMAND injected with `-o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new`. Idempotent cleanup via defer.
- **Validation**: golang.org/x/crypto/ssh parses key blob. Passphrase-protected keys rejected with `ErrSSHKeyPassphraseUnsupported` sentinel.
- **SSH TOFU trade-off**: `accept-new` accepted with documented ssh-keyscan mitigation; pinned host keys deferred to v2.
- **Tests**: 8 unit tests (passphrase rejection, env shape, cleanup lifecycle, host-mismatch reuse, malformed-blob rejection) + 3 integration tests proving tmpfile 0600 lifecycle, env propagation to child, cleanup-on-exec-failure, no-orphan-on-rejection.

### Audit Logging (Phase 6, commit 16a0f303)
- **Schema**: `emitSystemEnvInjectionAudit()` centralizes slog `security.system_env_injection` with host_scope_hash (SHA-256 first 8 hex, plaintext hostname omitted for PII safety). Operators pre-compute hash to grep audit streams.
- **Leak detection testing**: `TestEmitSystemEnvInjectionAudit_PAT` and `TestEmitSystemEnvInjectionAudit_SSH` assert that: (a) env NAMES go into audit, env VALUES do NOT; (b) PAT/PEM bytes never appear in log buffer.

### Extensibility Framework (Phase 2, commit 1fe7c5e0 + Phase 2b, commit 14cce5b9)
- **Interface**: `CredentialAdapter` with `ValidateCredential()` + `Inject()`. Passthrough default + git typed + psql stub (Phase 2b, proof of generalization).
- **WithExecCwd / ExecCwdFromContext helpers** (Phase 3): fixes latent design gap where adapter's pre-flight `git config --get` ran in goclaw's daemon CWD, not the agent's repo CWD.

## What We Tried

1. **memfd vs. tmpfile for SSH key**: Initial design used `/proc/self/fd/N` for memory-backed file. Validation revealed `/proc/self/fd/N` resolves "self" against the **caller** (goclaw), not the child process (git→ssh grandchild). git doesn't expose a mechanism to inherit fds without explicit cooperation. Reverted to 0600 tmpfile + defer cleanup.

2. **Sentinel values in leak-detection tests**: First attempt used short sentinels like "1" for `GIT_CONFIG_COUNT`. False-positive matches occurred in slog timestamp digits (e.g., `"timestamp":"2026-05-28T17:29:21.000123..."`). Fixed by using distinctly-formed sentinels: `SENTINEL_COUNT_VALUE`, `ghp_SENTINEL_PAT_VALUE_4242424242424242` (long enough that random substring collision is negligible).

3. **Passphrase-protected SSH keys**: Attempted to detect and prompt for passphrase during validation. Decision: reject them entirely. Rationale: no UX slot to store or retrieve the passphrase on each exec; kubernetes-style service-account flow (unencrypted keys) is the baseline, and v2 can add passphrase support if required.

## Root Cause Analysis

The original mistake was designing a credential injection system without typing. The `CredentialAdapter` interface was necessary because different tools have **entirely different** auth mechanisms (git: env vars + config, kubectl: kubeconfig file, npm: .npmrc auth field, aws: STS assume-role), and a single "paste env var" flow cannot adapt to all of them.

The silent-failure symptom persisted through phases 1–2 because the framework was incomplete: once typed, the git adapter needed explicit host-scope routing + HTTPS path (GIT_CONFIG_COUNT), SSH path (tmpfile), and audit logging. Each missing piece meant git would silently fall back to unauthenticated mode.

## Lessons Learned

1. **Sentinel values for "secret not in log" assertions must be distinctive enough that random text cannot match.** Short sentinels (1–2 chars) collide with digits in timestamps; use longer, structured sentinels (`SENTINEL_*` prefix, base16 suffix) so substring matches are meaningful.

2. **env-var-based auth has hard scaling limits.** git's flexibility (multiple auth backends) means no single env var controls it all. Typed adapters per tool are not optional.

3. **fd inheritance across process boundaries is tool-specific.** `/proc/self/fd/N` is not a portable secret-passing mechanism; tmpfile + 0600 + defer is simpler and more predictable, even if it hits the filesystem.

4. **Host-scope is the invariant.** v1 enforces (user, adapter, host_scope) → one credential. Wildcards and multi-host credential reuse are deferred because they require explicit escrow-side design (key rotation, credential audit hooks).

5. **Context propagation for exec environment must trace the agent's repo CWD, not the daemon's CWD.** WithExecCwd helpers prevent subtle auth failures when the adapter runs pre-flight checks in the wrong directory.

## Next Steps

**Shipped in commits b0dccbe3 → 16a0f303:**
- git adapter (PAT + SSH paths), psql stub (proof of extensibility), audit logging schema, 2 user guides (git-credential-adapter.md, credential-adapter-playbook.md).
- 4 new docs: 09-security.md (trust boundary + SSH TOFU caveats), 03-tools-system.md (audit shape integration).
- 17 i18n keys × 3 locales (en/vi/zh) + React credential dialog rework.
- 41 tests (unit + integration, PG + SQLite both exercised).

**Blocked / Deferred to v2:**
- **Sandbox incompatibility**: adapter cannot grant filesystem perms to bind-mount creds. Design needed for sandboxed exec contexts.
- **Credential-refresh primitive**: no mechanism for `aws sts assume-role` (multi-step auth). Requires OAuth-callback plumbing.
- **Dedicated audit_log table**: currently relies on slog→stderr→journald. Production operators prefer queryable DB table.
- **kubectl, docker, npm, aws, psql production adapters**: framework + playbook ready (credential-adapter-playbook.md), implementation staged for Q3 2026.

**Watch for:**
- Operators who copy legacy env-var workflows into the new system and expect them to "just work" — audit logs + docs should catch these, but social effort needed.
- SSH host-key TOFU attacks in zero-trust networks — ssh-keyscan baseline + pinned-host-keys (v2) are the mitigations.
