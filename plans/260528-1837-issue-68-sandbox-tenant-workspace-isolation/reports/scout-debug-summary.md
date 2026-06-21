# Scout Debug Summary

## Issue

`digitopvn/goclaw#68` reports P0 cross-tenant workspace exposure through Docker sandbox global workspace mounts. Upstream `nextlevelbuilder/goclaw#1163` is still open and describes the same mount-layer gap.

## Verified Code Evidence

- `internal/sandbox/docker.go:91-100` mounts the `workspace` argument into `cfg.ContainerWorkdir()` as `/workspace` by default.
- `internal/tools/shell.go:645-667` calls `sandboxMgr.Get(ctx, sandboxKey, t.workspace, ...)` and then executes `sh -c` in the mapped container cwd.
- `internal/tools/credentialed_exec.go:621-635` also calls sandbox manager with `t.workspace`.
- `internal/tools/filesystem.go:198-222`, `internal/tools/filesystem_write.go:242-291`, `internal/tools/filesystem_list.go:142-166`, and `internal/tools/edit.go:209-221` use the same global `t.workspace` for sandbox manager lookup.
- `internal/tools/context_keys.go:120-131` already provides effective request workspace via `ToolWorkspaceFromCtx(ctx)` / `store.RunContext.Workspace`.
- `internal/tools/sandbox_utils.go:37-55` clamps sandbox file-tool paths to `containerCwd`.
- `internal/sandbox/fsbridge.go:143-236` adds realpath validation inside the container.
- `internal/tools/sandbox_utils_test.go:92-145` and `internal/sandbox/docker_test.go:130-174` already test path-boundary behavior.

## Root Cause

Current mount isolation relies on setting the container cwd below `/workspace`, but the bind mount still contains the global workspace tree. File tools have defense-in-depth path clamps; shell/exec remains arbitrary code and can directly inspect `/workspace` unless the mount itself is scoped.

## Recommended Fix

Mount the effective tenant/session/team workspace, not the global workspace root, for every sandboxed tool path. Preserve `/workspace` as the container-visible root for that effective workspace.

## Unresolved Questions

- Master-scope global sandbox mode needs explicit product decision. Default P0 fix should not expose global workspace to tenant-scoped runs.
