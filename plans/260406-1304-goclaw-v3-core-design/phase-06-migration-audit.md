# Phase 6: Migration & Audit

## Context Links
- Brainstorm: `plans/reports/brainstorm-260406-1059-goclaw-v3-core-redesign.md` (sections 16, 17)
- Architecture report: `plans/reports/Explore-260406-thorough-v3-architecture.md` (all sections)
- Workspace forcing: `plans/reports/Explore-260406-workspace-forcing.md`
- Phase 1-5 designs: all prior phase files in this plan
- Current migrations: `migrations/000001_init_schema.up.sql` through `migrations/000036_secure_cli_agent_grants.up.sql`
- Schema version: `internal/upgrade/version.go` (RequiredSchemaVersion = 36)

## Overview
- **Priority:** P1 (blocks implementation start)
- **Status:** Complete
- **Effort:** 4h design

Final design phase — comprehensive migration plan, affected file inventory, test matrix, and audit checklist. Ensures no implementation begins without full impact analysis.

---

## Key Insights

1. Current RequiredSchemaVersion = 36 (migration 000036). V3 adds 3 new tables + 2 ALTER TABLE. Next migration = 000037.
2. KG tables (000013) lack temporal columns. Adding valid_from/valid_until via ALTER is non-breaking — NULL defaults work.
3. Memory system is hybrid: daily .md files in filesystem + memory_documents/memory_chunks in PG. V3 must ingest filesystem files into new episodic_summaries table.
4. Agent settings already uses JSONB — all new per-agent configs (retrieval thresholds, orchestration mode, self-evolution flags, dedup thresholds) fit without schema changes.
5. No SQLite changes in v3 — design is PG-first per brainstorm decision #17. SQLite compat deferred.

---

## Requirements

### Functional
- F1: New tables created via standard golang-migrate files
- F2: ALTER TABLE for KG temporal columns with safe defaults
- F3: Lazy data migration: v2 data auto-converted on first access
- F4: Feature flags per agent for gradual v3 rollout
- F5: Full affected file inventory with dependency order
- F6: Integration test plan covering all 6 workspace scenarios
- F7: Rollback path for every schema change

### Non-Functional
- NF1: Migration runs <30s on databases with 100K entities
- NF2: No downtime required — all changes additive (no DROP/RENAME)
- NF3: v2 agents continue working unchanged until explicitly migrated
- NF4: Test suite runnable in CI (<5 min)

---

## Architecture

### A. DB Schema Migration

#### Migration 000037: v3 memory + evolution tables

```sql
-- 000037_v3_memory_evolution.up.sql

-- Episodic summaries (Tier 2 memory)
CREATE TABLE episodic_summaries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key TEXT NOT NULL,

    summary     TEXT NOT NULL,              -- LLM-generated session summary
    key_topics  TEXT[] DEFAULT '{}',         -- extracted topic keywords
    embedding   vector(1536),               -- for similarity search
    source_type TEXT NOT NULL DEFAULT 'session', -- 'session', 'v2_daily', 'manual'
    source_id   TEXT,                        -- session key or v2 file path (dedup guard)
    turn_count  INT NOT NULL DEFAULT 0,      -- number of turns summarized
    token_count INT NOT NULL DEFAULT 0,      -- estimated tokens in original

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ                  -- NULL = no expiry. TTL configurable per agent.
);

CREATE INDEX idx_episodic_agent_user ON episodic_summaries(agent_id, user_id);
CREATE INDEX idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE INDEX idx_episodic_source ON episodic_summaries(agent_id, source_id);
CREATE INDEX idx_episodic_tsv ON episodic_summaries USING GIN(to_tsvector('simple', summary));
CREATE INDEX idx_episodic_vec ON episodic_summaries USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_episodic_expires ON episodic_summaries(expires_at) WHERE expires_at IS NOT NULL;

-- Evolution metrics (Stage 1 self-evolution)
CREATE TABLE agent_evolution_metrics (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_key TEXT NOT NULL,

    metric_type TEXT NOT NULL,    -- 'retrieval', 'tool', 'feedback'
    metric_key  TEXT NOT NULL,    -- tool name, retrieval source, signal type
    value       JSONB NOT NULL,   -- metric-specific payload

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evo_metrics_agent_type ON agent_evolution_metrics(agent_id, metric_type);
CREATE INDEX idx_evo_metrics_created ON agent_evolution_metrics(created_at);
CREATE INDEX idx_evo_metrics_tenant ON agent_evolution_metrics(tenant_id);

-- Evolution suggestions (Stage 2 self-evolution)
CREATE TABLE agent_evolution_suggestions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,

    suggestion_type TEXT NOT NULL,              -- 'threshold', 'tool_order', 'skill_add', 'memory_prune'
    suggestion      TEXT NOT NULL,
    rationale       TEXT NOT NULL,
    parameters      JSONB,                      -- machine-readable for auto-apply

    status          TEXT NOT NULL DEFAULT 'pending', -- 'pending','approved','rejected','applied','rolled_back'
    reviewed_by     TEXT,
    reviewed_at     TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evo_suggestions_agent ON agent_evolution_suggestions(agent_id, status);
CREATE INDEX idx_evo_suggestions_tenant ON agent_evolution_suggestions(tenant_id);
```

