---
phase: 4
title: Regression Verification and Ship
status: completed
priority: P1
effort: 1h
dependencies:
  - 3
---

# Phase 4: Regression Verification and Ship

## Overview

Run focused regression checks, update docs if behavior changed, review locally, then ship a beta PR.

## Requirements

- Functional: acceptance criteria from issue #172 are demonstrably covered for tool API, Telegram, and Discord/fallback behavior.
- Non-functional: no syntax/build errors in touched packages.
- Non-functional: no secrets or local env values in plan/issue/PR text.

## Architecture

Verification should stay package-focused unless touched files force broader coverage. Full integration tests are not required because no DB schema or live channel credential path changes.

## Related Code Files

- Modify if warranted: `docs/05-channels-messaging.md`
- Modify if warranted: `docs/project-changelog.md`
- Read: `.github/workflows/ci.yaml` for PR check expectations if local commands differ.

## Implementation Steps

1. Run focused tests:
   - `PATH=/usr/local/go/bin:$PATH go test ./internal/tools ./internal/channels/telegram ./internal/channels/discord ./internal/channels/slack`
2. Run compile checks:
   - `PATH=/usr/local/go/bin:$PATH go test ./internal/channels/... ./internal/tools/...`
   - `PATH=/usr/local/go/bin:$PATH go build ./...`
   - `PATH=/usr/local/go/bin:$PATH go build -tags sqliteonly ./...`
3. Update docs/changelog only if public behavior changed enough to warrant it.
4. Run local pending diff review and fix Critical/Important findings.
5. Commit, push branch, create PR to `dev`, run PR review/fix/reply, then add `ready to ship beta`.

## Success Criteria

- [x] Focused tests pass.
- [x] PG and SQLite builds pass.
- [x] PR targets `dev` and links issue #172.
- [x] Source issue and PR carry `ready to ship beta`; `ready to cook` removed after PR review.

## Risk Assessment

`go build ./...` may expose pre-existing platform/toolchain issues in unrelated packages. If so, record exact package/error and still run focused touched-package verification.
