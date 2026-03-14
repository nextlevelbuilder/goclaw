---
name: mcp-builder
description: Guide for creating high-quality MCP (Model Context Protocol) servers using Bun + TypeScript. Use when building MCP servers to integrate external APIs or services. Includes base template scaffolding, tool/resource/prompt patterns, testing with InMemoryTransport, and Docker containerization.
license: Complete terms in LICENSE.txt
---

# MCP Server Development Guide

## Overview

Create MCP (Model Context Protocol) servers that enable LLMs to interact with external services through well-designed tools. Uses **Bun + TypeScript** as the primary runtime with `@modelcontextprotocol/sdk`.

**Base template**: `templates/mcp-server/` — a working Bun/TypeScript MCP server with example tools, resources, prompts, tests, and Docker support. Use this as the starting point for all new MCP server projects.

---

# Process

## Phase 1: Deep Research and Planning

### 1.1 Understand Modern MCP Design

**API Coverage vs. Workflow Tools:**
Balance comprehensive API endpoint coverage with specialized workflow tools. When uncertain, prioritize comprehensive API coverage — agents can compose basic tools into workflows.

**Tool Naming and Discoverability:**
Use snake_case with service prefix: `{service}_{action}_{resource}` (e.g., `github_create_issue`, `slack_send_message`).

**Context Management:**
Design tools that return focused, relevant data. Support pagination and filtering for large result sets.

**Actionable Error Messages:**
Error messages should guide agents toward solutions with specific suggestions and next steps. Never throw from tool handlers — use `toolError()` helper.

### 1.2 Study MCP Protocol Documentation

**Navigate the MCP specification:**

Start with the sitemap: `https://modelcontextprotocol.io/sitemap.xml`

Fetch specific pages with `.md` suffix (e.g., `https://modelcontextprotocol.io/specification/draft.md`).

Key pages to review:
- Specification overview and architecture
- Transport mechanisms (streamable HTTP, stdio)
- Tool, resource, and prompt definitions

### 1.3 Study Framework Documentation

**Primary stack: Bun + TypeScript**

- **Runtime**: Bun (built-in TypeScript, native test runner, fast startup)
- **MCP SDK**: `@modelcontextprotocol/sdk` (official TypeScript SDK)
- **Validation**: Zod for schema validation with `.describe()` for LLM understanding
- **Transport**: stdio (default, local clients) or streamable HTTP via `node:http` (remote/multi-client). **Note:** `StreamableHTTPServerTransport` requires Node.js `IncomingMessage`/`ServerResponse` — use `node:http` (Bun-compatible), NOT `Bun.serve()` which uses incompatible Web API `Request`/`Response`

**Load framework documentation:**

- [📋 MCP Best Practices](./reference/mcp_best_practices.md) - Core guidelines
- [⚡ Bun/TypeScript Guide](./reference/bun_mcp_server.md) - **Primary implementation guide** based on the base template
- **TypeScript SDK**: Use WebFetch to load `https://raw.githubusercontent.com/modelcontextprotocol/typescript-sdk/main/README.md`
- **Bun APIs**: Reference `node_modules/bun-types/docs/**.mdx` for Bun-specific APIs

**For Python (alternative):**
- [🐍 Python Guide](./reference/python_mcp_server.md) - Python/FastMCP patterns
- **Python SDK**: Use WebFetch to load `https://raw.githubusercontent.com/modelcontextprotocol/python-sdk/main/README.md`

### 1.4 Plan Your Implementation

**Understand the API:**
Review the service's API documentation to identify key endpoints, authentication, and data models. Use web search and WebFetch as needed.

**Tool Selection:**
Prioritize comprehensive API coverage. List endpoints to implement, starting with the most common operations.

**Scaffold from template:**
Copy `templates/mcp-server/` as the base project, then:
1. Update `package.json` (name, description)
2. Remove example tools/resources/prompts
3. Add service-specific tools

---

## Phase 2: Implementation

