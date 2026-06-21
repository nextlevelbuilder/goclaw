---
phase: 3
title: "Runtime Readback and Documentation"
status: complete
priority: P2
effort: "2h"
dependencies: [2]
---

# Phase 3: Runtime Readback and Documentation

## Overview

Verify that companion files written by `skill_manage` are visible through existing runtime and HTTP readback paths, then update docs so agents know when to use `skill_manage` versus `publish_skill`.

## Requirements

- Functional: existing `/v1/skills/{id}/files` lists new companion files.
- Functional: existing `/v1/skills/{id}/files/{path}` reads new companion files.
- Documentation: update tool contract and examples.
- Out of scope: new UI editor, new REST update endpoint, SQLite Desktop support.

## Architecture

No new runtime API should be needed. Existing file APIs derive the version directory from `skills.file_path`, and `skill_manage` updates that path during patch. The validation task is to prove the new files are located under that directory.

## Related Code Files

- Read/test: `internal/http/skills_versions.go`
- Modify: `docs/21-agent-evolution-and-skill-management.md`
- Modify: `docs/16-skill-publishing.md` if cross-reference wording becomes stale
- Optional modify: `docs/15-core-skills-system.md` endpoint table only if needed
- Optional modify: `internal/agent/systemprompt.go` if agent guidance should mention companion files

## Implementation Steps

1. Add readback test if existing coverage does not already prove arbitrary companion files:
   - create or patch managed skill with `references/ship-workflow.md`
   - call list files helper/handler
   - call read file helper/handler
2. Confirm no frontend change is required:
   - `useSkills.getSkillFiles` already calls `/v1/skills/{id}/files`
   - `useSkills.getSkillFileContent` already reads a path
   - `SkillUploadDialog` already supports ZIP upload for UI acceptance
3. Update docs:
   - `skill_manage` now supports `files`
   - accepted paths and limits
   - examples for adding `references/*.md`
   - contrast with `publish_skill` for bulk directory publish
4. If `internal/agent/systemprompt.go` is changed, keep guidance to one concise line to avoid prompt bloat.

## Success Criteria

- [x] Existing file viewer API can list/read added reference files.
- [x] Docs describe `files` payload and security constraints.
- [x] Docs explicitly say UI editor remains out of scope; use ZIP upload in UI.
- [x] No stale statement remains that `skill_manage` is strictly `SKILL.md`-only.

## Risk Assessment

- Risk: docs overpromise arbitrary binary assets while payload is string-only. Mitigation: say text/file content payload; binary assets should continue through ZIP upload unless implementation adds encoding.
- Risk: prompt guidance causes agents to prefer `skill_manage` for bulk imports. Mitigation: docs recommend `publish_skill` for pre-existing directories and ZIP upload for UI.
