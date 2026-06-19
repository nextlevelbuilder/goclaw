---
phase: 1
title: "Characterize Risk and Write Tests"
status: complete
effort: "2h"
---

# Phase 1: Characterize Risk and Write Tests

## Context Links

- Issue: https://github.com/digitopvn/goclaw/issues/68
- Upstream issue: https://github.com/nextlevelbuilder/goclaw/issues/1163
- Docker mount creation: `internal/sandbox/docker.go`
- Sandbox path helpers: `internal/tools/sandbox_utils.go`
- Exec sandbox path: `internal/tools/shell.go`
- File tools: `internal/tools/filesystem.go`, `internal/tools/filesystem_write.go`, `internal/tools/filesystem_list.go`, `internal/tools/edit.go`
- FsBridge: `internal/sandbox/fsbridge.go`

## Overview

Priority P0. First lock current behavior with tests so implementation fixes the mount namespace, not just tool-layer symptoms.

## Key Insights

- `newDockerSandbox()` mounts the `workspace` argument at the container workdir when workspace access is enabled.
- `ExecTool.executeInSandbox()` calls `sandboxMgr.Get(ctx, sandboxKey, t.workspace, ...)`, where `t.workspace` is the global workspace root.
- `SandboxCwd()` maps effective request workspace to a subdirectory under `/workspace`, but shell commands can still address `/workspace` directly because the global tree is mounted.
- `ResolveSandboxPath()` and `FsBridge` already clamp file-tool paths to the effective container cwd; keep and test these protections.

## Requirements

- Add tests proving sandbox creation uses the effective workspace from context when present.
- Add tests proving `sandboxKey` or manager reuse cannot return a container for a different effective workspace.
- Add a regression test showing `/workspace` should represent only the effective tenant/session workspace.
- Keep tests deterministic; avoid real Docker dependency unless an existing integration harness already provides it.

## Related Code Files

- Modify tests: `internal/tools/sandbox_utils_test.go`
- Modify tests: `internal/tools/shell_test.go` or a new focused test file in `internal/tools/`
- Modify tests: `internal/sandbox/docker_test.go`
- Read only: `internal/sandbox/docker.go`, `internal/tools/shell.go`, file-tool sandbox callers

## Implementation Steps

1. Add a manager fake that records the `workspace` argument passed to `Get`.
2. Exercise `ExecTool.executeInSandbox()` with `WithToolWorkspace(ctx, tenantWorkspace)` and assert `Get` receives `tenantWorkspace`, not the global root.
3. Cover read/list/write/edit sandbox callers or extract a shared helper so one test protects all callers.
4. Add a config/key test proving two effective workspaces do not reuse the same sandbox container key.
5. Re-run existing sandbox path tests to keep absolute path clamp and symlink realpath behavior intact.

## Success Criteria

- Complete: new tests failed before implementation because `effectiveSandboxWorkspace`, `sandboxCwdForHostPath`, and `dockerCacheKey` did not exist yet.
- Complete: tests cover effective workspace selection, fail-closed missing tenant workspace, `/workspace` root mapping, exec/credentialed exec mount/cwd use, file-tool bridge mount use, and Docker cache-key separation.
- Complete: tests are unit-only and require no production Docker daemon.

## Risk Assessment

- Fake manager can under-test Docker args. Mitigate with a small `newDockerSandbox` argument-builder seam if needed.
- If tests reach private methods only through exported tools, keep package-level tests in `internal/tools` rather than adding production hooks.

## Security Considerations

- Tests must model malicious absolute `/workspace/...` shell paths and sibling tenant directory enumeration.

## Unresolved Questions

- None for characterization.
