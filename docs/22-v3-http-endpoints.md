# 22 — V3 HTTP Endpoints

GoClaw v3 introduces new HTTP endpoints for agent evolution, episodic memory, knowledge vault, and orchestration capabilities. All endpoints follow the standard authentication and error response patterns from [18 — HTTP REST API](18-http-api.md).

---

## 1. Evolution Metrics & Suggestions

Per-agent evolution metrics track tool usage, retrieval performance, and user feedback to drive automated agent improvements.

### Get Evolution Metrics

```
GET /v1/agents/{agentID}/evolution/metrics
```

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `type` | string | no | Filter by metric type: `tool`, `retrieval`, `feedback`. Omit for all types. |
| `aggregate` | boolean | no | Return aggregated metrics (grouped by tool/metric). Default: `false` (raw metrics). |
| `since` | ISO 8601 | no | Start timestamp (default: 7 days ago). Example: `2026-04-01T00:00:00Z` |
| `limit` | integer | no | Max results (default: 100, max: 500). |

**Response (raw metrics):**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "metric_type": "tool",
    "tool_name": "web_fetch",
    "metric_key": "invocation_count",
    "metric_value": 15,
    "metadata": {"status": "success"},
    "recorded_at": "2026-04-06T10:30:00Z"
  },
  {
    "id": "uuid",
    "agent_id": "uuid",
    "metric_type": "retrieval",
    "metric_key": "recall_rate",
    "metric_value": 0.78,
    "metadata": {"query_count": 42},
    "recorded_at": "2026-04-06T11:00:00Z"
  }
]
```

**Response (aggregated metrics):**

```json
{
  "tool_aggregates": [
    {
      "tool_name": "web_fetch",
      "invocation_count": 15,
      "success_count": 14,
      "failure_count": 1,
      "avg_duration_ms": 2340
    },
    {
      "tool_name": "exec",
      "invocation_count": 8,
      "success_count": 8,
      "failure_count": 0,
      "avg_duration_ms": 1200
    }
  ],
  "retrieval_aggregates": [
    {
      "query_count": 42,
      "avg_recall": 0.78,
      "avg_precision": 0.85,
      "avg_relevance_score": 0.81
    }
  ]
}
```

**Status codes:** `200` OK, `400` bad request, `404` agent not found, `500` server error.

### List Evolution Suggestions

```
GET /v1/agents/{agentID}/evolution/suggestions
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `status` | string | Filter: `pending`, `approved`, `applied`, `rejected`, `rolled_back`. Omit for all. |
| `limit` | integer | Max results (default: 50, max: 500). |

**Response:**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "suggestion_type": "low_retrieval_usage",
    "status": "pending",
    "title": "Improve retrieval threshold",
    "description": "Recent queries show low recall. Consider lowering retrieval_threshold from 0.5 to 0.4.",
    "parameters": {
      "current_threshold": 0.5,
      "proposed_threshold": 0.4,
      "confidence": 0.85
    },
    "created_at": "2026-04-06T09:00:00Z",
    "reviewed_by": null,
    "reviewed_at": null
  },
  {
    "id": "uuid",
    "agent_id": "uuid",
    "suggestion_type": "repeated_tool",
    "status": "pending",
    "title": "Tool usage pattern detected",
    "description": "Tool 'exec' called 5+ times in a row without context change. Consider skill creation.",
    "parameters": {
      "tool_name": "exec",
      "occurrence_count": 12,
      "pattern_score": 0.92
    },
    "created_at": "2026-04-05T14:30:00Z",
    "reviewed_by": null,
    "reviewed_at": null
  }
]
```

**Suggestion Types:**
- `low_retrieval_usage` — Retrieval recall is below threshold for recent queries.
- `tool_failure` — High failure rate detected for a tool.
- `repeated_tool` — Tool called repeatedly without context change; candidate for skill.

### Update Suggestion Status

```
PATCH /v1/agents/{agentID}/evolution/suggestions/{suggestionID}
```

**Request:**

```json
{
  "status": "approved",
  "reviewed_by": "optional-user-id"
}
```

**Valid status transitions:** `pending` → `approved`, `rejected`, `rolled_back`.

**Response:**

```json
{
  "status": "ok"
}
```

---

## 2. Episodic Memory Summaries

Episodic memory captures conversation summaries per user session for long-term context continuity.

### List Episodic Summaries

```
GET /v1/agents/{agentID}/episodic
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `user_id` | string | Filter by user ID (optional). |
| `limit` | integer | Max results (default: 20, max: 500). |
| `offset` | integer | Pagination offset (default: 0). |

