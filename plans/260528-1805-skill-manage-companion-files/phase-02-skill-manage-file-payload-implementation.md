---
phase: 2
title: "Skill Manage File Payload Implementation"
status: complete
priority: P1
effort: "4h"
dependencies: [1]
---

# Phase 2: Skill Manage File Payload Implementation

## Overview

Implement the smallest safe `files` extension inside `skill_manage`. Reuse existing copy behavior where it is correct, but add explicit payload validation because `publish_skill` copies trusted workspace directories while this path accepts direct model-provided content.

## Requirements

- Functional: `files` is optional object/map on `create` and `patch`.
- Functional: create requires `content`; patch requires at least one of `find`, `visibility`, or non-empty `files`.
- Functional: file-only patch creates a new immutable version, not a metadata-only update.
- Functional: visibility-only patch remains metadata-only and should not create a new version.
- Non-functional: no new database tables or migrations.
- Security: reject unsafe paths and oversize content before writing.

## Architecture

Add a small internal representation:

```go
type skillManagedFile struct {
    Path    string
    Content string
}
```

Parsing should accept a JSON-object-shaped value from tool args:

```json
{
  "files": {
    "references/ship-workflow.md": "# Ship workflow",
    "scripts/check.sh": "#!/usr/bin/env bash\n..."
  }
}
```

Validation rules:
- path is relative after separator normalization
- no `..` component
- no absolute path
- no Windows drive prefix
- no null byte
- not `SKILL.md`; main content stays controlled by `content` or `find`/`replace`
- not `skills.IsSystemArtifact(path)` nor any system-artifact path component
- file content fits per-file limit
- final companion copy + payload total fits `maxCopySize`

## Related Code Files

- Modify: `internal/tools/skill_manage.go`
- Optional helper extraction: `internal/tools/publish_skill.go`
- Do not modify: `internal/store/pg/skills_crud.go` unless metadata size calculation requires no alternative.

## Implementation Steps

1. Extend `SkillManageTool.Parameters()` with `files`.
2. Add parser helper for `files` from `map[string]any`, validating all values are strings.
3. Add path validation helper in `internal/tools/skill_manage.go` or a small shared helper if `publish_skill` can reuse it without churn.
4. Update create flow:
   - validate `SKILL.md` content
   - parse and validate files
   - create version dir
   - write `SKILL.md`
   - write files
   - compute directory size and hash
   - register skill
5. Update patch flow:
   - permit `files` without `find`
   - preserve visibility-only fast path when no content/files changes
   - read current `SKILL.md` from the latest version while the slug lock is held
   - validate final content and file payload
   - create new version dir
   - write final `SKILL.md`
   - copy existing companions
   - overlay payload files
   - compute directory size and hash
   - update DB
6. If copy/overlay fails after destination creation, remove the new version directory before returning error.
7. Keep response concise but mention count of companion files written.

## Success Criteria

- [x] Phase 1 tests pass.
- [x] `skill_manage patch` can add `references/*.md` without filesystem staging.
- [x] `skill_manage patch` with only `visibility` still does not create a new version.
- [x] Invalid file payloads fail without partial durable writes.
- [x] Code stays in existing tool boundary; no broad refactor.

## Risk Assessment

- Risk: model passes nested non-string values. Mitigation: fail clearly; no implicit JSON serialization.
- Risk: `file_size` remains `SKILL.md`-only. Mitigation: compute version directory size after writes or intentionally document if existing metadata semantics stay unchanged; preferred is directory size.
- Risk: helper reuse from `publish_skill` causes unnecessary churn. Mitigation: duplicate tiny validation if extraction would make unrelated code noisier.
