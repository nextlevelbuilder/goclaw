---
phase: 5
title: docs-ship
status: completed
effort: S
---

# Phase 5: docs-ship

## Overview

Update docs and run the requested git/ship flow.

## Implementation Steps

1. Update `docs/git-credential-adapter.md` and `docs/project-changelog.md`
   with the Agent Access flow and PAT/SSH validation fixes.
2. Run focused tests:
   - `go test ./internal/tools ./internal/http`
   - `cd ui/web && pnpm test -- cli-credentials`
3. Run compile checks appropriate for touched code:
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
   - `cd ui/web && pnpm build`
4. Stage, secret-scan, commit, push, open PR to `dev`, wait for checks, reply to
   issue #117 with evidence, and merge if checks pass.

## Success Criteria

- [ ] Plan validates with `ck plan validate --strict`.
- [ ] Red-team report has no unresolved blocker.
- [ ] Focused tests and compile checks pass or any external blocker is recorded.
- [ ] PR links issue #117 and includes validation evidence.
