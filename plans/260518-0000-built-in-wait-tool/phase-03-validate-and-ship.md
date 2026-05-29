---
phase: 3
title: "Validate and Ship"
status: complete
effort: "1h"
---

# Phase 3: Validate and Ship

## Overview

Validate the implementation with focused Go tests, compile checks, adversarial review, scoped commit/push, and beta PR against `dev`.

## Implementation Steps

1. Run focused tests:
   - `go test ./internal/tools -run "TestWaitTool|TestToolGroups|TestInferMetadata"`
   - `go test ./internal/store -run TestParseToolsConfig`
   - `go test ./cmd -run BuiltinTool`
2. Run compile checks:
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
3. Run code review on changed files and address correctness findings.
4. Update docs/changelog only if implementation changes user-facing admin docs.
5. Stage only plan + implementation files; run `git diff --cached --check` and staged secret scan.
6. Commit with conventional message, push `codex/feat-wait-tool`, and create PR to `digitopvn/goclaw:dev`.

## Success Criteria

- [x] Focused tests pass.
- [x] Both PG and SQLite builds pass, or unrelated baseline failures are documented with evidence.
- [x] Code review has no unresolved critical/high correctness findings.
- [x] PR links issue #1097 and targets `dev`.

## Unresolved Questions

- Should long waits emit user-facing progress later? Deferred for v1 unless reviewer finds existing status callback.
- Should `wait_until` be a separate future tool? Deferred.