**Down migration:**
```sql
-- 000037_v3_memory_evolution.down.sql
DROP TABLE IF EXISTS agent_evolution_suggestions;
DROP TABLE IF EXISTS agent_evolution_metrics;
DROP TABLE IF EXISTS episodic_summaries;
```

#### Migration 000038: KG temporal columns

```sql
-- 000038_kg_temporal_columns.up.sql

-- Add temporal validity windows to KG entities and relations
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ; -- NULL = current/valid

ALTER TABLE kg_relations ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE kg_relations ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;

-- Index for temporal queries: "current facts" (most common query)
CREATE INDEX idx_kg_entities_current ON kg_entities(agent_id, user_id)
    WHERE valid_until IS NULL;

-- Index for historical queries: "facts valid at time T"
CREATE INDEX idx_kg_entities_temporal ON kg_entities(agent_id, user_id, valid_from, valid_until);

CREATE INDEX idx_kg_relations_current ON kg_relations(agent_id, user_id)
    WHERE valid_until IS NULL;

CREATE INDEX idx_kg_relations_temporal ON kg_relations(agent_id, user_id, valid_from, valid_until);
```

**Down migration:**
```sql
-- 000038_kg_temporal_columns.down.sql
DROP INDEX IF EXISTS idx_kg_relations_temporal;
DROP INDEX IF EXISTS idx_kg_relations_current;
DROP INDEX IF EXISTS idx_kg_entities_temporal;
DROP INDEX IF EXISTS idx_kg_entities_current;

ALTER TABLE kg_relations DROP COLUMN IF EXISTS valid_until;
ALTER TABLE kg_relations DROP COLUMN IF EXISTS valid_from;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS valid_until;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS valid_from;
```

#### RequiredSchemaVersion Bump

```go
// internal/upgrade/version.go
const RequiredSchemaVersion uint = 38
```

---

### B. Data Migration Strategy (Dual-Mode)

#### Principle: Lazy Migration on First Access

V3 reads v2 format on first access, auto-migrates in background. No bulk migration script. No downtime.

#### V2 Memory Files -> Episodic Summaries

```
Trigger: Agent with v3_memory_enabled=true, first session start
Flow:
1. Check if agent has unmigrated v2 daily files:
   - List files matching memory/YYYY-MM-DD.md in workspace
   - Check episodic_summaries for source_id matching file path
   - If no matching episodic record: file is unmigrated
2. For each unmigrated file:
   a. Read file content
   b. Insert episodic_summaries row:
      - summary: file content (already summarized by v2 flush)
      - source_type: "v2_daily"
      - source_id: file path (dedup guard)
      - key_topics: extract from content (simple keyword extraction)
      - turn_count: 0 (unknown for v2)
      - token_count: len(content) / 4 (estimate)
   c. Generate embedding in background (non-blocking)
3. Mark migration complete in agent settings: v2_memory_migrated=true
```

#### V2 KG Entities -> Temporal