**Response:**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "user_id": "user-123",
    "summary": "User asked about deployment pipeline optimization. Discussed GitHub Actions, Docker layers, caching strategies. User implemented multi-stage builds.",
    "key_entities": ["GitHub Actions", "Docker", "CI/CD"],
    "sentiment": "positive",
    "interaction_count": 5,
    "tokens_exchanged": 4200,
    "created_at": "2026-04-05T10:00:00Z",
    "updated_at": "2026-04-05T11:30:00Z"
  }
]
```

### Search Episodic Summaries

```
POST /v1/agents/{agentID}/episodic/search
```

**Request:**

```json
{
  "query": "Docker optimization strategies",
  "user_id": "optional-user-id",
  "max_results": 10,
  "min_score": 0.5
}
```

Runs hybrid search combining BM25 (keyword) and vector (semantic) matching.

**Response:**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "user_id": "user-123",
    "summary": "User asked about deployment pipeline optimization...",
    "similarity_score": 0.92,
    "created_at": "2026-04-05T10:00:00Z"
  }
]
```

---

## 3. Knowledge Vault

Persistent knowledge vault stores documents with vector embeddings and outbound/backlink graph connections.

### List Vault Documents

Cross-agent listing:
```
GET /v1/vault/documents
```

Per-agent listing:
```
GET /v1/agents/{agentID}/vault/documents
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `scope` | string | Filter by scope (e.g., `team`, `user`, `global`). |
| `doc_type` | string | Comma-separated doc types (e.g., `guide,reference,note`). |
| `limit` | integer | Max results (default: 20, max: 500). |
| `offset` | integer | Pagination offset. |
| `agent_id` | string | (Cross-agent only) Filter by specific agent. |

**Response:**

```json
[
  {
    "id": "uuid",
    "agent_id": "uuid",
    "title": "Database Indexing Best Practices",
    "doc_type": "guide",
    "scope": "team",
    "content_preview": "Indexes are crucial for query performance. Types include...",
    "created_at": "2026-04-01T09:00:00Z",
    "updated_at": "2026-04-05T14:20:00Z",
    "outlink_count": 3,
    "backlink_count": 2
  }
]
```

### Get Single Document

```
GET /v1/agents/{agentID}/vault/documents/{docID}
```

**Response:**

```json
{
  "id": "uuid",
  "agent_id": "uuid",
  "title": "Database Indexing Best Practices",
  "doc_type": "guide",
  "scope": "team",
  "content": "Indexes are crucial for query performance. Types include: B-tree, Hash, Bitmap...",
  "metadata": {"version": 2, "author": "team-lead"},
  "created_at": "2026-04-01T09:00:00Z",
  "updated_at": "2026-04-05T14:20:00Z"
}
```

### Search Vault Documents

```
POST /v1/agents/{agentID}/vault/search
```

**Request:**

```json
{
  "query": "index performance optimization",
  "scope": "team",
  "doc_types": ["guide", "reference"],
  "max_results": 10
}
```

Hybrid FTS+vector search combining keyword and semantic matching.

**Response:**

```json
[
  {
    "id": "uuid",
    "title": "Database Indexing Best Practices",
    "doc_type": "guide",
    "relevance_score": 0.94,
    "snippet": "...Indexes are crucial for query performance. Types include B-tree, Hash, Bitmap...",
    "created_at": "2026-04-01T09:00:00Z"
  }
]
```

### Get Document Links

```
GET /v1/agents/{agentID}/vault/documents/{docID}/links
```

Returns outgoing links and backlinks for a document.

**Response:**

```json
{
  "outlinks": [
    {
      "target_doc_id": "uuid",
      "target_title": "Query Optimization Techniques",
      "link_type": "references"
    }
  ],
  "backlinks": [
    {
      "source_doc_id": "uuid",
      "source_title": "Performance Tuning Guide",
      "link_type": "referenced_by"
    }
  ]
}
```

---

## 4. Graph Visualization

Lightweight endpoints for rendering vault and knowledge graph relationships.

### Get Vault Graph

```
GET /v1/vault/graph
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `agent_id` | string | Filter by agent (optional). |
| `team_id` | string | Scope by team (optional). |
| `limit` | integer | Max nodes to return (default: 500, max: 2000). |

