# Phase 2b: Alpine APK Update Flow + pkg-helper v2 Protocol

**Date**: 2026-04-20 09:25
**Severity**: High (breaking protocol change)
**Component**: Packages update system (Alpine APK, pkg-helper IPC)
**Status**: Resolved

## Context

Completed Phase 2b of the packages-update feature: Alpine `apk` package update flow via privileged pkg-helper daemon. Commit `8fd0ba9f` merged to `feat/packages-update-phase2b-apk-pkghelper`. Feature gates at Standard/Full edition only (Lite unsupported). Stacked on Phase 2a (pip/npm) which was still unmerged at implementation time.

## Key Technical Decisions

**1. Non-root gateway → privileged helper for all apk ops**
- Initial scout assumed only write operations (`apk add/upgrade`) needed root. Audit revealed **both read and write are privileged**: `apk update` (fetch index) and `apk list --upgradable` (scan outdated) fail as uid 1000.
- Solution: route ALL apk CLI through helper IPC. Simpler than fine-grain permission escalation.

**2. pkg-helper v2 = breaking protocol atomic bump**
- Added `code`/`data` response fields (structured error classification + payload return).
- Expanded from 2 actions (install/uninstall) to 5: check_apk, check_pip, check_npm, exec_apk, exec_pip.
- No version field, no backward compatibility shim. Container/desktop upgrade boundary makes atomic rebuild cheap.

**3. Renewable 10-minute deadline instead of removing 30s**
- Red-team flagged: removing deadline lets maxConns=3 semaphore starvation cause indefinite hangs (DoS).
- Compromise: set before scanner loop, **renew per successful Scan**. Allows slow apk operations without exposing process-wide timeout bypass.

**4. Process-wide apkMutex inside helper**
- Alpine apk database is single-writer; `/var/lib/apk/db.lock` conflicts if gateway sent parallel requests.
- Helper serializes at apk boundary instead of retry loops in gateway.

**5. Executor acquires NO locks**
- `UpdateRegistry.Apply()` already holds `PackageLocker` (non-reentrant chan).
- Re-acquiring would deadlock. Documented in header; planner initially missed this pattern.

**6. Public SetAvailability() wrapper**
- Standard edition on non-Alpine host must emit `availability.apk=false` for UI (show "not applicable").
- Lite skips both registration and availability marker (key absent in response).

**7. Edition double-gate: compile-time + runtime**
- `edition.Current().SupportsApk && skills.IsAlpineRuntime()` — both must hold.
- Standard-Debian variants pass edition gate but fail `/etc/alpine-release` check.
- Lite on Alpine fails edition gate (even if runtime check passes).

**8. APK name regex allows `+` for libstdc++, gtk+3.0**
- Separate `validApkName` (stricter, lowercase-only) for apk-specific grammar.
- Keep historical `validPkgName` for install/uninstall (pip/npm cross-runtime compat).

## Red-Team Audit Catches (Pre-Code)

4 blocking issues surfaced in plan validation (trust-but-verify pattern, before Phase 1 started) — all resolved in phase files before implementation:

| Issue | Root Cause | Resolution |
|-------|-----------|-----------|
| C-1: Executor self-deadlock | Planner instructed to re-acquire PackageLocker | Removed re-acquire; document PackageLocker already held |
| C-2: No editor for availability map | SetAvailability() wrapper missing | Added public wrapper; wiring calls for Standard+non-Alpine |
| H-1: Deadline removal DoS | Naive removal of 30s cap | Renew-per-scan instead of unconditional remove |
| H-2: Zero-value edition silently disables | Default `bool` == false | Explicit `edition.SupportsApk = true` in Standard/Full presets |

## Outcomes

- **3,212 insertions**, 37 files modified
- **97/97 tests passing** (37.9s total, 0 race condition warnings)
- **Reviewer verdict**: APPROVE (0 critical/high/medium, 3 low cosmetic)
- Full stack: gateway wiring → checker → executor → helper protocol → frontend source pill

## Lessons

1. **Dockerfile verdict comes before code.** Permission model assumptions from package docs often diverge from actual runtime uid/gid. Inspect entrypoint and compare with runtime context (1000 vs 0).

2. **Breaking protocol changes are cheapest at atomic-rebuild boundaries.** Desktop/container upgrade boundaries make v1→v2 protocol jumps viable; avoid wire-compat shims unless two-operator rolling upgrade is in scope.

3. **Trust-but-verify Red-Team pattern works.** Scout → Planner → Red-Team audit (before token spend on implementation) caught structural deadlock and missing primitives. Prevented rework post-code.

4. **Renewable deadlines trade sophistication for safety.** Removing fixed timeout entirely opens DoS; renewing per-success-item lets slow operations complete while preventing starvation-based indefinite hangs.

5. **Edition double-gate (compile + runtime) beats runtime-only.** Catches mismatched environment early (Standard-Debian, Lite-Alpine) instead of silent availability glitches in production.

## Next Steps

- Phase 2b stacked on unmerged Phase 2a; await Phase 2a merge to main for CI/CD.
- Desktop .dmg release will auto-detect Alpine (via /etc/alpine-release sync.Once) and show apk sources in update UI.
- Standard edition: if deployed to non-Alpine, apk source shows "unavailable" (availability=false) instead of hidden.

**Unresolved**: none.

**Status**: DONE
