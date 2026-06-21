---
phase: 6
title: "Validation and Zuey Beta Handoff"
status: in_progress
priority: P1
effort: "0.5d"
dependencies: [1, 2, 3, 4, 5]
---

# Phase 6: Validation and Zuey Beta Handoff

## Overview

Run the local and CI validation needed before shipping to `dev`, then verify the
automatic beta release and zuey deploy.

## Requirements

- Functional: all backend and web checks relevant to channel behavior pass.
- Functional: beta workflow completes after merge to `dev`.
- Functional: zuey public health is verified after beta deploy.
- Non-functional: do not expose zuey secrets in logs, docs, PR, or chat.

## Architecture

Use the repo's normal beta path: PR to `dev`, merge, `Dev CI and Beta Release`
creates the beta tag and deploys zuey. Use JSON polling for late workflow jobs
because `gh run watch` is noisy in this workflow family.

## Related Code Files

- Modify: `CHANGELOG.md`
- Modify: `docs/05-channels-messaging.md`
- Modify: `docs/project-changelog.md`
- Verify: `.github/workflows/dev-beta-release.yaml`
- Verify: `scripts/ci/dev-beta-release-workflow.test.mjs`

## Implementation Steps

1. Run targeted Go tests:
   `go test ./internal/channels ./internal/config ./cmd ./internal/agent ./internal/usage/caps`.
2. Run SQLite-tag coverage for shared behavior:
   `go test -tags sqliteonly ./internal/channels ./internal/config ./cmd`.
3. Run broad compile checks:
   `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`.
4. Run web checks:
   `cd ui/web && pnpm test -- --run && pnpm build`.
5. If desktop schema changed, run:
   `cd ui/desktop/frontend && pnpm build`.
6. Update changelog/docs with the exact behavior contract and override order.
7. Ship beta via PR to `dev`, then verify:
   `gh run view <run-id> --repo digitopvn/goclaw --json status,conclusion,jobs`.
8. Verify zuey after deploy:
   `/opt/goclaw/current/goclaw version`, `systemctl is-active goclaw`,
   local `/health`, and public `https://goclaw.zuey.me/health`.
9. Smoke-test a channel with Workspace default, Agent override, and Channel
   override to prove effective behavior.
10. Smoke-test explicit sidecar provider/model by choosing a cheap provider and
    verifying logs/traces show sidecar delivery purpose without adding delivery
    text to session history.

## Success Criteria

- [x] Local targeted tests and builds pass.
- [x] Web build passes.
- [ ] PR checks pass.
- [ ] Beta workflow conclusion is `success`.
- [ ] New beta release tag is visible.
- [ ] Zuey service is active and health returns `{"status":"ok","protocol":3}`.
- [ ] Manual smoke confirms sidecar ack is visible but absent from session
      history.
- [ ] Manual smoke confirms sidecar intermediate reply is visible but absent from
      session history and does not match main assistant tool-call content.

## Risk Assessment

Risk: zuey deploy can be healthy before Docker tail jobs finish. Do not report
beta shipped until the whole workflow has `conclusion=success`.
