# 07 - Bootstrap, Skills & Memory

Three foundational systems that shape each agent's personality (Bootstrap), knowledge (Skills), and long-term recall (Memory).

### Responsibilities

- Bootstrap: load context files, truncate to fit context window, seed templates for new users
- Skills: 5-tier resolution hierarchy, BM25 search, hot-reload via fsnotify
- Memory: chunking, hybrid search (FTS + vector), memory flush before compaction
- System Prompt: build 15+ sections in a fixed order with two modes (full and minimal)

---

## 1. Bootstrap Files -- 13 Files (6 Template + 3 Virtual + 4 Memory Variants)

Bootstrap files are loaded at agent initialization and embedded into the system prompt. The system distinguishes between **stored template files** (with embedded defaults), **virtual system-injected files** (not stored on disk), and **memory files** (loaded separately from bootstrap).

### Stored Template Files (6 files)

Markdown files with embedded templates in `internal/bootstrap/templates/`. These are seeded on agent/user creation and can be customized.

| # | File | Role | Full Session | Subagent/Cron | Agent Level | Per-User |
|---|------|------|:---:|:---:|:---:|:---:|
| 1 | AGENTS.md | Operating instructions, memory rules, safety guidelines | Yes | Yes | predefined | both |
| 2 | SOUL.md | Persona, tone of voice, boundaries | Yes | No | predefined | open only |
| 3 | TOOLS.md | Local tool notes (camera, SSH, TTS, etc.) | Yes | Yes | predefined | open only |
| 4 | IDENTITY.md | Agent name, creature, vibe, emoji | Yes | No | predefined | open only |
| 5 | USER.md | User profile (name, timezone, preferences) | Yes | No | — | both |
| 6 | BOOTSTRAP.md | First-run ritual (deleted after completion) | Yes | No | — | both |

**Additional per-agent file:**
- USER_PREDEFINED.md (agent-level only): Baseline user-handling rules for predefined agents, shared across all users

Subagent and cron sessions load only AGENTS.md + TOOLS.md (the `minimalAllowlist`).

### Virtual Context Files (3 files)

System-injected files not stored on disk or in the database. Rendered in `<system_context>` tags.

| File | Condition | Content | Bootstrap Skip |
|------|-----------|---------|:---:|
| DELEGATION.md | Agent has agent links (manual delegation) | ≤15 targets: static list inline. >15 targets: description-only (no tool needed) | Yes |
| TEAM.md | Agent is a member of a team | Team name, role, teammate list with descriptions | Yes |
| AVAILABILITY.md | Always present (in negative contexts) | Agent availability status and scope limitations | Yes |

Virtual files skip during first-run bootstrap to avoid wasting tokens when the agent should focus on onboarding.

### Memory Files (4 file variants)

NOT part of bootstrap template loading. Loaded separately by the memory system.

| File | Role | Storage | Search |
|------|------|---------|--------|
| MEMORY.md | Curated memory (Markdown) | Per-agent + per-user | FTS + vector |
| memory.md | Fallback name for MEMORY.md | Checked if MEMORY.md missing | FTS + vector |
| MEMORY.json | Machine-readable memory index | Deprecated | — |

---

## 2. Truncation Pipeline

Bootstrap content can exceed the context window budget. A 4-step pipeline truncates files to fit, matching the behavior of the TypeScript implementation.

```mermaid
flowchart TD
    IN["Ordered list of bootstrap files"] --> S1["Step 1: Skip empty or missing files"]
    S1 --> S2["Step 2: Per-file truncation<br/>If > MaxCharsPerFile (20K):<br/>Keep 70% head + 20% tail<br/>Insert [...truncated] marker"]
    S2 --> S3["Step 3: Clamp to remaining<br/>total budget (starts at 24K)"]
    S3 --> S4{"Step 4: Remaining budget < 64?"}
    S4 -->|Yes| STOP["Stop processing further files"]
    S4 -->|No| NEXT["Continue to next file"]
```

### Truncation Defaults

| Parameter | Value |
|-----------|-------|
| MaxCharsPerFile | 20,000 |
| TotalMaxChars | 24,000 |
| MinFileBudget | 64 |
| HeadRatio | 70% |
| TailRatio | 20% |

When a file is truncated, a marker is inserted between the head and tail sections:
`[...truncated, read SOUL.md for full content...]`

---

## 3. Seeding -- Template Creation

Templates are embedded in the binary via Go `embed` (directory: `internal/bootstrap/templates/`). Seeding automatically creates default files at agent creation (agent-level) and first-chat (per-user).

