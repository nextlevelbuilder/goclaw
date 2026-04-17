---
date: 2026-04-16
branch: feat/packages-update-flow
issue: nextlevelbuilder/goclaw#900
plan: plans/260415-1400-packages-update-flow/
status: shipped
severity: High
---

# Packages Update Flow Phase 1: What Went Wrong (And How We Caught It)

**Date**: 2026-04-16 16:35
**Issue**: [#900](https://github.com/nextlevelbuilder/goclaw/issues/900)
**Branch**: `feat/packages-update-flow`
**Completion**: 8 phases, 3.2k LOC, ship blockers identified and fixed before merge

## What We Built

Proactive update checker + atomic binary swap for GitHub-installed packages. ETag-based polling eliminates redundant GitHub API calls; SWR cache serves stale updates in background while refresh happens off-thread. Atomic `.bak`-rename swap ensures install↔update serialization and guaranteed rollback on failure. Interfaces ready for pip/npm/apk in Phase 2.

All 16 pre-flight hardening items from red-team review landed in code. Tests pass `-race`. Build works under both PostgreSQL and SQLite (`sqliteonly`) tags.

## What Went Wrong (And How We Caught It)

### CRIT-1: Double-Write HTTP Response on Invalid JSON Body

**Symptom**: Malformed JSON in `POST /v1/packages/apply-all` produces valid 200 response instead of 400 validation error.

**Root Cause**: `bindJSON(w, r, locale, &req)` writes its own 400 response on decode failure AND returns false. Handler ignored the bool (`_ = bindJSON(...)`), assumed empty body was valid, and executed with zero packages selected. Result: two HTTP status codes written, silent "apply everything" on corrupt input.

**Fix**: Read body into buffer first, check for empty explicitly (Content-Length 0 or io.EOF), skip JSON decode if empty, else call bindJSON with mandatory success. Three lines, compiles clean.

**Lesson**: Helpers that both write-and-return should never be called with `_ = ...`. Linter could catch this pattern (`"ignoring bool return from func that writes"`).

---

### CRIT-2: Lock-Key Divergence Between Installer and Update Executor

**Symptom**: Concurrent install of `cli/cli@vX` + update of `gh → vY` both execute without serialization, racing on manifest file.

**Root Cause**: Installer acquires lock on `parsed.Repo` ("cli/cli" → key `"github:cli"`). Executor acquires lock on the manifest `Name` via registry (`"github:gh"`). When `canonicalPackageName()` diverges, the "shared" PackageLocker doesn't actually serialize — they acquire different mutexes. The installer's internal `sync.Mutex` protects manifest writes, so data survives, but the invariant "one install/update per package at a time" is broken.

**Fix**: Both paths lock on the repo-portion of the spec, not the canonical name. Executor loads entry first, extracts repo, derives lock key from that. Both installer and executor now key by Repo — they serialize.

**Lesson**: "Shared locker" is a lie if the KEY is not shared. Document the key derivation rule explicitly. Unit test the rule: concurrent install+update on same package via both name and repo lookup should block.

---

### CRIT-3: Two-Phase Swap Rollback False-Alarms on Fresh Installs

**Symptom**: First-time package install, then update attempted → update fails mid-swap → rollback logs spurious `ENOENT` errors that wake ops, even though update failure was unrelated (e.g., download timeout).

**Root Cause**: Phase A (backup old binaries) skips entries where `os.Stat(dest)` returns ENOENT (fresh install). But Phase A still appends them to the rollback list. Phase B (move new binaries) then fails. Rollback code unconditionally calls `os.Rename(backup, dest)` for every entry — including ones where `backup` never existed, producing "rename ErrNotExist" logs. Alarm system treats these as rollback failures.

**Fix**: Add `hadBackup bool` flag to each swap target. Set true only after a real rename succeeds. Rollback skips where false. One extra bool per target, idempotent.

**Lesson**: Separate the "nothing to restore" branch from the "happy path." Don't let successful skips contaminate the rollback list. Think about the all-paths (nothing to backup, backup succeeds, backup fails, new fails, rollback succeeds, rollback fails) separately.

---

### HIGH-1: Lock Key Acquisition Spans Context Lifetime

**Symptom**: Acquire returns `(release, error)` but if ctx cancels after acquire, the release closure is never called, leak persists until goroutine exit.

**Root Cause**: `Acquire(ctx, source, name)` spawns a goroutine to monitor ctx cancelation. If ctx cancels before release() call, the release closure is never called by the caller. The monitor goroutine is never notified, lock never released.

**Fix**: `Acquire` uses `sync.Once` inside the release closure to make it idempotent; caller MUST `defer release()` immediately. Done. Tests verify defer pattern under context cancellation.

**Lesson**: Composable locks that return release closures should have single-call-only semantics. Document "must defer immediately." Test the defer+cancel path explicitly.

---

### HIGH-4: ETag Keyspace Collision Between Two Endpoints

**Symptom**: Pre-release user on `v1.0.0-rc.1` → GitHub releases stable `v1.0.0` → refresh checks both `/releases/latest` and `/releases?per_page=5` endpoints. ETag cache stored under one key ("lazygit"), so second endpoint 304 cache-hit masks the fact that latest changed.

**Root Cause**: `cache.GitHubETags["repo"]` used for both endpoints. Endpoints are independent resources with separate ETags. Storing both under one key means second endpoint's cache-hit shadows first endpoint's new data.

**Fix**: Two distinct keys: `cache.GitHubETags[repo]` and `cache.GitHubETags[repo + ":list"]`. Endpoints now have separate cache entries.

**Lesson**: Every GitHub endpoint is a resource with its own ETag. Do not alias. Document the key schema in the cache struct comment.

---

### MED: Pre-Release Transition Requires Semver Ordering

**Symptom**: User on `v1.0.0-rc.1`, stable `v1.0.0` released. Regex pre-release check (`(?i)-(alpha|beta|rc|...)`) flags current as pre-release, triggers dual-fetch. Naive string comparison would say `"v1.0.0-rc.1" < "v1.0.0"` is false (ASCII).

**Root Cause**: Pre-release handling was correct but the selector (`pickNewestRelease`) needed semver.Compare, not string inequality.

**Fix**: Import `golang.org/x/mod/semver`, use `semver.Compare(tag1, tag2)` for both-semver case. Falls back to string inequality for non-semver tags. Both functions return correct ordering.

**Lesson**: Check what production tools (Dependabot, Renovate) do before inventing ordering. Semver 2.0 has a clear spec; use it.

---

## Design Decisions That Paid Off

1. **Separate cache file** (not manifest bloat) — `/app/data/.runtime/updates-cache.json` is atomic tmp+rename, never touched by uninstall. Manifest path stays clean.

2. **Keyed lock shared between installer and update path** — Prevents install↔update race at logical boundary (locker key), not internal mutex. Extensible to pip/npm/apk in Phase 2 (all register checkers/executors with shared locker).

3. **SWR with `context.WithoutCancel`** — Background refresh on its own context, never blocks GET. Caller sees cache immediately + age metadata, decides staleness tolerance.

4. **ETag preservation verbatim** — Weak ETags kept with `W/` prefix, sent as-is in `If-None-Match`. No normalization, no parsing — delegates to GitHub's 304 logic.

5. **Rollback per-binary, not per-package** — Each binary swap is atomic; partial failure still leaves manifest consistent (we never write manifest until ALL binaries are moved). Forensic trace via `.failed-<ns>` dir.

6. **Red-team review pre-implementation** — 16 critical/high findings applied to plan before coding started. Post-implementation code review caught 3 more criticals. Total ~19 potential-production-bugs, caught before PR.

7. **Subagent parallelism worked** — Phase 4 (HTTP) + Phase 5 (events/i18n) + Phase 6 (frontend) ran in parallel; no file-ownership overlap. Combined context ~190K, fit well.

---

## Lessons for Phase 2

- Lock-key derivation is a contract. Document it in registry interface.
- Every HTTP endpoint has its own ETag; don't deduplicate.
- Helpers that write + return error should never be silently ignored; design API to prevent `_ = ...` pattern.
- Pre-release detection is simple; semver ordering is not — always use stdlib or battle-tested lib.
- Atomic swaps need explicit "nothing to swap" handling in rollback paths.

---

## Stats

| Metric | Count |
|--------|-------|
| Backend files created | 6 |
| Backend files modified | 8 |
| Frontend files created | 4 |
| Frontend files modified | 2 |
| Test files | 5 |
| Net LOC additions | 3,200 |
| Unit tests | 45+ |
| Integration tests | 1 |
| Benchmark tests | 2 |
| Build pass (PG + SQLite) | ✓ |
| `go vet` clean | ✓ |
| `-race` clean | ✓ |
| Code review status | APPROVE_WITH_CONDITIONS (3 critical fixes applied) |
| Red-team findings addressed | 16/16 |

---

## Open Questions / Tech Debt

1. **Multi-replica cache coherence**: Two gateway replicas share `/app/data/.runtime/updates-cache.json` — will race on `SaveUpdateCache`. Current single-process gateway is fine; document as invariant or add fd-lock.

2. **GitHubPackagesConfig.GitHubToken source**: Phase 1 stubs the field in JSON5. Phase 2 plan says env-only. Remove JSON field now or clarify intent.

3. **Secondary rate-limit ripple**: When `Check` aborts mid-sweep, partial Updates list is cached, so UI "forgets" already-known updates. Intended UX or should registry preserve prior Updates?

4. **Apply-all failure ordering**: Results preserve original slice order. Intentional? If so, document or implement stable ordering.

---

**Shipped**: 2026-04-16. All critical issues fixed. Ready for PR merge and Phase 2 (pip/npm/apk).
