# 17 - MCP Builder Skill & Server Registration

How the bundled `mcp-builder` skill guides agents through MCP server development, and how the `register_mcp_server` builtin tool bridges the gap between building and using MCP servers in GoClaw.

---

## 1. Overview

The MCP builder system consists of two components working together:

| Component | Type | Purpose |
|-----------|------|---------|
| `mcp-builder` | Core skill (bundled) | Guides agents through MCP server design, implementation, testing, evaluation, and registration |
| `register_mcp_server` | Builtin tool | Registers a built MCP server in the `mcp_servers` database table, making its tools available to agents |

This mirrors the `skill-creator` + `publish_skill` pattern used for skill development.

---

## 2. Skill Structure

```
skills/mcp-builder/
├── SKILL.md                         # Core instructions (5 phases)
├── LICENSE.txt                      # Apache 2.0
├── references/
│   ├── mcp_best_practices.md        # Universal MCP guidelines
│   ├── node_mcp_server.md           # TypeScript/Node.js implementation guide
│   ├── python_mcp_server.md         # Python/FastMCP implementation guide
│   ├── evaluation.md                # Eval creation guide
│   └── goclaw-mcp-integration.md    # Native GoClaw registration guide
├── scripts/
│   ├── connections.py               # MCP client connection helpers
│   ├── evaluation.py                # Eval harness (Anthropic API)
│   ├── example_evaluation.xml       # Sample eval file
│   └── requirements.txt             # Python deps (anthropic, mcp)
```

---

## 3. Five-Phase Workflow

```
Phase 1: Deep Research and Planning
    → Study MCP protocol, SDK docs, API being integrated

Phase 2: Implementation
    → Set up project, implement tools with proper schemas

Phase 3: Review and Test
    → Code quality review, build verification, MCP Inspector testing

Phase 4: Create Evaluations
    → 10 QA pairs testing LLM effectiveness with the server

Phase 5: Register in GoClaw        ← NEW (native integration)
    → register_mcp_server tool → mcp_servers table → auto-grant
```

---

## 4. register_mcp_server Tool

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Unique server name |
| `transport` | string | Yes | `stdio`, `sse`, or `streamable-http` |
| `command` | string | stdio only | Command to run |
| `args` | string[] | stdio only | Command arguments |
| `url` | string | sse/http only | Server URL |
| `headers` | object | No | HTTP headers (encrypted) |
| `env` | object | stdio only | Env vars (encrypted) |
| `api_key` | string | No | API key (AES-256-GCM encrypted) |
| `display_name` | string | No | Human-friendly name |
| `tool_prefix` | string | No | Prefix for tool names |
| `timeout_sec` | int | No | Timeout (default: 30) |

### Execution Flow

```
register_mcp_server(args)
    │
    ├─ Validate transport-specific fields
    ├─ Check name uniqueness (GetServerByName)
    ├─ Encrypt sensitive fields (api_key, headers, env)
    ├─ CreateServer() → INSERT into mcp_servers
    ├─ Auto-grant to calling agent (GrantToAgent)
    └─ Return server ID + status
```

### Registration

- **Gateway startup**: `cmd/gateway.go` registers the tool when `pgStores.MCP` is available
- **Builtin tools seed**: Listed in `cmd/gateway_builtin_tools.go` under category `"mcp"`
- **i18n**: Keys in `keys.go`, translations in all 3 catalogs

---

## 5. Comparison with skill-creator Pattern

| Aspect | skill-creator + publish_skill | mcp-builder + register_mcp_server |
|--------|-------------------------------|-----------------------------------|
| **Skill guidance** | Creates SKILL.md + scripts | Creates MCP server project |
| **Registration tool** | `publish_skill` → `skills` table | `register_mcp_server` → `mcp_servers` table |
| **Storage** | Versioned copies in `skills-store/` | Config in DB (code stays in workspace) |
| **Auto-grant** | Grants skill to calling agent | Grants MCP server to calling agent |
| **Hash tracking** | SHA-256 of SKILL.md | N/A (config-based, no file tracking) |
| **Dependency check** | Python/Node import scan | N/A (server manages its own deps) |
| **Encryption** | N/A | AES-256-GCM for api_key, headers, env |

---

## 6. Database Schema

The `register_mcp_server` tool writes to the existing `mcp_servers` table:

```sql
CREATE TABLE mcp_servers (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    display_name VARCHAR(255),
    transport VARCHAR(20) NOT NULL,     -- stdio, sse, streamable-http
    command VARCHAR(255),                -- stdio only
    args JSONB,                          -- stdio only
    url TEXT,                            -- sse/http only
    headers JSONB,                       -- encrypted
    env JSONB,                           -- encrypted (stdio only)
    api_key TEXT,                        -- AES-256-GCM encrypted
    tool_prefix VARCHAR(100),
    timeout_sec INT DEFAULT 30,
    settings JSONB,
    enabled BOOLEAN DEFAULT true,
    created_by VARCHAR(255),
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);
```

Auto-grant writes to `mcp_agent_grants`:

```sql
CREATE TABLE mcp_agent_grants (
    id UUID PRIMARY KEY,
    server_id UUID REFERENCES mcp_servers,
    agent_id UUID REFERENCES agents,
    enabled BOOLEAN DEFAULT true,
    tool_allow JSONB,
    tool_deny JSONB,
    config_overrides JSONB,
    granted_by VARCHAR(255),
    created_at TIMESTAMPTZ
);
```

---

## 7. Seeding (Automatic)

The `mcp-builder` skill is bundled in `skills/mcp-builder/` and seeded automatically at gateway startup via the same `Seeder` mechanism as all core skills:

1. Seeder reads `skills/mcp-builder/SKILL.md`
2. Parses frontmatter (name, slug, description)
3. Computes SHA-256 hash
4. `UpsertSystemSkill()` → DB insert/update
5. Copies files to `skills-store/mcp-builder/<version>/`
6. `CheckDepsAsync()` → scans `scripts/requirements.txt` → detects `anthropic`, `mcp`

No code changes needed for seeding — the skill is discovered automatically.

---

## 8. Related Files

| File | Purpose |
|------|---------|
| `skills/mcp-builder/SKILL.md` | Core skill instructions |
| `skills/mcp-builder/references/*.md` | Reference docs (5 files) |
| `skills/mcp-builder/scripts/*.py` | Eval harness + connection helpers |
| `internal/tools/register_mcp_server.go` | Builtin tool implementation |
| `internal/store/mcp_store.go` | MCPServerStore interface |
| `internal/store/pg/mcp_servers.go` | PostgreSQL implementation |
| `internal/store/pg/mcp_servers_access.go` | Agent/user grant management |
| `cmd/gateway.go` | Tool registration at startup |
| `cmd/gateway_builtin_tools.go` | Seed data for builtin tools |
| `internal/i18n/keys.go` | i18n message keys |
| `internal/i18n/catalog_{en,vi,zh}.go` | Translations |
