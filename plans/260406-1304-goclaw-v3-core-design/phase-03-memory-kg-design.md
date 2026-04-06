# Phase 3: Memory & KG Design

## Context Links

- Brainstorm: `plans/reports/brainstorm-260406-1059-goclaw-v3-core-redesign.md` (decisions 2, 3, 4, 6, 7)
- Research: `plans/reports/researcher-260406-1059-memory-context-kg-research.md`
- Architecture: `plans/reports/Explore-260406-thorough-v3-architecture.md` (sections 1, 2, 8, 10)
- Phase 1: `phase-01-foundation-interfaces.md` (TokenCounter, DomainEventBus)
- Phase 2: `phase-02-pipeline-loop-design.md` (PruneStage, MemoryFlushStage, ObserveStage)
- Current memory store: `internal/store/memory_store.go`
- Current KG store: `internal/store/knowledge_graph_store.go`
- Current KG migration: `migrations/000013_knowledge_graph.up.sql`
- Current memory migration: `migrations/000001_init_schema.up.sql` (lines 163-192)

## Overview

- **Priority:** P1 (core cognitive capability)
- **Status:** Complete
- **Effort:** 8h design
- **Depends on:** Phase 1 (TokenCounter for L0 budget, DomainEventBus for consolidation), Phase 2 (pipeline integration points)

Design 3-tier memory system, temporal KG, consolidation pipeline, L0/L1/L2 context tiering, and smart auto-inject retrieval. This phase transforms GoClaw from append-only memory to a curated, searchable, self-maintaining knowledge system.

## Key Insights

- Current memory is append-only daily .md files, never revisited. Contradictions accumulate, no consolidation, no dedup
- KG extraction is synchronous (blocks loop). Dedup thresholds hardcoded (0.98/0.90). No temporal awareness -- stale facts never expire
- Memory search is hybrid FTS+vector but flat -- no tier awareness, no relevance-based injection
- Karpathy insight: memory should be curated wiki, not append-only log
- OpenViking insight: L0/L1/L2 tiering saves 10x tokens vs full-context loading
- Graphiti insight: temporal validity windows on KG facts enable contradiction resolution

---

## A. 3-Tier Memory Schema

### Tier 1: Working Memory

In context window. No new storage needed. Managed by MessageBuffer in pipeline.

- Content: recent 20-50 messages (conversation turns)
- Lifecycle: per-turn, free (no persistence cost)
- Size: up to 80% of context window budget
- Pruned by: PruneStage (token-accurate via TokenCounter)

### Tier 2: Episodic Memory

New table. Session summaries + key interactions with pgvector embeddings.

```sql
-- Migration: 000XXX_episodic_summaries.up.sql

CREATE TABLE episodic_summaries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key VARCHAR(500) NOT NULL,

    -- Content
    summary       TEXT NOT NULL,           -- LLM-generated session summary
    key_entities  JSONB DEFAULT '[]',      -- entity names extracted (for downstream KG)
    key_topics    JSONB DEFAULT '[]',      -- topic tags for BM25
    message_count INT NOT NULL DEFAULT 0,  -- messages in summarized session
    token_count   INT NOT NULL DEFAULT 0,  -- total tokens in session

    -- Embeddings for semantic search
    embedding     vector(1536),
    tsv           tsvector GENERATED ALWAYS AS (to_tsvector('simple', summary)) STORED,

    -- L0 abstract (pre-computed, ~50 tokens)
    l0_abstract   TEXT NOT NULL DEFAULT '',

    -- Lifecycle
    source_id     VARCHAR(255) NOT NULL DEFAULT '', -- dedup key (session_key + compaction_count)
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    expires_at    TIMESTAMPTZ,                      -- NULL = no expiry, default 90d from creation

    UNIQUE(agent_id, user_id, source_id)
);

CREATE INDEX idx_episodic_scope ON episodic_summaries(agent_id, user_id);
CREATE INDEX idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE INDEX idx_episodic_created ON episodic_summaries(agent_id, user_id, created_at DESC);
CREATE INDEX idx_episodic_expires ON episodic_summaries(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX idx_episodic_tsv ON episodic_summaries USING gin(tsv);
CREATE INDEX idx_episodic_embedding ON episodic_summaries USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 50);
```

