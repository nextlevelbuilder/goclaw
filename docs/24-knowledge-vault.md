# 24 - Knowledge Vault

The Knowledge Vault is a query layer above existing memory systems (episodic memory, knowledge graph, memory files). It provides document registration, bidirectional wikilinks, filesystem sync, and unified search across all knowledge sources.

**Not a replacement** — extends capability. The vault sits between agents and episodic/KG stores, enabling agents to curate workspace documents with explicit relationships.

---

## 1. Architecture Overview

### Components

| Component | Role |
|-----------|------|
| **VaultStore** | Interface: document CRUD, link management, hybrid search (FTS+vector) |
| **VaultService** | Search coordinator: fan-out across vault, episodic, KG; parallel queries with weighted ranking |
| **VaultSyncWorker** | Filesystem watcher: detects file changes (create/write/delete), syncs content hashes back to registry |
| **VaultRetriever** | Retrieval adapter: bridges vault search into agent L0 memory system |
| **HTTP Handlers** | REST endpoints: list, get, search, links |

### Data Flow

```
Agent writes document → Workspace FS
                    ↓
              VaultSyncWorker detects change
                    ↓
         Update vault_documents (hash, metadata)
                    ↓
         On agent query: vault_search tool
                    ↓
    VaultSearchService (parallel fan-out)
         ↙        ↓        ↘
    Vault    Episodic    Knowledge Graph
         ↘        ↓        ↙
      Normalize & Weight Scores
         ↓
   Return Top Results
```

### Tenant & Scope Isolation

Documents are scoped by **tenant** (isolation), **agent** (per-agent namespace), and **document scope** (personal/team/shared):

- **personal**: Agent-specific documents (agent_context_files, per-user work)
- **team**: Team workspace documents (team_context_files)
- **shared**: Cross-tenant shared knowledge (future)

### Document Scope & Ownership

| scope | agent_id | team_id | Visibility |
|-------|----------|---------|------------|
| personal | set  | NULL   | Owning agent only (within tenant) |
| team     | NULL | set    | Members of the team (within tenant) |
| shared   | NULL | NULL   | All agents within the tenant |
| custom   | any  | any    | User-defined via `custom_scope` |

#### DB Invariant (migration 000055)

A CHECK constraint enforces the above table: `vault_documents_scope_consistency`. Rejects inserts/updates that violate the scope × ownership relationship. `scope='custom'` is unconstrained (user-defined scopes).

#### Agent Read Semantics

`vault_search`, `ListDocuments`, `CountDocuments` return:
- Docs owned by the agent (`agent_id = <agent>`)
- PLUS shared docs (`agent_id IS NULL`)

Within a team context (RunContext with TeamID set), results also include team-scoped docs for that team. Tenant isolation (`tenant_id = <tenant>`) is always enforced.

---

## 2. Data Model

### vault_documents

Document registry: metadata pointers. Content lives on filesystem; registry holds path, hash, embeddings, and links.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `tenant_id` | UUID | Multi-tenant isolation |
| `agent_id` | UUID | Per-agent namespace |
| `scope` | TEXT | personal \| team \| shared |
| `path` | TEXT | Workspace-relative path (workspace/notes/foo.md) |
| `title` | TEXT | Display name |
| `doc_type` | TEXT | context, memory, note, skill, episodic |
| `content_hash` | TEXT | SHA-256 of file content (detects changes) |
| `embedding` | vector(1536) | pgvector: semantic similarity |
| `tsv` | tsvector | Generated: FTS index on title+path |
| `metadata` | JSONB | Optional custom fields |
| `created_at`, `updated_at` | TIMESTAMPTZ | Timestamps |
| **Unique constraint** | (agent_id, scope, path) | One doc per path per scope |

**Indices:**
- `idx_vault_docs_tenant` — tenant_id (multi-tenant queries)
- `idx_vault_docs_agent_scope` — agent_id, scope (agent-scoped filters)
- `idx_vault_docs_type` — agent_id, doc_type (type filters)
- `idx_vault_docs_hash` — content_hash (change detection)
- `idx_vault_docs_embedding` — HNSW vector (semantic search)
- `idx_vault_docs_tsv` — GIN FTS index (keyword search)

