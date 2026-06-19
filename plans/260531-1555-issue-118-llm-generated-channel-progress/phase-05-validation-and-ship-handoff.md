---
phase: 5
title: Validation and Ship Handoff
status: completed
priority: P1
effort: ''
dependencies:
  - 1
  - 2
  - 3
  - 4
---

# Phase 5: Validation and Ship Handoff

## Overview

Run focused and broad validation, update plan status/docs, ship a beta PR, run PR review/fix loop, and report back on issue #118.

## Requirements

- Functional: every acceptance criterion in `plan.md` is validated by tests, build, or explicit review.
- Functional: issue #118 gets plan comment, implementation report, branch/PR link, and label transition.
- Non-functional: do not push secrets or unrelated changes.

## Architecture

This phase is process-only. It uses the existing GitHub CLI workflow and repo validation commands. Beta shipping targets `dev` because README describes `dev` branch pushes as beta release path.

## Related Code Files

- Modify: plan status files in this plan directory
- Modify: docs only if implementation changes public channel behavior
- No runtime files unless review finds actionable issues

## Implementation Steps

1. Run focused Go tests:
   `go test ./internal/channels ./internal/config ./internal/gateway/methods`
2. Run SQLite-tag focused tests:
   `go test -tags sqliteonly ./internal/channels ./internal/config ./internal/gateway/methods`
3. Run broad compile/static checks:
   `go build ./...`
   `go build -tags sqliteonly ./...`
   `go vet ./...`
4. Run web validation:
   `cd ui/web && pnpm test -- --run`
   `cd ui/web && pnpm build`
5. Run `git diff --check` and inspect `git status --short`.
6. Commit and push implementation with `ck:git cp` semantics.
7. Create beta PR to `dev` and link issue #118.
8. Run review-pr fix loop until no actionable findings remain.
9. Comment issue #118 with implementation summary, PR URL, validation status, and final label update.

## Success Criteria

- [ ] All focused tests pass.
- [ ] PG and SQLite build pass.
- [ ] Web build passes.
- [ ] PR exists against `dev`.
- [ ] Review/fix loop reports approve or no actionable findings.
- [ ] Issue label moves from `ready to implement` to `ready to ship beta`.

## Risk Assessment

Risk: broad integration/race tests require local services and may be unavailable. Mitigation: run all local deterministic checks; report any unavailable service-backed checks honestly.