### Tier 3: Semantic Memory

Existing KG tables (`kg_entities`, `kg_relations`) + temporal fields (section B). Facts extracted from episodic summaries via consolidation pipeline. Permanent, deduplicated.

No new table needed. Temporal fields added to existing tables.

### Tier Flow

```
Working (Tier 1)                    Episodic (Tier 2)              Semantic (Tier 3)
[in context window]                 [episodic_summaries]           [kg_entities + kg_relations]
        |                                   |                              |
        |-- session end / compaction ------>|                              |
        |   (session.completed event)       |                              |
        |                                   |-- extraction worker -------->|
        |                                   |   (episodic.created event)   |
        |                                   |                              |
        |<-- auto-inject L0 summaries ------|                              |
        |<-- memory_search L1/L2 ---------->|                              |
        |<-- kg_search ------------------------------------------->|       |
```

---

## B. Temporal KG Schema

### Schema Changes

```sql
-- Migration: 000XXX_kg_temporal.up.sql

-- Add temporal validity to entities
ALTER TABLE kg_entities
    ADD COLUMN valid_from  TIMESTAMPTZ DEFAULT NOW(),
    ADD COLUMN valid_until TIMESTAMPTZ;  -- NULL = currently valid

-- Add temporal validity to relations
ALTER TABLE kg_relations
    ADD COLUMN valid_from  TIMESTAMPTZ DEFAULT NOW(),
    ADD COLUMN valid_until TIMESTAMPTZ;

-- Backfill existing data: valid_from = created_at, valid_until = NULL (current)
UPDATE kg_entities SET valid_from = created_at WHERE valid_from IS NULL;
UPDATE kg_relations SET valid_from = created_at WHERE valid_from IS NULL;

-- Partial index for current facts (most common query)
CREATE INDEX idx_kg_entities_current ON kg_entities(agent_id, user_id)
    WHERE valid_until IS NULL;
CREATE INDEX idx_kg_relations_current ON kg_relations(agent_id, user_id)
    WHERE valid_until IS NULL;

-- Add configurable dedup thresholds (per-agent, stored in agents.other_config JSONB)
-- No schema change needed -- uses existing agents.other_config field:
-- { "kg_dedup_auto_threshold": 0.98, "kg_dedup_flag_threshold": 0.90 }
```

### Query Patterns

```sql
-- Current facts (default, most queries)
SELECT * FROM kg_entities
WHERE agent_id = $1 AND user_id = $2 AND valid_until IS NULL;

-- Historical: what was true at a specific time?
SELECT * FROM kg_entities
WHERE agent_id = $1 AND user_id = $2
  AND valid_from <= $3
  AND (valid_until IS NULL OR valid_until >= $3);

-- Supersede a fact: new contradicting fact arrives
UPDATE kg_entities SET valid_until = NOW(), updated_at = NOW()
WHERE agent_id = $1 AND user_id = $2 AND external_id = $3 AND valid_until IS NULL;
-- Then INSERT new entity with valid_from = NOW()
```

### Contradiction Resolution

When extraction produces a fact that contradicts an existing current fact:

```
1. Extractor produces: "User works at CompanyB" (confidence 0.85)
2. Existing current fact: "User works at CompanyA" (confidence 0.90)
3. If same external_id + same entity_type:
   a. Supersede old: SET valid_until = NOW() on CompanyA fact
   b. Insert new: CompanyB with valid_from = NOW()
4. If different external_id but related via relation:
   a. Flag as dedup candidate (human review)
```

### Configurable Thresholds

```go
// Per-agent KG config (stored in agents.other_config JSONB)
type KGConfig struct {
    DedupAutoThreshold float64 `json:"kg_dedup_auto_threshold"` // default 0.98
    DedupFlagThreshold float64 `json:"kg_dedup_flag_threshold"` // default 0.90
    ExtractionMinConf  float64 `json:"kg_extraction_min_conf"`  // default 0.75
    EnableTemporal     bool    `json:"kg_enable_temporal"`       // default true
}
```

