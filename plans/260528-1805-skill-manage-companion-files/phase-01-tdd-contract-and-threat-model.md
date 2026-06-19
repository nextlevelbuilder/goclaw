---
phase: 1
title: "TDD Contract and Threat Model"
status: complete
priority: P1
effort: "2h"
dependencies: []
---

# Phase 1: TDD Contract and Threat Model

## Overview

Lock the tool contract and threat model with failing tests before implementation. The core risk is not versioning; it is allowing agents to write arbitrary relative paths into a managed skill directory without path traversal, system-artifact, or size-limit gaps.

## Requirements

- Functional: define `files` payload for `skill_manage` create and patch.
- Functional: patch can be file-only or `find`/`replace` plus files.
- Functional: existing companion files copy forward and payload entries overlay them.
- Non-functional: no filesystem staging required before `skill_manage`.
- Non-functional: no UI file editor and no SQLite work in this round.
- Security: reject unsafe paths before any disk write.

## Architecture

`skill_manage` remains the only changed public tool. It should treat `files` as a version payload:

1. Resolve current skill and manage permission.
2. Build final `SKILL.md` content.
3. Validate final `SKILL.md` with `skills.GuardSkillContent`.
4. Validate companion file payload paths, names, sizes, and total size.
5. Create new version dir.
6. Copy existing companions from previous version.
7. Overlay payload files.
8. Update DB metadata and bump loader version.

## Related Code Files

- Modify: `internal/tools/skill_manage.go`
- Read: `internal/tools/publish_skill.go`
- Read: `internal/skills/guard.go`
- Read: `internal/skills/archive_extract.go`
- Read: `internal/http/skills_versions.go`
- Modify/create tests near existing `internal/tools` tests.

## Implementation Steps

1. Add tests first for successful file-only patch:
   - create managed skill v1 with `SKILL.md`
   - call `skill_manage patch` with `files: {"references/ship-workflow.md": "# Ship"}`
   - assert v2 directory has `SKILL.md` and `references/ship-workflow.md`
   - assert DB version moved to v2
2. Add tests for patch with `find`/`replace` plus `files` in the same call.
3. Add tests for create with `content` plus `files`.
4. Add tests that existing companion files copy forward:
   - v1 has `assets/logo.txt`
   - patch adds `references/a.md`
   - v2 has both files
5. Add rejection tests before implementation:
   - absolute path
   - `../escape.md`
   - Windows drive path such as `C:/x`
   - null byte path
   - system artifacts such as `.git/config`, `.DS_Store`, `__MACOSX/x`
   - total payload/copy size above limit
6. Keep test fixtures small; do not add stress or benchmark tests.

## Success Criteria

- [x] Tests fail for missing `files` support before implementation.
- [x] Tests cover manage permission path by using existing owner/manage grant helpers where practical.
- [x] Path rejection tests prove no unsafe file lands on disk.
- [x] Test names describe behavior, not plan/finding labels.

## Risk Assessment

- Risk: validating only raw strings but not cleaned paths. Mitigation: test both raw and cleaned escape forms.
- Risk: partial version directory left after rejected write. Mitigation: validation must happen before creating destination; add cleanup expectation if destination is created.
- Risk: test setup over-couples to PG. Mitigation: keep Phase 1 tests at tool/filesystem level with a fake or existing test store where possible; add PG-specific coverage only if needed for version metadata.
