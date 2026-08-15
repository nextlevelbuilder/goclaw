# Credential Adapter Playbook

Developer guide for authoring a `CredentialAdapter`. Use this when adding a new
typed-credential CLI binary (kubectl, docker, npm, aws, psql…) beyond the
shipped `git` adapter.

This is the **R1 extensibility deliverable** from the issue #82 brainstorm:
proof that the framework generalizes past git. Read [git-credential-adapter.md](./git-credential-adapter.md)
first for the user-facing story; this doc is the implementer's manual.

## Interface contract

Source of truth: [`internal/tools/credential_adapter.go`](../internal/tools/credential_adapter.go).

```go
type CredentialAdapter interface {
    Name() string                                // adapter_name column value
    ShouldInject(argv []string) bool             // gate: skip local-only subcommands
    Prepare(ctx context.Context,
            bin *store.SecureCLIBinary,
            cred *store.SecureCLIUserCredential,
            argv []string) (*Injection, error)
}
```

- `Name()` is the string operators set in `secure_cli_binaries.adapter_name`.
  It is also the value logged in `security.system_env_injection.adapter`.
- `ShouldInject` decides whether the adapter runs for THIS invocation. For
  tools without subcommands (`psql`, `docker push`, etc.) return `true`. For
  tools where some subcommands are local-only (`git status`, `kubectl version`)
  return `false` to skip injection and audit-noise.
- `Prepare` produces a single `*Injection` per exec. Returning a non-nil error
  aborts the exec — no fallback to un-credentialed run, so reserve errors for
  malformed credentials, not "credential not found" (return `&Injection{}` for
  that case).

Register in your adapter file's `init()`:

```go
func init() { RegisterAdapter(myAdapter{}) }
```

Lookup falls back to the `passthrough` no-op adapter on unknown names, so a
typo in `adapter_name` degrades to legacy behavior with a clear audit trail
rather than breaking exec.

## The four Injection fields

```go
type Injection struct {
    ArgvPrefix  []string            // spliced between binary and user args
    Env         map[string]string   // merged on top of base env
    Cleanup     func() error        // deferred after exec
    ScrubValues []string            // redacted from stdout/stderr/Result/errors
}
```

### `ArgvPrefix`

Use when the tool reads auth from CLI flags AND those flags do NOT carry the
secret directly. Example: `kubectl --kubeconfig <path>` is fine (path, not
secret). `aws --profile <name>` is fine. NEVER use for the secret itself —
argv is world-readable via `/proc/<pid>/cmdline` on Linux.

The PAT path of the git adapter deliberately uses `Env` (not `ArgvPrefix`)
for `http.<remote>.extraheader` precisely because `git -c http.…=<token>`
would leak the token via `ps`.

### `Env`

The primary injection channel. Tools that read auth from a config file env
var (`KUBECONFIG`, `DOCKER_CONFIG`, `NPM_CONFIG_USERCONFIG`,
`AWS_SHARED_CREDENTIALS_FILE`, `PGPASSFILE`) all flow through here.

Env is scoped to the spawned child process; goclaw's own env is unchanged.

### `Cleanup`

Required when `Prepare` writes any filesystem material. Always pair with
`materializeEphemeral`'s returned cleanup closure (see below).

### `ScrubValues`

List every secret byte sequence the subprocess might echo back. The
per-request `ScrubCredentials` bag (see [`internal/tools/scrub.go`](../internal/tools/scrub.go))
strips these from:

- live stdout/stderr streamed to the agent,
- the final `Result.Content`,
- error messages,
- the slog audit line.

If the tool's error path embeds the secret in a URL or config dump (git's
`fatal: unable to access 'https://<token>@host/'…`), this is your only
defense — register both the raw secret AND any predictable wrapping.

## Ephemeral filesystem material — `materializeEphemeral`

Source: [`internal/tools/credential_ephemeral.go`](../internal/tools/credential_ephemeral.go).

```go
path, cleanup, err := materializeEphemeral(ctx, []byte(content), "kubecfg")
if err != nil { return nil, err }
return &Injection{
    Env:         map[string]string{"KUBECONFIG": path},
    Cleanup:     cleanup,
    ScrubValues: []string{bearerToken},
}, nil
```

Guarantees:

