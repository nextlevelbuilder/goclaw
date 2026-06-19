---
phase: 3
title: "Validate Security and Ship"
status: complete
effort: "2h"
---

# Phase 3: Validate Security and Ship

## Context Links

- Phase 2 implementation: `./phase-02-implement-tenant-scoped-sandbox-mounts.md`
- Post-implementation checklist: `CLAUDE.md`
- Related issue: https://github.com/digitopvn/goclaw/issues/68

## Overview

Validate the P0 isolation fix locally, review with security focus, update changelog, then ship through the normal `dev` PR path.

## Requirements

- Fresh verification only; no completion claim from stale test output.
- No load/stress/benchmark tests.
- No dotenv or secret files staged.
- Both PostgreSQL and SQLite builds must compile because sandbox code is shared by standard and desktop builds.

## Implementation Steps

1. Run focused tests:
   - `go test ./internal/tools -run 'Sandbox|ExecTool|ReadFile|WriteFile|ListFiles|Edit'`
   - `go test ./internal/sandbox -run 'Docker|FsBridge|Workspace|Path'`
2. Run compile/static checks:
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
   - `go vet ./...`
3. If implementation changes schema or user-facing docs, update both roadmap/changelog docs per repo rules.
4. Run security review focused on tenant isolation, sandbox reuse, symlink/absolute-path escapes, and fail-open fallback.
5. Stage only relevant code, tests, and docs; run:
   - `git diff --cached --check`
   - `git diff --cached | grep -iE '(api[_-]?key|token|password|secret|credential)'`
6. Commit with conventional message and push branch.
7. Create PR to `digitopvn/goclaw:dev`, link issue #68, and monitor CI.

## Success Criteria

- Complete: focused sandbox tests pass and cover effective mount selection plus cache-key isolation.
- Complete: `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, and `go test -race -tags integration ./tests/integration/` pass locally.
- Complete: changelog security note added in `docs/project-changelog.md`.
- Pending ship actions: PR creation and issue #68 comment/label after push.

## Risk Assessment

- CI may show pre-existing flaky `TestShellAbort_ProcessGroupKilled`; only treat as unrelated after local reproduction and check log proof.
- Docker-dependent tests can flake on CI. Prefer unit tests around args/workspace selection plus one optional integration test if harness exists.

## Security Considerations

- Verify no mount path falls back to global workspace for tenant-scoped runs.
- Verify sandbox `WorkspaceAccess=rw` cannot modify sibling tenant data because sibling data is absent from the mount.

## Unresolved Questions

- None for validation.
