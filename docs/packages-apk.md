# apk (Alpine Package Keeper) Updates (Phase 2b)

Extends the Phase 2a pip + npm update flow to Alpine Linux system packages.
GoClaw manages system packages via a privileged `pkg-helper` sidecar over a
Unix socket. This document covers how apk updates are detected, applied, and
what to do when things go wrong.

See also: [GitHub binary updates](./packages-github.md) · [pip + npm updates](./packages-pip-npm.md)

---

## 1. Overview

When the gateway runs inside an Alpine-based Docker image (`latest`, `full`,
`base`, `otel` variants) in **Standard edition**, `GET /v1/packages/updates`
includes system package updates alongside GitHub binaries, pip, and npm.

Two gates must both pass for apk to appear in the availability map:

1. **Runtime check:** `/etc/alpine-release` is present at startup. On Debian,
   Ubuntu, or macOS desktop images, apk is silently omitted — no error, no update
   results, `availability.apk = false`.
2. **Edition check:** `edition.Current().SupportsApk == true`. Standard
   edition: always true. Lite desktop (macOS/Windows): always false — system
   package management is not available outside containers.

Architecture note: the gateway process runs as `uid 1000` (goclaw) and never
calls `apk` directly. All apk operations are delegated to `/app/pkg-helper`
(root-owned), which listens on `/tmp/pkg.sock` (0600, accessible only to
goclaw). This keeps the main process unprivileged.

---

## 2. Command Matrix

Commands are executed inside `pkg-helper` (not by the gateway directly).

| Operation | Command inside helper | Timeout |
|---|---|---|
| Refresh index | `apk update` | 60 s |
| List outdated | `apk version -l '<'` | 30 s |
| Upgrade one package | `apk add -u <name>` | 5 min |
| Install new (dep install) | `apk add <name>` | 5 min |
| Remove | `apk del <name>` | 5 min |

The checker runs `apk update` + `apk version -l '<'` on every `Check()` call.
The executor runs `apk add -u <name>` on `POST /v1/packages/update`.

---

## 3. Behavior

### How the checker works

1. `GET /v1/packages/updates` triggers `ApkUpdateChecker.Check()`.
2. The checker sends an `update-index` action to pkg-helper (runs `apk update`
   inside the container — refreshes the remote index from Alpine mirrors).
3. On success, it sends a `list-outdated` action (runs `apk version -l '<'`).
4. Output is parsed line-by-line. Each line has the form:
   ```
   <name>-<installed_ver> < <available_ver>
   ```
   The parser uses the rightmost `-<digit>` boundary to split name from version,
   correctly handling names that contain hyphens (e.g. `py3-pip`, `ca-certificates`).
5. Malformed lines are skipped with a warning log; well-formed entries produce
   `UpdateInfo` structs with `Source="apk"`.
6. Results are cached with the global `UpdatesCheckTTL` (default 1 hour).
   The cache is invalidated on successful upgrade.

### Output parsing

`apk version -l '<'` format:

```
bash-5.2.21-r6 < 5.2.26-r0
py3-pip-22.0.4-r0 < 22.3-r0
ca-certificates-20230506-r0 < 20240226-r0
```

Name/version split uses the rightmost `hyphen-digit` boundary:
- `py3-pip-22.0.4-r0` → name=`py3-pip`, version=`22.0.4-r0`
- `ca-certificates-20230506-r0` → name=`ca-certificates`, version=`20230506-r0`

### How the executor works

`POST /v1/packages/update` with body `{"package": "apk:<name>"}`:

1. HTTP handler validates the package name (strict regex — no metacharacters).
2. `UpdateRegistry.Apply()` acquires a `PackageLocker` lock on `("apk", name)`.
3. `ApkUpdateExecutor.Update()` sends an `upgrade` action to pkg-helper.
4. pkg-helper acquires an in-process `sync.Mutex` (serializes all apk ops).
5. pkg-helper runs `apk add -u <name>`. On success, returns `{"ok":true}`.
6. On success, the cache entry for the package is removed; HTTP returns 200.

The per-source `PackageLocker` and the in-process `apkMutex` in pkg-helper
form a two-layer serialization guard:
- `PackageLocker`: prevents concurrent gateway-level operations on the same
  `(source, name)` pair (e.g., dep install + update-apply racing).