```mermaid
flowchart TD
    subgraph "Agent Level (SeedToStore)"
        SB["New agent created"] --> SB1{"Agent type = open?"}
        SB1 -->|Yes| SKIP_AGENT["Skip agent-level files<br/>(open agents use per-user only)"]
        SB1 -->|No| SB2["predefined agent"]
        SB2 --> SB3["Seed to agent_context_files:<br/>AGENTS.md, SOUL.md, IDENTITY.md,<br/>USER_PREDEFINED.md"]
        SB3 --> SB4["(skip USER.md, TOOLS.md,<br/>BOOTSTRAP.md)"]
        SB4 --> SB5{"File already has content?"}
        SB5 -->|Yes| SKIP2["Skip"]
        SB5 -->|No| WRITE2["Write embedded template"]
    end

    subgraph "Per-User (SeedUserFiles)"
        MC["First chat for user"] --> MC1{"Agent type?"}
        MC1 -->|open| OPEN["Seed all 6 files:<br/>AGENTS.md, SOUL.md, TOOLS.md,<br/>IDENTITY.md, USER.md, BOOTSTRAP.md"]
        MC1 -->|predefined| PRED["Seed 2 files:<br/>USER.md (with agent fallback),<br/>BOOTSTRAP.md (predefined template)"]
        OPEN --> CHECK{"File already has content?"}
        PRED --> CHECK
        CHECK -->|Yes| SKIP3["Skip -- never overwrite"]
        CHECK -->|No| WRITE3["Write embedded template"]
    end
```

`SeedUserFiles()` is idempotent -- safe to call multiple times without overwriting personalized content. For predefined agents seeding USER.md, if the agent-level USER.md has content (e.g., configured by wizard/dashboard), that content is used as the per-user seed instead of the blank template, ensuring owner profiles propagate correctly.

### Predefined Agent Bootstrap Ritual

`BOOTSTRAP.md` is seeded per-user for both open and predefined agents. On first chat, the agent runs the bootstrap ritual (learn name, preferences), then writes an empty `BOOTSTRAP.md` which triggers deletion. The empty-write deletion is ordered *before* the template write-block in `ContextFileInterceptor` to prevent an infinite bootstrap loop.

---

## 4. Agent Type Routing

Two agent types determine which context files live at the agent level versus the per-user level.

| Agent Type | Agent-Level Files | Per-User Files |
|------------|-------------------|----------------|
| `open` | None (all per-user) | AGENTS.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, BOOTSTRAP.md |
| `predefined` | AGENTS.md, SOUL.md, IDENTITY.md, USER_PREDEFINED.md (shared) | USER.md, BOOTSTRAP.md (personalized per-user) |

**Open agents:** Each user gets their own full set of context files with personal preferences and identity. Reading checks per-user copy first.

**Predefined agents:** All users share the same agent-level persona, identity, and tools. Each user has their own USER.md (profile) and BOOTSTRAP.md (first-run ritual). USER_PREDEFINED.md provides baseline user-handling rules at the agent level, allowing the model to adjust behavior per-user while maintaining consistency.

| Storage | Location |
|---------|----------|
| Agent-level | `agent_context_files` table |
| Per-user | `user_context_files` table |

---

## 5. System Prompt -- 17+ Sections

`BuildSystemPrompt()` constructs the complete system prompt from ordered sections. Two modes control which sections are included.

```mermaid
flowchart TD
    START["BuildSystemPrompt()"] --> S1["1. Identity<br/>'You are a personal assistant<br/>running inside GoClaw'"]
    S1 --> S1_5{"1.5 BOOTSTRAP.md present?"}
    S1_5 -->|Yes| BOOT["First-run Bootstrap Override<br/>(mandatory BOOTSTRAP.md instructions)"]
    S1_5 -->|No| S2
    BOOT --> S2["2. Tooling<br/>(tool list + descriptions)"]
    S2 --> S3["3. Safety<br/>(hard safety directives)"]
    S3 --> S4["4. Skills (full only)"]
    S4 --> S5["5. Memory Recall (full only)"]
    S5 --> S6["6. Workspace"]
    S6 --> S6_5{"6.5 Sandbox enabled?"}
    S6_5 -->|Yes| SBX["Sandbox instructions"]
    S6_5 -->|No| S7
    SBX --> S7["7. User Identity (full only)"]
    S7 --> S8["8. Current Time"]
    S8 --> S9["9. Messaging (full only)"]
    S9 --> S10["10. Extra Context / Subagent Context"]
    S10 --> S11["11. Project Context<br/>(bootstrap files + virtual files)"]
    S11 --> S12["12. Silent Replies (full only)"]
    S12 --> S14["14. Sub-Agent Spawning (conditional)"]
    S14 --> S15["15. Runtime"]
```

