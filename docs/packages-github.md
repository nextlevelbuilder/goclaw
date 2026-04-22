# GitHub Binary Installer

Install CLI tools directly from GitHub Releases at runtime. Covers Go, Rust,
shell, and other binary-distributed tools not available via `apk` / `pip` / `npm`.

Closes [#741](https://github.com/nextlevelbuilder/goclaw/issues/741).

## Install Syntax

```
github:owner/repo[@tag]
```

Examples:
- `github:cli/cli` → latest release
- `github:jesseduffield/lazygit@v0.42.0` → specific version
- `github:sharkdp/fd@v9.0.0` → specific version with dot separator

## How It Works

1. Fetches release metadata from the GitHub API
2. Auto-selects asset matching `linux` + current arch (amd64 / arm64)
3. Streams download to a temp file, enforcing a max size cap
4. Verifies SHA256 if the publisher ships `checksums.txt` / `SHA256SUMS`
5. Validates ELF magic bytes + 64-bit class + machine matches runtime arch
6. Extracts archive safely (tar.gz / zip / raw binary) with path-traversal + zip-bomb guards
7. Installs to `/app/data/.runtime/bin/` (prepended to `$PATH`)
8. Persists a manifest for later listing + uninstall

## Usage

### Web UI

1. Admin Settings → Packages page
2. Scroll to **GitHub Binaries** section
3. Enter `owner/repo[@tag]` or click **Browse releases** to pick a version
4. Click **Install**

### HTTP API

```bash
# Install
curl -X POST http://gateway/v1/packages/install \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"package": "github:jesseduffield/lazygit@v0.42.0"}'

# List installed (includes pip/npm/system + github)
curl http://gateway/v1/packages -H "Authorization: Bearer $ADMIN_TOKEN"

# Browse releases (picker UI uses this)
curl 'http://gateway/v1/packages/github-releases?repo=cli/cli&limit=10' \
  -H "Authorization: Bearer $VIEWER_TOKEN"

# Uninstall
curl -X POST http://gateway/v1/packages/uninstall \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"package": "github:lazygit"}'
```

## Admin Configuration

All configuration is driven by environment variables — **never** place the
token in `config.json`.

| Env var | Default | Notes |
|---------|---------|-------|
| `GOCLAW_PACKAGES_GITHUB_TOKEN` | `""` | Optional PAT: rate 60/hr → 5000/hr + private repo access |
| `GOCLAW_PACKAGES_MAX_ASSET_SIZE_MB` | `200` | Applies to both download cap and 2× uncompressed cap |
| `GOCLAW_PACKAGES_GITHUB_ALLOWED_ORGS` | `""` | Comma-separated allowlist (empty = all orgs allowed) |
| `GOCLAW_PACKAGES_GITHUB_BIN_DIR` | `/app/data/.runtime/bin` | Where extracted binaries land |
| `GOCLAW_PACKAGES_GITHUB_MANIFEST` | `{bin_dir}/../github-packages.json` | Manifest path |

Token scopes:
- public-only repos: no scopes required
- private repos: `repo`
- org-SSO-enforced repos: must be SSO-authorized PAT

## Security

- HTTPS-only downloads with SSRF host allowlist:
  `github.com`, `api.github.com`, `objects.githubusercontent.com`,
  `release-assets.githubusercontent.com`, `codeload.github.com`
- Every redirect hop re-validated (blocks redirect-based host escape)
- Literal IP hostnames (v4 / v6) always rejected (blocks cloud-metadata access)
- SHA256 verification when publisher ships `checksums.txt` / `SHA256SUMS` (constant-time compare)
- ELF magic + 64-bit class + machine-arch validation before `chmod +x`
- Path-traversal prevention in archive extraction (rejects `..`, absolute, Windows drive, null byte)
- Zip-bomb guard (cumulative uncompressed bytes capped at 2× max asset size)
- Symlink / hardlink entries skipped, never written
- Admin-only API + master-scope guard on install/uninstall
- Picker endpoint `/v1/packages/github-releases` throttled per user (30 req/min, burst 10) to protect GitHub API quota; anonymous fallback keyed by remote IP. Response is `429 Too Many Requests` with `Retry-After: 60` when tripped.
- Token never logged (startup log prints `token_set=bool`)

## Troubleshooting

### "glibc not found" / segfault on execution

GoClaw runs on Alpine Linux (musl libc). Many Go/Rust binaries target glibc.

**Fix:** pick a musl-compatible release asset. Look for names containing:
- `*-musl.tar.gz` (explicit musl)
- `*-linux-static*` (fully static)
- Go binaries with `CGO_ENABLED=0` typically work out of the box

Known-good musl releases:
- `ripgrep`: `ripgrep-*-x86_64-unknown-linux-musl.tar.gz`
- `starship`: `starship-x86_64-unknown-linux-musl.tar.gz`
- `gh`: `gh_*_linux_amd64.tar.gz` (static)

### "no matching asset found"

Asset naming doesn't fit the heuristic. Open the release page and confirm assets
exist for `linux` + your arch. Workaround: file an upstream issue asking for
standard `linux_amd64` / `linux_arm64` naming.

### "arch mismatch"

Binary is `amd64` but runtime is `arm64` (or vice versa). Pick a release asset
matching the host arch — the release picker UI filters automatically.

### "rate limit exceeded"

Anonymous GitHub API is capped at 60 req/hr. Set
`GOCLAW_PACKAGES_GITHUB_TOKEN` to bump to 5000/hr.

### "checksum mismatch"

Hard-fail. Indicates tampered download or publisher re-signing without updating
the release. Do not force-install; report upstream.

## Limitations (Phase 1)

- Linux-only (Lite/Desktop editions not yet supported)
- Docker edition only (runtime dir `/app/data/.runtime/bin`)
- Installs all top-level executables in an archive (no interactive picker if
  archive contains multiple binaries)
- No version history / rollback — re-installing replaces in place
- Global manifest (not per-tenant)

## See Also

- [`docs/14-skills-runtime.md`](./14-skills-runtime.md) — Overview of the runtime packages system
- Issue [#741](https://github.com/nextlevelbuilder/goclaw/issues/741) — Original feature request
