---
title: "Built-in Wait Tool with Delay Parameter"
description: "Add a general-purpose wait tool with bounded millisecond delays and per-agent limit overrides."
status: complete
priority: P1
issue: 1097
branch: "codex/feat-wait-tool"
tags: [tools, runtime, tdd, issue-1097]
blockedBy: []
blocks: []
created: "2026-05-18T14:29:45.859Z"
createdBy: "ck:plan"
source: skill
---

# Built-in Wait Tool with Delay Parameter

## Overview

Add a built-in `wait` tool so agents can pause between actions without using browser-only wait, polling loops, or cron handoffs.

Scope is intentionally narrow: bounded sleep inside tool execution, context cancellation support, gateway registration, builtin-tool seed visibility, and focused tests. `wait_until` and progress notifications stay out of v1 unless code review finds an existing event surface that makes them trivial.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Research and TDD Design](./phase-01-research-and-tdd-design.md) | Complete |
| 2 | [Implement Wait Tool](./phase-02-implement-wait-tool.md) | Complete |
| 3 | [Validate and Ship](./phase-03-validate-and-ship.md) | Complete |

## Dependencies

- Related issue: nextlevelbuilder/goclaw#1097
- Existing tool contract: `internal/tools/types.go`, `internal/tools/registry.go`
- Existing registration surfaces: `cmd/gateway_setup.go`, `cmd/gateway_tools_wiring.go`, `cmd/gateway_builtin_tools.go`
- Ordering barrier: `internal/pipeline/tool_stage.go` parallelizes multi-tool responses unless a tool opts out