### vault_links

Bidirectional links between documents (wikilinks, explicit references).

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `from_doc_id` | UUID | Source document |
| `to_doc_id` | UUID | Target document |
| `link_type` | TEXT | wikilink, reference, etc. |
| `context` | TEXT | ~50 chars: surrounding text snippet |
| `created_at` | TIMESTAMPTZ | Creation timestamp |
| **Unique constraint** | (from_doc_id, to_doc_id, link_type) | No duplicate links |

**Indices:**
- `idx_vault_links_from` — from_doc_id (outgoing links)
- `idx_vault_links_to` — to_doc_id (backlinks)

### vault_versions

Version history (v3.1+ preparation; empty in v3.0).

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `doc_id` | UUID | Document reference |
| `version` | INT | Version number |
| `content` | TEXT | Snapshot of document content |
| `changed_by` | TEXT | User/agent identifier |
| `created_at` | TIMESTAMPTZ | Snapshot timestamp |

---

## 3. Wikilinks

Bidirectional markdown links in the `[[target]]` format.

### Parsing & Extraction

`ExtractWikilinks(content string)` parses all `[[...]]` patterns:

- **Format:** `[[path/to/file.md]]` or `[[name|display text]]` (display text ignored)
- **Returns:** `[]WikilinkMatch` with target, context (~50 chars), and byte offset

Examples:
```markdown
See [[architecture/components]] for details.
Reference [[SOUL.md|agent persona]] here.
Link [[../parent-project]] up.
```

**Edge cases:**
- Empty target `[[]]` → skipped
- Whitespace-only targets → trimmed and skipped
- Paths with spaces: `[[foo bar]]` → preserved
- File extensions: `.md` auto-appended if missing

### Resolution Strategy

`ResolveWikilinkTarget()` finds the document matching a wikilink target:

1. **Exact path match** — `GetDocument(tenantID, agentID, "path/to/file.md")`
2. **With .md suffix** — If target lacks `.md`, retry with suffix
3. **Basename search** — Linear search through all agent docs; match by basename (case-insensitive)
4. **Unresolved** — Return nil (not an error; backlinks can be incomplete)

Example: `[[SOUL.md]]` → lookup SOUL.md → fallback to lookup SOUL → scan for any doc with basename SOUL

### Link Sync

`SyncDocLinks(ctx, vaultStore, doc, content, tenantID, agentID)` keeps wikilinks in vault_links in sync with document content:

1. Extract `[[...]]` patterns from content
2. Delete all outgoing links for the document (replace strategy)
3. For each match:
   - Resolve target via strategy above
   - Create `vault_link` row if resolved
4. Return (errors logged, not thrown)

Called during:
- Document upsert (agent writing to workspace)
- VaultSyncWorker processing file changes

---

## 4. Search

Hybrid search integrates vault FTS, vector embeddings, episodic memory, and knowledge graph.

### Vault Search (Store-Level)

`VaultStore.Search(ctx, opts VaultSearchOptions)` on single vault:

- **FTS**: PostgreSQL `plainto_tsquery()` on tsv (title+path keywords)
- **Vector**: pgvector cosine similarity on embedding (semantic)
- **Combined scoring**: Normalize each method's scores (0–1), then apply query-time weights
- **Results:** Top N documents with score

### Unified Search (Cross-Store)

`VaultSearchService.Search(ctx, opts UnifiedSearchOptions)`:

**Parallel fan-out:**
```
Query → ├─ VaultStore.Search()      [0.4 weight]
        ├─ EpisodicStore.Search()   [0.3 weight]
        └─ KGStore.SearchEntities() [0.3 weight]
                ↓
         Normalize each source
         (max score = 1.0, then * weight)
                ↓
         Merge & deduplicate by ID
                ↓
         Sort by final score DESC
                ↓
         Return top N
```