```
Trigger: Migration 000038 runs (ALTER TABLE with defaults)
Flow:
1. ALTER TABLE adds valid_from DEFAULT NOW() — new rows get current timestamp
2. Existing rows: valid_from = NOW() at migration time (not created_at)
3. Optional backfill (non-blocking, cron job):
   UPDATE kg_entities SET valid_from = created_at WHERE valid_from > created_at + INTERVAL '1 second';
   UPDATE kg_relations SET valid_from = created_at WHERE valid_from > created_at + INTERVAL '1 second';
4. valid_until remains NULL for all existing entities (all considered "current")
```

> **Decision:** Default valid_from to NOW() at migration time, not created_at. Backfill is optional — historical accuracy is nice-to-have, not required. Existing entities are "currently valid" regardless of when they were created.

#### V2 Sessions -> V3 Sessions

No migration needed. V3 pipeline operates on same session data. Sessions use same `sessions` table, same `SessionData` struct. V3 adds pipeline stages that wrap existing behavior.

#### Feature Flags

All stored in existing `agents.settings` JSONB column:

```json
{
  "v3_pipeline_enabled": false,
  "v3_memory_enabled": false,
  "v3_retrieval_enabled": false,
  "self_evolution_metrics": false,
  "self_evolution_suggestions": false,
  "self_evolution_auto_adapt": false,
  "v2_memory_migrated": false,
  "retrieval_threshold": 0.3,
  "dedup_auto_merge_threshold": 0.98,
  "dedup_flag_threshold": 0.90,
  "orchestration_mode": "spawn"
}
```

**Rollout plan:**
1. Deploy v3 binary with all flags false (default) — no behavior change
2. Enable v3_memory_enabled per agent — triggers lazy v2 file migration
3. Enable v3_pipeline_enabled per agent — switches to Stage-based pipeline
4. Enable v3_retrieval_enabled per agent — activates L0 auto-inject
5. Enable self_evolution_metrics per agent — starts recording
6. Enable at tenant level for bulk rollout

---

### C. Code Migration Plan — Affected Files Inventory

#### Module 1: Foundation (Phase 1)
| File | Change Type | Dependency |
|------|-------------|------------|
| `internal/agent/loop_types.go` | MODIFY: split RunState into substates | None |
| `internal/agent/loop_compact.go` | MODIFY: replace chars/4 with TokenCounter | TokenCounter |
| `internal/agent/pruning.go` | MODIFY: use TokenCounter for thresholds | TokenCounter |
| `internal/tools/context_keys.go` | MODIFY: replace dual workspace keys with WorkspaceContext | None |
| `internal/tools/workspace_resolver.go` | MODIFY: return WorkspaceContext instead of string | None |
| `internal/tools/workspace_dir.go` | MODIFY: use WorkspaceContext | WorkspaceContext |
| `internal/bus/types.go` | MODIFY: add new event topics + typed payloads | None |
| `internal/bus/bus.go` | No change (existing Broadcast mechanism sufficient) | None |
| `internal/providers/types.go` | MODIFY: add ProviderCapabilities struct | None |
| `internal/providers/anthropic.go` | MODIFY: implement ProviderAdapter interface | ProviderAdapter |
| `internal/providers/openai.go` | MODIFY: implement ProviderAdapter interface | ProviderAdapter |
| `internal/providers/dashscope.go` | MODIFY: implement ProviderAdapter interface | ProviderAdapter |
| `internal/providers/codex.go` | MODIFY: implement ProviderAdapter interface | ProviderAdapter |
| `internal/providers/claude_cli.go` | MODIFY: implement ProviderAdapter interface | ProviderAdapter |
| `internal/providers/acp_provider.go` | MODIFY: implement ProviderAdapter interface | ProviderAdapter |

**New files:**
| File | Purpose |
|------|---------|
| `internal/agent/token_counter.go` | TokenCounter interface + tiktoken impl |
| `internal/tools/workspace_context.go` | WorkspaceContext struct + helpers |
| `internal/providers/adapter.go` | ProviderAdapter interface |
| `internal/providers/capabilities.go` | ProviderCapabilities struct |