---

## C. Consolidation Pipeline

Event-driven, async, non-blocking. User never waits for consolidation.

### Event Flow

```
session.completed
    |
    v
EpisodicWorker
    |-- Summarize session messages via LLM
    |-- Extract key entities (names only, for downstream)
    |-- Generate L0 abstract (~1 sentence, ~50 tokens)
    |-- Store in episodic_summaries
    |-- Generate embedding (async)
    |-- Publish: episodic.created
    |
    v
SemanticExtractionWorker
    |-- Read episodic summary
    |-- Run KG extractor on summary (not full session -- cheaper)
    |-- Upsert entities + relations with temporal fields
    |-- Publish: entity.upserted
    |
    v
DedupWorker
    |-- Read newly upserted entity IDs
    |-- Compare embeddings against existing entities
    |-- Auto-merge (> auto_threshold + name match)
    |-- Flag candidates (> flag_threshold)
    |-- Handle temporal supersession for contradictions
```

### Worker Implementations

```go
// Package: internal/consolidation

// EpisodicWorker processes session.completed events.
type EpisodicWorker struct {
    store       store.EpisodicStore
    provider    providers.Provider // for LLM summarization
    tokenCounter tokencount.TokenCounter
    eventBus    eventbus.DomainEventBus
}

func (w *EpisodicWorker) Handle(ctx context.Context, event eventbus.DomainEvent) error {
    payload := event.Payload.(eventbus.SessionCompletedPayload)

    // Skip if already processed (idempotent by source_id)
    sourceID := fmt.Sprintf("%s:%d", payload.SessionKey, payload.CompactionCount)
    if w.store.ExistsBySourceID(ctx, event.AgentID, event.UserID, sourceID) {
        return nil // already processed
    }

    // 1. Summarize session
    summary, err := w.summarizeSession(ctx, payload)
    if err != nil {
        return fmt.Errorf("summarize: %w", err)
    }

    // 2. Extract key entities (lightweight -- name extraction, not full KG)
    entities := extractEntityNames(summary)

    // 3. Generate L0 abstract
    l0 := generateL0Abstract(summary) // extractive: first sentence or topic sentence

    // 4. Store
    ep := &store.EpisodicSummary{
        AgentID:      event.AgentID,
        UserID:       event.UserID,
        TenantID:     event.TenantID,
        SessionKey:   payload.SessionKey,
        Summary:      summary,
        KeyEntities:  entities,
        L0Abstract:   l0,
        MessageCount: payload.MessageCount,
        TokenCount:   payload.TokensUsed,
        SourceID:     sourceID,
        ExpiresAt:    timePtr(time.Now().Add(90 * 24 * time.Hour)), // 90d TTL
    }
    if err := w.store.Create(ctx, ep); err != nil {
        return fmt.Errorf("store episodic: %w", err)
    }

    // 5. Publish for downstream
    w.eventBus.Publish(eventbus.DomainEvent{
        Type:    eventbus.EventEpisodicCreated,
        AgentID: event.AgentID,
        UserID:  event.UserID,
        Payload: eventbus.EpisodicCreatedPayload{
            EpisodicID:  ep.ID,
            SessionKey:  payload.SessionKey,
            Summary:     summary,
            KeyEntities: entities,
        },
    })

    return nil
}
```

### Summarization Prompt (Episodic)

```
Summarize this conversation session concisely. Focus on:
- Key decisions made
- Facts learned about the user or project
- Tasks completed or in-progress
- Important technical details
- User preferences expressed

Output: 2-4 paragraph summary. Include entity names (people, projects, tools) explicitly.
Do NOT include greetings, filler, or metadata.
```

### L0 Abstract Generation

V3.0: extractive (first meaningful sentence of summary). V3.1+: LLM-generated.

