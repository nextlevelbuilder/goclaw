# Git Credential Adapter

User-facing guide for the `git` typed credential adapter (issue #82, ships with
v3.x).

## Why a typed adapter?

The legacy CLI credential flow asks the user to paste arbitrary environment
variables (`GH_TOKEN`, `KUBECONFIG`, etc.). `git` does not read its
authentication from a stable, single env var — credentials live in
`.git/config`, `~/.git-credentials`, the OS credential helper, or per-remote
URLs. Pasting a PAT into `GIT_TOKEN` did nothing, which surprised users and
silently failed every clone.

The typed `git` adapter accepts either a **Personal Access Token (PAT)** or an
**SSH private key**, validates it server-side, then injects it into the spawned
`git` process via a transient mechanism that never touches disk or argv.

## When to use which credential type

| Type      | Use when                                                                        | Limits                                                       |
| --------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| **PAT**   | GitHub/GitLab/Gitea over HTTPS. You already have a `ghp_…` or `glpat-…` token.  | Token must be unscoped to specific repos, OR cover all repos goclaw will touch. |
| **SSH**   | Self-hosted git over SSH. You manage `~/.ssh/known_hosts` or accept TOFU risk.  | Passphrase-protected keys are NOT supported (see below).     |
| **Env**   | Legacy path — you have a custom env-var-driven workflow.                        | Loses host-scoped routing; same trust profile as other CLIs. |

## Adding an agent credential (UI)

Agent credentials are the default path for git auth. They avoid channel-user
ID ambiguity: the selected agent owns the credential, and anyone allowed to use
that agent can cause it to run git with the stored credential.

1. Open **Packages → CLI Credentials**.
2. Pick the `git` row and open **Agent Access**.
3. Use the **Credential** tab to select the agent.
4. Choose **Credential Type**: `Personal Access Token` or `SSH Private Key`.
5. Enter **Host Scope** (required for PAT/SSH): the hostname the credential
   authenticates to.
   - Examples: `github.com`, `gitlab.example.com`, `gitea.internal:8443`.
   - Case-insensitive. Punycode normalized via `idna.ToASCII`.
   - Port included only when non-default for the scheme.
6. Paste the token (PAT) or the unencrypted PEM body (SSH).
7. Save.

Use the **Access policy** tab in the same Agent Access dialog when you need to
change deny args, timeout, tips, or env overrides for that agent. Agent Access
is one dialog on purpose: policy and secret storage stay separate internally,
but operators should manage them as one access decision.

## Advanced user overrides

Per-user credentials remain available for personal overrides and backward
compatibility. Use them only when a stable tenant user ID is the intended
credential boundary.

1. Open **Packages → CLI Credentials → Advanced User Overrides → Add**.
2. Select user.
3. Choose **Credential Type**: `Personal Access Token` or `SSH Private Key`.
4. Enter **Host Scope** (required for PAT/SSH): the hostname the credential
   authenticates to.
   - Examples: `github.com`, `gitlab.example.com`, `gitea.internal:8443`.
   - Case-insensitive. Punycode normalized via `idna.ToASCII`.
   - Port included only when non-default for the scheme.
5. Paste the token (PAT) or the unencrypted PEM body (SSH).
6. Save.

The stored secret is encrypted (AES-256-GCM) and can never be read back through
the API or UI. Editing the row shows a `••••••••` placeholder; leaving the
secret field blank preserves the stored value, typing a new value replaces it.

Effective credential precedence is:

1. User override.
2. Channel/context credential.
3. Agent credential.
4. Binary-level env defaults.

## What gets auto-injected

The adapter runs ONLY for these subcommands:

- `clone`
- `fetch`
- `pull`
- `push`
- `submodule`

Any other subcommand (`status`, `log`, `diff`, `commit`, `branch`, etc.) runs
WITHOUT credentials — these are local operations and never reach a remote.

Implementation: see `internal/tools/credential_adapter_git.go::ShouldInject`.

## Host-scope semantics

`host_scope` is the **exact** ASCII hostname (with optional port) the
credential is valid for. v1 does NOT support wildcards.

Stored `github.com` matches:

- ✓ `git clone https://github.com/org/repo.git`
- ✗ `git clone https://api.github.com/...` (different host)
- ✗ `git clone https://github.com:8443/...` (different port — port is part of
  the scope key)

Stored `gitea.example.com:8443` matches:

- ✓ `git clone https://gitea.example.com:8443/...`
- ✗ `git clone https://gitea.example.com/...` (default port — still mismatch)

If you run a self-hosted server on the scheme's default port (443 HTTPS, 22
SSH), omit the port. If you run on a non-default port, include it.

When no typed PAT/SSH credential is selected, or the selected credential cannot
match the resolved remote host, adapter-managed remote commands fail closed
with a GoClaw diagnostic. `git` is not allowed to fall through to an
interactive username/password prompt in agent runtime.

## Security model

### PAT path

- Injected via `GIT_CONFIG_COUNT` + `GIT_CONFIG_KEY_*` / `GIT_CONFIG_VALUE_*`
  environment variables.
- The PAT itself goes into a value that synthesizes an `http.<remote>.extraheader`
  config entry with `Authorization: Basic base64("x-access-token:<token>")`.
