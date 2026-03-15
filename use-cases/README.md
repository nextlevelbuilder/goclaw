# Use Cases

Pre-built agent configurations for common GoClaw workflows. Each use case includes the agent definition, context files, and documentation needed to deploy a specialized agent.

## Available Use Cases

| Use Case | Agent Key | Description |
|----------|-----------|-------------|
| [MCP Builder](./mcp-builder/) | `mcp-builder` | Build MCP servers (TypeScript/Python) — research, implement, test, deploy, register |

## Structure

Each use case folder follows this layout:

```
use-cases/{name}/
├── README.md              # Overview, behavior, examples, quality checklist
├── agent.json             # Agent creation payload (POST /v1/agents)
└── context-files/         # Predefined context files
    ├── SOUL.md            # Core identity, boundaries, expertise, workflow
    ├── IDENTITY.md        # Name, emoji, role, traits
    ├── AGENTS.md          # Operating rules, tool usage, quality standards
    └── USER_PREDEFINED.md # Baseline user context (target audience, communication rules)
```

## How to Deploy

### Option 1: Via API

```bash
# 1. Create agent
curl -X POST http://localhost:3000/v1/agents \
  -H "Authorization: Bearer $GOCLAW_GATEWAY_TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-GoClaw-User-Id: admin" \
  -d @use-cases/{name}/agent.json

# 2. Update context files (replace AGENT_ID with the returned id)
# Use the web dashboard: Agents > select agent > Context Files tab
# Or update via database for bulk setup
```

### Option 2: Via Web Dashboard

1. Go to **Agents** page
2. Click **Create Agent**
3. Fill in fields from `agent.json`
4. After creation, go to **Context Files** tab
5. Paste content from `context-files/SOUL.md`, `IDENTITY.md`, `AGENTS.md`

## Creating a New Use Case

1. Create folder: `use-cases/{name}/`
2. Add `agent.json` with the agent creation payload
3. Add `context-files/` with SOUL.md, IDENTITY.md, AGENTS.md
4. Add `README.md` documenting behavior, examples, and quality standards
5. Update this file's table with the new entry
