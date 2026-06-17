---
title: "Skill Download and Selective Export Web UI"
description: "Add admin-only skill download actions on /skills with selected/system skill support and zip/tar.gz export formats."
status: implemented
priority: P2
issue: 80
branch: "codex/issue-80-skill-download-plan"
tags: [skills, export, web-ui, tdd, issue-80]
blockedBy: []
blocks: []
created: "2026-05-28T11:00:02.557Z"
createdBy: "ck:plan"
source: skill
---

# Skill Download and Selective Export Web UI

## Overview

Implement issue #80 by extending the existing skills export surface instead of creating a parallel subsystem.

User decisions locked:
- Archive formats: support popular server-generated formats: `zip`, `tar.gz`, and `tgz` alias.
- Scope: downloadable skills include custom and system/core skills when explicitly selected.
- Permission model: admin-only.

Backward compatibility:
- Existing full skills export remains compatible with `/import-export`: no `ids` means tenant-scoped custom skills by default.
- `/skills` detail and bulk actions use selected IDs, so system/core skills can be exported without changing full backup defaults.

## Current Code Context

- Existing backend export: `internal/http/skills_export.go`
- Existing export routes: `internal/http/skills.go`
- Existing export queries: `internal/store/pg/skills_export_queries.go`
- Existing file/version helpers: `internal/http/skills_versions.go`
- Existing import/export UI: `ui/web/src/pages/import-export/hooks/use-capabilities-export.ts`
- Existing skills UI selection: `ui/web/src/pages/skills/skills-page.tsx`
- Existing bulk toolbar: `ui/web/src/pages/skills/skill-bulk-actions-toolbar.tsx`
- Existing i18n namespace: `ui/web/src/i18n/locales/{en,vi,zh}/skills.json`

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [API Contract and Export Writers](./phase-01-api-contract-and-export-writers.md) | Complete |
| 2 | [Skills UI Download Actions](./phase-02-skills-ui-download-actions.md) | Complete |
| 3 | [Regression Validation and Issue Handoff](./phase-03-regression-validation-and-issue-handoff.md) | Complete |

## Dependencies

- GitHub issue: `digitopvn/goclaw#80`
- No blocking plan found. Existing pending merge-train plan touches release flow, not this feature area.

## API Contract Target

`GET /v1/skills/export`

Query params:
- `stream=true|false`: keep existing behavior.
- `format=tar.gz|tgz|zip`: default `tar.gz`; `tgz` aliases to gzip tar writer.
- `ids=<uuid>,<uuid>` or repeated `id=<uuid>`: optional selected skill IDs.
- `include_system=true|false`: optional for full export only. Selected `ids` can include system skills without this flag.

Response filenames:
- Single selected skill: `goclaw-skill-<slug>-v<version>.<ext>`
- Multi/full export: `goclaw-skills-export-YYYYMMDD-HHmm.<ext>`

Archive layout:
```text
skills/<slug>/metadata.json
skills/<slug>/SKILL.md
skills/<slug>/references/**
skills/<slug>/scripts/**
skills/<slug>/assets/**
skills/<slug>/grants.jsonl
```

## Validation Commands

- `go test ./internal/http ./internal/store/pg`
- `pnpm -C ui/web test -- skills`
- `pnpm -C ui/web build`
- If backend code changes touch wider store contracts: `go build ./...`

## Out of Scope

- Viewer/operator downloads.
- RAR/7z export creation.
- Changing skills import to accept ZIP in this plan unless implementation discovers import/export mismatch that blocks issue #80 acceptance.
- Auto-publishing release or merging implementation PR.

## Unresolved Questions

None for planning. RAR/7z intentionally excluded from "popular archive formats" because they require extra dependencies or non-standard writers; ZIP and tar.gz cover browser/download and Unix portability.