### 2.1 Project Scaffolding

Start from the base template at `templates/mcp-server/`:

```
{service}-mcp-server/
├── src/
│   ├── index.ts              # Entry point: McpServer + transport selection
│   ├── logging.ts            # Stderr-only logging (safe for stdio)
│   ├── errors.ts             # toolError() + withErrorHandling() wrapper
│   ├── tools/
│   │   ├── index.ts          # registerTools() hub
│   │   └── {tool}.ts         # One file per tool
│   ├── resources/
│   │   ├── index.ts          # registerResources() hub
│   │   └── {resource}.ts     # One file per resource
│   └── prompts/
│       ├── index.ts          # registerPrompts() hub
│       └── {prompt}.ts       # One file per prompt
├── tests/
│   └── tools.test.ts         # InMemoryTransport tests
├── package.json              # Bun project, MCP SDK + Zod
├── tsconfig.json             # Strict TypeScript, ESNext
├── Dockerfile                # Multi-stage Alpine build
├── .env.example              # Environment variables
└── README.md
```

See [⚡ Bun/TypeScript Guide](./reference/bun_mcp_server.md) for complete project setup.

### 2.2 Implement Core Infrastructure

Create shared utilities:
- API client with authentication (use native `fetch()`, not axios)
- Error handling with `withErrorHandling()` wrapper from `errors.ts`
- Logging with `log()` helper from `logging.ts` — NEVER use `console.log()`
- Pagination support

### 2.3 Implement Tools

For each tool, follow the pattern from the base template:

**Registration Pattern:**
```typescript
server.tool(
  "tool_name",                          // snake_case with service prefix
  "What this tool does (shown to LLM)", // Description
  { param: z.string().describe("...") }, // Zod schema
  withErrorHandling(async ({ param }) => ({
    content: [{ type: "text", text: `Result: ${param}` }],
  })),
);
```

**Key rules:**
- Use `server.tool()` API (not deprecated `registerTool`)
- Wrap handlers with `withErrorHandling()` — tools must NEVER throw
- Add `.describe()` to all Zod schema fields for LLM understanding
- Return `toolError()` for expected failures
- Use native `fetch()` for HTTP requests (Bun built-in), not axios

**Annotations:**
- `readOnlyHint`: true/false
- `destructiveHint`: true/false
- `idempotentHint`: true/false
- `openWorldHint`: true/false

### 2.4 Implement Resources & Prompts

**Resources** — expose read-only data:
```typescript
server.resource("name", "uri://scheme", { description, mimeType },
  async () => ({ contents: [{ uri, mimeType, text }] }));
```

**Prompts** — reusable prompt templates:
```typescript
server.prompt("name", "Description", { param: z.string() },
  ({ param }) => ({ messages: [{ role: "user", content: { type: "text", text: param } }] }));
```

---

## Phase 3: Review and Test

### 3.1 Code Quality

Review for:
- No duplicated code (DRY principle)
- Consistent error handling with `withErrorHandling()`
- Full Zod schema coverage with `.describe()` on all fields
- SSRF protection on tools that access URLs (see `fetch-url.ts` example)
- No `console.log()` usage (only `console.error()` or `log()`)

### 3.2 Build and Test

```bash
# Run tests (InMemoryTransport, no real stdio/HTTP)
bun test

# Interactive testing with MCP Inspector
bun run inspect

# Verify TypeScript types
bunx tsc --noEmit
```

**Testing pattern** with InMemoryTransport:
```typescript
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";

const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
const server = createServer();
const client = new Client({ name: "test", version: "1.0.0" });
await Promise.all([server.connect(serverTransport), client.connect(clientTransport)]);

const result = await client.callTool({ name: "tool_name", arguments: { ... } });
expect(result.content).toEqual([...]);
```

### 3.3 Docker Build

```bash
docker build -t {service}-mcp-server .
docker run -p 3000:3000 -e MCP_TRANSPORT=http {service}-mcp-server
```