#### Module 2: Pipeline (Phase 2)
| File | Change Type | Dependency |
|------|-------------|------------|
| `internal/agent/loop.go` | MAJOR: refactor into pipeline stages | Phase 1 foundation |
| `internal/agent/loop_run.go` | MODIFY: pipeline entry point | Pipeline stages |
| `internal/agent/loop_context.go` | MODIFY: becomes ContextStage | WorkspaceContext |
| `internal/agent/loop_history.go` | MODIFY: becomes part of ContextStage | PromptBuilder |
| `internal/agent/loop_tools.go` | MODIFY: becomes ToolStage | ToolRegistry |
| `internal/agent/toolloop.go` | MODIFY: integrated into ToolStage | ToolStage |
| `internal/agent/loop_compact.go` | MODIFY: becomes PruneStage | TokenCounter |
| `internal/agent/loop_finalize.go` | MODIFY: becomes FinalizeStage | EventBus |
| `internal/agent/loop_tracing.go` | MODIFY: per-stage tracing | Pipeline stages |

**New files:**
| File | Purpose |
|------|---------|
| `internal/agent/pipeline.go` | Pipeline, Stage interface, runner |
| `internal/agent/stage_context.go` | ContextStage |
| `internal/agent/stage_think.go` | ThinkStage (LLM call) |
| `internal/agent/stage_prune.go` | PruneStage (token counting + pruning) |
| `internal/agent/stage_tool.go` | ToolStage (tool execution) |
| `internal/agent/stage_observe.go` | ObserveStage (loop detection, metrics) |
| `internal/agent/stage_checkpoint.go` | CheckpointStage (session save) |

#### Module 3: Memory & KG (Phase 3)
| File | Change Type | Dependency |
|------|-------------|------------|
| `internal/memory/embeddings.go` | MODIFY: integrate with episodic store | EpisodicStore |
| `internal/knowledgegraph/extractor.go` | MODIFY: async extraction via events | EventBus |
| `internal/knowledgegraph/extractor_prompt.go` | No change | |
| `internal/store/memory_store.go` | MODIFY: add EpisodicSummaryStore interface | None |
| `internal/store/knowledge_graph_store.go` | MODIFY: add temporal query methods | None |
| `internal/store/pg/memory_docs.go` | No change (v2 compat) | |
| `internal/store/pg/memory_search.go` | MODIFY: integrate episodic search | EpisodicStore |
| `internal/store/pg/knowledge_graph.go` | MODIFY: temporal WHERE clauses | KG temporal cols |
| `internal/store/pg/knowledge_graph_dedup.go` | MODIFY: configurable thresholds | Agent settings |
| `internal/store/pg/knowledge_graph_relations.go` | MODIFY: temporal WHERE clauses | KG temporal cols |
| `internal/store/pg/knowledge_graph_traversal.go` | MODIFY: temporal filter in traversal | KG temporal cols |
| `internal/store/pg/knowledge_graph_embedding.go` | No change | |
| `internal/agent/memoryflush.go` | MODIFY: v3 path uses event-driven consolidation | EventBus |
| `internal/agent/extractive_memory.go` | No change (fallback for v2 mode) | |

**New files:**
| File | Purpose |
|------|---------|
| `internal/store/episodic_store.go` | EpisodicSummaryStore interface |
| `internal/store/pg/episodic_summaries.go` | PG implementation |
| `internal/agent/consolidation_worker.go` | Event-driven session -> episodic -> semantic |
| `internal/agent/memory_migrator.go` | Lazy v2 daily files -> episodic migration |

#### Module 4: System Integration (Phase 4)
| File | Change Type | Dependency |
|------|-------------|------------|
| `internal/agent/systemprompt.go` | MAJOR: replace with PromptBuilder | PromptConfig types |
| `internal/agent/systemprompt_sections.go` | REMOVE: logic moves to template | PromptBuilder |
| `internal/tools/types.go` | MODIFY: add Metadata() to Tool interface | None |
| `internal/tools/registry.go` | MODIFY: store ToolMetadata | ToolMetadata |
| `internal/tools/policy.go` | MODIFY: capability rules + role param | ToolCapability |
| `internal/tools/filesystem.go` | MODIFY: WorkspaceContextFromCtx() | WorkspaceContext |
| `internal/tools/filesystem_write.go` | MODIFY: WorkspaceContextFromCtx() | WorkspaceContext |
| `internal/tools/filesystem_list.go` | MODIFY: WorkspaceContextFromCtx() | WorkspaceContext |
| `internal/tools/shell.go` | MODIFY: WorkspaceContextFromCtx() | WorkspaceContext |
| `internal/tools/edit.go` | MODIFY: WorkspaceContextFromCtx() | WorkspaceContext |
| `internal/tools/memory.go` | MODIFY: add depth parameter | L0/L1/L2 |
| `internal/tools/knowledge_graph.go` | MODIFY: add as_of temporal param | KG temporal |
| `internal/tools/skill_search.go` | MODIFY: hybrid search integration | HybridSearcher |
| `internal/skills/loader.go` | MODIFY: ACL-aware BuildSummary() | SkillAccessStore |
| `internal/skills/search.go` | MODIFY: expose for fusion with embedding | None |

