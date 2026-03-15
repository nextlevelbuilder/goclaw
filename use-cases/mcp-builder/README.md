# Use Case: MCP Builder

> Specialized agent for building MCP (Model Context Protocol) servers — and nothing else.

## Overview

| Field | Value |
|-------|-------|
| **Agent Key** | `mcp-builder` |
| **Agent Type** | `predefined` |
| **Provider** | `claude-acp` |
| **Model** | `claude` |
| **Skill** | `mcp-builder` |
| **Emoji** | 🔌 |

MCP Builder is a focused, single-purpose agent that designs, implements, tests, and deploys MCP servers using TypeScript (preferred) or Python. It strictly refuses any request unrelated to MCP server development.

## Agent Behavior

### What It Does

1. **Research** target API/service documentation
2. **Design** optimal MCP tool sets with proper naming and schemas
3. **Implement** production-quality MCP servers (TypeScript or Python)
4. **Test** via build verification and MCP Inspector
5. **Evaluate** with QA evaluation suites (10 complex questions)
6. **Deploy** locally (stdio), remotely (streamable-http), or on Kubernetes
7. **Register** in GoClaw via `register_mcp_server`

### What It Refuses

- General coding, chat, Q&A, or any non-MCP task
- Non-MCP API integrations
- Document/content writing
- System administration (except MCP deployment)

Response when asked off-topic:
> "I'm MCP Builder — I only build MCP servers. Please describe the MCP server you'd like me to create, or ask your question about MCP development."

## Technical Defaults

| Setting | Default |
|---------|---------|
| Language | TypeScript |
| Transport (remote) | `streamable-http` |
| Transport (local) | `stdio` |
| TS SDK | `@modelcontextprotocol/sdk` + `McpServer.registerTool()` |
| Python SDK | `FastMCP` + `@mcp.tool()` |
| Input validation | Zod `.strict()` (TS) / Pydantic v2 `BaseModel` (Python) |
| Tool naming | `{service}_{action}_{resource}` (snake_case) |
| Response formats | JSON + Markdown (switchable via `response_format` param) |

## Context Files

| File | Purpose |
|------|---------|
| `SOUL.md` | Core identity, boundaries, 5-phase workflow, technical standards |
| `IDENTITY.md` | Name, emoji, role, traits, scope (do/don't) |
| `AGENTS.md` | 6 operating rules, tool usage, quality checklist |
| `USER_PREDEFINED.md` | Baseline user context — target audience, communication rules, defaults for all users |

### Key Design Decisions

- **`self_evolve: false`** — Agent cannot modify its own SOUL.md. Its identity is locked to MCP building only.
- **`agent_type: predefined`** — Shared context files across all users. Each user only gets USER.md + BOOTSTRAP.md.
- **`max_tool_iterations: 30`** — Higher than default (20) because MCP server implementation involves many file writes, builds, and tests.

## Workflow

```
User Request
    │
    ▼
┌──────────────────────────┐
│ Phase 1: Research & Plan │ ← use_skill("mcp-builder"), fetch API docs, load SDK README
└───────────┬──────────────┘
            ▼
┌──────────────────────────┐
│ Phase 2: Implementation  │ ← Project structure, shared utils, tool-by-tool implementation
└───────────┬──────────────┘
            ▼
┌──────────────────────────┐
│ Phase 3: Review & Test   │ ← DRY check, npm run build / py_compile, MCP Inspector
└───────────┬──────────────┘
            ▼
┌──────────────────────────┐
│ Phase 4: Evaluation      │ ← 10 QA pairs (XML), read-only, stable, complex
└───────────┬──────────────┘
            ▼
┌──────────────────────────┐
│ Phase 5: Deploy & Reg.   │ ← stdio/HTTP/K8s, register_mcp_server, grant to agents
└──────────────────────────┘
```

## Example Prompts

### Build a GitHub MCP Server

```
Build an MCP server for the GitHub REST API. Focus on:
- Repository operations (list, get, create, search)
- Issue operations (list, get, create, comment)
- Pull request operations (list, get, create, merge)
Use TypeScript with streamable-http transport.
```

### Build a Slack MCP Server (Python)

```
Create a Python MCP server for Slack Web API. Include:
- Channel listing and info
- Message sending and search
- User lookup
- File upload
Deploy locally via stdio.
```

### Deploy Existing Server to Kubernetes

```
I have a working MCP server at ./github-mcp-server/.
Deploy it to our Kubernetes cluster and register in GoClaw.
```

### Create Evaluation Suite

```
Create an evaluation suite for the GitHub MCP server we just built.
The server is registered as "github-mcp" in GoClaw.
```

## Quality Checklist

Every MCP server built by this agent must satisfy:

- [ ] Service-prefixed tool names (`{service}_{action}_{resource}`)
- [ ] Full input validation with Zod `.strict()` or Pydantic `BaseModel`
- [ ] Tool annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`)
- [ ] Comprehensive tool descriptions (args, returns, examples, error docs)
- [ ] Both JSON and Markdown response format support
- [ ] Pagination for list operations (`limit`, `offset`, `has_more`, `next_offset`)
- [ ] `CHARACTER_LIMIT` constant with truncation for large responses
- [ ] Actionable error messages
- [ ] No code duplication (DRY)
- [ ] Successful build (`npm run build` / `python -m py_compile`)
- [ ] API keys via environment variables only

## Related Files

| Resource | Path |
|----------|------|
| Skill definition | `skills/mcp-builder/SKILL.md` |
| Best practices | `skills/mcp-builder/references/mcp_best_practices.md` |
| TypeScript guide | `skills/mcp-builder/references/node_mcp_server.md` |
| Python guide | `skills/mcp-builder/references/python_mcp_server.md` |
| Evaluation guide | `skills/mcp-builder/references/evaluation.md` |
| GoClaw integration | `skills/mcp-builder/references/goclaw-mcp-integration.md` |
| K8s deployment | `skills/mcp-builder/references/kubernetes-mcp-deployment.md` |
| Evaluation script | `skills/mcp-builder/scripts/evaluation.py` |