```go
// generateL0Abstract produces a ~50 token abstract from episodic summary.
// V3.0: extractive (first sentence). Future: LLM-generated.
func generateL0Abstract(summary string) string {
    // Split into sentences, take first non-trivial one
    sentences := splitSentences(summary)
    for _, s := range sentences {
        s = strings.TrimSpace(s)
        if len(s) > 20 { // skip very short fragments
            if len(s) > 200 { // truncate if too long
                s = s[:200] + "..."
            }
            return s
        }
    }
    // Fallback: first 200 chars
    if len(summary) > 200 {
        return summary[:200] + "..."
    }
    return summary
}
```

---

## D. L0/L1/L2 Context Tiering

### Layer Definitions

| Layer | Content | Token cost | When loaded | Storage |
|-------|---------|-----------|-------------|---------|
| **L0** | 1-sentence abstract | ~50 tokens | Always (auto-inject if relevant) | `episodic_summaries.l0_abstract` |
| **L1** | Structured overview | ~200 tokens | When topic relevant | Generated on-demand, cached |
| **L2** | Full content | Full | On-demand via tool call | `episodic_summaries.summary` (episodic) or original document (memory docs) |

### L1 Generation

On-demand when agent requests deeper context or auto-inject detects high relevance.

```go
// L1Cache caches generated L1 overviews. LRU, 500 entries, 1h TTL.
type L1Cache struct {
    cache *lru.Cache[string, string] // key: episodic_id or doc_path
}

// GenerateL1 creates structured overview from episodic summary or memory doc.
func (c *L1Cache) GenerateL1(id string, fullContent string) string {
    if cached, ok := c.cache.Get(id); ok {
        return cached
    }
    // Extractive: key bullet points from full content
    l1 := extractKeyPoints(fullContent, 200) // ~200 tokens budget
    c.cache.Add(id, l1)
    return l1
}
```

### L2 Access

Full content loaded via tool call. Agent calls `memory_search(query, depth="full")` or `memory_expand(id)`.

```go
// memory_expand tool: loads L2 full content for a specific memory entry
// Parameters: id (episodic ID or document path)
// Returns: full content (summary or document)
```

---

## E. Smart Auto-Inject Retrieval

Per-turn relevance check + L0 injection. Non-blocking, lightweight.

### Flow

```
User message arrives (in ContextStage)
    |
    v
1. Extract intent keywords from user message
   - Tokenize, remove stopwords, keep nouns/verbs/proper nouns
   - If message too short ("hello", "ok"): skip injection (no relevant context)
    |
    v
2. BM25 + embedding relevance check against episodic index
   - BM25 on episodic_summaries.tsv (fast, FTS)
   - Embedding similarity on episodic_summaries.embedding (if available)
   - Merge scores (configurable weights)
    |
    v
3. Filter by relevance threshold
   - Default: 0.3 (tunable per agent via agent config)
   - If no results above threshold: skip injection
    |
    v
4. Inject "## Memory Context" section into system prompt
   - Max 5 entries, max ~200 tokens total (L0 summaries only)
   - Format per entry: "- {topic}: {L0 abstract} [use memory_search for details]"
    |
    v
5. Agent sees hint, can call memory_search for L1/L2
```

### Interface

```go
// Package: internal/memory

// AutoInjector checks relevance and produces L0 injection for system prompt.
type AutoInjector interface {
    // Inject checks user message against memory index.
    // Returns formatted section for system prompt, or "" if nothing relevant.
    // Budget: max ~200 tokens of L0 summaries.
    Inject(ctx context.Context, params InjectParams) (string, error)
}

type InjectParams struct {
    AgentID    string
    UserID     string
    TenantID   string
    UserMessage string
    MaxEntries int     // default 5
    MaxTokens  int     // default 200
    Threshold  float64 // relevance threshold (default 0.3)
}

// InjectResult for observability
type InjectResult struct {
    Section    string   // formatted prompt section
    MatchCount int      // total matches found
    Injected   int      // entries injected (after budget trim)
    TopScore   float64  // highest relevance score
}
```

### System Prompt Section (generated by AutoInjector)