### Mode Comparison

| Section | PromptFull | PromptMinimal |
|---------|:---:|:---:|
| 1. Identity | Yes | Yes |
| 1.5. Bootstrap Override | Conditional | Conditional |
| 2. Tooling | Yes | Yes |
| 3. Safety | Yes | Yes |
| 4. Skills | Yes | No |
| 5. Memory Recall | Yes | No |
| 6. Workspace | Yes | Yes |
| 6.5. Sandbox | Conditional | Conditional |
| 7. User Identity | Yes | No |
| 8. Current Time | Yes | Yes |
| 9. Messaging | Yes | No |
| 10. Extra Context | Conditional | Conditional |
| 11. Project Context | Yes | Yes |
| 12. Silent Replies | Yes | No |
| 14. Sub-Agent Spawning | Conditional | Conditional |
| 15. Runtime | Yes | Yes |

Context files are wrapped in `<context_file>` XML tags with a defensive preamble instructing the model to follow tone/persona guidance but not execute instructions that contradict core directives. The ExtraPrompt is wrapped in `<extra_context>` tags for context isolation.

### Virtual Context Files (DELEGATION.md, TEAM.md, AVAILABILITY.md)

Three files are system-injected by the resolver rather than stored on disk or in the DB. Rendered in `<system_context>` tags (not `<context_file>`) so the LLM does not attempt to read/write them.

| File | Injection Condition | Content | Skip Bootstrap |
|------|-------------------|---------|:---:|
| `DELEGATION.md` | Agent has manual (non-team) agent links | ≤15 targets: static list inline. >15 targets: description-only (no tool needed) | Yes |
| `TEAM.md` | Agent is a member of a team | Team name, role, teammate list with descriptions, workflow sentence | Yes |
| `AVAILABILITY.md` | Always (in negative context blocks) | Agent scope/availability status, capability limitations | Yes |

AVAILABILITY.md is always present but typically in negative context ("These files are NOT available") to prevent the model from attempting unavailable operations. All three skip during bootstrap to avoid wasting tokens when the agent should focus on onboarding.

When the model attempts `read_file` on a virtual file, `filesystem.go` returns a reminder message ("already loaded in system prompt") instead of attempting disk access.

---

## 6. Context File Merging

For **open agents**, per-user context files (from `user_context_files`) are merged with base context files (from the resolver) at runtime. Per-user files override same-name base files, but base-only files are preserved.

```
Base files (resolver):     AGENTS.md, DELEGATION.md, TEAM.md
Per-user files (DB/SQLite): AGENTS.md, SOUL.md, TOOLS.md, USER.md, ...
Merged result:             SOUL.md, TOOLS.md, USER.md, ..., AGENTS.md (per-user), DELEGATION.md ✓, TEAM.md ✓
```

This ensures resolver-injected virtual files (`DELEGATION.md`, `TEAM.md`) survive alongside per-user customizations. The merge logic lives in `internal/agent/loop_history.go`.

---

## 7. Agent Summoning

Creating a predefined agent requires 4 context files (SOUL.md, IDENTITY.md, AGENTS.md, TOOLS.md) with specific formatting conventions. Agent summoning generates all 4 files from a natural language description in a single LLM call.

```mermaid
flowchart TD
    USER["User: 'sarcastic Rust reviewer'"] --> API["Backend (POST /v1/agents/{id}/summon)"]
    API -->|"status: summoning"| DB["Database"]
    API --> LLM["LLM call with structured XML prompt"]
    LLM --> PARSE["Parse XML output into 5 files"]
    PARSE --> STORE["Write files to agent_context_files"]
    STORE -->|"status: active"| READY["Agent ready"]
    LLM -.->|"WS events"| UI["Dashboard modal with progress"]
```

The LLM outputs structured XML with each file in a tagged block. Parsing is done server-side in `internal/http/summoner.go`. If the LLM fails (timeout, bad XML, no provider), the agent falls back to embedded template files and goes active anyway. The user can retry via "Edit with AI" later.

**Why not `write_file`?** The `ContextFileInterceptor` blocks predefined file writes from chat by design. Bypassing it would create a security hole. Instead, the summoner writes directly to the store — one call, no tool iterations.

---