**Response:**

```json
{
  "nodes": [
    {
      "id": "uuid",
      "label": "Document Title",
      "type": "document",
      "agent_id": "uuid",
      "metadata": {}
    }
  ],
  "edges": [
    {
      "id": "link-uuid",
      "source": "doc-uuid-1",
      "target": "doc-uuid-2",
      "type": "wikilink"
    }
  ],
  "total_nodes": 150,
  "total_edges": 120
}
```

Used for vault document relationship visualization with FA2 layout (web worker rendering).

### Get Knowledge Graph (Compact)

```
GET /v1/agents/{agentID}/kg/graph/compact
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `limit` | integer | Max nodes (default: 500, max: 2000). Optimized 10k node load. |

**Response:** Same format as vault graph; nodes represent semantic entities, edges represent relationship types.

**Features:**
- Semantic zoom support
- Optimized for 10k+ entity graphs
- FA2 layout computed via web worker

---

## 5. Orchestration Mode

Determines how an agent routes requests (standalone, delegation, team-based).

### Get Agent Orchestration Mode

```
GET /v1/agents/{agentID}/orchestration
```

**Response:**

```json
{
  "mode": "delegate",
  "delegate_targets": [
    {
      "agent_key": "research-agent",
      "display_name": "Research Specialist"
    },
    {
      "agent_key": "doc-agent",
      "display_name": "Documentation Expert"
    }
  ],
  "team": null
}
```

Or in team mode:

```json
{
  "mode": "team",
  "delegate_targets": [],
  "team": {
    "id": "uuid",
    "name": "Platform Team"
  }
}
```

**Mode values:**
- `standalone` — No delegation. Agent handles all requests directly.
- `delegate` — Routes complex requests to specialized agents (via agent links).
- `team` — Routes to team members via task system.

---

## 6. V3 Feature Flags

Per-agent feature flags control v3 system capabilities (evolution, episodic memory, vault, etc.).

### Get V3 Flags

```
GET /v1/agents/{agentID}/v3-flags
```

**Response:**

```json
{
  "evolution_enabled": true,
  "episodic_enabled": true,
  "vault_enabled": true,
  "orchestration_enabled": false,
  "skill_evolve": true,
  "self_evolve": false
}
```

### Update V3 Flags

```
PATCH /v1/agents/{agentID}/v3-flags
```

Accepts partial updates. Flag keys are validated against recognized v3 flags.

**Request:**

```json
{
  "evolution_enabled": true,
  "episodic_enabled": false,
  "vault_enabled": true
}
```

**Response:**

```json
{
  "status": "ok"
}
```

---

## File Reference

| File | Purpose |
|------|---------|
| `internal/http/evolution_handlers.go` | Metrics + suggestions endpoints |
| `internal/http/episodic_handlers.go` | Episodic memory list + search endpoints |
| `internal/http/vault_handlers.go` | Knowledge vault document + link endpoints |
| `internal/http/vault_graph_handler.go` | Vault + KG graph visualization endpoints |
| `internal/http/orchestration_handlers.go` | Orchestration mode info endpoint |
| `internal/http/v3_flags_handlers.go` | V3 feature flag get/toggle endpoints |
| `internal/store/evolution_store.go` | Evolution metrics/suggestions store interface |
| `internal/store/episodic_store.go` | Episodic memory store interface |
| `internal/store/vault_store.go` | Knowledge vault store interface |
| `internal/store/pg/evolution_metrics.go` | PostgreSQL evolution metrics impl |
| `internal/store/pg/evolution_suggestions.go` | PostgreSQL evolution suggestions impl |
| `internal/store/pg/episodic.go` | PostgreSQL episodic memory impl |
| `internal/store/pg/vault.go` | PostgreSQL vault documents/links impl |

---

## Cross-References

- [18 — HTTP REST API](18-http-api.md) — Standard authentication, error responses, common headers
- [19 — WebSocket RPC Methods](19-websocket-rpc.md) — V3 WebSocket methods (evolution, orchestration)
- [21 — Agent Evolution & Skill Management](21-agent-evolution-and-skill-management.md) — Evolution system architecture and suggestion engine