**Score normalization:** Each source's scores scaled to 0–1 (max_score / weight), then weighted:
- Vault: 0.4 (keyword + semantic)
- Episodic: 0.3 (session summaries)
- KG: 0.3 (entity relationships)

Default max results per source: `maxResults * 2` (then deduplicate + cap to maxResults).

### Parameters

| Param | Type | Default | Notes |
|-------|------|---------|-------|
| `Query` | string | — | Required: natural language |
| `AgentID` | string | — | Scope to agent |
| `TenantID` | string | — | Scope to tenant |
| `Scope` | string | all | personal, team, shared |
| `DocTypes` | []string | all | context, memory, note, skill, episodic |
| `MaxResults` | int | 10 | Results per final result set |
| `MinScore` | float64 | 0.0 | Filter by minimum score |

---

## 5. Filesystem Sync

`VaultSyncWorker` watches workspace directories for changes and keeps vault_documents hashes in sync.

### Watcher Loop

Uses `fsnotify` to detect Write, Create, Remove events:

1. Debounce: 500ms (multiple rapid changes → one batch)
2. For each file change:
   - Compute SHA-256 hash of file content
   - Compare to `vault_documents.content_hash`
   - If different: update hash in DB
   - If file deleted: mark `metadata["deleted"] = true`

### Constraints

- Only syncs **registered** documents (files already in vault_documents)
- New files must be registered by agent (via agent write) first
- Unreadable files → marked as deleted

### Setup

```go
syncer := vault.NewVaultSyncWorker(vaultStore)
go syncer.Watch(ctx, workspaceDir, tenantID, agentID)
```

---

## 6. HTTP API

RESTful endpoints for vault operations.

### List Documents

**Endpoint:** `GET /v1/agents/{agentID}/vault/documents`

**Query params:**
- `scope` — personal, team, or shared (optional)
- `doc_type` — comma-separated types (optional)
- `limit` — default 20, max 500
- `offset` — pagination

**Response:**
```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "path": "workspace/notes/foo.md",
    "title": "Foo Notes",
    "doc_type": "note",
    "content_hash": "sha256hex",
    "created_at": "2026-04-07T00:00:00Z"
  }
]
```

### Get Document

**Endpoint:** `GET /v1/agents/{agentID}/vault/documents/{docID}`

**Response:** Single VaultDocument object

### Search

**Endpoint:** `POST /v1/agents/{agentID}/vault/search`

**Request body:**
```json
{
  "query": "authentication flow",
  "scope": "team",
  "doc_types": ["context", "note"],
  "max_results": 10
}
```

**Response:**
```json
[
  {
    "document": { /* VaultDocument */ },
    "score": 0.87,
    "source": "vault"
  },
  {
    "document": { /* episodic record */ },
    "score": 0.65,
    "source": "episodic"
  }
]
```

### Get Links

**Endpoint:** `GET /v1/agents/{agentID}/vault/documents/{docID}/links`

**Response:**
```json
{
  "outlinks": [
    {
      "id": "uuid",
      "to_doc_id": "uuid",
      "link_type": "wikilink",
      "context": "See [[target]] for details."
    }
  ],
  "backlinks": [
    {
      "id": "uuid",
      "from_doc_id": "uuid",
      "link_type": "wikilink",
      "context": "Reference [[SOUL.md]] here."
    }
  ]
}
```

### List (Cross-Agent)

**Endpoint:** `GET /v1/vault/documents`

**Query params:**
- `agent_id` — optional filter to specific agent (otherwise all tenant agents)
- `scope`, `doc_type`, `limit`, `offset` — same as per-agent endpoint

---

## 7. Tools

Agents access vault via two tools.

### vault_search

**Primary discovery tool:** Search across all knowledge sources (vault, episodic, KG) with unified ranking.

**Parameters:**
```json
{
  "query": "string (required)",
  "scope": "string (optional: personal|team|shared)",
  "types": "string (optional: comma-separated doc types)",
  "maxResults": "number (optional: default 10)"
}
```

**Example:**
```
Agent: "Find documents about authentication"
Tool call: vault_search(query="authentication", types="context,note")
Result: Top 10 results from vault + episodic + KG
```