See [⚡ Bun/TypeScript Guide](./reference/bun_mcp_server.md) for quality checklist.

### 3.4 Docker Build Verification (MANDATORY)

After completing the MCP server source code, you MUST verify the build succeeds:

```bash
docker build -t mcp-{project-name}:latest .
```

**This step is non-negotiable.** The Docker build serves as the final verification that:
1. All source files are syntactically correct
2. All dependencies install correctly (`bun install --frozen-lockfile`)
3. The project structure matches the Dockerfile expectations
4. The final image is production-ready and can be used immediately

If the Docker build fails:
- Read the error log carefully
- Fix the source code issues
- Re-run `docker build` until it succeeds
- Do NOT consider the MCP server complete until Docker build passes

The Docker image tag `mcp-{project-name}:latest` will be used directly when registering the server — ensuring zero gap between what was built and what runs in production.

---

## Phase 4: Create Evaluations

After implementing your MCP server, create comprehensive evaluations to test its effectiveness.

**Load [✅ Evaluation Guide](./reference/evaluation.md) for complete evaluation guidelines.**

### 4.1 Understand Evaluation Purpose

Use evaluations to test whether LLMs can effectively use your MCP server to answer realistic, complex questions.

### 4.2 Create 10 Evaluation Questions

Follow the process outlined in the evaluation guide:

1. **Tool Inspection**: List available tools and understand their capabilities
2. **Content Exploration**: Use READ-ONLY operations to explore available data
3. **Question Generation**: Create 10 complex, realistic questions
4. **Answer Verification**: Solve each question yourself to verify answers

### 4.3 Evaluation Requirements

Ensure each question is:
- **Independent**: Not dependent on other questions
- **Read-only**: Only non-destructive operations required
- **Complex**: Requiring multiple tool calls and deep exploration
- **Realistic**: Based on real use cases humans would care about
- **Verifiable**: Single, clear answer that can be verified by string comparison
- **Stable**: Answer won't change over time

### 4.4 Output Format

Create an XML file with this structure:

```xml
<evaluation>
  <qa_pair>
    <question>Find discussions about AI model launches with animal codenames. One model needed a specific safety designation that uses the format ASL-X. What number X was being determined for the model named after a spotted wild cat?</question>
    <answer>3</answer>
  </qa_pair>
<!-- More qa_pairs... -->
</evaluation>
```

---

# Reference Files

## Documentation Library

Load these resources as needed during development:

### Core MCP Documentation (Load First)
- **MCP Protocol**: Start with sitemap at `https://modelcontextprotocol.io/sitemap.xml`, then fetch specific pages with `.md` suffix
- [📋 MCP Best Practices](./reference/mcp_best_practices.md) - Universal MCP guidelines

### Primary Implementation Guide (Load During Phase 1/2)
- [⚡ Bun/TypeScript Guide](./reference/bun_mcp_server.md) - **Primary guide** based on the base template, covering:
  - Project scaffolding from `templates/mcp-server/`
  - Bun runtime patterns (no build step, native TypeScript)
  - `server.tool()` / `server.resource()` / `server.prompt()` registration
  - `withErrorHandling()` + `toolError()` error handling
  - `log()` stderr-only logging (safe for stdio)
  - SSRF protection patterns
  - Testing with `bun:test` + `InMemoryTransport`
  - Docker containerization with `oven/bun:1-alpine`
  - Quality checklist
- **TypeScript SDK**: Fetch from `https://raw.githubusercontent.com/modelcontextprotocol/typescript-sdk/main/README.md`

### Alternative: Python (Load if needed)
- [🐍 Python Implementation Guide](./reference/python_mcp_server.md) - Python/FastMCP guide
- **Python SDK**: Fetch from `https://raw.githubusercontent.com/modelcontextprotocol/python-sdk/main/README.md`

### Evaluation Guide (Load During Phase 4)
- [✅ Evaluation Guide](./reference/evaluation.md) - Complete evaluation creation guide