- `apkMutex`: prevents concurrent apk database access from any code path
  inside the helper process.

### pkg-helper v2 protocol

The helper uses a JSON line-oriented protocol over `/tmp/pkg.sock`:

**Request:**
```json
{"action": "upgrade", "package": "curl"}
```

**Success response:**
```json
{"ok": true, "data": ""}
```

**Error response:**
```json
{"ok": false, "error": "ERROR: unable to select packages", "code": "not_found"}
```

New v2 fields compared to v1:
- `code` — typed error classification (see Error Classes section)
- `data` — opaque payload for `list-outdated` results
- New actions: `upgrade`, `update-index`, `list-outdated`

v1 callers that omit `code` on error responses receive `system_error` by default
in the client — backward-compat for split deployments where helper is not yet
rebuilt. However, new actions (`upgrade`, `update-index`, `list-outdated`) return
`unknown action` on a v1 helper — feature is degraded, not crashed.

---

## 4. Pre-Release Handling

**Not applicable.** Alpine repositories do not distinguish stable vs pre-release
in the `apk version` output. `apk version -l '<'` lists all packages where the
installed version is older than the repository version. There is no pre-release
channel concept in the Alpine package ecosystem.

The apk checker always reports available upgrades without pre-release filtering.

---

## 5. Availability — Edition × Runtime Truth Table

| Edition | Runtime | `availability.apk` | apk checker registered? |
|---|---|---|---|
| Standard | Alpine (`/etc/alpine-release` present) | `true` | Yes |
| Standard | Debian / Ubuntu | `false` | No (runtime gate) |
| Standard | macOS (dev / testing) | `false` | No (runtime gate) |
| Lite (desktop) | Any | `false` | No (edition gate) |

When `availability.apk = false`:
- `GET /v1/packages/updates` response includes `"availability": {"apk": false}`.
- The frontend hides the apk source from the filter bar.
- `POST /v1/packages/update` with `apk:<name>` returns 503 (source not registered)
  or 409 (Lite edition gate — source never wired).

The runtime check (`/etc/alpine-release` stat) is performed once at checker
initialization and cached. It does not re-probe on subsequent calls.

---

## 6. Error Classes

Sentinel errors are defined in `internal/skills/pkg_update_helpers.go`.
The `code` field in pkg-helper responses maps to these sentinels.

| Sentinel | code value | Trigger |
|---|---|---|
| `ErrInvalidApkPackageName` | `validation` | Package name fails regex (metacharacter, uppercase, etc.) |
| `ErrUpdateApkNotFound` | `not_found` | `apk add -u <name>` reports "unable to select" |
| `ErrUpdateApkConflict` | `conflict` or `constraint` | Dependency conflict / unsatisfiable constraints |
| `ErrUpdateApkLocked` | `locked` | `/var/lib/apk/db.lock` held by another process |
| `ErrUpdateApkNetwork` | `network` | Mirror fetch timeout, DNS failure |
| `ErrUpdateApkPermission` | `permission` | Write permission denied in `/var/lib/apk` |
| `ErrUpdateApkDiskFull` | `disk_full` | No space left on `/var/cache/apk` or `/` |
| `ErrUpdateApkHelperUnavail` | `helper_unavailable` | Socket dial failure (helper not running) |

Unclassified errors (`code=""` or `system_error`) fall back to `ClassifyApkStderr`
pattern matching, then to a generic wrapped error with truncated stderr (≤ 500 chars,
ANSI-stripped before logging).

HTTP status mapping (via `packages_updates.go`):

| Sentinel | HTTP status |
|---|---|
| `ErrInvalidApkPackageName` | 400 Bad Request |
| `ErrUpdateApkNotFound` | 404 Not Found |
| `ErrUpdateApkConflict` | 409 Conflict |
| `ErrUpdateApkLocked` | 409 Conflict |
| `ErrUpdateApkNetwork` | 502 Bad Gateway |
| `ErrUpdateApkPermission` | 500 Internal Server Error |
| `ErrUpdateApkDiskFull` | 500 Internal Server Error |
| `ErrUpdateApkHelperUnavail` | 503 Service Unavailable |

---

