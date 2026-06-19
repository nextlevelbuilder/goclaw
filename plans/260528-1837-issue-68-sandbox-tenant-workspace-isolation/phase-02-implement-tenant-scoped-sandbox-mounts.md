---
phase: 2
title: "Implement Tenant-Scoped Sandbox Mounts"
status: complete
effort: "3h"
---

# Phase 2: Implement Tenant-Scoped Sandbox Mounts

## Context Links

- Phase 1 tests: `./phase-01-characterize-risk-and-write-tests.md`
- Context workspace source: `internal/tools/context_keys.go`
- Sandbox manager: `internal/sandbox/docker.go`
- Exec sandbox caller: `internal/tools/shell.go`
- Credentialed sandbox caller: `internal/tools/credentialed_exec.go`
- File tool sandbox callers: `internal/tools/filesystem*.go`, `internal/tools/edit.go`

## Overview

Mount only the effective workspace subtree into the sandbox container while preserving the inside-container path convention: the effective workspace appears as `/workspace`.

## Requirements

- Functional: tenant/session workspace is the only workspace mounted for sandboxed exec and file tools.
- Backward compatible UX: existing relative paths and `/workspace/...` paths inside the effective workspace continue to work.
- Safety: if no effective workspace exists in context, either fail closed for non-master requests or explicitly fall back only for verified master/global scope.
- Reuse: sandbox manager key must include enough scope to avoid container reuse across workspaces/config.

## Architecture

Add one shared helper in `internal/tools` that resolves the sandbox mount workspace:

1. Prefer `ToolWorkspaceFromCtx(ctx)`.
2. Fall back to `t.workspace` only when no request workspace is available.
3. Return both mount workspace and container cwd mapping.

When the mount workspace is already the effective workspace, `SandboxCwd()` should return `/workspace`. If a team/shared workspace is intentionally the effective workspace, mount that path directly.

## Related Code Files

- Modify: `internal/tools/shell.go`
- Modify: `internal/tools/credentialed_exec.go`
- Modify: `internal/tools/filesystem.go`
- Modify: `internal/tools/filesystem_write.go`
- Modify: `internal/tools/filesystem_list.go`
- Modify: `internal/tools/edit.go`
- Modify if needed: `internal/tools/sandbox_utils.go`
- Modify if needed: `internal/sandbox/docker.go`

## Implementation Steps

1. Introduce a small `effectiveSandboxWorkspace(ctx, globalWorkspace)` helper.
2. Replace sandbox `Get(ctx, sandboxKey, t.workspace, ...)` calls with the effective workspace for exec, credentialed exec, read, write, list, and edit.
3. Update `SandboxCwd()` usage so the effective workspace maps to `/workspace` after scoped mounting.
4. Ensure sandbox reuse key differentiates effective workspace; prefer appending a stable hash of effective mount path and workspace access mode.
5. Keep `ResolveSandboxPath()` and `FsBridge` validations unchanged unless Phase 1 exposes a real gap.
6. Add concise security log on fallback to global workspace, if fallback remains.

## Success Criteria

- Complete: sandboxed exec and file tools now pass the effective tenant/session workspace to `sandbox.Manager.Get`.
- Complete: sandboxed normal exec and credentialed exec map host cwd under the effective mount to `/workspace`.
- Complete: Docker sandbox cache identity now includes workspace, workspace access, workdir, and image.
- Complete: host-mode workspace restrictions were not changed.

## Risk Assessment

- Relative path delivery for sandboxed `write_file` currently joins `ToolWorkspaceFromCtx(ctx)` with user path; verify absolute path inputs do not produce bad host delivery paths.
- Sandbox containers may be reused longer than expected. Include effective workspace in key or manager lookup identity.

## Security Considerations

- Do not whitelist sibling tenant paths as allowed mounts.
- Treat shell as untrusted arbitrary code; mount isolation is mandatory even if deny patterns remain.

## Unresolved Questions

- Whether master-scope maintenance agents should retain a global sandbox mode needs product decision; default implementation should prefer fail closed or explicit opt-in.