```
## Memory Context

Relevant memories from past sessions (summaries only -- use memory_search for details):
- Project Alpha: Discussed migration from PostgreSQL to CockroachDB, decided to stay with PG for now.
- User preferences: Prefers Go over Python for backend, uses Neovim as primary editor.
- Bug fix: Resolved auth token expiration issue in session middleware (2026-03-15).

Use `memory_search` tool to retrieve full details on any topic above.
```

### Agent Config

```go
// Per-agent memory config (extends existing config.MemoryConfig)
type MemoryConfig struct {
    // Existing fields...
    Enabled bool `json:"enabled"`

    // V3 additions
    AutoInjectEnabled   bool    `json:"auto_inject_enabled"`    // default true
    AutoInjectThreshold float64 `json:"auto_inject_threshold"`  // default 0.3
    AutoInjectMaxTokens int     `json:"auto_inject_max_tokens"` // default 200
    EpisodicTTLDays     int     `json:"episodic_ttl_days"`      // default 90 (0 = no expiry)
    ConsolidationEnabled bool   `json:"consolidation_enabled"`  // default true
}
```

---

## F. MemoryStore v3 Interface

Extends existing `MemoryStore` with tier-aware operations.

```go
// Package: internal/store

// EpisodicSummary represents a Tier 2 episodic memory entry.
type EpisodicSummary struct {
    ID           string    `json:"id"`
    TenantID     string    `json:"tenant_id"`
    AgentID      string    `json:"agent_id"`
    UserID       string    `json:"user_id"`
    SessionKey   string    `json:"session_key"`
    Summary      string    `json:"summary"`
    KeyEntities  []string  `json:"key_entities"`
    KeyTopics    []string  `json:"key_topics"`
    L0Abstract   string    `json:"l0_abstract"`
    MessageCount int       `json:"message_count"`
    TokenCount   int       `json:"token_count"`
    SourceID     string    `json:"source_id"`
    CreatedAt    time.Time `json:"created_at"`
    ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// EpisodicSearchResult is a search hit with tier indicator.
type EpisodicSearchResult struct {
    EpisodicID  string  `json:"episodic_id"`
    L0Abstract  string  `json:"l0_abstract"` // always populated
    Score       float64 `json:"score"`
    CreatedAt   time.Time `json:"created_at"`
    SessionKey  string  `json:"session_key"`
}

// EpisodicStore manages Tier 2 episodic memory.
type EpisodicStore interface {
    // CRUD
    Create(ctx context.Context, ep *EpisodicSummary) error
    Get(ctx context.Context, id string) (*EpisodicSummary, error)
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, agentID, userID string, limit, offset int) ([]EpisodicSummary, error)

    // Search (hybrid FTS + vector, returns L0 by default)
    Search(ctx context.Context, query string, agentID, userID string, opts EpisodicSearchOptions) ([]EpisodicSearchResult, error)

    // Lifecycle
    ExistsBySourceID(ctx context.Context, agentID, userID, sourceID string) (bool, error)
    PruneExpired(ctx context.Context) (int, error) // delete where expires_at < NOW()

    // Embedding
    SetEmbeddingProvider(provider EmbeddingProvider)
    Close() error
}

type EpisodicSearchOptions struct {
    MaxResults   int
    MinScore     float64
    VectorWeight float64
    TextWeight   float64
}

// MemorySearchResultV3 extends search result with tier indicator.
type MemorySearchResultV3 struct {
    MemorySearchResult        // embed existing fields
    Tier               string `json:"tier"` // "working", "episodic", "semantic"
    EpisodicID         string `json:"episodic_id,omitempty"`
    L0Abstract         string `json:"l0_abstract,omitempty"`
}

// UnifiedMemorySearch combines Tier 2 (episodic) and existing memory doc search.
// Used by memory_search tool.
type UnifiedMemorySearch interface {
    // Search across all tiers. Returns results with tier indicators.
    Search(ctx context.Context, query string, agentID, userID string, opts UnifiedSearchOptions) ([]MemorySearchResultV3, error)

    // Expand loads full content for a specific memory entry (L2).
    Expand(ctx context.Context, id string, tier string) (string, error)
}

type UnifiedSearchOptions struct {
    MaxResults   int
    MinScore     float64
    Tiers        []string // filter: ["episodic", "document"] (empty = all)
    Depth        string   // "l0", "l1", "l2" (default "l1")
}
```