**New files:** (listed in Phase 4 design)

#### Module 5: Orchestration & Intelligence (Phase 5)
| File | Change Type | Dependency |
|------|-------------|------------|
| `internal/tools/policy.go` | MODIFY: orchestration mode filtering | OrchestrationMode |
| `internal/bus/types.go` | MODIFY: delegation + evolution event types | None |
| `internal/agent/loop.go` | MODIFY: metrics recording hooks | EvolutionMetrics |
| `cmd/gateway.go` | MODIFY: register delegate tool, event subscribers | All Phase 5 |

**New files:** (listed in Phase 5 design)

#### Breaking Changes Inventory

| Change | Impact | Backward Compat |
|--------|--------|-----------------|
| Tool.Metadata() added to interface | All tool implementations | Wrapper: `defaultMetadata(tool)` |
| WorkspaceContext replaces dual keys | All file tools + loop_context | Single migration: grep `ToolWorkspaceFromCtx` (15 sites) |
| PromptBuilder replaces BuildSystemPrompt() | loop_history.go, tests | Bridge: old func calls PromptBuilder.Build() |
| Pipeline stages replace runLoop() | loop.go (804 lines) | Feature flag: v3_pipeline_enabled per agent |
| TokenCounter replaces chars/4 | loop_compact.go, pruning.go | Fallback: chars/4 for unknown models |
| KG temporal columns | All KG queries | Default: WHERE valid_until IS NULL (same as pre-temporal) |
| Episodic summaries table | New (no existing code depends on it) | Fully additive |
| Evolution metrics/suggestions | New (no existing code depends on it) | Fully additive |

---

### D. Integration Test Plan

#### Test Matrix: Workspace Scenarios

From workspace forcing report — 6 scenarios must pass:

| # | Scenario | ActivePath | Scope | Key Assertion |
|---|----------|------------|-------|---------------|
| 1 | Solo DM, isolated | `{base}/{userID}` | personal | Files scoped to user dir |
| 2 | Solo DM, shared | `{base}` | personal | Files in shared agent dir |
| 3 | Team dispatch (member) | `{base}/teams/{teamID}/{chatID}` | team-shared | Workspace forced to team dir |
| 4 | Team dispatch (leader) | `{base}/teams/{teamID}/{chatID}` | team-shared | Leader sees team workspace |
| 5 | Team inbound (member) | `{base}/teams/{teamID}/{chatID}` | team-isolated | Per-chat isolation within team |
| 6 | Delegate workspace | delegatee workspace + shared area | delegate | Read-only exports enforced |

#### Memory Tier Tests

| Test | Input | Expected |
|------|-------|----------|
| Working memory (Tier 1) | 20 messages in session | All messages in context window |
| Episodic creation | Session end event | episodic_summaries row created |
| Episodic dedup | Same session end event replayed | No duplicate (source_id unique) |
| Semantic extraction | Episodic with entities | KG entities upserted |
| L0 auto-inject | User message with relevant topic | Memory section in system prompt |
| L0 skip | "hello" message (low relevance) | No Memory section |
| V2 migration | Agent with memory/2026-01-01.md | Episodic row with source_type=v2_daily |
| TTL expiry | Episodic with expires_at in past | Cleaned by cron |

#### KG Temporal Tests

| Test | Input | Expected |
|------|-------|----------|
| New entity | UpsertEntity() | valid_from=NOW(), valid_until=NULL |
| Supersede entity | Contradicting fact | Old: valid_until=NOW(). New: valid_from=NOW() |
| Current query | SearchEntities() default | Only valid_until IS NULL entities |
| Historical query | SearchEntities(as_of="2026-01-01") | Entities valid at that date |
| Temporal traversal | Traverse() with temporal entities | Only current relations followed |
| Configurable dedup | Agent with threshold 0.95 | Auto-merge at 0.95, not hardcoded 0.98 |

