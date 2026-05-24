# pip + npm Package Updates (Phase 2a)

Extends the Phase 1 GitHub binary update flow to system-wide pip and npm packages.
Closes #900 (Phase 2a).

See also: [GitHub binary updates](./packages-github.md) · [apk system package updates](./packages-apk.md)

---

## Overview

When the gateway is running in Standard edition with `pip3` and/or `npm` on PATH,
`GET /v1/packages/updates` includes pip and npm update results alongside GitHub
binaries. The UI shows a per-source pill filter; sources without a binary on PATH
are hidden automatically.

pip scope: **system-wide** (`--break-system-packages`). pip venv / user-site is not
supported in Phase 2a.

npm scope: **global** (`--global`). Per-project `node_modules` are not touched.

---

## Command Matrix

| Source | Check command | Update command | Check timeout | Update timeout |
|--------|---------------|----------------|---------------|----------------|
| pip | `pip3 list --outdated --format json --break-system-packages` | `pip3 install --upgrade --no-cache-dir --break-system-packages --upgrade-strategy only-if-needed <name>` | 30 s | 5 min |
| npm | `npm outdated --global --json` | `npm install --global <name>@<version>` | 30 s | 5 min |

Pre-release pip check appends `--pre` in a secondary call (see Pre-Release Handling).

---

## Behavior

### pip

- `pip3 list --outdated --format json` emits a JSON array; each element has
  `name`, `version`, `latest_version`, `latest_filetype`.
- Exit code is always 0 whether or not updates exist.
- stderr is classified via `ClassifyPipStderr` into sentinel errors (see Error Classes).

### npm

npm's exit-code semantics are non-standard:

| Condition | Exit code | Interpretation |
|-----------|-----------|----------------|
| No outdated packages | 0 | No updates |
| Outdated packages found | 1 | Updates — parse JSON stdout |
| Real npm error (ERESOLVE, network, etc.) | 1 | stderr contains `npm ERR!` |
| Ambiguous (exit 1, no stdout, no stderr) | 1 | Treated as no-updates |

The checker inspects exit code **and** stderr for `npm ERR!` before deciding
whether exit 1 means "updates available" or "real error".

---

## Pre-Release Handling

### pip

Two-call merge strategy:

1. **Primary call** (stable only, no `--pre`): baseline list of outdated packages.
2. If any currently-installed package has a pre-release version (`IsPipPreRelease()`):
   **Secondary call** with `--pre` to surface the best available upgrade target.
3. Results are merged by package name; when a name appears in both, the entry
   with the lexicographically higher `latest_version` wins.

Gate: `IsPipPreRelease` matches PEP 440 patterns — `a`, `b`, `rc`, `dev`, `.pre`,
`.preview` (case-insensitive, digits optional).

### npm

Single-call strategy with a skip gate:

- If `latest` contains an npm pre-release label
  (`-alpha`, `-beta`, `-rc`, `-pre`, `-preview`, `-dev`, `-nightly`, `-snapshot`)
  **and** `current` does not → entry is skipped.
- If `current` is already a pre-release and `latest` is too → entry kept (user on
  pre-release channel receives the newest pre-release update).

This prevents unexpected upgrades from stable channels to unstable channels.

---

## Availability Detection

A source is considered **available** when two gates both pass:

1. **Binary present**: `exec.LookPath("pip3")` / `exec.LookPath("npm")` succeeds.
2. **Edition allows it**: `edition.Current().SupportsPipNpm == true` (always true
   for Standard; always false for Lite desktop).

When a source is unavailable:

- Its checker returns `UpdateCheckResult{Available: false}` (no error, no updates).
- `UpdateRegistry.Availability()` maps that source to `false`.
- `GET /v1/packages/updates` response includes `"availability": {"pip": false, "npm": false}`.
- The frontend hides that source from the filter bar.

Lite edition: `gateway_packages_wiring.go` checks `edition.Current().SupportsPipNpm`
before calling `RegisterChecker` / `RegisterExecutor`. Pip and npm checkers are
never instantiated — `registry.Sources()` returns `["github"]` only.

---

## Error Classes

Sentinel errors are defined in `internal/skills/pkg_update_helpers.go`.

### pip sentinels