## 8. Skills -- 5-Tier Hierarchy

Skills are loaded from multiple directories with a priority ordering. Higher-tier skills override lower-tier skills with the same name.

```mermaid
flowchart TD
    T1["Tier 1 (highest): Workspace skills<br/>workspace/skills/name/SKILL.md"] --> T2
    T2["Tier 2: Project agent skills<br/>workspace/.agents/skills/"] --> T3
    T3["Tier 3: Personal agent skills<br/>~/.agents/skills/"] --> T4
    T4["Tier 4: Global/managed skills<br/>~/.goclaw/skills/"] --> T5
    T5["Tier 5 (lowest): Builtin skills<br/>(bundled with binary)"]

    style T1 fill:#e1f5fe
    style T5 fill:#fff3e0
```

Each skill directory contains a `SKILL.md` file with YAML/JSON frontmatter (`name`, `description`). The `{baseDir}` placeholder in SKILL.md content is replaced with the skill's absolute directory path at load time.

---

## 9. Skills -- Inline vs Search Mode

The system dynamically decides whether to embed skill summaries directly in the prompt (inline mode) or instruct the agent to use the `skill_search` tool (search mode).

```mermaid
flowchart TD
    COUNT["Count filtered skills<br/>Estimate tokens = sum(chars of name+desc) / 4"] --> CHECK{"skills <= 20<br/>AND tokens <= 3500?"}
    CHECK -->|Yes| INLINE["INLINE MODE<br/>BuildSummary() produces XML<br/>Agent reads available_skills directly"]
    CHECK -->|No| SEARCH["SEARCH MODE<br/>Prompt instructs agent to use skill_search<br/>BM25 ranking returns top 5"]
```

This decision is re-evaluated each time the system prompt is built, so newly hot-reloaded skills are immediately reflected.

---

## 10. Skills -- BM25 Search

An in-memory BM25 index provides keyword-based skill search. The index is lazily rebuilt whenever the skill version changes.

**Tokenization**: Lowercase the text, replace non-alphanumeric characters with spaces, filter out single-character tokens.

**Scoring formula**: `IDF(t) x tf(t,d) x (k1 + 1) / (tf(t,d) + k1 x (1 - b + b x |d| / avgDL))`

| Parameter | Value |
|-----------|-------|
| k1 | 1.2 |
| b | 0.75 |
| Max results | 5 |

IDF is computed as: `log((N - df + 0.5) / (df + 0.5) + 1)`

---

## 11. Skills -- Embedding Search

Skill search uses a hybrid approach combining BM25 and vector similarity.

```mermaid
flowchart TD
    Q["Search query"] --> BM25["BM25 search<br/>(in-memory index)"]
    Q --> EMB["Generate query embedding"]
    EMB --> VEC["Vector search<br/>pgvector cosine distance<br/>(embedding <=> operator)"]
    BM25 --> MERGE["Weighted merge"]
    VEC --> MERGE
    MERGE --> RESULT["Final ranked results"]
```

| Component | Weight |
|-----------|--------|
| BM25 score | 0.3 |
| Vector similarity | 0.7 |

**Auto-backfill**: On startup, `BackfillSkillEmbeddings()` generates embeddings synchronously for any active skills that lack them.

---

## 12. Skills Grants & Visibility

Skill access is controlled through a 3-tier visibility model with explicit agent and user grants.

```mermaid
flowchart TD
    SKILL["Skill record"] --> VIS{"visibility?"}
    VIS -->|public| ALL["Accessible to all agents and users"]
    VIS -->|private| OWNER["Accessible only to owner<br/>(owner_id = userID)"]
    VIS -->|internal| GRANT{"Has explicit grant?"}
    GRANT -->|skill_agent_grants| AGENT["Accessible to granted agent"]
    GRANT -->|skill_user_grants| USER["Accessible to granted user"]
    GRANT -->|No grant| DENIED["Not accessible"]
```

### Visibility Levels

| Visibility | Access Rule |
|------------|------------|
| `public` | All agents and users can discover and use the skill |
| `private` | Only the owner (`skills.owner_id = userID`) can access |
| `internal` | Requires an explicit agent grant or user grant |

### Grant Tables

| Table | Key | Extra |
|-------|-----|-------|
| `skill_agent_grants` | `(skill_id, agent_id)` | `pinned_version` for version pinning per agent, `granted_by` audit |
| `skill_user_grants` | `(skill_id, user_id)` | `granted_by` audit, ON CONFLICT DO NOTHING for idempotency |