## 7. Runbook

### "pkg-helper unavailable" (503)

`/app/pkg-helper` is not running, or `/tmp/pkg.sock` does not exist.

1. Check container logs: `docker logs <container> 2>&1 | grep pkg-helper`
2. Verify the binary exists: `docker exec <container> ls -la /app/pkg-helper`
3. If missing, the Docker image was NOT rebuilt after the pkg-helper v2 upgrade.
   Pull the new image and recreate the container.
4. If the binary exists but the socket is missing, check that the container
   entrypoint starts the helper before the gateway: `ENTRYPOINT ["/app/entrypoint.sh"]`.

Logging: the gateway emits `slog.Info("package.update.apk.unavailable")` when
the helper socket is unreachable. Grep for this key to confirm the symptom.

### "Package database is locked" (409)

`/var/lib/apk/db.lock` is held by another apk process.

1. Wait ~10 seconds and retry — an in-progress `apk add` from the dep-installer
   may still be running (the apkMutex serializes gateway operations, but manual
   `docker exec apk add` from outside the gateway bypasses it).
2. If the lock persists: `docker exec <container> ls -la /var/lib/apk/db.lock`
   — if the owning PID is dead, the lock is stale. Restart the container.
3. Do NOT run `rm /var/lib/apk/db.lock` manually — apk may be mid-write.

Logging: `slog.Warn("package.update.apk.outcome", "code", "locked")`.

### "Disk full" (500)

`/var/cache/apk` or `/` is out of space.

1. Check disk: `docker exec <container> df -h /`
2. Clean cache: `docker exec <container> apk cache clean`
3. Expand the container volume or prune unused images on the host.

### "Dependency conflict" (409)

`apk` cannot resolve dependencies for the requested upgrade.

1. SSH into the container: `docker exec -it <container> sh`
2. Run manually: `apk add -u <name> --simulate` to see the conflict details.
3. Resolution typically requires upgrading a conflicting package first, or
   accepting cascade upgrades. The GoClaw UI warns about cascade risk for
   system packages.
4. If unresolvable, the package must be pinned via Dockerfile `RUN apk add`.

### Debugging helper protocol issues

The helper logs all actions to stderr (`docker logs <container>`). To trace
a specific action:

```bash
# Manual socket test (requires jq on PATH):
echo '{"action":"list-outdated","package":""}' | \
  nc -U /tmp/pkg.sock | jq .
```

Expected response shape:
```json
{"ok": true, "data": "bash-5.2.21-r6 < 5.2.26-r0\n"}
```

---

## 8. Minimum Versions

| Component | Minimum | Notes |
|---|---|---|
| Alpine Linux | 3.19 | `apk version -l '<'` output format stable since 3.12; 3.19 tested |
| apk-tools | 2.14 | Bundled with Alpine 3.19+; older versions may have different `version -l` output |
| pkg-helper | v2 (Phase 2b) | v1 helpers lack `upgrade` / `update-index` / `list-outdated` actions |
| Docker image | Phase 2b build | Image must be rebuilt to include the new pkg-helper binary |

---

## 9. Fixture Regeneration

Test fixtures for the apk parser live in `internal/skills/testdata/`. When the
Alpine version is upgraded and `apk version -l '<'` output format changes:

```bash
# Capture live output from a running container:
docker exec <container> apk update && \
  docker exec <container> apk version -l '<' \
  > internal/skills/testdata/apk_outdated_alpine319.txt

# Verify the parser handles the new format:
go test -run TestParseApkOutdated ./internal/skills/...

# Update test cases in apk_update_checker_test.go to reference the new fixture
# and expected name/version values.
```

Fixture files are named with the Alpine version (`alpine319`) so drift between
CI environments is detectable by `git diff`.

### Updating pkg-helper v2 protocol tests

If the helper wire format changes (new fields, action names):

1. Update `apk_helper_call_test.go` — `servePkgHelper` / `dialHelper` helpers.
2. Update `apk_update_checker_test.go` and `apk_update_executor_test.go` —
   canned response maps.
3. Update `cmd/pkg-helper/main_test.go` — v2 protocol action dispatch tests.
4. Run: `go test ./internal/skills/... ./cmd/pkg-helper/...` to verify.