#### Pipeline Stage Tests

| Test | Stage | Input | Expected |
|------|-------|-------|----------|
| Context build | ContextStage | RunRequest + agent config | WorkspaceContext resolved, system prompt built |
| LLM call | ThinkStage | Messages array | Provider called via adapter |
| Token pruning | PruneStage | Messages exceeding 85% context | Older messages pruned, token count accurate |
| Tool exec | ToolStage | Tool call from LLM | Tool executed via registry with policy check |
| Loop detect | ObserveStage | 3x same tool call | Loop detected, force stop |
| Session save | CheckpointStage | Completed run | Session persisted, events emitted |
| Full pipeline | All stages | Complete request | End-to-end: request -> response -> session saved |

#### Provider Adapter Tests

| Test | Provider | Input | Expected |
|------|----------|-------|----------|
| Anthropic wire | Anthropic | Internal ChatRequest | Valid Anthropic API JSON |
| OpenAI wire | OpenAI | Internal ChatRequest | Valid OpenAI API JSON |
| DashScope no-stream-tools | DashScope | ChatRequest with tools | Streaming disabled, tools present |
| Codex OAuth | Codex | ChatRequest | OAuth token refreshed if needed |
| Claude CLI | ClaudeCLI | ChatRequest | Stdio command built correctly |
| Capabilities | All providers | Capabilities() | Correct flags per provider |
| Round-trip | All providers | ToRequest -> FromResponse | Internal format preserved |

#### Regression Tests

| Test | Assertion |
|------|-----------|
| V2 agent with all flags false | Identical behavior to pre-v3 binary |
| V2 memory flush | Daily .md files still written when v3_memory_enabled=false |
| V2 KG without temporal | Queries return same results (WHERE valid_until IS NULL matches all) |
| V2 system prompt | PromptBuilder with v2 config produces identical output |
| V2 tool registry | Tools without Metadata() get default metadata wrapper |

---

### E. Audit Checklist

#### Interface Contracts
- [ ] All interface contracts have method signatures with ctx context.Context as first param
- [ ] All interfaces have at least one implementation (PG store)
- [ ] No interface has >10 methods (split if so)
- [ ] Error returns are typed (not raw string errors)
- [ ] Context propagation: tenant_id, agent_id, user_id carried through all store calls

#### DB Schemas
- [ ] All new tables have tenant_id column with FK to tenants(id)
- [ ] All FKs have ON DELETE CASCADE where appropriate
- [ ] Indexes exist for: primary lookup, tenant filter, time-range queries
- [ ] No new columns on existing tables (all config in JSONB settings)
- [ ] Down migrations cleanly reverse up migrations
- [ ] No PG-specific features in interface definitions (SQLite compat future)

#### Data Flow
- [ ] Memory flow: user message -> auto-inject L0 -> response -> session end -> episodic -> semantic
- [ ] KG flow: text -> extract -> upsert -> dedup -> temporal validity
- [ ] Pipeline flow: ContextStage -> ThinkStage -> PruneStage -> ToolStage -> ObserveStage -> CheckpointStage
- [ ] Delegation flow: delegate() -> permission check -> workspace setup -> AgentRunner -> result delivery
- [ ] Event flow: stage completion -> EventBus.Broadcast() -> subscriber handlers

#### Breaking Changes
- [ ] Tool.Metadata() — wrapper provided for existing tools
- [ ] WorkspaceContext — single context key replaces 2 (grep all call sites)
- [ ] BuildSystemPrompt() — bridge function wraps PromptBuilder
- [ ] Pipeline stages — feature flag gates new behavior
- [ ] Token counting — fallback to chars/4 for unknown models

#### Security Review
- [ ] Workspace isolation: WorkspaceContext.ActivePath enforced by resolvePath()
- [ ] Delegate read-only exports: validated against delegator's workspace boundary
- [ ] Delegate shared area: created with restrictive permissions (0750)
- [ ] Tenant boundary: all new tables have tenant_id, all queries filter by it
- [ ] Metrics data: no PII stored (queries + tool names, not user content)
- [ ] Template injection: PromptConfig data passed via {{ printf "%s" }} not {{ }}
- [ ] Auto-adapt scope: only modifies retrieval params, not security settings