- **The PAT never appears on argv** — so `ps`, `/proc/<pid>/cmdline`, and
  shell-history echoes don't expose it.
- The raw PAT, base64 payload, and full injected header are all registered with
  the scrubber before tool output is returned to the agent.
- The injected env vars are scoped to the spawned `git` process only; they are
  NOT inherited by goclaw, by other tools, or by sibling exec calls.

### SSH path

- The PEM key is written to an `0600`-mode tmpfile in `os.TempDir()` (per-user
  on POSIX) with a `goclaw-gitkey-*` prefix.
- `GIT_SSH_COMMAND` is set to
  `ssh -i <tmpfile> -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new`.
- The tmpfile is removed via `defer` on the exec wrapper. **SIGKILL of goclaw
  leaves the file orphaned** — see the Operator Notes section below.
- SSH private keys are validated twice at save time: first with Go's SSH parser,
  then with OpenSSH via `ssh-keygen -y -f <tmpfile>` when `ssh-keygen` is
  available. This catches keys that would otherwise save successfully but fail
  later with OpenSSH diagnostics such as `error in libcrypto`.
- `StrictHostKeyChecking=accept-new` accepts unknown host keys on first
  contact (TOFU). A network attacker positioned between goclaw and the git
  host CAN capture the SSH session on the first connection. Operators should
  pre-seed `~/.ssh/known_hosts`:

  ```sh
  ssh-keyscan github.com >> ~/.ssh/known_hosts
  ```

  v2 will support per-credential pinned host keys.

### Passphrase-protected SSH keys: rejected

The adapter rejects encrypted SSH keys at validation time with `error_key =
git.cred_ssh_passphrase_unsupported`. Reason: we have no UX or storage slot
for the passphrase, and ssh-agent forwarding is outside the goclaw security
model. Re-export your key without a passphrase, or use a dedicated deploy key.

### Redaction across output channels

Every credential adapter registers its secret bytes with the per-request
`ScrubCredentials` bag (`internal/tools/scrub.go`). The scrubber removes the
secret from:

- Live stdout / stderr streamed to the agent.
- The final `Result.Content` returned by the tool.
- Error messages bubbled up to the agent.
- The audit log line (`security.system_env_injection`) — see below.

Plaintext hostnames are also kept out of the audit log: `host_scope_hash` is
the SHA-256 first 8 hex chars of the normalized scope.

## Auditability

Every successful credential injection emits exactly one structured log line:

```
level=WARN msg=security.system_env_injection
  adapter=git binary=git user_id=<uuid> credential_source=agent
  env_keys=[GIT_CONFIG_COUNT,GIT_CONFIG_KEY_0,GIT_CONFIG_VALUE_0]
  argv_prefix_len=0
  host_scope_hash=3aeb0024
```

`env_keys` lists NAMES only — values never appear. `host_scope_hash` is the
first 8 hex chars of `sha256(normalized_host_scope)`. Operators wanting to
grep for activity against a specific host pre-compute the hash:

```sh
echo -n "github.com" | sha256sum | cut -c1-8
```

See `docs/09-security.md` → "CLI credential adapters" for the full schema.

## Migration from legacy env-paste

Existing rows in `secure_cli_user_credentials` with `credential_type IS NULL`
or `= 'env'` continue to work via the passthrough adapter. Existing user
overrides remain higher precedence than agent credentials. There is no forced
migration.

To move to the agent-scoped model, create a matching Agent Credential for the
agent and remove the user override when the override is no longer needed.

## Operator notes

- **Tmpfile sweep**: high-security deployments should sweep stale tmpfiles
  every few minutes:

  ```sh
  find "$TMPDIR" -name 'goclaw-gitkey-*' -mmin +60 -delete
  find "$TMPDIR" -name 'goclaw-pgpass-*' -mmin +60 -delete
  ```

- **Pre-seed known_hosts** to defeat TOFU MITM (see SSH path above).
- **Log aggregation**: route `security.*` slog events to your SIEM. The
  schema is pinned by `TestEmitSystemEnvInjectionAudit_*` — alert on any
  change.
- **No sandbox support v1**: the adapter mutates the parent process's
  forked-child environment, which is incompatible with the bind-mount-based
  sandbox path. Sandbox + credentialed exec is on the v2 roadmap.

## Known limitations (v1)

- One credential per (agent, binary) row, plus legacy one credential per
  (user, binary) override.
- No multi-host wildcard (`*.github.com`).
- No passphrase-protected SSH keys.
- No persistent `known_hosts` per credential (TOFU only).
- No sandbox support.
- PAT scope cannot be inspected — goclaw stores the token opaquely.

## Future work

Tracked separately:

- v2: OAuth device-flow for GitHub/GitLab — eliminates PAT paste.
- v2: Multi-credential per user with host routing logic.
- v2: Sandbox/Docker exec path support (per-call key bind-mount).
- v2: Pinned SSH host keys per credential.
- v2: Migrate `gh`/`aws`/`gcloud` to non-passthrough adapters as use cases
  arise (e.g. `aws assume-role` needs argv mutation).
- v2: Dedicated `audit_log` table for `security.system_env_injection` events.