**Resolution**: `ListAccessible(agentID, userID)` performs a DISTINCT join across `skills`, `skill_agent_grants`, and `skill_user_grants` with the visibility filter, returning only active skills the caller can access.

**Tier 4**: Global skills (Tier 4 in the hierarchy) are loaded from the `skills` PostgreSQL table instead of the filesystem.

---

## 12.5. Per-Agent Skill Filtering

In addition to visibility grants, agents can restrict which skills they have access to through a per-agent skill allow list.

```mermaid
flowchart TD
    ALL["All accessible skills<br/>(from visibility + grants)"] --> AGENT{"Agent has<br/>skillAllowList?"}
    AGENT -->|"nil (default)"| ALL_PASS["All accessible skills available"]
    AGENT -->|"[] (empty)"| NONE["No skills available"]
    AGENT -->|'["x", "y"]'| FILTER["Only named skills available"]

    FILTER --> REQUEST{"Per-request<br/>SkillFilter?"}
    ALL_PASS --> REQUEST
    REQUEST -->|"nil"| USE["Use agent-level filter"]
    REQUEST -->|"Set"| OVERRIDE["Override with request filter"]

    USE --> MODE{"Count + tokens?"}
    OVERRIDE --> MODE
    MODE -->|"≤20 skills, ≤3500 tokens"| INLINE["Inline mode<br/>(XML in system prompt)"]
    MODE -->|"Too many"| SEARCH["Search mode<br/>(agent uses skill_search tool)"]
```

### Configuration

| Setting | Value | Behavior |
|---------|-------|----------|
| `skillAllowList = nil` | Default | All accessible skills available |
| `skillAllowList = []` | Empty list | No skills — agent has no skill access |
| `skillAllowList = ["billing-faq", "returns"]` | Named skills | Only these specific skills are available |

### Per-Request Override

Channels can override the skill allow list per request via message metadata. For example, Telegram forum topics can configure different skills per topic (see [05-channels-messaging.md](./05-channels-messaging.md) Section 5). The per-request filter takes priority over the agent-level setting.

---

## 13. Hot-Reload

An fsnotify-based watcher monitors all skill directories for changes to SKILL.md files.

```mermaid
flowchart TD
    S1["fsnotify detects SKILL.md change"] --> S2["Debounce 500ms"]
    S2 --> S3["BumpVersion() sets version = timestamp"]
    S3 --> S4["Next system prompt build detects<br/>version change and reloads skills"]
```

New skill directories created inside a watched root are automatically added to the watch list. The debounce window (500ms) is shorter than the memory watcher (1500ms) because skill changes are lightweight.

---

## 14. Memory -- Indexing Pipeline

Memory documents are chunked, embedded, and stored for hybrid search.

```mermaid
flowchart TD
    IN["Document changed or created"] --> READ["Read content"]
    READ --> HASH["Compute SHA256 hash (first 16 bytes)"]
    HASH --> CHECK{"Hash changed?"}
    CHECK -->|No| SKIP["Skip -- content unchanged"]
    CHECK -->|Yes| DEL["Delete old chunks for this document"]
    DEL --> CHUNK["Split into chunks<br/>(max 1000 chars, prefer paragraph breaks)"]
    CHUNK --> EMBED{"EmbeddingProvider available?"}
    EMBED -->|Yes| API["Batch embed all chunks"]
    EMBED -->|No| SAVE
    API --> SAVE["Store chunks + tsvector index<br/>+ vector embeddings + metadata"]
```

### Chunking Rules

- Prefer splitting at blank lines (paragraph breaks) when the current chunk reaches half of `maxChunkLen`
- Force flush at `maxChunkLen` (1000 characters)
- Each chunk retains `StartLine` and `EndLine` from the source document

### Memory Paths

- `MEMORY.md` or `memory.md` at the workspace root
- `memory/*.md` (recursive, excluding `.git`, `node_modules`, etc.)

---

## 15. Hybrid Search

Combines full-text search and vector search with weighted merging.

```mermaid
flowchart TD
    Q["Search(query)"] --> FTS["FTS Search<br/>tsvector + plainto_tsquery"]
    Q --> VEC["Vector Search<br/>pgvector (cosine distance)"]
    FTS --> MERGE["hybridMerge()"]
    VEC --> MERGE
    MERGE --> NORM["Normalize FTS scores to 0..1<br/>Vector scores already in 0..1"]
    NORM --> WEIGHT["Weighted sum<br/>textWeight = 0.3<br/>vectorWeight = 0.7"]
    WEIGHT --> BOOST["Per-user scope: 1.2x boost<br/>Dedup: user copy wins over global"]
    BOOST --> RESULT["Sorted + filtered results"]
```