#### Performance Review
- [ ] tiktoken-go: benchmark Count() for Claude models (<1ms per 1000 tokens)
- [ ] pgvector: benchmark episodic_summaries search at 100K rows (<50ms)
- [ ] Event bus: benchmark Broadcast throughput (10K events/sec target)
- [ ] Template rendering: benchmark Build() (<1ms, no allocation-heavy paths)
- [ ] L0 auto-inject: benchmark memory index query (<10ms per turn)
- [ ] KG temporal: benchmark index scan with valid_until IS NULL filter
- [ ] Metrics recording: verify non-blocking (goroutine, no mutex on hot path)

---

## Related Code Files

### Migration Files to Create
| File | Purpose |
|------|---------|
| `migrations/000037_v3_memory_evolution.up.sql` | episodic_summaries, agent_evolution_metrics, agent_evolution_suggestions |
| `migrations/000037_v3_memory_evolution.down.sql` | Drop above tables |
| `migrations/000038_kg_temporal_columns.up.sql` | ALTER kg_entities/kg_relations add valid_from/valid_until |
| `migrations/000038_kg_temporal_columns.down.sql` | DROP added columns + indexes |

### Files to Modify
| File | Change |
|------|--------|
| `internal/upgrade/version.go` | RequiredSchemaVersion = 38 |

### Full Affected File Count by Module

| Module | Files Modified | Files Created | Total |
|--------|---------------|---------------|-------|
| Foundation (Phase 1) | 15 | 4 | 19 |
| Pipeline (Phase 2) | 9 | 7 | 16 |
| Memory & KG (Phase 3) | 14 | 4 | 18 |
| System Integration (Phase 4) | 15 | 9 | 24 |
| Orchestration (Phase 5) | 4 | 10 | 14 |
| Migration (Phase 6) | 1 | 4 | 5 |
| **Total** | **58** | **38** | **96** |

---

## Design Steps

1. Write migration 000037 DDL (episodic_summaries, evolution tables)
2. Write migration 000038 DDL (KG temporal columns)
3. Write down migrations for both
4. Document lazy v2 memory migration flow
5. Document KG temporal backfill strategy
6. List all feature flags with defaults
7. Inventory all affected files per module (complete)
8. Document breaking changes with migration path
9. Write workspace scenario test matrix
10. Write memory tier test scenarios
11. Write KG temporal test scenarios
12. Write pipeline stage test scenarios
13. Write provider adapter test matrix
14. Write regression test list
15. Complete audit checklist

---

## Todo List

- [ ] A1: Migration 000037 DDL (3 new tables)
- [ ] A2: Migration 000038 DDL (KG temporal ALTER)
- [ ] A3: Down migrations for both
- [ ] A4: RequiredSchemaVersion bump to 38
- [ ] B1: V2 memory -> episodic lazy migration design
- [ ] B2: KG temporal backfill strategy
- [ ] B3: Feature flags catalog with defaults
- [ ] B4: Session v2->v3 compatibility notes
- [ ] C1: Affected files inventory per module (complete)
- [ ] C2: Code change dependency order
- [ ] C3: Breaking changes list with backward-compat path
- [ ] D1: Workspace scenario test matrix (6 scenarios)
- [ ] D2: Memory tier tests (8 scenarios)
- [ ] D3: KG temporal tests (6 scenarios)
- [ ] D4: Pipeline stage tests (7 scenarios)
- [ ] D5: Provider adapter tests (7 scenarios)
- [ ] D6: Regression tests (5 scenarios)
- [ ] E1: Interface contracts audit
- [ ] E2: DB schema audit
- [ ] E3: Data flow audit
- [ ] E4: Breaking changes audit
- [ ] E5: Security review
- [ ] E6: Performance review

---

## Success Criteria

1. Both migrations run cleanly on empty DB and on DB with existing v2 data
2. Down migrations fully reverse up migrations (no orphaned objects)
3. RequiredSchemaVersion matches highest migration number
4. All 6 workspace scenarios pass integration tests
5. V2 agents with all flags false behave identically to pre-v3
6. Every breaking change has documented backward-compat path
7. Audit checklist 100% complete before implementation begins