### KnowledgeGraphStore v3 Extensions

```go
// Temporal query additions to existing KnowledgeGraphStore interface.

// TemporalQueryOptions extends entity queries with time awareness.
type TemporalQueryOptions struct {
    AsOf       *time.Time // point-in-time query (nil = current only)
    IncludeExpired bool   // include superseded facts
}

// New methods added to KnowledgeGraphStore:

// ListEntitiesTemporal queries entities with temporal awareness.
// When opts.AsOf is nil, returns current facts only (valid_until IS NULL).
// When opts.AsOf is set, returns facts valid at that timestamp.
ListEntitiesTemporal(ctx context.Context, agentID, userID string,
    listOpts EntityListOptions, temporal TemporalQueryOptions) ([]Entity, error)

// SupersedeEntity marks entity as no longer valid and inserts replacement.
// Atomic: UPDATE old SET valid_until=NOW() + INSERT new in single tx.
SupersedeEntity(ctx context.Context, old *Entity, replacement *Entity) error

// GetDedupConfig returns per-agent dedup thresholds from agents.other_config.
// Returns defaults if not configured.
GetDedupConfig(ctx context.Context, agentID string) (*KGConfig, error)
```

### Entity Struct v3

```go
// Extended Entity with temporal fields.
type Entity struct {
    // ... existing fields unchanged ...
    ID          string            `json:"id"`
    AgentID     string            `json:"agent_id"`
    UserID      string            `json:"user_id,omitempty"`
    ExternalID  string            `json:"external_id"`
    Name        string            `json:"name"`
    EntityType  string            `json:"entity_type"`
    Description string            `json:"description,omitempty"`
    Properties  map[string]string `json:"properties,omitempty"`
    SourceID    string            `json:"source_id,omitempty"`
    Confidence  float64           `json:"confidence"`
    CreatedAt   int64             `json:"created_at"`
    UpdatedAt   int64             `json:"updated_at"`

    // V3 temporal fields
    ValidFrom  *time.Time `json:"valid_from,omitempty"`
    ValidUntil *time.Time `json:"valid_until,omitempty"` // nil = currently valid
}

// Relation with temporal fields (same pattern).
type Relation struct {
    // ... existing fields unchanged ...

    // V3 temporal fields
    ValidFrom  *time.Time `json:"valid_from,omitempty"`
    ValidUntil *time.Time `json:"valid_until,omitempty"`
}
```

---

## Pipeline Integration Points

### Where Memory/KG Connects to Pipeline Stages

| Pipeline stage | Memory/KG interaction |
|---------------|----------------------|
| **ContextStage** | AutoInjector.Inject() -> produces ## Memory Context section |
| **PruneStage** | Triggers MemoryFlushStage before compaction |
| **MemoryFlushStage** | Writes to memory docs (existing). Publishes `session.completed` |
| **FinalizeStage** | Publishes `session.completed` for episodic consolidation |
| **ToolStage** | `memory_search` tool calls UnifiedMemorySearch |
| **ToolStage** | `memory_expand` tool calls UnifiedMemorySearch.Expand() |
| **ToolStage** | `kg_search` tool uses temporal queries (default: current facts) |

### Consolidation Pipeline Registration

```go
// At gateway startup, register consolidation workers with DomainEventBus.

func RegisterConsolidationPipeline(eb eventbus.DomainEventBus, deps ConsolidationDeps) {
    episodicWorker := NewEpisodicWorker(deps)
    semanticWorker := NewSemanticExtractionWorker(deps)
    dedupWorker := NewDedupWorker(deps)

    eb.Subscribe(eventbus.EventSessionCompleted, episodicWorker.Handle)
    eb.Subscribe(eventbus.EventEpisodicCreated, semanticWorker.Handle)
    eb.Subscribe(eventbus.EventEntityUpserted, dedupWorker.Handle)
}
```