### Search Implementation

| Aspect | Detail |
|--------|--------|
| Storage | PostgreSQL + tsvector + pgvector |
| FTS | `plainto_tsquery('simple')` |
| Vector | pgvector type |
| Scope | Per-agent + per-user |

When both FTS and vector search return results, scores are merged using the weighted sum. When only one channel returns results, its scores are used directly (weights normalized to 1.0).

---

## 16. Memory Flush -- Pre-Compaction

Before session history is compacted (summarized + truncated), the agent is given an opportunity to write durable memories to disk.

```mermaid
flowchart TD
    CHECK{"totalTokens >= threshold?<br/>(contextWindow - reserveFloor - softThreshold)<br/>AND not flushed in this cycle?"} -->|Yes| FLUSH
    CHECK -->|No| SKIP["Continue normal operation"]

    FLUSH["Memory Flush"] --> S1["Step 1: Build flush prompt<br/>asking to save memories to memory/YYYY-MM-DD.md"]
    S1 --> S2["Step 2: Provide tools<br/>(read_file, write_file, exec)"]
    S2 --> S3["Step 3: Run LLM loop<br/>(max 5 iterations, 90s timeout)"]
    S3 --> S4["Step 4: Mark flush done<br/>for this compaction cycle"]
    S4 --> COMPACT["Proceed with compaction<br/>(summarize + truncate history)"]
```

### Flush Defaults

| Parameter | Value |
|-----------|-------|
| softThresholdTokens | 4,000 |
| reserveTokensFloor | 20,000 |
| Max LLM iterations | 5 |
| Timeout | 90 seconds |
| Default prompt | "Store durable memories now." |

The flush is idempotent per compaction cycle -- it will not run again until the next compaction threshold is reached.

---

## 17. V3 Three-Tier Memory & Auto-Injection (New in v3)

V3 introduces a comprehensive 3-tier memory system with event-driven consolidation and intelligent auto-injection.

### Architecture Overview

**Working Memory (L0):** Current conversation in `sessions.messages`. Auto-compacted via summarization at context threshold.

**Episodic Memory (L1):** Session summaries stored in `episodic_summaries` table with:
- Full summary + ~50-token L0 abstract (pre-computed)
- Embedding vector for hybrid search
- Key topics array for quick filtering
- 90-day retention by default

**Semantic Memory (L2):** Knowledge Graph in `kg_entities` + `kg_relations` with temporal validity (`valid_from`, `valid_until`). Long-term structured knowledge.

### Auto-Injection (L0 Loading)

Runs in ContextStage once per turn. Checks user message against episodic index. If relevant matches found, injects L0 abstracts into system prompt.

**Config** (stored in agent settings):
```json
{
  "auto_inject_enabled": true,
  "auto_inject_threshold": 0.3,
  "auto_inject_max_tokens": 200,
  "episodic_ttl_days": 90,
  "consolidation_enabled": true
}
```

**Return value:** Formatted section (~200 tokens max) with top K summaries, or empty string if no relevant matches.

### Progressive Tool Access

Three tool-based memory interactions:

| Tool | Purpose | Tier | Example |
|------|---------|------|---------|
| (auto-inject) | Automatic context injection | L0 | System prompt includes 3 relevant past sessions |
| `memory_search(query)` | Hybrid search L1 + L2 | L1 | "Find past discussions about billing" |
| `memory_expand(id)` | Deep retrieval from episodic | L2 | "Show me full summary + linked facts from session XYZ" |

---

## 18. Consolidation Pipeline (Event-Driven Workers)

After a session ends (`run.completed` event), async workers extract and consolidate memory into long-term storage.

```mermaid
flowchart LR
    RUN["run.completed<br/>event"] --> EP["EpisodicWorker<br/>extract summary<br/>+ L0 abstract"]
    EP --> ES["episodic_summaries<br/>table"]
    ES --> EPEV["episodic.created<br/>event"]
    EPEV --> SW["SemanticWorker<br/>extract entities<br/>& relations"]
    SW --> KG["kg_entities<br/>kg_relations"]
    KG --> ENT["entity.upserted<br/>event"]
    ENT --> DW["DedupWorker<br/>merge duplicates<br/>via embeddings"]
    DW --> CONSOLIDATE["Consolidate<br/>duplicate nodes"]
    EPEV -->|"10m debounce"| DREAM["DreamingWorker<br/>batch synthesis<br/>via LLM"]
    DREAM --> SYNTH["Long-term<br/>memory output"]
```

