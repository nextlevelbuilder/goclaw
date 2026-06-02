---
phase: 1
title: "Research and TDD Design"
status: complete
effort: "1h"
---

# Phase 1: Research and TDD Design

## Overview

Verify current tool registration and policy paths before coding. Write tests first for the new tool contract, policy visibility, and per-agent configuration.

## Context Links

- Issue: nextlevelbuilder/goclaw#1097
- Tool interface: `internal/tools/types.go`
- Tool registry execution and cancellation path: `internal/tools/registry.go`
- Built-in groups/profiles: `internal/tools/policy.go`
- Per-agent `tools_config` parsing: `internal/store/agent_store.go`
- Agent context injection: `internal/agent/loop_context.go`

## Key Insights

- `browser` already supports `act.kind=wait`, but only through `pkg/browser/tool.go`.
- General tools are `internal/tools.Tool` implementations and are registered into `tools.Registry`.
- Built-in DB visibility is separate from runtime registration; `cmd/gateway_builtin_tools.go` must seed `wait`.
- Per-agent knobs can fit existing `agents.tools_config` JSON without a migration by extending `config.ToolPolicySpec`.
- `ToolStage` currently runs multi-tool model responses through the parallel raw-tool path. A same-turn `message, wait, message` sequence must force sequential execution or both messages can run before the sleep completes.

## Implementation Steps

1. Add tests for `wait` validation: missing `timeMs`, below 100ms, above 300000ms, fractional numbers, success message, and context cancellation.
2. Add policy tests showing `wait` belongs to `group:runtime`, `group:goclaw`, and coding/full visibility.
3. Add config parsing test for `tools_config.wait.min_ms/max_ms`.
4. Re-run grep for `runtime` group and `builtinToolSeedData` before implementation to avoid missing catalog surfaces.

## Success Criteria

- [x] Tests fail before implementation for the missing `wait` tool.
- [x] Plan cites only live files and existing extension points.
- [x] No DB migration is required for per-agent settings.
