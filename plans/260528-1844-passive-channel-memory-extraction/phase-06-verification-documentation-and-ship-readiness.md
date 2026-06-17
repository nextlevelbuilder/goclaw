---
phase: 6
title: "Verification Documentation and Ship Readiness"
status: complete
priority: P1
effort: "1d"
dependencies: [1, 2, 3, 4, 5]
---

# Phase 6: Verification Documentation and Ship Readiness

## Context Links

- Post-implementation checklist in `CLAUDE.md`
- Docs to update: `docs/05-channels-messaging.md`, `docs/06-store-data-model.md`, `docs/07-bootstrap-skills-memory.md`, `docs/09-security.md`, `docs/18-http-api.md`, `docs/project-changelog.md`
- Local verification memory: `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`

## Overview

Prove the feature is safe, documented, and ready for implementation PR review. This phase owns the final consistency sweep and release notes.

## Requirements

- Functional: all new tests pass; user-facing docs describe opt-in, review queue, redaction, retention, deletion limits.
- Non-functional: compile in PG and SQLite builds, no known tenant isolation gaps, no stale plan references in code comments.

## Architecture

No new architecture. This phase verifies all layers together:

1. schema migration
2. store contracts
3. worker batching/redaction
4. review queue writer
5. HTTP/UI controls
6. docs

## Related Code Files

- Modify: `docs/05-channels-messaging.md`
- Modify: `docs/06-store-data-model.md`
- Modify: `docs/07-bootstrap-skills-memory.md`
- Modify: `docs/09-security.md`
- Modify: `docs/18-http-api.md`
- Modify: `docs/project-changelog.md`
- Possibly modify: `docs/codebase-summary.md`

## Implementation Steps

1. Run focused Go tests:
   - `go test ./internal/consolidation`
   - `go test ./internal/http`
   - `go test ./internal/store/pg`
   - `go test -tags sqliteonly ./internal/store/sqlitestore`
2. Run compile/static checks:
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
   - `go vet ./...`
3. Run web checks:
   - `cd ui/web && pnpm build`
4. If UI changed, run local dev server and browser-check channel detail at desktop/mobile widths.
5. Update docs listed above.
6. Update `docs/project-changelog.md`.
7. Whole-plan consistency sweep:
   - search code and docs for stale names such as `passiveExtraction`, `channel_memory`, `memory-extraction`
   - verify route names match docs and UI hooks
   - verify schema version numbers match migrations
8. Prepare PR summary with test results and privacy caveats.

## Todo List

- [ ] Focused tests pass.
- [ ] PG build passes.
- [ ] SQLite build passes.
- [ ] Vet passes.
- [ ] Web build passes.
- [ ] Browser verification done if UI implemented.
- [ ] Docs updated.
- [ ] Changelog updated.
- [ ] Whole-plan consistency sweep clean.

## Success Criteria

- [ ] Feature satisfies all issue #64 acceptance criteria selected for v1.
- [ ] No failing tests ignored.
- [ ] Any unsupported v1 behavior documented explicitly.
- [ ] PR is ready for code review.

## Risk Assessment

Integration tests with PG/pgvector may require local service. If unavailable, document exactly which integration tests were skipped and why; do not claim full verification.

## Security Considerations

Final review must include a targeted scan for raw message body persistence, secrets in logs, missing tenant predicates, and unauthorized mutation paths.

## Next Steps

After plan approval, execute with `/ck:cook <plan-path>` or equivalent implementation workflow.
