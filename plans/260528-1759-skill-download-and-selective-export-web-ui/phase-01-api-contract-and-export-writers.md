---
phase: 1
title: "API Contract and Export Writers"
status: complete
priority: P1
effort: "1d"
dependencies: []
---

# Phase 1: API Contract and Export Writers

## Context Links

- Issue: `digitopvn/goclaw#80`
- Backend route registration: `internal/http/skills.go`
- Existing export handler: `internal/http/skills_export.go`
- Existing export queries: `internal/store/pg/skills_export_queries.go`
- Skill file/version helpers: `internal/http/skills_versions.go`
- Existing import parser: `internal/http/skills_import.go`

## Overview

Extend the backend export path to support selected skills, system skills when explicitly selected, and multiple archive formats while keeping existing all-custom-skills export compatible.

## Requirements

- Functional: admin can export selected skill IDs, including system/core skills.
- Functional: admin can choose `zip`, `tar.gz`, or `tgz`.
- Functional: archive preserves skill directory content, including `SKILL.md`, `references/`, `scripts/`, `assets/`, and metadata.
- Functional: no `ids` preserves current tenant-scoped custom-skill export behavior.
- Non-functional: no shelling out to `zip`, `tar`, `gzip`, or external tools.
- Non-functional: keep path traversal, symlink escape, and export size protections.

## Architecture

Add a small archive-writer abstraction:

```go
type skillArchiveWriter interface {
    AddFile(name string, data []byte) error
    Close() error
    ContentType() string
    Extension() string
}
```

Use two implementations:
- `tar.gz`: existing `gzip.Writer` + `tar.Writer`
- `zip`: Go standard library `archive/zip`

Skill selection flow:
1. Parse `format`, `ids`, and `include_system`.
2. Resolve admin auth through existing `adminMiddleware`.
3. Query custom skills by tenant for full export.
4. Query selected skills by IDs for selected export; enforce tenant scope and allow system skills only when selected.
5. For each skill, resolve a readable root:
   - managed custom root from `file_path`
   - system root from version directory or bundled fallback, mirroring file-browser behavior
6. Walk allowed files and add normalized archive paths.

## Related Code Files

- Modify: `internal/http/skills_export.go`
- Modify: `internal/store/pg/skills_export_queries.go`
- Possibly modify: `internal/http/skills_versions.go` to share root-resolution helper
- Create: `internal/http/skills_export_test.go`
- Create or extend: `internal/store/pg/skills_export_queries_test.go`

## Tests Before

1. Backend handler contract tests fail first:
   - `GET /v1/skills/export?format=zip&id=<system-id>` returns ZIP content type and contains `skills/<slug>/SKILL.md`.
   - `GET /v1/skills/export?format=tgz&id=<custom-id>` returns gzip tar content type.
   - unsupported `format=rar` returns `400`.
   - non-admin caller gets `403`.
2. Store query tests fail first:
   - selected skill query returns system and custom rows when IDs are explicit.
   - full export query remains custom-only by default.
   - tenant scope blocks selected skills from another tenant.

## Refactor

1. Extract current tar writer into a reusable archive-writer helper.
2. Add ZIP writer using standard library only.
3. Add format parser with aliases:
   - `tar.gz`, `tgz`, empty -> gzip tar
   - `zip` -> ZIP
4. Add selected skill resolver query in `pg`.
5. Add directory walker that:
   - normalizes slash paths
   - skips symlinks and OS junk
   - rejects `..`, absolute paths, Windows drives, and null bytes
   - enforces existing `maxExportSize`
6. Include `metadata.json` and `grants.jsonl` as generated files.

## Tests After

1. Archive content tests:
   - nested `references/`, `scripts/`, and `assets/` files are present.
   - generated `metadata.json` excludes internal file path.
   - `grants.jsonl` remains included for custom skill grants.
2. Compatibility tests:
   - no `ids` still exports custom skills only.
   - existing SSE path still returns `download_url`.
   - direct download sets correct `Content-Disposition` filename.

## Implementation Steps

1. Write failing tests for format parsing, selected IDs, system inclusion, and folder walking.
2. Add `parseSkillExportRequest(r)` to centralize query parsing.
3. Add `ExportSelectedSkills(ctx, db, ids, includeSystem)` in `internal/store/pg/skills_export_queries.go`.
4. Replace direct `tar.Writer` code with archive-writer interface.
5. Add ZIP implementation.
6. Replace single `SKILL.md` read with safe directory walk from resolved root.
7. Keep existing SSE token flow and direct response flow.
8. Update `docs/18-http-api.md` only if implementation changes the documented API shape.

## Todo List

- [x] Backend tests written before implementation.
- [x] Store selected-skill query is tenant-scoped.
- [x] Archive writer abstraction supports ZIP and gzip tar.
- [x] System/core selected skills export with full directory contents.
- [x] Full export backward-compatible.
- [x] HTTP docs updated if needed.

## Success Criteria

- [x] `format=zip`, `format=tar.gz`, and `format=tgz` work.
- [x] Selected system/core skill archive contains `SKILL.md` and resource dirs when present.
- [x] Full export without `ids` does not accidentally include bundled system skills.
- [x] Unauthorized/non-admin export is rejected.
- [x] `go test ./internal/http ./internal/store/pg` passes.

## Risk Assessment

- Risk: including system skills in full backups bloats archives and changes import semantics.
  Mitigation: only include system skills when selected or `include_system=true`.
- Risk: path traversal or symlink escape during directory walk.
  Mitigation: reuse strict archive path sanitizer and skip symlinks.
- Risk: ZIP export breaks import compatibility.
  Mitigation: document that issue #80 download supports ZIP; existing full import/export remains tar.gz-compatible unless implementation adds ZIP import support later.

## Security Considerations

- Admin-only gate stays in `RegisterRoutes`.
- Tenant scope must be enforced in SQL, not only UI filtering.
- Generated metadata must not leak server filesystem paths.

## Next Steps

Phase 2 consumes the backend API from the Skills page and should not duplicate archive assembly client-side.