| Sentinel | Trigger pattern in stderr | i18n key |
|----------|--------------------------|----------|
| `ErrUpdatePipExternallyManaged` | `externally-managed-environment` / `EXTERNALLY-MANAGED` | `packages.update.pip.externally_managed` |
| `ErrUpdatePipPermission` | `Permission denied` / `EACCES` | `packages.update.pip.permission` |
| `ErrUpdatePipNotFound` | `No matching distribution` / `Could not find a version` | `packages.update.pip.not_found` |
| `ErrUpdatePipNetwork` | `Read timed out` / `ConnectionError` / `network` | `packages.update.pip.network` |
| `ErrUpdatePipConflict` | `incompatible` / `dependency resolver` / `Shallow backtracking` | `packages.update.pip.conflict` |

### npm sentinels

| Sentinel | Trigger pattern in stderr | i18n key |
|----------|--------------------------|----------|
| `ErrUpdateNpmPermission` | `EACCES` | `packages.update.npm.permission` |
| `ErrUpdateNpmConflict` | `ERESOLVE` | `packages.update.npm.conflict` |
| `ErrUpdateNpmNetwork` | `ETIMEDOUT` / `ENOTFOUND` / `getaddrinfo` | `packages.update.npm.network` |
| `ErrUpdateNpmTargetMissing` | `ETARGET` | `packages.update.npm.target_missing` |
| `ErrUpdateNpmNotFound` | `E404` / `404` / `not in this registry` | `packages.update.npm.not_found` |

Unclassified stderr returns a generic wrapped error with a truncated reason
(≤ 500 chars, ANSI-stripped).

---

## Runbook

| Symptom | Fix |
|---------|-----|
| **pip EACCES** — gateway lacks write to site-packages | Run gateway as an owner of `/usr/lib/python3/dist-packages`, or set `PIP_TARGET=/app/data/.pip` + add it to `PYTHONPATH` |
| **npm EACCES** — global prefix owned by root | `npm config set prefix ~/.npm-global`; add `~/.npm-global/bin` to `PATH` in entrypoint |
| **npm ERESOLVE** — peer conflict blocks install | SSH into container: `npm install -g <name>@<version> --legacy-peer-deps`; re-check will clear the entry |
| **pip externally-managed (PEP 668)** | Set env var `PIP_BREAK_SYSTEM_PACKAGES=1`, or upgrade pip to ≥ 23.3 (respects the CLI flag without the env var) |

---

## Minimum Versions

| Runtime | Minimum | Recommended | Notes |
|---------|---------|-------------|-------|
| pip | 20.0 | ≥ 23.3 | `--format json` requires 20+; `--break-system-packages` without env var requires 23.3+ |
| npm | 6.0 | ≥ 10 | Older versions may not emit JSON exit 1 correctly |
| Node.js | 12 | ≥ 18 LTS | npm 10 requires Node 18+ |

---

## Shared Locker

`InstallSingleDep` (skill dep install) and `PipUpdateExecutor.Update` / `NpmUpdateExecutor.Update`
(update apply) share a single `PackageLocker` instance injected via `SetSharedPackageLocker`.

This means concurrent `pip install requests` (from a skill) and `pip upgrade requests`
(from the update flow) are serialized by the same per-key mutex. The lock key is
the bare package name (e.g. `"requests"`) scoped to the source (`"pip"` or `"npm"`).

Operators must not bypass the gateway and call `pip install` directly in parallel
with gateway operations — doing so defeats the shared lock and risks a partial-install
race.

---

## Fixture Regeneration

Test fixtures capture `pip3 list --outdated --format json` and `npm outdated -g --json`
output. When the environment's package versions change, regenerate them:

```bash
# pip fixture — include pip version in filename for drift tracking
pip3 --version  # e.g., pip 24.0
pip3 list --outdated --format json --break-system-packages \
  > internal/skills/testdata/pip_outdated_pip24.json

# npm fixture — include npm version in filename
npm --version   # e.g., 10.5.0
npm outdated --global --json \
  > internal/skills/testdata/npm_outdated_npm10.json
# Note: npm exits 1 when packages are outdated — that's expected.

# Update test cases to reference the new filename and expected values.
```

Fixture files are version-stamped in their names so drift between CI environments
is detectable by `git diff`.
