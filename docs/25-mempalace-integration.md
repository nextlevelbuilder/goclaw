# MemPalace Integration

GoClaw can integrate with [MemPalace](https://github.com/milla-jovovich/mempalace), an open-source AI memory system that uses a palace architecture for organizing and retrieving memories. MemPalace achieves 96.6% R@5 on LongMemEval — the highest published score requiring no API key — and runs entirely locally.

## Overview

MemPalace adds 19 MCP tools to your agents, providing:

- **Palace architecture** — Wings (people/projects), rooms (topics), halls (memory types), tunnels (cross-wing connections). The structure delivers a +34% retrieval improvement over flat search.
- **Semantic search** — ChromaDB-backed vector search across all stored memories.
- **Temporal knowledge graph** — SQLite-based entity-relationship triples with time validity windows.
- **AAAK dialect** — Experimental lossy compression for reducing token usage at scale.
- **4-layer memory stack** — L0 identity (~50 tokens) + L1 critical facts (~120 tokens) always loaded; L2/L3 on-demand search.
- **Agent diaries** — Per-agent diary entries in AAAK format, persisted across sessions.

Everything runs locally as a sidecar container. No external API calls, no cloud, no subscription.

## Quick Setup

```bash
# Docker Compose
make up WITH_MEMPALACE=1

# Or manually:
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.mempalace.yml up -d --build
```

That's it. The overlay automatically:
1. Starts a MemPalace sidecar container (Debian-based, with ChromaDB + supergateway)
2. Exposes the MCP server as a streamable-http endpoint on the internal Docker network
3. Registers the MCP server in the database via the init script (visible in admin dashboard)
4. Auto-grants the server to all existing agents
5. GoClaw's MCP bridge connects on agent resolution and registers all 19 tools

## Architecture

```
┌─────────────────────┐         ┌──────────────────────┐
│  GoClaw Container   │  HTTP   │  MemPalace Sidecar   │
│  (Alpine)           │         │  (Debian-slim)       │
│                     │         │                      │
│  ┌───────────────┐  │  MCP    │  ┌────────────────┐  │
│  │ GoClaw Gateway│◄─┼────────►│  │ supergateway   │  │
│  │               │  │         │  │(stdio→strmhttp)│  │
│  └───────┬───────┘  │         │  └───────┬────────┘  │
│          │          │         │          │           │
│          │ pgvector │         │          │ stdio     │
│          ▼          │         │          ▼           │
│  ┌───────────────┐  │         │  ┌────────────────┐  │
│  │  PostgreSQL   │  │         │  │ MemPalace MCP  │  │
│  │  (main DB)    │  │         │  │   Server       │  │
│  └───────────────┘  │         │  └──┬──────┬──────┘  │
│                     │         │     │      │         │
└─────────────────────┘         │  ChromaDB  SQLite   │
                                │     ▼      ▼         │
                                │  ┌──────┐ ┌───────┐  │
                                │  │Palace│ │  KG   │  │
                                │  │Data  │ │       │  │
                                │  └──────┘ └───────┘  │
                                └──────────────────────┘
```

**Why a sidecar?** MemPalace depends on ChromaDB, which requires native C/Rust extensions (hnswlib, numpy). GoClaw's main image uses Alpine (musl libc) where these cannot be compiled. The sidecar uses Debian-slim (glibc) with pre-built wheels, keeping the main image small and the build fast.

GoClaw's native memory system (pgvector hybrid search) and MemPalace run side by side:

- **GoClaw memory** (`memory_search`) — Agent's working memory (MEMORY.md, memory/*.md). Backed by PostgreSQL with FTS + vector hybrid search. Handles per-agent, per-user scoping with multi-tenant isolation.
- **MemPalace** (`mempalace__search`) — Long-term structured memory with palace architecture. Backed by ChromaDB + SQLite knowledge graph. Organizes memories into wings/rooms/halls for 34% better retrieval.

Agents can use both systems: GoClaw memory for session-level context, MemPalace for cross-session knowledge.

## MCP Tools Available

Once enabled, agents have access to these tools (prefixed with `mempalace__`):

### Palace (Read)

| Tool | Description |
|------|-------------|
| `mempalace__status` | Palace overview + AAAK spec + memory protocol |
| `mempalace__list_wings` | List all wings with drawer counts |
| `mempalace__list_rooms` | List rooms within a wing |
| `mempalace__get_taxonomy` | Full wing → room → count tree |
| `mempalace__search` | Semantic search with wing/room filters |
| `mempalace__check_duplicate` | Check for duplicates before filing |
| `mempalace__get_aaak_spec` | AAAK dialect reference |

### Palace (Write)

| Tool | Description |
|------|-------------|
| `mempalace__add_drawer` | File verbatim content with wing/room metadata |
| `mempalace__delete_drawer` | Remove a drawer by ID |

### Knowledge Graph

| Tool | Description |
|------|-------------|
| `mempalace__kg_query` | Query entity relationships (supports temporal filtering) |
| `mempalace__kg_add` | Add relationship triple |
| `mempalace__kg_invalidate` | Mark a fact as ended |
| `mempalace__kg_timeline` | Chronological entity timeline |
| `mempalace__kg_stats` | Graph overview |

### Navigation

| Tool | Description |
|------|-------------|
| `mempalace__traverse` | Walk the graph from a room across wings |
| `mempalace__find_tunnels` | Find rooms bridging two wings |
| `mempalace__graph_stats` | Graph connectivity overview |

### Agent Diary

| Tool | Description |
|------|-------------|
| `mempalace__diary_write` | Write AAAK diary entry |
| `mempalace__diary_read` | Read recent diary entries |

## Data Persistence

Palace data is stored in the `mempalace-data` Docker volume:

```
/home/mempalace/.mempalace/
├── palace/                    # ChromaDB persistent storage (drawers)
├── identity.txt               # Layer 0 identity file (auto-created)
└── knowledge_graph.sqlite3    # Temporal KG (auto-created by MCP server)
```

Data persists across container restarts and rebuilds.

## Configuration

### Custom Identity

Create or edit the identity file inside the sidecar container:

```bash
docker compose exec mempalace sh -c 'cat > /home/mempalace/.mempalace/identity.txt << EOF
I am Atlas, a senior engineering assistant for the Driftwood project.
Team: Kai (backend), Maya (frontend), Priya (PM).
Stack: Go + React + PostgreSQL.
EOF'
```

### MCP Server Registration

The init script (`scripts/mempalace-init.sh`) registers MemPalace as a DB-backed MCP server, making it visible in the admin dashboard under MCP Servers. The equivalent manual registration:

```sql
INSERT INTO mcp_servers (name, display_name, transport, url, tool_prefix, timeout_sec, enabled, created_by, tenant_id)
VALUES ('mempalace', 'MemPalace', 'streamable-http', 'http://mempalace:8000/mcp', 'mempalace', 30, true, 'system', '<your-tenant-id>');
```

### Per-Agent Access Control

By default, the init script auto-grants MemPalace to all agents. To manage access:

- **Via admin dashboard** — Navigate to MCP Servers → MemPalace → Agent Grants
- **Via API** — `POST /v1/mcp/servers/{id}/grants` with agent-specific allow/deny lists
- **Tool filtering** — Grant specific tools only (e.g., allow `search` and `kg_query` but deny `delete_drawer`)

## Mining Existing Data

To import existing conversations or project files into MemPalace:

```bash
# Enter the sidecar container
docker compose exec mempalace sh

# Mine project files (mount or copy them first)
python -m mempalace mine /data/projects --wing my-project

# Mine conversation exports
python -m mempalace mine /data/chats --mode convos --wing team

# Check palace status
python -m mempalace status
```

## Combining with GoClaw Memory

Best practices for using both memory systems together:

| Use Case | System |
|----------|--------|
| Agent's working notes (current session) | GoClaw `memory_search` |
| Long-term knowledge (cross-session) | MemPalace `mempalace__search` |
| Entity relationships (who works on what) | MemPalace `mempalace__kg_query` |
| Recent decisions and context | GoClaw `memory_search` |
| Historical decisions and reasoning | MemPalace `mempalace__search` |
| Conversation summaries and diaries | MemPalace `mempalace__diary_write` |

## Troubleshooting

### MemPalace tools not appearing

Check that the sidecar started and MemPalace is registered in the database:

```bash
# Check sidecar status
docker compose ps mempalace

# Check DB registration
docker compose exec postgres psql -U goclaw -d goclaw \
  -c "SELECT name, transport, enabled FROM mcp_servers WHERE name = 'mempalace'"

# Check agent grants
docker compose exec postgres psql -U goclaw -d goclaw \
  -c "SELECT COUNT(*) FROM mcp_agent_grants mag JOIN mcp_servers ms ON ms.id = mag.server_id WHERE ms.name = 'mempalace'"

# Check GoClaw init logs
docker compose logs goclaw | grep -i "mempalace"
# Expected: "MemPalace: registered in database" and "MemPalace: N agent grant(s) active"
```

### Sidecar not starting

Check sidecar logs:

```bash
docker compose logs mempalace
```

Common issues:
- Port conflict: another service using port 8000 on the Docker network
- Volume permissions: ensure the mempalace-data volume is writable

### Health check failures

The sidecar's health check pings the `/healthz` endpoint. GoClaw also pings the MCP server periodically. Occasional health warnings in logs (`mcp.server.health_failed`) are normal — the MCP bridge auto-reconnects.

### Palace data not persisting

Check the volume mount:

```bash
docker volume inspect goclaw_mempalace-data
docker compose exec mempalace ls -la /home/mempalace/.mempalace/
```

## References

- [MemPalace GitHub](https://github.com/milla-jovovich/mempalace)
- [MemPalace Benchmarks](https://github.com/milla-jovovich/mempalace/tree/main/benchmarks)
- [GoClaw MCP Documentation](03-tools-system.md)
