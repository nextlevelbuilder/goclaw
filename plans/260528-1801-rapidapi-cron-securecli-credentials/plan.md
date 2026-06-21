---
title: "RapidAPI cron SecureCLI credential fix"
description: "Diagnose and fix RapidAPI CLI credential injection from cron-triggered agent turns without changing cron credential architecture."
status: complete
priority: P1
effort: 6h
issue: 74
branch: "codex/issue-74-rapidapi-cron-plan"
tags: [bugfix, backend, security, cron, secure-cli, tdd]
blockedBy: []
blocks: []
created: "2026-05-28T11:01:28.893Z"
createdBy: "ck:plan"
source: skill
---

# RapidAPI cron SecureCLI credential fix

## Overview

Issue #74 reports `rapidapi` CLI failing from cron with `RAPIDAPI_KEY required`.
Broader cron credential context is already fixed by storing `payload.credentialUserId` on create and injecting it into cron runs. This plan keeps that architecture and focuses on RapidAPI-specific SecureCLI setup, diagnostics, and regression smoke.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Diagnose Current RapidAPI Credential Path](./phase-01-diagnose-current-rapidapi-credential-path.md) | Complete |
| 2 | [Add RapidAPI Preset and Credential Diagnostics](./phase-02-add-rapidapi-preset-and-credential-diagnostics.md) | Complete |
| 3 | [Validate Cron Smoke and Issue Handoff](./phase-03-validate-cron-smoke-and-issue-handoff.md) | Complete |

## Dependencies

- GitHub issue: `digitopvn/goclaw#74`
- Related closed issue: `digitopvn/goclaw#54`
- Cron context path: `cmd/gateway_cron.go`, `internal/store/cron_store.go`, `internal/gateway/methods/cron.go`
- SecureCLI path: `internal/tools/credentialed_exec.go`, `internal/tools/credential_presets.go`, `internal/store/secure_cli_store.go`
- Docs to update if behavior changes: `docs/project-changelog.md`, `docs/03-tools-system.md`

## Recommended Scope

- First prove whether live config is missing `rapidapi`, missing agent grant, missing user env, or legacy cron payload.
- Add built-in RapidAPI preset only if config gap confirmed or reproducible from local tests.
- Add logs/tests that distinguish: no SecureCLI config, no grant, no `RAPIDAPI_KEY`, invalid env JSON, binary not found.

## Out of Scope

- New credential store.
- New cron auth model.
- Exposing credential values in API/logs.
- Load/stress tests.
- Direct live write smoke unless operator provides explicit credentialed environment.

## Validation Gates

- `go test ./internal/tools ./internal/store ./cmd`
- Targeted cron/SecureCLI regression tests before implementation changes.
- Manual or documented smoke: cron-created-after-fix invokes harmless `rapidapi` command and does not print secret.

## Completion Notes

- 2026-05-29: all code/test/build gates passed, including PG and SQLite builds, `go vet`, and integration race suite with local PG test DB.
- Real RapidAPI cron smoke remains operator-gated because this session did not include an approved RapidAPI key, target SecureCLI grant, or harmless endpoint.

## Unresolved Questions

- Is `rapidapi` registered in the live SecureCLI table now?
- Was the failing cron created before `v3.12.0-beta.25` / commit `f9440bac`?
- Which harmless RapidAPI endpoint should be the operator-approved smoke target?
