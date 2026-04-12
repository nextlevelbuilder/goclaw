# GoClaw Agentic OS Upgrade Plan

## From 43% to ~85% Pattern Matching with Claude Code's 18 Architectural Patterns

Based on the analysis from "Giai phau mot Agentic Operating System" (Lam Nguyen, April 2026),
this plan upgrades GoClaw from a multi-tenant AI gateway into a full **Agentic Operating System**.

---

## Current State: 43% matching (39/90 points)

| Pattern | Current | Target |
|---------|---------|--------|
| #1 Async generator | 3/5 (goroutines) | 3/5 (keep — Go native) |
| #2 Stop-reason state machine | 4/5 (BreakLoop/AbortRun) | 5/5 |
| #3 Escalating recovery | 3/5 (RetryDo only) | 5/5 |
| #4 Concurrency-safe partition | 2/5 (static CapReadOnly) | 5/5 |
| #5 Streaming tool execution | 2/5 (stage-sequential) | 5/5 |
| #6 Context modifier chain | 1/5 (direct state mutation) | 4/5 |
| #7 Coordinator restriction | 5/5 | 5/5 (keep) |
| #8 Fork isolation worktree | 0/5 | 4/5 |
| #9 Context defense 5 layers | 2/5 (PruneStage only) | 5/5 |
| #10 Permission classification | 3/5 (5-layer RBAC) | 5/5 |
| #11 Conditional skill activation | 2/5 | 4/5 |
| #12 Shell-in-prompt | 1/5 | 4/5 |
| #13 Dynamic skill discovery | 2/5 (DB-backed) | 4/5 |
| #14 Plugin 4-point extension | 0/5 | 4/5 |
| #15 Plugin security sandbox | 3/5 (Docker sandbox) | 4/5 |
| #16 Reconciliation install | 0/5 | 4/5 |
| #17 Pure native replacement | 5/5 (modernc/sqlite) | 5/5 (keep) |
| #18 Terminal-as-browser | 0/5 (Web SPA instead) | 0/5 (N/A) |

**Target: ~85% matching (76/90)**

---

## Checkpoint Structure

Each checkpoint is a self-contained implementation unit with:
- **Objective**: What we're building and why
- **Files to create**: New files with full specs
- **Files to modify**: Existing files with exact change locations
- **Implementation steps**: Ordered steps with code references
- **Verification**: How to test the checkpoint is complete
- **Dependencies**: Which checkpoints must be done first

```
CP-00  Current State Analysis (reference only — no code changes)
  |
  v
CP-01  Context Defense 5 Layers ──────────────────────┐
  |                                                    |
  v                                                    |
CP-02  Concurrency-safe Partitioning ──┐               |
  |                                    |               |
  v                                    v               |
CP-03  Streaming Tool Execution ───────┘               |
  |                                                    |
  v                                                    |
CP-04  Escalating Recovery ────────────────────────────┘
  |
  v
CP-05  Context Modifier Chain + Fork Isolation
  |
  v
CP-06  Permission Classification Pipeline
  |
  v
CP-07  Skill System Upgrade (Path Activation + Shell-in-Prompt + Discovery)
  |
  v
CP-08  Plugin Ecosystem (Commands + Agents + Hooks + Servers)
  |
  v
CP-09  Capability Gating + Blocker State + Action Policy
```

### Dependency Rules
- **CP-01** is independent — start here
- **CP-02** is independent — can parallel with CP-01
- **CP-03** depends on CP-02 (needs IsConcurrencySafe interface)
- **CP-04** depends on CP-01 (needs reactive compact in context defense)
- **CP-05** is independent
- **CP-06** is independent
- **CP-07** is independent
- **CP-08** depends on CP-07 (plugin hooks extend skill concepts)
- **CP-09** depends on CP-02, CP-04, and CP-06 (tool metadata, recovery, permission semantics)

### Parallelization Strategy (for fastest completion)

```
Week 1-2:  CP-01 + CP-02 (parallel)
Week 2-3:  CP-03 + CP-04 (parallel, after CP-01/02)
Week 3-4:  CP-05 + CP-06 (parallel)
Week 4-5:  CP-07
Week 5-10: CP-08 (largest, can start week 5)
Week 5-6:  CP-09 (can overlap late CP-08 work; high leverage for production stability)
```

---

## File Index

| Document | Content |
|----------|---------|
| [CP-00-current-state.md](CP-00-current-state.md) | Analysis of current codebase architecture |
| [CP-01-context-defense.md](CP-01-context-defense.md) | 5-layer context defense system |
| [CP-02-concurrency-partition.md](CP-02-concurrency-partition.md) | Per-invocation tool concurrency |
| [CP-03-streaming-tool-exec.md](CP-03-streaming-tool-exec.md) | Stream-time tool execution |
| [CP-04-escalating-recovery.md](CP-04-escalating-recovery.md) | 5-tier error recovery |
| [CP-05-context-modifier-fork.md](CP-05-context-modifier-fork.md) | Context modifier chain + Git worktree |
| [CP-06-permission-classification.md](CP-06-permission-classification.md) | Bash classifier + dangerous patterns |
| [CP-07-skill-system.md](CP-07-skill-system.md) | Path activation + shell-in-prompt + discovery |
| [CP-08-plugin-ecosystem.md](CP-08-plugin-ecosystem.md) | 4-point extension + hooks + marketplace |
| [CP-09-capability-gating-state-policy.md](CP-09-capability-gating-state-policy.md) | Capability snapshot + blocker state + action-family gating |
| [CP-09-structured-constraints-spec.md](CP-09-structured-constraints-spec.md) | Final implementation-grade v1 design for structured constraints |
| [CP-09-structured-constraints-checkpoints.md](CP-09-structured-constraints-checkpoints.md) | Execution checkpoints and ship gate for the structured-constraints rollout |

---

## Conventions

### Branch naming
```
feature/cp-01-context-defense
feature/cp-02-concurrency-partition
...
```

### Commit message format
```
[CP-01] Add tool result truncation (Layer 1)
[CP-01] Add microcompact stage (Layer 2)
[CP-02] Add IsConcurrencySafe to Tool interface
```

### Testing
Each checkpoint includes test requirements. Run:
```bash
go test ./internal/pipeline/... -v
go test ./internal/tools/... -v
go test ./internal/permissions/... -v
```

### Feature flags
New features behind build tags or config flags:
```go
// config.json
{
  "features": {
    "streaming_tool_exec": true,
    "context_collapse": true,
    "plugin_system": false
  }
}
```