### Worker Responsibilities

**EpisodicWorker** (`internal/consolidation/episodic_worker.go`):
1. Listens to `run.completed` events
2. Checks for duplicate via `source_id` = `session_key:compaction_count`
3. Uses compaction summary if available, else calls LLM to summarize
4. Generates L0 abstract via `generateL0Abstract()` (~50 tokens)
5. Extracts entity names via `extractEntityNames()`
6. Sets 90-day expiry
7. Stores in `episodic_summaries`
8. Publishes `episodic.created` for downstream workers

**SemanticWorker** (`internal/consolidation/semantic_worker.go`):
1. Listens to `episodic.created` events
2. Parses summary for entity mentions + relationships
3. Inserts entities into `kg_entities` with confidence score
4. Inserts relations into `kg_relations`
5. Publishes `entity.upserted` for dedup

**DedupWorker** (`internal/consolidation/dedup_worker.go`):
1. Listens to `entity.upserted` events
2. Searches for similar entities via embedding cosine distance
3. Merges duplicates by redirecting relations
4. Updates consolidation timestamps

**DreamingWorker** (`internal/consolidation/dreaming_worker.go`):
1. Listens to `episodic.created` events with 10-minute debounce
2. Collects unpromoted episodic summaries (limit: configurable, default 10)
3. Calls LLM for batch synthesis/insight pass
4. Writes results to long-term storage (vault, KG expansion, etc.)
5. Marks summaries as promoted via `MarkPromoted()`

### Consolidation Flow

| Stage | Event | Worker | Output |
|-------|-------|--------|--------|
| 1 | `run.completed` | EpisodicWorker | `episodic_summaries` row + `episodic.created` |
| 2 | `episodic.created` | SemanticWorker | `kg_entities` + `kg_relations` rows + `entity.upserted` |
| 3 | `entity.upserted` | DedupWorker | Merged KG nodes |
| 4 | `episodic.created` (debounced) | DreamingWorker | Promoted episodic + synthetic memory |

---

## 19. Episodic Summaries Table Schema

| Column | Type | Purpose |
|--------|------|---------|
| `id` | UUID | Primary key |
| `tenant_id` | UUID | Multi-tenant scope |
| `agent_id` | UUID | Agent owner |
| `user_id` | VARCHAR(255) | Chat participant (empty for team) |
| `session_key` | TEXT | Reference to original session |
| `summary` | TEXT | Full conversation summary (2-4 paragraphs) |
| `l0_abstract` | TEXT | Short abstract (~50 tokens) for auto-inject |
| `key_topics` | TEXT[] | Extracted entity names for filtering |
| `embedding` | vector(1536) | Vector embedding of full summary |
| `source_type` | TEXT | "session", "v2_daily", "manual" |
| `source_id` | TEXT | Dedup key (unique per source) |
| `turn_count` | INT | Message count in session |
| `token_count` | INT | Total tokens used |
| `created_at` | TIMESTAMPTZ | Creation timestamp |
| `expires_at` | TIMESTAMPTZ | Auto-expiry (90 days default) |

**Indexes:** GIN on `to_tsvector`, HNSW on embedding, unique on `(agent_id, user_id, source_id)`, on `(agent_id, user_id)` for scoped queries.

---

## 20. Knowledge Graph Temporal Validity

Migration 000037 adds temporal columns to KG tables for time-bounded facts.

**Added columns:**
- `valid_from` (TIMESTAMPTZ, default NOW()) — when fact becomes true
- `valid_until` (TIMESTAMPTZ, nullable) — when fact expires (NULL = current)

**Usage pattern:**
```sql
-- Query only current facts
SELECT * FROM kg_entities 
WHERE agent_id = $1 AND valid_until IS NULL;

-- Query facts valid at point in time
SELECT * FROM kg_entities
WHERE agent_id = $1 
  AND valid_from <= $2 
  AND (valid_until IS NULL OR valid_until > $2);
```

**Benefits:**
- Track fact lifecycle (learned → updated → deprecated)
- Support temporal reasoning ("what did we know in January?")
- Auto-expire outdated information via DedupWorker consolidation

---

## File Reference

