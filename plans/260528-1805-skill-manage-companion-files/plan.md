---
title: "Skill Manage Companion Files"
description: "TDD plan for issue #72: let skill_manage create new immutable skill versions with SKILL.md plus companion files."
status: in_progress
priority: P2
branch: "codex/issue-72-skill-manage-files-plan"
tags: [skills, tools, tdd, issue-72]
blockedBy: []
blocks: []
created: "2026-05-28T11:05:51.716Z"
createdBy: "ck:plan"
source: skill
---

# Skill Manage Companion Files

## Overview

Fix `digitopvn/goclaw#72` by extending the agent-facing `skill_manage` tool so agents with manage access can add or overwrite companion files while creating a new immutable managed-skill version.

Scope is intentionally narrow:
- PostgreSQL Standard only for this round.
- No web UI file editor; existing ZIP upload and file viewer satisfy UI acceptance.
- No change to `publish_skill` except optional helper reuse.
- No execution/install behavior for added scripts; only store and expose files safely.

Approved contract:
- `skill_manage(action="create"|"patch", files={...})` accepts relative file paths under the skill root.
- Allowed files include `references/**/*.md`, `scripts/**`, `assets/**`, and arbitrary non-system files.
- Patch with only `files` is valid and creates one new immutable version.
- Patch with `find`/`replace` plus `files` creates one new immutable version.
- Existing companion files copy forward; new payload overlays additions/updates.
- Security scanner still validates final `SKILL.md`; companion paths and sizes are separately validated.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [TDD Contract and Threat Model](./phase-01-tdd-contract-and-threat-model.md) | Complete |
| 2 | [Skill Manage File Payload Implementation](./phase-02-skill-manage-file-payload-implementation.md) | Complete |
| 3 | [Runtime Readback and Documentation](./phase-03-runtime-readback-and-documentation.md) | Complete |
| 4 | [Validation and Issue Handoff](./phase-04-validation-and-issue-handoff.md) | Pending |

## Dependencies

- Related issue: https://github.com/digitopvn/goclaw/issues/72
- Existing tool surface: `internal/tools/skill_manage.go`
- Existing directory publish behavior: `internal/tools/publish_skill.go`
- Existing runtime readback: `internal/http/skills_versions.go`
- Existing docs: `docs/21-agent-evolution-and-skill-management.md`, `docs/16-skill-publishing.md`

## Success Criteria

- Agent with manage access can patch an existing skill and add `references/ship-workflow.md`.
- New version contains updated `SKILL.md` plus newly added files.
- Existing companion files survive patch unless overwritten.
- Runtime/API file reader can read newly added files.
- Invalid paths and system artifacts are rejected before disk write.
- Focused Go tests cover success and rejection paths.

## Unresolved Questions

None. User approved scope decisions on 2026-05-28.
