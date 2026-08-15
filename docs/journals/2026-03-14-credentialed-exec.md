# Credentialed Exec Feature: How We Nearly Shipped With Sandbox Blindness

**Date**: 2026-03-14 14:02
**Severity**: High (caught in implementation phase)
**Component**: Shell execution, sandbox mode, credential handling
**Status**: Resolved

## What Happened

Implemented the Credentialed Exec feature (GH-197) allowing authenticated users to execute binaries with stored credentials. The feature shipped in ~1 hour with full test coverage and code review. But we almost missed a critical gap: the original requirements report failed to address sandbox mode's identical vulnerability to direct exec mode.

## The Brutal Truth

This is exactly how security bugs survive: requirements look complete, design review approves them, and implementation teams optimize for speed. We got lucky because we launched aggressive gap analysis before touching code. If we'd followed the original report blindly, production would ship with two injection vectors instead of one. That's the kind of mistake that keeps me up at night—not because we made it, but because it's so easy to make.

## Technical Details

### The Gap (GAP 1 - CRITICAL)
Original report addressed shell injection in **direct exec mode** (`sh -c "$cmd"`) but was **silent on sandbox mode**. Sandbox also uses shell:
```go
docker exec CONTAINER sh -c "command here"
```
This means credentials could be injected via the same shell metacharacter attack. The fix: apply the same Direct Exec pattern to sandbox by passing arguments as env vars via `docker exec -e CRED=value`, bypassing the shell entirely.

### Secondary Gaps
**GAP 2**: Windows compatibility wasn't mentioned. Shell syntax differs; `sh -c` doesn't exist on Windows. Solution: detect OS, use native shell (cmd.exe, PowerShell, sh).

**GAP 3**: Argument parsing. If a credential contains escaped quotes, naive shell-word splitting breaks. Implemented: `go-shellwords` library for robust tokenization.

## What We Tried

1. **Read the final report** → Found it incomplete (3 critical gaps identified)
2. **Launched parallel scouts** → Explored shell.go, sandbox impl, store patterns
3. **Created 7-phase plan** → Planned fixes before implementation
4. **Code review cycle** → 8.5/10; caught 3 issues (scrub values, field allowlist, preset sorting)
5. **All tests passed** → Build, vet, race detector clean

## Root Cause Analysis

The requirements report was written by analyzing static code patterns without dynamic execution context. It answered "how is shell.go currently used?" but didn't ask "where else could this vulnerability exist?" This is a classic requirements failure: domain knowledge gap + checklist-driven thinking = incomplete threat model.

Also: the team optimized for velocity. Nobody said "let's pause and audit the sandbox implementation"—it took explicit gap analysis tasks to surface the issue. Good thing we built that friction point into the workflow.

## Lessons Learned

1. **Requirements aren't designs.** A completed requirements doc doesn't mean you've found all the issues. Treat requirement completeness as a working hypothesis, not ground truth.

2. **Scout parallel gaps early.** We spawned 3 scouts in 5 minutes to explore different code areas. Cost: nothing. Benefit: surfaced sandbox gap before any implementation. This pattern works.

3. **Ask "where else?"** After identifying a vulnerability class (shell injection), systematically hunt for all code paths using that pattern. Don't assume the requirements author did this.

4. **Separate concerns ruthlessly.** Putting credentialed exec logic in a separate `credentialed_exec.go` file instead of patching `shell.go` made the fix self-documenting. Future developers can see at a glance: "oh, this binary has credentials, so it uses Direct Exec mode."

5. **Direct Exec beats sanitization.** We could've tried to escape shell metacharacters (fragile, OS-specific). Instead, we bypassed the shell entirely. This is a general principle: remove the dangerous component rather than trying to use it safely.

## Metrics

- **Feature completeness**: 7/7 phases ✓
- **Code review**: 8.5/10 (3 medium issues fixed)
- **Test coverage**: all paths covered
- **Security issues found**: 1 critical, 3 medium (all fixed)
- **Build status**: green (Go + React)
- **Files changed**: 38 files, ~2000 lines
- **Time-to-ship**: ~1 hour (brainstorm + plan + impl + review)

## Next Steps

1. Merge branch `feat/197-credentialed-exec` to main
2. Update `docs/project-changelog.md` with feature entry
3. Update `docs/development-roadmap.md` progress
4. Post-implementation: monitor usage patterns in production for any edge cases we missed

---

**Key Decision**: Direct Exec mode (bypass shell) + env-var injection in sandbox = eliminates the injection vector entirely rather than trying to sanitize shell input. Credentialed binaries auto-bypass approval flow because they're trusted by definition.
