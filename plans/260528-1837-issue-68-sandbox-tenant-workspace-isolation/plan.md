---
title: "Issue 68 Sandbox Tenant Workspace Isolation"
description: "Plan P0 fix for Docker sandbox global workspace mount visibility across tenants."
status: complete
priority: P0
issue: 68
branch: "codex/issue-68-sandbox-tenant-workspace-plan"
tags: [sandbox, security, tenant-isolation, issue-68]
blockedBy: []
blocks: []
created: "2026-05-28T18:37:00+07:00"
createdBy: "codex"
source: github-issue
---

# Issue 68 Sandbox Tenant Workspace Isolation

## Overview

Fix the sandbox isolation gap where Docker containers receive the global workspace root as `/workspace`. Current file tools already clamp sandbox file paths to the effective container cwd, but shell/exec still runs inside a container that can enumerate the full mounted tree.

## Evidence

- Issue: digitopvn/goclaw#68, upstream nextlevelbuilder/goclaw#1163
- Mount source: `internal/sandbox/docker.go:91-100`
- Shell sandbox caller passes global workspace: `internal/tools/shell.go:645-667`
- File-tool sandbox callers also request sandbox by global workspace: `internal/tools/filesystem.go:198-222`, `internal/tools/filesystem_write.go:242-291`, `internal/tools/filesystem_list.go:142-166`, `internal/tools/edit.go:209-221`
- Existing mitigations: `internal/tools/sandbox_utils.go:37-55`, `internal/sandbox/fsbridge.go:143-236`, `internal/tools/sandbox_utils_test.go:92-145`, `internal/sandbox/docker_test.go:130-174`

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Characterize Risk and Write Tests](./phase-01-characterize-risk-and-write-tests.md) | Complete |
| 2 | [Implement Tenant-Scoped Sandbox Mounts](./phase-02-implement-tenant-scoped-sandbox-mounts.md) | Complete |
| 3 | [Validate Security and Ship](./phase-03-validate-security-and-ship.md) | Complete |

## Key Decisions

- Do not rely on cwd-only isolation for shell. The mount namespace must not include sibling tenant workspaces.
- Keep existing `ResolveSandboxPath` and `FsBridge` realpath validation as defense in depth.
- Preserve current `/workspace` UX inside the sandbox by mounting the effective workspace subtree at `/workspace`, not by exposing `/workspace/<relative-subtree>`.
- Reuse `ToolWorkspaceFromCtx(ctx)` / `store.RunContext.Workspace` as the effective tenant/session workspace source.

## Success Criteria

- A sandboxed exec for tenant A cannot list or read tenant B workspace paths.
- Sandboxed file tools still read/write/list/edit inside tenant A workspace.
- Sandbox reuse key cannot reuse a container created for a different effective workspace/config.
- Focused tests pass, plus `go build ./...`, `go build -tags sqliteonly ./...`, and `go vet ./...`.

## Docs Impact

Minor. Add changelog/security note after implementation lands.

## Unresolved Questions

- Should team shared workspace be a separate allowed mount, or should team runs set `ToolWorkspaceFromCtx` to the team/session workspace before sandbox creation?
