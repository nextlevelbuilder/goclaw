---
phase: 3
title: "Regression Validation and Issue Handoff"
status: complete
priority: P2
effort: "0.5d"
dependencies: [1, 2]
---

# Phase 3: Regression Validation and Issue Handoff

## Context Links

- API docs: `docs/18-http-api.md`
- Changelog: `docs/project-changelog.md`
- GitHub issue: `digitopvn/goclaw#80`
- Existing beta workflow note: `dev-beta-release.yaml` is the release check surface for later implementation, not for this plan-only branch.

## Overview

Run the TDD regression gates, update docs if API changed, and prepare the implementation handoff for issue #80.

## Requirements

- Functional: all planned acceptance criteria are validated by tests or manual checks.
- Functional: issue #80 gets a concise summary and implementation PR path.
- Non-functional: ship by PR only; do not merge implementation in this phase.
- Non-functional: keep reports concise and list unresolved questions last.

## Architecture

Validation is layered:

```text
Backend tests
  -> archive formats
  -> selected/system skill scope
  -> permissions

Web tests/build
  -> URL params and filename helpers
  -> component prop wiring
  -> locale completeness

Docs/handoff
  -> HTTP API docs if route contract changed
  -> issue comment with plan path
```

## Related Code Files

- Modify if needed: `docs/18-http-api.md`
- Modify if implementation lands: `docs/project-changelog.md`
- No planned source changes in this phase beyond docs/test fixes.

## Tests Before

Before implementing final polish, confirm expected failures from Phases 1-2 are real:
- backend selected export tests fail before backend changes.
- UI helper tests fail before UI helper changes.

## Refactor

Keep refactor minimal:
- remove duplicated archive filename construction.
- remove duplicated query param building.
- keep docs updated only for changed API behavior.

## Tests After

Run:

```bash
go test ./internal/http ./internal/store/pg
pnpm -C ui/web test -- skills
pnpm -C ui/web build
```

If archive writer changes wider shared helpers, also run:

```bash
go build ./...
```

## Implementation Steps

1. Run backend focused tests.
2. Run UI focused tests.
3. Run UI build.
4. Run Go build if shared backend helpers changed.
5. Update `docs/18-http-api.md` with `format`, `ids`, and system-skill selection semantics.
6. Update `docs/project-changelog.md` after implementation lands.
7. Comment issue #80 with:
   - selected approach
   - archive formats
   - admin-only permission model
   - plan path
   - validation commands to expect.

## Todo List

- [x] Backend focused tests pass.
- [x] Web focused tests pass.
- [x] Web build passes.
- [x] Go build runs if needed.
- [x] HTTP API docs match actual query params.
- [x] Issue #80 comment posted with plan path.

## Success Criteria

- [x] All acceptance criteria from issue #80 mapped to phases.
- [x] No unresolved API questions remain.
- [x] Issue #80 has an implementation summary comment.
- [x] Implementation is ready for PR review and beta shipping label.

## Risk Assessment

- Risk: docs claim ZIP import support when implementation only supports ZIP export.
  Mitigation: docs must say export/download explicitly unless ZIP import is actually implemented.
- Risk: selected export tests pass but import/export page behavior regresses.
  Mitigation: include no-ids tar.gz compatibility test.
- Risk: issue comment over-promises implementation.
  Mitigation: comment lists completed validation commands and links the PR.

## Security Considerations

- Issue comment should not include local secrets, env paths, or private tokens.
- Commit only implementation, plan, and docs artifacts for this issue branch.

## Next Steps

Create PR, wait for CI, then comment and relabel issue #80.
