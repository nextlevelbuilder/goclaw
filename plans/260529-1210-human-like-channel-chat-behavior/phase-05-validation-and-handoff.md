---
phase: 5
title: "Validation and Handoff"
status: pending
priority: P1
effort: "0.5d"
dependencies: [1, 2, 3, 4]
---

# Phase 5: Validation and Handoff

## Overview

Run focused and broad validation, update docs/changelog if warranted, then prepare beta PR and issue handoff.

## Requirements

- Functional: all acceptance criteria proven by tests or build artifacts.
- Non-functional: no syntax/build errors in Go PG, Go sqliteonly, or web UI.
- Non-functional: no public contract break without explicit callout.

## Architecture

Validation follows repo checklist, with focused package tests first and broader compile gates after.

Docs impact expected:
- `docs/project-changelog.md` entry for feature.
- `docs/05-channels-messaging.md` update only if runtime behavior changes channel semantics enough to document.

## Related Code Files

- Modify: `docs/project-changelog.md` if implementation lands.
- Maybe modify: `docs/05-channels-messaging.md`.
- GitHub issue: `digitopvn/goclaw#67`.

## Implementation Steps

1. Run focused backend tests:
   `go test ./internal/channels ./internal/config ./internal/gateway/methods`
2. Run sqliteonly focused tests:
   `go test -tags sqliteonly ./internal/channels ./internal/config ./internal/gateway/methods`
3. Run compile/static gates:
   `go build ./...`
   `go build -tags sqliteonly ./...`
   `go vet ./...`
4. Run web gates:
   `cd ui/web && pnpm test -- --run`
   `cd ui/web && pnpm build`
5. Run `git diff --check`.
6. Update issue #67 with implementation summary and validation.

## Success Criteria

- [ ] All focused and broad validation commands pass or documented blocker exists.
- [ ] PR targets `dev` for beta shipping.
- [ ] Issue #67 has final comment and `ready to ship be` label after review/fix loop.

## Risk Assessment

Risk: full integration race tests may require external DB. Mitigation: run repo-standard compile/unit gates locally; only run integration DB tests if environment available and relevant.