### Bootstrap Files & Constants
| File | Description |
|------|-------------|
| `internal/bootstrap/files.go` | File constants (AgentsFile, SoulFile, UserPredefinedFile, DelegationFile, TeamFile, AvailabilityFile, MemoryFile, etc.), loading, session filtering |
| `internal/bootstrap/seed.go` | Workspace bootstrap seeding (EnsureWorkspaceFiles, embedded template FS) |
| `internal/bootstrap/seed_store.go` | Store seeding (SeedToStore for agent-level, SeedUserFiles for per-user) |
| `internal/bootstrap/load_store.go` | Load context files from DB (LoadFromStore) |
| `internal/bootstrap/truncate.go` | Truncation pipeline (head/tail split, budget clamping) |
| `internal/bootstrap/templates/*.md` | Embedded template files: AGENTS.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, USER_PREDEFINED.md, BOOTSTRAP.md, BOOTSTRAP_PREDEFINED.md |

### System Prompt & Context Injection
| File | Description |
|------|-------------|
| `internal/agent/systemprompt.go` | System prompt builder (BuildSystemPrompt, PromptFull/PromptMinimal modes) |
| `internal/agent/systemprompt_sections.go` | Section renderers (17+ sections), virtual file handling (DELEGATION.md, TEAM.md, AVAILABILITY.md) |
| `internal/agent/resolver.go` | Agent resolution, virtual file injection, negative context blocks |
| `internal/agent/loop_history.go` | Context file merging (base + per-user, base-only preserved) |
| `internal/agent/memoryflush.go` | Memory flush logic (shouldRunMemoryFlush, runMemoryFlush) |
| `internal/http/summoner.go` | Agent summoning -- LLM-powered context file generation |
| `internal/tools/filesystem.go` | File access interception (write_file, read_file), virtual file reminder handling |

### Skills System
| File | Description |
|------|-------------|
| `internal/skills/loader.go` | Skill loader (5-tier hierarchy, BuildSummary, inline/search mode decision) |
| `internal/skills/search.go` | BM25 search index (tokenization, IDF scoring) |
| `internal/skills/watcher.go` | fsnotify watcher (500ms debounce, hot-reload, version bumping) |
| `internal/store/pg/skills.go` | Managed skill store (embedding search, auto-backfill) |
| `internal/store/pg/skills_grants.go` | Skill grants (agent/user visibility, version pinning, RBAC) |

### V3 Memory System (New)
| File | Description |
|------|-------------|
| `internal/memory/auto_injector.go` | AutoInjector interface for L0 auto-injection into system prompt |
| `internal/memory/auto_injector_impl.go` | AutoInjector implementation (episodic search + relevance filtering) |
| `internal/memory/unified_search.go` | Hybrid search across episodic summaries + KG |
| `internal/memory/l1_cache.go` | L1 cache for fast episodic lookups |
| `internal/consolidation/workers.go` | Worker registration + event subscriptions |
| `internal/consolidation/episodic_worker.go` | Extract summaries from sessions → episodic_summaries |
| `internal/consolidation/semantic_worker.go` | Extract entities/relations from episodic → KG |
| `internal/consolidation/dedup_worker.go` | Merge duplicate entities via embeddings |
| `internal/consolidation/dreaming_worker.go` | Batch synthesis of episodic → long-term memory (10m debounce) |
| `internal/consolidation/l0_abstract.go` | L0 abstract generation (~50 tokens) |

### Memory Store
| File | Description |
|------|-------------|
| `internal/store/episodic_store.go` | EpisodicStore interface (CRUD, search, promotion lifecycle) |
| `internal/store/evolution_store.go` | EvolutionMetricsStore, EvolutionSuggestionStore interfaces |
| `internal/store/vault_store.go` | VaultStore interface (document registry, links, search) |
| `internal/store/pg/episodic*.go` | PG implementation of episodic store |
| `internal/store/pg/memory_docs.go` | Memory document store (chunking, indexing, embedding, scoping) |
| `internal/store/pg/memory_search.go` | Hybrid search (FTS + vector merge, weighted scoring, scope filtering) |

---

## Cross-References

| Document | Relevant Content |
|----------|-----------------|
| [00-architecture-overview.md](./00-architecture-overview.md) | Startup sequence, event bus setup, consolidation worker registration |
| [01-agent-loop.md](./01-agent-loop.md) | Agent loop calls BuildSystemPrompt, auto-injection point, compaction flow |
| [03-tools-system.md](./03-tools-system.md) | ContextFileInterceptor routing, memory_search + memory_expand tools |
| [06-store-data-model.md](./06-store-data-model.md) | episodic_summaries, evolution, vault, KG temporal tables; EpisodicStore, EvolutionStore, VaultStore interfaces |