- `0600` perms, per-user `os.TempDir()` on POSIX.
- Idempotent cleanup (concurrent callers don't double-remove).
- Prefix becomes `goclaw-<prefix>-<random>` so operators can sweep stale files
  with one glob.

### Why not memfd?

Originally proposed (brainstorm R6); dropped during validation. `/proc/self/fd/N`
resolves "self" against the calling process. For grandchildren (e.g. git → ssh),
the kernel resolves against the child, which sees `EBADF` unless the parent
passed the fd via `ExecCommand.ExtraFiles` AND the child binary reads from that
fd number. None of git/psql/docker/kubectl have an API to forward fds to their
subprocesses. Tmpfile + `defer remove` is the safe, portable default.

**SIGKILL caveat:** if goclaw is killed with SIGKILL (-9), `defer cleanup()`
never fires and the 0600 tmpfile lingers. Operators should sweep — see
[git-credential-adapter.md → Operator notes](./git-credential-adapter.md#operator-notes).

## Host-scope semantics

`secure_cli_user_credentials.host_scope` is the **exact** ASCII hostname (with
optional port) the credential authenticates to. v1 does not support wildcards.

| Tool | host_scope value | Matched against |
| ---- | ---------------- | --------------- |
| git | `github.com`, `gitea.internal:8443` | remote URL host from argv |
| kubectl | `prod.example.com:6443` | current-context cluster API endpoint |
| docker | `registry.gitlab.com`, `ghcr.io` | first argv after `push`/`pull` |
| npm | `registry.npmjs.org` or `npm.pkg.github.com` | argv `--registry` or `.npmrc` default |
| aws | `<account>:<region>` (composite) | parsed from `--profile`/region flags |
| psql | `db.example.com:5432` | `-h`/`-p` argv or `PGHOST` env |

Normalize via `idna.ToASCII` and lowercase. Hash with the shared
`hashHostScope()` helper for audit logs — **never** log plaintext hostname.

## Worked mappings

Each subsection below is a sketch. Production adapters should add validation,
edge-case handling, and per-adapter tests mirroring `credential_adapter_git_test.go`.

### kubectl

```go
func (kubectlAdapter) Prepare(ctx context.Context, _ *store.SecureCLIBinary,
    cred *store.SecureCLIUserCredential, _ []string) (*Injection, error) {
    if cred == nil { return &Injection{}, nil }
    var k struct {
        Kubeconfig  string `json:"kubeconfig"`   // full YAML body
        BearerToken string `json:"bearer_token"` // optional
    }
    if err := json.Unmarshal(cred.EncryptedEnv, &k); err != nil {
        return nil, fmt.Errorf("decode kubeconfig cred: %w", err)
    }
    path, cleanup, err := materializeEphemeral(ctx, []byte(k.Kubeconfig), "kubecfg")
    if err != nil { return nil, err }
    scrub := []string{}
    if k.BearerToken != "" { scrub = append(scrub, k.BearerToken) }
    return &Injection{
        Env:         map[string]string{"KUBECONFIG": path},
        Cleanup:     cleanup,
        ScrubValues: scrub,
    }, nil
}
```

- **Injection fields**: `Env` + `Cleanup` + `ScrubValues`.
- **Host scope**: cluster API server hostname:port from current context.
- **Skip subcommands**: `kubectl version`, `kubectl config view --raw`,
  `kubectl --help`. Most subcommands DO hit the API server so default-true is
  acceptable; refine if false-positive audit noise becomes an issue.
- **Tests to write**: kubeconfig path injected into env not argv; tmpfile
  removed post-exec; bearer token scrubbed from `kubectl get pods` 401 stderr.

### docker

```go
// DOCKER_CONFIG points to a DIRECTORY containing config.json, not the file.
func (dockerAdapter) Prepare(ctx context.Context, _ *store.SecureCLIBinary,
    cred *store.SecureCLIUserCredential, _ []string) (*Injection, error) {
    if cred == nil { return &Injection{}, nil }
    var d struct {
        Auths map[string]struct{ Auth string `json:"auth"` } `json:"auths"`
    }
    if err := json.Unmarshal(cred.EncryptedEnv, &d); err != nil {
        return nil, fmt.Errorf("decode docker cred: %w", err)
    }
    dir, cleanup, err := materializeEphemeralDir(ctx, "dockercfg")
    if err != nil { return nil, err }
    body, _ := json.Marshal(d)
    if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o600); err != nil {
        _ = cleanup(); return nil, err
    }
    scrub := []string{}
    for _, a := range d.Auths { scrub = append(scrub, a.Auth) }
    return &Injection{
        Env:         map[string]string{"DOCKER_CONFIG": dir},
        Cleanup:     cleanup,
        ScrubValues: scrub,
    }, nil
}
```

- **Note**: `DOCKER_CONFIG` is a directory, not a file. Requires a small
  `materializeEphemeralDir` sibling helper (not yet shipped — add when first
  docker adapter lands).
- **Host scope**: registry hostname (`registry.gitlab.com`, `ghcr.io`,
  `<account>.dkr.ecr.<region>.amazonaws.com`).
- **Scrub**: the base64-encoded `auth` value AND the decoded `user:pass` form,
  because docker error output can decode and echo either.

### npm

```go
func (npmAdapter) Prepare(ctx context.Context, _ *store.SecureCLIBinary,
    cred *store.SecureCLIUserCredential, _ []string) (*Injection, error) {
    if cred == nil { return &Injection{}, nil }
    var n struct {
        Registry  string `json:"registry"`
        AuthToken string `json:"auth_token"`
    }
    if err := json.Unmarshal(cred.EncryptedEnv, &n); err != nil {
        return nil, fmt.Errorf("decode npm cred: %w", err)
    }
    line := fmt.Sprintf("//%s/:_authToken=%s\n",
        strings.TrimPrefix(strings.TrimPrefix(n.Registry, "https://"), "http://"),
        n.AuthToken)
    path, cleanup, err := materializeEphemeral(ctx, []byte(line), "npmrc")
    if err != nil { return nil, err }
    return &Injection{
        Env:         map[string]string{"NPM_CONFIG_USERCONFIG": path},
        Cleanup:     cleanup,
        ScrubValues: []string{n.AuthToken},
    }, nil
}
```

- **Host scope**: registry hostname.
- **Subcommand gate**: only `install`/`publish`/`ci`/`audit` (network ops).
  Skip `npm list`, `npm version`, `npm run …`.

### aws

```go
func (awsAdapter) Prepare(ctx context.Context, _ *store.SecureCLIBinary,
    cred *store.SecureCLIUserCredential, _ []string) (*Injection, error) {
    if cred == nil { return &Injection{}, nil }
    var a struct {
        AccessKeyID     string `json:"access_key_id"`
        SecretAccessKey string `json:"secret_access_key"`
        SessionToken    string `json:"session_token,omitempty"`
        Profile         string `json:"profile"`
    }
    if err := json.Unmarshal(cred.EncryptedEnv, &a); err != nil {
        return nil, fmt.Errorf("decode aws cred: %w", err)
    }
    body := fmt.Sprintf("[%s]\naws_access_key_id=%s\naws_secret_access_key=%s\n",
        a.Profile, a.AccessKeyID, a.SecretAccessKey)
    if a.SessionToken != "" {
        body += fmt.Sprintf("aws_session_token=%s\n", a.SessionToken)
    }
    path, cleanup, err := materializeEphemeral(ctx, []byte(body), "awscreds")
    if err != nil { return nil, err }
    return &Injection{
        Env: map[string]string{
            "AWS_SHARED_CREDENTIALS_FILE": path,
            "AWS_PROFILE":                 a.Profile,
        },
        Cleanup:     cleanup,
        ScrubValues: []string{a.SecretAccessKey, a.SessionToken},
    }, nil
}
```

- **Host scope**: composite `<account-id>:<region>` (composite key for the
  rare case where the same operator runs multi-account workflows).
- **v2 flag — credential refresh**: `aws sts assume-role` returns a short-lived
  STS credential. v1's "one credential per exec" model cannot refresh
  mid-flight. Defer until a refresh primitive is added (track in roadmap).

### psql

Already shipped as the framework-validation stub.
See [`internal/tools/credential_adapter_psql.go`](../internal/tools/credential_adapter_psql.go).

- **Injection fields**: `Env: {PGPASSFILE: path}` + `Cleanup` + `ScrubValues`.
- **Host scope**: `db.example.com:5432`.
- **Subcommand gate**: psql has no subcommands; `ShouldInject` returns `true`
  always.
- **Edge case handled**: `escapePgpass()` escapes backslash and colon per the
  libpq `.pgpass` spec to prevent injection of a second entry via a `:` in
  the password.

## Interface validation gate

Before merging a new adapter, answer these three gate questions in writing
(PR description or phase file). Phase 2b introduced this discipline; reuse it
verbatim:

1. **Does `Prepare` fit the four Injection fields cleanly, or did you need a
   fifth?** If the latter, the framework needs a change BEFORE your adapter
   lands — not a bypass.

2. **Can the secret reach the subprocess without ever touching `ArgvPrefix`?**
   If no, document why and accept the `/proc/<pid>/cmdline` exposure
   explicitly in the security section of your phase file.

3. **Does the tool's error path emit the secret in a form `ScrubValues`
   wouldn't catch?** (e.g. base64-wrapped, URL-encoded, partially echoed.)
   Enumerate the wrappings and add each to `ScrubValues`.

If any answer is "no" or "unsure", stop and revise the design.

## Anti-patterns — do NOT

- **Do NOT add adapter-specific branches to `credentialed_exec.go`.** The
  whole point of the framework is that the hot path stays agnostic. If your
  adapter needs special handling, put it in `Prepare` or extend the
  `Injection` shape (and update every other adapter).
- **Do NOT introduce a parallel `materializeFoo` helper.** Use the shared
  `materializeEphemeral` (or extend it). One helper means one place to audit
  the perms/cleanup contract.
- **Do NOT log plaintext `host_scope` to audit.** Use `hashHostScope()` —
  operators recover the host by pre-computing the hash, not by reading it
  out of logs.
- **Do NOT inject the secret via `ArgvPrefix`.** See gate question #2.
- **Do NOT skip `ShouldInject` gating** for tools with mixed local/remote
  subcommands. Every unnecessary injection is an extra audit-log line, a tmp
  file, and a potential leak surface.
- **Do NOT return a hard error for "no credential matches".** Return
  `&Injection{}, nil` and let the subprocess fail with its own clear auth
  error. Hard error here breaks the un-credentialed fallback path.
