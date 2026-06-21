---
phase: 4
title: "Validate Document and Ship"
status: in_progress
priority: P1
effort: ""
dependencies: [3]
---

# Phase 4: Validate Document and Ship

## Context Links

- Project docs: `docs/03-tools-system.md`, `docs/01-agent-loop.md`, `docs/10-tracing-observability.md`
- Post-implementation checklist: `CLAUDE.md`
- GitHub issue: digitopvn/goclaw#81

## Overview

Run focused and repo-level verification, update docs if behavior wording changes, then prepare the branch for PR/issue handoff.

## Requirements

- Functional: all acceptance criteria from issue #81 covered by tests or documented constraints.
- Non-functional: no flaky load/stress/benchmark tests.
- Non-functional: docs describe conservative read-only-only default.

## Architecture

Validation stays layered:
- focused unit tests first
- agent/tool package tests next
- compile checks for PostgreSQL and SQLite builds
- docs update only for changed runtime semantics

## Related Code Files

- Modify if needed: `docs/03-tools-system.md`
- Modify if needed: `docs/01-agent-loop.md`
- Modify if needed: `docs/10-tracing-observability.md`
- Read: `CLAUDE.md`

## Implementation Steps

1. Run focused tests:
   - `go test ./internal/pipeline`
   - `go test ./internal/agent`
   - `go test ./internal/tools`
2. Run compile checks:
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
3. Run `go vet ./...` if compile checks pass.
4. Update docs only where current docs overstate behavior or omit new conservative eligibility.
5. Review diff for accidental schema/UI/config changes.
6. Commit and push implementation.
7. Open PR against `dev`.
8. Reply to issue #81 with PR link, validation proof, and any out-of-scope follow-up.

## Success Criteria

- [x] Focused tests pass.
- [x] PostgreSQL and SQLite builds pass.
- [x] `go vet ./...` passes or any existing unrelated failure is documented with evidence.
- [x] Docs match shipped behavior.
- [ ] GitHub issue has implementation summary and PR link.

## Risk Assessment

Risk: full integration tests require external PostgreSQL/pgvector. Mitigation: do focused unit/build checks here; run integration only if a touched path warrants DB coverage and environment exists.

## Security Considerations

Verify no new config, secrets, or credential values are committed. Keep mutating tool fallback conservative.

## Next Steps

After PR is open, watch CI and fix failures. Do not mark issue complete until PR is merged or maintainer accepts the plan.
