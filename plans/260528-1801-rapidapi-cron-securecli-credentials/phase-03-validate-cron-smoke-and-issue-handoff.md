---
phase: 3
title: "Validate Cron Smoke and Issue Handoff"
status: complete
priority: P1
effort: "2h"
dependencies: [2]
---

# Phase 3: Validate Cron Smoke and Issue Handoff

## Context Links

- Cron methods: `internal/gateway/methods/cron.go`
- Cron handler: `cmd/gateway_cron.go`
- Cron docs: `docs/08-scheduling-cron.md`
- SecureCLI docs: `docs/03-tools-system.md`

## Overview

Validate cron-triggered RapidAPI usage through tests and one operator-approved smoke path. Then update docs/changelog and reply to issue #74 with exact result and plan path.

## Key Insights

- Existing tests already cover cron credential context injection.
- #74 acceptance asks for smoke test and clear logs.
- Live smoke may need real RapidAPI key; no fake credential should be used to claim success.
- 2026-05-29 validation:
  - Cron credential context injection and redaction tests are already present and covered by targeted test runs.
  - RapidAPI direct exec is covered with a local script fixture and injected `RAPIDAPI_KEY`.
  - Real RapidAPI cron smoke was not run because no operator-approved key, target agent grant, or harmless endpoint was provided in this session.

## Requirements

- Functional: cron-created-after-fix can access configured RapidAPI credential through SecureCLI.
- Functional: missing credentials return actionable error.
- Functional: GitHub issue gets concise summary and plan path.
- Non-functional: no secret printing or durable secret artifacts.
- Non-functional: skip load/stress tests per repo rule.

## Architecture

Validation combines:

- unit tests for context and env wiring,
- optional integration/manual smoke for real RapidAPI CLI,
- issue handoff comment with branch/plan path.

## Related Code Files

- Modify if implementation occurred: `docs/project-changelog.md`
- Modify if docs need command examples: `docs/03-tools-system.md`
- Test: `cmd/gateway_cron_test.go`
- Test: `internal/tools/credentialed_exec*_test.go`
- Test: `internal/store/*secure_cli*_test.go`

## Implementation Steps

### Tests Before

1. Add/confirm regression proving cron context carries `credentialUserId` into scheduler run.
2. Add test proving redacted cron API response never exposes `credentialUserId`.
3. Add test proving RapidAPI env key can be merged for credentialed exec without exposing value.

### Validation

4. Run targeted tests:
   - `go test ./cmd ./internal/tools ./internal/store`
5. Run build gates if code changed:
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
6. If real RapidAPI credentials available and user approves:
   - create/update SecureCLI `rapidapi` config with sanitized workflow.
   - grant target agent.
   - create fresh cron job after #54 fix.
   - run harmless RapidAPI read command.
   - verify run log success and no secret output.
7. If live credentials unavailable:
   - document skipped live smoke explicitly.
   - keep issue open or mark plan blocked by credential availability.

### Handoff

8. Update changelog/docs only if implementation changes behavior.
9. Reply GitHub issue #74:
   - plan path,
   - root-cause hypothesis,
   - chosen approach,
   - validation gates,
   - open operator questions.

## Todo List

- [x] Regression tests pass.
- [x] Build gates pass if code changed.
- [x] Smoke result recorded or skipped with reason.
- [ ] Issue #74 replied with plan summary and filepath after PR is opened.

## Success Criteria

- [x] Tests prove cron and SecureCLI credential path for RapidAPI.
- [x] Manual smoke either passes or is clearly blocked by missing real credentials.
- [x] No secrets in logs, tests, docs, or issue comment.
- [ ] GitHub issue comment links this plan and summarizes next implementation path after PR is opened.

## Validation

- `go test ./cmd ./internal/tools ./internal/store`
- `go build ./...`
- `go build -tags sqliteonly ./...`
- `go vet ./...`
- `TEST_DATABASE_URL="postgres://postgres:test@localhost:5433/goclaw_test?sslmode=disable" go test -race -tags integration ./tests/integration/`

## Risk Assessment

- Risk: smoke uses fake data and gives false confidence. Mitigation: fake keys only test error wording; real smoke must use real read-only endpoint.
- Risk: issue comment overstates completion. Mitigation: label plan as pending until implementation/smoke complete.
- Risk: docs drift from actual CLI syntax. Mitigation: verify RapidAPI CLI command syntax before adding command examples.

## Security Considerations

- Do not paste RapidAPI key in commands.
- Do not use command forms that echo env.
- Keep issue comment plan-only, no live config values.

## Next Steps

- After plan approval, run `/ck:cook /Volumes/GOON/www/digitop/goclaw/plans/260528-1801-rapidapi-cron-securecli-credentials/plan.md --tdd`.