---

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| Migration fails on large KG tables | MEDIUM | ALTER TABLE ADD COLUMN with DEFAULT is metadata-only in PG12+ (instant). No table rewrite. |
| V2 memory files not found (path differences) | LOW | Lazy migration lists actual files in workspace. Files missing = nothing to migrate. |
| Feature flag combinatorial explosion | MEDIUM | 6 boolean flags = 64 combos. Test key combos only: all-off (v2), all-on (v3), individual toggles. |
| 96 affected files scope | HIGH | Implementation phased by module. Each module independently deployable behind feature flags. |
| Episodic summaries table grows large | MEDIUM | expires_at column + cleanup cron. Index on expires_at for efficient deletion. |
| Temporal KG backfill slow | LOW | Backfill is optional, runs as cron in background. No downtime. Default valid_from=NOW() sufficient. |
| SQLite compat deferred too long | MEDIUM | Interfaces designed PG-agnostic. SQLite impl can be added later without interface changes. |

---

## Rollback Plan

### Per-Migration Rollback
```bash
./goclaw migrate down 2  # reverts 000038, then 000037
```

### Per-Feature Rollback
Set feature flag to false in agent settings:
```json
{ "v3_pipeline_enabled": false, "v3_memory_enabled": false }
```
Agent immediately reverts to v2 behavior. No code rollback needed.

### Full v3 Rollback
1. Disable all v3 feature flags for all agents
2. Run `migrate down 2` to remove v3 tables
3. Deploy v2 binary
4. v2 memory files, KG entities, sessions — all intact (v3 tables were additive)

---

## Security Considerations

- **Migration safety:** All changes additive (CREATE TABLE, ALTER ADD COLUMN). No DROP, RENAME, or data modification in up migration. Down migration only drops v3-added objects.
- **Feature flag isolation:** v3 code paths gated by flags. Flag false = v2 code path. No mixed state.
- **Tenant isolation in new tables:** All 3 new tables have tenant_id FK. All queries must include tenant_id filter. Store implementations enforce via context.
- **Evolution metrics cleanup:** Cron job with configurable TTL (default 90 days). Prevents unbounded growth.
- **Temporal KG queries:** Default WHERE valid_until IS NULL produces same results as pre-temporal. No data exposure from temporal columns.

---

## Implementation Dependency Order

```
Foundation (can start immediately, no dependencies):
  1. TokenCounter (internal/agent/token_counter.go)
  2. WorkspaceContext (internal/tools/workspace_context.go)
  3. EventBus topics (internal/bus/types.go)
  4. ProviderAdapter interface (internal/providers/adapter.go)

Core (depends on Foundation):
  5. Pipeline stages (internal/agent/pipeline.go + stage_*.go)
  6. PromptBuilder + templates (internal/agent/prompt_builder.go)
  7. Tool Metadata + enhanced registry (internal/tools/capability.go)

Memory & KG (depends on Foundation + migrations):
  8. DB migrations 000037 + 000038
  9. EpisodicSummaryStore (internal/store/episodic_store.go)
  10. KG temporal queries (modify existing pg/ files)
  11. Consolidation worker (internal/agent/consolidation_worker.go)
  12. L0 retriever (internal/agent/retriever.go)

Integration (depends on Core + Memory):
  13. Hybrid skill searcher (internal/skills/hybrid_searcher.go)
  14. Tool policy capability rules (internal/tools/policy.go)
  15. Provider adapters (all 6 providers)

Orchestration (depends on Integration):
  16. DelegateTool (internal/tools/delegate_tool.go)
  17. Orchestration mode config (internal/agent/orchestration_mode.go)
  18. Evolution metrics (internal/store/pg/evolution_metrics.go)
  19. Evolution suggestions (internal/agent/evolution_suggestion_engine.go)

Testing (depends on all above):
  20. Integration tests (tests/integration/v3_*.go)
  21. Regression tests (tests/integration/v2_compat_*.go)
```

Items 1-4 are independent and can be built in parallel.
Items 5-7 are independent of each other but depend on 1-4.
Items 8-12 are sequential (migrations first, then stores, then workers).