---

## Requirements

### Functional
- F1: Episodic summaries created automatically after session completion
- F2: L0 abstracts pre-computed at episodic creation (~50 tokens each)
- F3: Auto-inject produces relevant L0 summaries for user messages (skip for trivial messages)
- F4: Temporal KG queries default to current facts; historical queries via `AsOf` parameter
- F5: Contradiction resolution: supersede old fact, insert new with valid_from = NOW()
- F6: Dedup thresholds configurable per agent (not hardcoded)
- F7: Consolidation pipeline non-blocking (user never waits)
- F8: `memory_search` tool returns results with tier indicators
- F9: `memory_expand` tool loads L2 full content
- F10: Expired episodic entries pruned automatically (cron or lazy)

### Non-Functional
- NF1: Auto-inject latency < 50ms p99 (BM25 + embedding lookup)
- NF2: Episodic summarization completes within 30s (LLM call)
- NF3: Consolidation pipeline handles 100 session.completed events/minute
- NF4: pgvector index efficient up to 100K embeddings per agent
- NF5: L0 injection budget: max 200 tokens per turn

## Related Code Files

### Files to modify

| File | Change |
|------|--------|
| `internal/store/knowledge_graph_store.go` | Add temporal fields to Entity/Relation, new temporal query methods |
| `internal/store/pg/knowledge_graph*.go` | Implement temporal queries, supersede logic |
| `internal/store/memory_store.go` | No change (existing store retained for Tier 1/document memory) |
| `internal/knowledgegraph/extractor.go` | Source extraction from episodic summaries (lighter than raw messages) |
| `internal/agent/systemprompt.go` | Add ## Memory Context section from AutoInjector |
| `internal/tools/memory_tools.go` | Update memory_search to use UnifiedMemorySearch, add memory_expand |

### New files to create

| File | Purpose |
|------|---------|
| `internal/store/episodic_store.go` | EpisodicStore interface + types |
| `internal/store/pg/episodic_summaries.go` | PG implementation |
| `internal/memory/auto_injector.go` | AutoInjector implementation |
| `internal/memory/l1_cache.go` | L1 on-demand generation + LRU cache |
| `internal/consolidation/episodic_worker.go` | session.completed -> episodic summary |
| `internal/consolidation/semantic_worker.go` | episodic.created -> KG extraction |
| `internal/consolidation/dedup_worker.go` | entity.upserted -> dedup check |
| `internal/consolidation/registration.go` | Worker registration with EventBus |
| `migrations/000XXX_episodic_summaries.up.sql` | Episodic table + indexes |
| `migrations/000XXX_kg_temporal.up.sql` | Temporal fields on KG tables |

## Design Steps

1. Define `episodic_summaries` DDL with indexes
2. Define `EpisodicStore` interface
3. Design temporal KG schema changes (ALTER TABLE + backfill)
4. Define temporal query methods for KnowledgeGraphStore
5. Design consolidation pipeline event flow
6. Design EpisodicWorker with summarization prompt + L0 generation
7. Design AutoInjector with BM25 + embedding relevance check
8. Define UnifiedMemorySearch interface for cross-tier search
9. Design L0/L1/L2 depth parameter for search results
10. Define per-agent memory config extensions

## Todo List

