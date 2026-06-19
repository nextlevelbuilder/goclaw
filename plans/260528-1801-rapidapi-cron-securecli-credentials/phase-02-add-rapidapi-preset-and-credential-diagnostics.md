---
phase: 2
title: "Add RapidAPI Preset and Credential Diagnostics"
status: complete
priority: P1
effort: "2.5h"
dependencies: [1]
---

# Phase 2: Add RapidAPI Preset and Credential Diagnostics

## Context Links

- Preset registry: `internal/tools/credential_presets.go`
- SecureCLI execution: `internal/tools/credentialed_exec.go`
- Env parsing/flattening: `internal/store/secure_cli_env.go`
- UI consumes presets through existing SecureCLI preset endpoints.

## Overview

Add the smallest product support needed for RapidAPI cron use: a SecureCLI preset and precise diagnostics. Do not add RapidAPI-specific runtime branches.

## Key Insights

- SecureCLI presets are static templates only; adding `rapidapi` is low blast radius.
- Credentialed exec logs currently show lookup found/no found, but not enough operator-safe reason detail.
- Host fall-through exec scrubs credential env keys; that is correct and should not be bypassed.
- 2026-05-29 implementation:
  - Added built-in `rapidapi` preset with required `RAPIDAPI_KEY`.
  - Added required preset env validation before binary resolution/execution.
  - Added safe env-key-only diagnostics for merged env and missing required keys.
  - Added `RAPIDAPI_KEY` to fall-through exec env scrub list.

## Requirements

- Functional: `rapidapi` appears in preset list with required `RAPIDAPI_KEY`.
- Functional: credentialed exec logs safe metadata for lookup and injection attempt.
- Functional: missing/empty merged env yields actionable error, not only downstream `RAPIDAPI_KEY required`.
- Non-functional: no secret values in logs; key names allowed.
- Non-functional: no cron API contract change.

## Architecture

Use existing SecureCLI:

```text
Preset -> admin creates binary -> optional agent grant -> optional user env -> exec direct argv mode
```

Diagnostics live at two layers:

- Lookup layer: found config? agent grant? credential user id present?
- Injection layer: merged env key names count, required key missing, binary path resolution.

## Related Code Files

- Modify: `internal/tools/credential_presets.go`
- Modify: `internal/tools/credentialed_exec.go`
- Modify: tests near `internal/tools/credential*_test.go`
- Modify if needed: `docs/project-changelog.md`
- Avoid unless required: DB migrations, cron schema, HTTP API contracts.

## Implementation Steps

### Tests Before

1. Add preset test:
   - `GetPreset("rapidapi")` exists.
   - env vars include required `RAPIDAPI_KEY`.
   - deny patterns avoid interactive/export/debug secret leakage if RapidAPI CLI has such commands.
2. Add credentialed env validation test:
   - `rapidapi` config with no env returns an actionable missing-env error before executing binary.
   - `rapidapi` config with `RAPIDAPI_KEY` proceeds to binary resolution/execution path.
3. Add logging behavior test only where stable without asserting log formatting too tightly.

### Refactor / Implementation

4. Add `rapidapi` preset:
   - `BinaryName: "rapidapi"`
   - `EnvVars: RAPIDAPI_KEY`
   - conservative timeout, e.g. 60s.
   - tips: use read-only/list/query commands; avoid debug output.
5. Add optional required-env guard for preset-required keys if current SecureCLI model supports it without schema change.
   - If schema change would be needed, skip guard and rely on diagnostics/logging; YAGNI.
6. Improve operator-safe diagnostics:
   - log binary, agent id, tenant id, credential user id presence, env key names only.
   - distinguish no credential row vs found row with empty env.
   - never log env values.
7. Ensure non-credentialed fallback remains scrubbed and blocked for registered non-global binaries.

### Tests After

8. Run focused tests:
   - `go test ./internal/tools`
   - `go test ./internal/store`
9. Run compile gate:
   - `go build ./...`

## Todo List

- [x] Preset regression test first.
- [x] Missing-env diagnostic test first.
- [x] Add minimal preset.
- [x] Add safe diagnostics.
- [x] Update changelog if code changes.

## Success Criteria

- [x] `rapidapi` preset available through existing preset API.
- [x] Missing `RAPIDAPI_KEY` is distinguishable from policy block and binary-not-found.
- [x] Logs expose only safe metadata.
- [x] Existing SecureCLI security tests still pass.

## Validation

- `go test ./internal/tools -run 'RapidAPI|MissingRequiredEnv|MergeCredentialedEnv'`
- `go test ./internal/tools`

## Risk Assessment

- Risk: required-env guard changes all SecureCLI behavior. Mitigation: only add if scoped and test-protected; otherwise skip.
- Risk: logs leak identity detail. Mitigation: no values, no command args beyond existing truncation, key names only.
- Risk: RapidAPI CLI binary name differs. Mitigation: verify command package before final implementation; if command is not `rapidapi`, update preset name to real binary.

## Security Considerations

- Keep direct argv execution for credentialed mode.
- Do not support shell wrappers for RapidAPI credentials.
- Deny verbose/debug options if RapidAPI CLI can print HTTP headers.

## Next Steps

- Phase 3 validates cron path end-to-end and prepares GitHub issue reply.
