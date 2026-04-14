# When Vectors Aren't Enough: Building a Knowledge Graph on PostgreSQL

**Date:** 2026-03-09

---

An agent in a Telegram group chat handles 50 messages a day. Alice tells Bob about Project Zalo. Bob reports a bug to the team lead. The lead assigns the fix to Charlie. Three days later, someone asks: "Who is working on the Zalo bug?" The agent searches memory — vector similarity finds chunks mentioning "Zalo" and "bug" separately, but it can't connect the dots. Who told who? Who assigned what? The answer sits across three different conversations, linked by people and actions. That's where a knowledge graph comes in.

```mermaid
graph TB
    subgraph people ["👤 People"]
        direction LR
        Alice(["🧑 Alice"])
        Bob(["🧑 Bob"])
        Lead(["👔 Team Lead"])
        Charlie(["🧑 Charlie"])
    end

    subgraph projects ["📁 Projects"]
        direction LR
        Zalo{{"🚀 Project Zalo"}}
        Docs{{"📄 Zalo Docs"}}
    end

    subgraph tasks ["📋 Tasks"]
        direction LR
        Bug[/"⚠️ Login Bug"/]
        Deploy[/"🔧 Deploy Fix"/]
    end

    subgraph orgs ["🏢 Organizations"]
        NLB["🏛️ NextLevelBuilder"]
    end

    Alice -. "told_about" .-> Bob
    Bob -- "reported_to" --> Lead
    Lead -- "assigned_to" --> Charlie
    Alice -- "works_on" --> Zalo
    Bob -- "works_on" --> Zalo
    Bug -- "belongs_to" --> Zalo
    Charlie -- "fixes" --> Bug
    Charlie -- "deploys" --> Deploy
    Deploy -- "targets" --> Zalo
    Docs -- "documents" --> Zalo
    Lead -- "member_of" --> NLB
    Alice -- "member_of" --> NLB

    style Alice fill:#dbeafe,stroke:#3b82f6,color:#1e40af,stroke-width:2px
    style Bob fill:#dbeafe,stroke:#3b82f6,color:#1e40af,stroke-width:2px
    style Lead fill:#e0e7ff,stroke:#6366f1,color:#3730a3,stroke-width:2px
    style Charlie fill:#dbeafe,stroke:#3b82f6,color:#1e40af,stroke-width:2px
    style Zalo fill:#dcfce7,stroke:#22c55e,color:#166534,stroke-width:2px
    style Docs fill:#dcfce7,stroke:#22c55e,color:#166534,stroke-width:2px
    style Bug fill:#fef3c7,stroke:#f59e0b,color:#92400e,stroke-width:2px
    style Deploy fill:#fef3c7,stroke:#f59e0b,color:#92400e,stroke-width:2px
    style NLB fill:#fee2e2,stroke:#ef4444,color:#991b1b,stroke-width:2px

    linkStyle 0 stroke:#94a3b8,stroke-dasharray:5
    linkStyle 1 stroke:#3b82f6,stroke-width:2px
    linkStyle 2 stroke:#3b82f6,stroke-width:2px
    linkStyle 3 stroke:#22c55e,stroke-width:2px
    linkStyle 4 stroke:#22c55e,stroke-width:2px
    linkStyle 5 stroke:#f59e0b,stroke-width:2px
    linkStyle 6 stroke:#ef4444,stroke-width:2px
    linkStyle 7 stroke:#f59e0b,stroke-width:1px
    linkStyle 8 stroke:#f59e0b,stroke-width:1px
    linkStyle 9 stroke:#22c55e,stroke-width:1px
    linkStyle 10 stroke:#6366f1,stroke-width:1px
    linkStyle 11 stroke:#6366f1,stroke-width:1px
```

**Query: "Who is working on the Zalo bug?"** — Traverse: `Zalo` ←[belongs_to]— `Login Bug` ←[fixes]— `Charlie`. Answer: **Charlie**, reached in 2 hops. Vector search alone cannot follow this chain.

---

## What It Brings

The knowledge graph doesn't replace vector memory — it complements it. Vector search answers "find me something similar to X." The knowledge graph answers "who is connected to what, and how?"

```mermaid
flowchart LR
    subgraph Before
        A[Agent writes memory] --> B[Chunks + Embeddings]
        B --> C[Vector search only]
        C --> D["Similar text matches"]
    end

    subgraph After
        E[Agent writes memory] --> F[Chunks + Embeddings]
        E --> G[LLM Entity Extraction]
        G --> H[Entities + Relations]
        F --> I[Vector search]
        H --> J[Graph traversal]
        I --> K["Semantic matches + Relationship chains"]
        J --> K
    end
```

| | Before | After |
|---|---|---|
| "Who works on Zalo?" | Finds chunks mentioning "Zalo" | Traverses `assigned_to` relation to find `Charlie` |
| Multi-hop | Can't connect A→B→C | Walks 2-3 hops via recursive CTE |
| Admin visibility | Data buried in vectors | Interactive graph visualization |

---

## How It Works

