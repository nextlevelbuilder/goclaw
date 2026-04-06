---
title: "GoClaw v3 Core Architecture Design"
description: "Comprehensive design plan for v3 core redesign — 18 modules covering memory, KG, agent loop, providers, workspace, orchestration"
status: complete
priority: P1
effort: 40h
branch: dev-v3
tags: [refactor, backend, architecture, design]
blockedBy: []
blocks: []
created: 2026-04-06
---

# GoClaw v3 Core Architecture Design

## Overview

Design-first plan for comprehensive v3 core redesign. Produces interface contracts, DB schemas, data flow diagrams, and dependency graphs — NOT implementation code. Implementation plans created per-phase after design audit.

**Brainstorm report:** `plans/reports/brainstorm-260406-1059-goclaw-v3-core-redesign.md`

## Cross-Plan Dependencies

None. This plan blocks future implementation plans (to be created after design audit).

## Key Decisions (from brainstorm)

| # | Module | Decision |
|---|--------|----------|
| 1 | Loop | Pipeline stages (pluggable interfaces) |
| 2 | Memory | 3-tier (working/episodic/semantic) |
| 3 | Context | L0/L1/L2 progressive loading |
| 4 | KG | Temporal validity windows |
| 5 | Token counting | tiktoken-go accurate |
| 6 | Consolidation | Async event-driven |
| 7 | Retrieval | Smart auto-inject L0 + on-demand |
| 8 | Workspace | Unified WorkspaceContext |
| 9 | System prompt | Template-based |
| 10 | Providers | Capability + unified wire + plugin |
| 11 | Self-evolution | Progressive (metrics→suggest→auto) |
| 12 | Orchestration | Layered: delegate base + team optional |
| 13 | Tools | Redesign (registry, policy, workspace-aware) |
| 14 | Events | Redesign (structured, typed) |
| 15 | Skills | Semantic search + ACL + deps |
| 16 | Context files | DB + better abstraction |
| 17 | Migration | Dual-mode v2→v3 |
| 18 | SQLite | Deferred |

## Phases

| Phase | Name | Status | Effort |
|-------|------|--------|--------|
| 1 | [Foundation Interfaces](./phase-01-foundation-interfaces.md) | **Complete** | 8h |
| 2 | [Pipeline Loop Design](./phase-02-pipeline-loop-design.md) | **Complete** | 8h |
| 3 | [Memory & KG Design](./phase-03-memory-kg-design.md) | **Complete** | 8h |
| 4 | [System Integration Design](./phase-04-system-integration-design.md) | **Complete** | 6h |
| 5 | [Orchestration & Intelligence Design](./phase-05-orchestration-intelligence-design.md) | **Complete** | 6h |
| 6 | [Migration & Audit](./phase-06-migration-audit.md) | **Complete** | 4h |

## Dependency Graph

```
Phase 1: Foundation ─────────────────────────┐
  (TokenCounter, WorkspaceContext,            │
   EventBus, ProviderAdapter)                 │
         │                                    │
         ▼                                    │
Phase 2: Pipeline Loop ──────────┐            │
  (Stage interfaces, RunState)   │            │
         │                       │            │
         ▼                       ▼            ▼
Phase 3: Memory & KG      Phase 4: System Integration
  (3-tier, temporal,         (Template prompt, tool
   consolidation, L0/L1/L2)  registry, skills, retrieval)
         │                       │
         └───────────┬───────────┘
                     ▼
           Phase 5: Orchestration & Intelligence
             (Delegate, self-evolution)
                     │
                     ▼
           Phase 6: Migration & Audit
             (v2→v3 dual-mode, test plan)
```

## Deliverables Per Phase

Each phase produces:
- **Interface contracts** — Go interface definitions with method signatures
- **DB schemas** — SQL DDL for new/modified tables
- **Data flow diagrams** — Textual flow descriptions
- **Config schemas** — JSON/YAML configuration structures
- **Dependency map** — Which existing files are affected

## References

- Brainstorm: `plans/reports/brainstorm-260406-1059-goclaw-v3-core-redesign.md`
- Research: `plans/reports/researcher-260406-1059-memory-context-kg-research.md`
- Architecture: `plans/reports/Explore-260406-thorough-v3-architecture.md`
- Loop deep dive: `plans/reports/Explore-260406-GoClaw-Agent-Loop-v3-Deep-Dive.md`
- Workspace forcing: `plans/reports/Explore-260406-workspace-forcing.md`