- [x] A1: episodic_summaries DDL — documented in phase (SQL in Phase 6)
- [x] A2: EpisodicStore interface — `internal/store/episodic_store.go`
- [x] A3: EpisodicSummary struct + EpisodicSearchResult — `internal/store/episodic_store.go`
- [x] B1: kg_entities temporal fields DDL — documented in phase (SQL in Phase 6)
- [x] B2: kg_relations temporal fields DDL — documented in phase (SQL in Phase 6)
- [x] B3: Backfill migration — documented in phase design
- [x] B4: Temporal query methods — `internal/store/knowledge_graph_temporal.go`
- [x] B5: SupersedeEntity atomic method — `internal/store/knowledge_graph_temporal.go`
- [x] B6: Configurable dedup thresholds — `internal/store/knowledge_graph_temporal.go` KGConfig
- [x] C1: Consolidation event flow design — documented in phase
- [x] C2: EpisodicWorker handler — `internal/consolidation/workers.go` (stub)
- [x] C3: SemanticExtractionWorker handler — `internal/consolidation/workers.go` (stub)
- [x] C4: DedupWorker handler — `internal/consolidation/workers.go` (stub)
- [x] C5: Worker registration at startup — `internal/consolidation/workers.go` Register()
- [x] D1: L0 abstract generation — documented in phase design (extractive v3.0)
- [x] D2: L1 on-demand generation + LRU cache — documented in phase design
- [x] D3: L2 access via memory_expand tool — documented in phase design
- [x] E1: AutoInjector interface — `internal/memory/auto_injector.go`
- [x] E2: Relevance check (BM25 + embedding) — documented in phase design
- [x] E3: System prompt ## Memory Context section — documented in phase design
- [x] E4: Per-agent threshold config — `internal/memory/auto_injector.go` MemoryConfig
- [x] F1: UnifiedMemorySearch interface — documented in phase design
- [x] F2: MemorySearchResultV3 with tier indicator — documented in phase design
- [x] F3: memory_search tool update — documented in phase design
- [x] F4: memory_expand tool definition — documented in phase design

## Success Criteria

- episodic_summaries DDL passes `psql` validation
- Temporal KG migration backward-compatible (existing data has valid_from=created_at, valid_until=NULL)
- All consolidation workers idempotent (safe to replay events)
- AutoInjector returns "" for trivial messages ("hello", "ok")
- AutoInjector returns relevant L0s for topic-specific messages
- UnifiedMemorySearch merges results across tiers with correct tier labels
- Per-agent dedup thresholds configurable, defaults match current hardcoded values

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| Episodic summarization LLM cost | MEDIUM | Use smaller/cheaper model for summarization (e.g. GPT-4o-mini). Configurable per agent. |
| Auto-inject false positives (irrelevant memories injected) | MEDIUM | Conservative default threshold (0.3). Agent can override. Monitor via evolution metrics. |
| pgvector index performance at scale (100K+ embeddings) | MEDIUM | ivfflat index with lists=50. Benchmark needed. Increase lists for larger datasets. |
| Consolidation pipeline backlog during high load | LOW | Lane-based concurrency in EventBus. Workers are idempotent. Backlog clears naturally. |
| L0 extractive quality insufficient | LOW | Extractive is "good enough" for v3.0. LLM-generated L0 in v3.1. |
| Temporal backfill migration slow on large KG | LOW | Backfill is simple UPDATE SET DEFAULT. Add timeout guard. |
| Cross-agent memory leakage | HIGH | All queries scoped by tenant_id + agent_id + user_id. Verify in code review. |

## Security Considerations

- Tenant isolation: ALL episodic + KG queries must include tenant_id scope. Missing tenant_id = data leak.
- User isolation: episodic summaries scoped by user_id unless shared memory enabled
- L0/L1 content is derived from user conversations -- same access controls as original messages
- Consolidation workers run in system context -- must not expose cross-tenant data
- AutoInjector results filtered by same agent_id + user_id scope as memory_search

## Unresolved Questions

1. **Episodic TTL**: 90 days default. Should this be configurable per agent? (Likely yes, via MemoryConfig)
2. **L0 for memory documents**: Should existing memory_documents also get L0 abstracts? (Deferred to v3.1 -- episodic first)
3. **Cross-agent memory**: Should agents in same tenant share semantic memory? (Brainstorm Q2, deferred)
4. **Embedding model**: Continue OpenAI text-embedding-3-small or switch to open model? (Brainstorm Q3, deferred)
5. **Concurrent consolidation**: Multiple sessions ending simultaneously -> same episodic worker. EventBus dedup by source_id handles this, but verify ordering.

## Next Steps

- Phase 4 (System Integration) integrates AutoInjector into ContextStage template
- Phase 5 (Orchestration) uses self-evolution metrics from consolidation pipeline
- Phase 6 (Migration) handles v2 daily memory files -> episodic import