When an agent writes to memory, a background goroutine kicks off automatically. It reads KG settings from the database (provider, model, confidence threshold), sends the content to an LLM for entity extraction, then stores the results atomically.

```mermaid
sequenceDiagram
    participant Agent
    participant Memory as write_file tool
    participant BG as Background extraction
    participant LLM as Extraction LLM
    participant KG as Knowledge Graph DB

    Agent->>Memory: write memory/notes.md
    Memory->>BG: trigger extraction (background)
    BG->>LLM: "Extract top 15 entities from this text"
    LLM-->>BG: JSON {entities, relations}
    BG->>BG: Filter by confidence threshold
    BG->>KG: Atomic upsert (entities + relations)
```

Settings are read from the database on every call — no caching. Admin changes the extraction model in the UI? The very next memory write picks it up. No restart needed.

The extraction prompt is deliberately constrained: "top 15 entities only, descriptions under 50 chars, no code blocks." Without these limits, the LLM produces verbose output that gets truncated mid-JSON — we learned this the hard way.

---

## The Bugs That Bit Us

### The Silent Panic

The worst bug produced zero logs. The extraction goroutine ran in the background. When it panicked, nothing surfaced — the agent kept working normally, but KG stayed empty.

**Root cause:** The LLM returns entities with human-readable IDs like `"alice"`. Relations reference these IDs. But the database uses UUIDs. The code tried to parse `"alice"` as a UUID — instant panic, swallowed by the goroutine.

**Fix:** Upsert each entity with `RETURNING id`, build a lookup map from external_id to actual UUID. Relations resolve through this map. Unknown references are silently skipped instead of crashing.

### The Empty Global View

Clicking "Global" in the UI showed zero entities, even though the database had 26. The SQL was filtering `WHERE user_id = ''` — but every entity has a real user_id like `"group:telegram:-100xxx"`. Empty string matched nothing.

**Fix:** Dynamic WHERE clause. When user_id is empty, simply omit the filter. Applied to all four query methods.

### LLM Output Truncation

Early tests with Qwen showed `finish_reason=length` — the JSON was cut off mid-sentence. Three changes fixed it: input capped at 6K chars (was 12K), max_tokens bumped to 8192 (was 4096), and the prompt changed from "extract ALL" to "extract top 15." Less is more.

---

## The Graph Visualization

The UI offers both a table view and an interactive force-directed graph built with `@xyflow/react` and `d3-force`. Nodes are color-coded by entity type (person, project, task, etc.), edges show relation types, and clicking a node opens its detail dialog.

Getting the layout right took a few iterations. Disconnected clusters kept drifting apart because the only centering force was `forceCenter`, which acts on the centroid, not individual nodes. Adding `forceX`/`forceY` gravity forces solved it — they pull every node gently toward the center, keeping clusters visible without collapsing them.

---

## The Swappable Backend

Everything depends on a single Go interface: `KnowledgeGraphStore`. PostgreSQL is just one implementation. Traversal uses a recursive CTE with a 5-second timeout and cycle detection. If we ever hit PG limits with deep traversals or community detection, swapping in Neo4j means implementing the same interface with Cypher queries. The HTTP handlers, agent tools, and extraction pipeline don't change at all.

---

## Files

| File | What |
|---|---|
| `internal/store/knowledge_graph_store.go` | Interface + types |
| `internal/store/pg/knowledge_graph.go` | PostgreSQL CRUD, search, ingestion |
| `internal/store/pg/knowledge_graph_traversal.go` | Recursive CTE traversal |
| `migrations/000013_knowledge_graph.up.sql` | Schema + indexes |
| `internal/knowledgegraph/extractor.go` | LLM extraction pipeline |
| `internal/knowledgegraph/extractor_prompt.go` | Extraction prompt |
| `internal/tools/knowledge_graph.go` | Agent search/traversal tool |
| `internal/tools/memory_interceptor.go` | Auto-extraction hook |
| `cmd/gateway_managed.go` | Wiring + settings reader |
| `internal/http/knowledge_graph.go` | HTTP handler + routes |
| `internal/http/knowledge_graph_handlers.go` | API endpoints |
| `ui/web/src/pages/memory/kg-entities-tab.tsx` | Entity table + graph toggle |
| `ui/web/src/pages/memory/kg-graph-view.tsx` | Force-directed graph visualization |
| `ui/web/src/pages/memory/kg-entity-detail-dialog.tsx` | Entity detail dialog |
| `ui/web/src/pages/memory/kg-extract-dialog.tsx` | Manual extraction dialog |
| `ui/web/src/pages/memory/hooks/use-knowledge-graph.ts` | React hooks |

---

## Takeaway

Vector memory and knowledge graph serve different purposes. Vectors find similar content. Graphs find connected content. Together, they give agents both fuzzy recall and precise relationship chains.

The interface-based design paid off immediately — every bug fix happened in the PostgreSQL layer without touching anything above it. And the silent goroutine panic taught us a simple rule: always log at the entry point of background work, so you at least know the function was called before it blew up.
