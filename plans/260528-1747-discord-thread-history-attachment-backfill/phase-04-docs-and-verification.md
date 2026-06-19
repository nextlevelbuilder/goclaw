---
phase: 4
title: "Docs and Verification"
status: complete
priority: P2
effort: "2h"
dependencies: [3]
---

# Phase 4: Docs and Verification

## Overview

Document the new Discord thread behavior and run focused verification for compile and regression safety.

## Requirements

- Functional: docs explain that Discord thread mention triggers bounded REST history backfill.
- Non-functional: no misleading promise for channels/groups, no claim that history works without Discord `READ_MESSAGE_HISTORY`.

## Architecture

Docs live in existing channel docs. Verification follows repo checklist but scoped to files touched unless a broader compile catches integration issues.

## Related Code Files

- Modify: `docs/05-channels-messaging.md`
- Optional: `docs/project-changelog.md` if implementation is considered significant user-facing fix
- Run: `go test ./internal/channels/discord ./internal/agent`
- Run: `go build ./...`
- Run: `go build -tags sqliteonly ./...`

## Implementation Steps

1. Update Discord section with thread history backfill behavior, bounds, and permission caveat.
2. Add changelog entry if implementation changes user-facing Discord behavior.
3. Run focused tests for Discord and agent media packages.
4. Run Go compile checks for PG and SQLite builds.
5. If tests need live Discord, do not add them; keep live verification as manual note.
6. Reply to GitHub issue #69 with implemented behavior and verification commands after code is done.

## Success Criteria

- [x] Docs mention Discord thread-only scope.
- [x] Focused tests pass.
- [x] `go build ./...` passes.
- [x] `go build -tags sqliteonly ./...` passes.
- [x] GitHub issue reply includes summary and verification.

## Risk Assessment

Risk: full integration tests require local Postgres/pgvector and may be unavailable. Mitigation: run compile + focused unit tests; list skipped integration tests explicitly.