### vault_link

**Create explicit link:** Connect two documents (similar to [[wikilink]]).

**Parameters:**
```json
{
  "from": "string (source document path, required)",
  "to": "string (target document path, required)",
  "context": "string (optional: relationship description)"
}
```

**Example:**
```
Agent: "Link the authentication guide to the SOUL file"
Tool call: vault_link(from="docs/auth.md", to="SOUL.md", context="Persona reference")
Result: Explicit link created in vault_links
```

---

## 8. Retriever Integration

`VaultRetriever` bridges vault search into agent L0 memory system.

### Usage

```go
retriever := vault.NewVaultRetriever(searchService)
summaries, err := retriever.RetrieveL0(ctx, agentID, userID, query, config)
// Returns []memory.L0Summary with highest-ranked results
```

### Configuration

```go
type RetrieverConfig struct {
  RelevanceThreshold float64  // default 0.3
  MaxL0Items         int      // default 5
  TenantID           string
}
```

Used by agent to retrieve relevant documents during think phase (before plan/act).

---

## 9. Web UI (v3)

Vault page in dashboard displays:

- **Document list** — workspace documents filtered by scope/type
- **Graph visualization** — nodes = documents, edges = wikilinks, interactive pan/zoom
- **Search interface** — unified cross-store search with source badges
- **Link editor** — create/remove links between documents

---

## 10. Feature Flags & Configuration

Knowledge Vault is **v3-only** feature.

- **Edition:** Standard and Lite (full support)
- **Prerequisite:** PostgreSQL with pgvector extension
- **Storage:** vault_* tables created by migration 000038
- **Workspace:** Documents organized in agent-level workspace directory (e.g., `~/.goclaw/workspace/agent_name/`)

No explicit feature flag; vault is enabled if:
1. Migration 000038 ran successfully
2. VaultStore initialized during gateway startup
3. VaultSyncWorker started

---

## 11. Examples

### Adding a Document

Agent writes to workspace:
```
~/.goclaw/workspace/myagent/notes/architecture.md
```

On next sync (or immediate write):
1. VaultSyncWorker detects file creation
2. Computes SHA-256 hash
3. Vault document auto-registered with metadata

### Creating a Wikilink

Markdown content:
```markdown
See [[architecture.md]] for system design.
See [[SOUL.md|persona]] for agent personality.
```

Agent calls `vault_search("system design")`:
1. ExtractWikilinks finds `[[architecture.md]]` and `[[SOUL.md]]`
2. ResolveWikilinkTarget matches to registered documents
3. SyncDocLinks creates vault_link rows
4. Backlinks available via `/links` endpoint

### Search Example

Agent: "Find notes about authentication"

Request:
```json
POST /v1/agents/agent-123/vault/search
{
  "query": "authentication flow",
  "scope": "personal",
  "max_results": 5
}
```

Response (parallel results from vault + episodic + KG):
```json
[
  {
    "document": {
      "id": "doc-456",
      "path": "notes/auth.md",
      "title": "Authentication Flow",
      "doc_type": "note"
    },
    "score": 0.92,
    "source": "vault"
  },
  {
    "id": "episodic-789",
    "title": "Session-2026-04-06",
    "source": "episodic",
    "score": 0.68
  }
]
```

---

## 12. Limitations

- **Vault docs do not auto-embed in system prompt** — Must be retrieved via agent tools
- **No full-text indexing on document content** — Only title+path FTS; content requires embeddings
- **Sync is one-way** — Filesystem changes sync to vault; vault does not write back to FS
- **No conflict resolution** — Concurrent edits not detected; last write wins
- **Version history empty** — vault_versions table prepared for v3.1

---

## File References

- **Vault service:** `internal/vault/*.go`
- **Store interface:** `internal/store/vault_store.go`
- **HTTP handlers:** `internal/http/vault_handlers.go`
- **Tools:** `internal/tools/vault_*.go`
- **Migration:** `migrations/000038_vault_tables.up.sql`
