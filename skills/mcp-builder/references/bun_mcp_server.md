# Bun MCP Server Implementation Guide

## Overview

This document provides Bun/TypeScript-specific best practices and examples for implementing MCP servers using the MCP TypeScript SDK with Bun runtime. It covers project structure, server setup, tool registration patterns, input validation with Zod, error handling, transport options, Dockerfile, and complete working examples.

**IMPORTANT: Use Bun instead of Node.js for ALL operations.** Bun is faster, has built-in TypeScript support (no transpilation needed), and provides a simpler development experience.

---

## Quick Reference

### Key Imports

```typescript
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { createServer as createHttpServer } from "node:http";
import { z } from "zod";
```

### Server Initialization

```typescript
const server = new McpServer({
  name: "service-mcp-server",
  version: "1.0.0",
});
```

### Tool Registration Pattern

```typescript
server.tool(
  "tool_name",
  "What this tool does (shown to LLM)",
  {
    param: z.string().describe("Description for LLM"),
  },
  withErrorHandling(async ({ param }) => ({
    content: [{ type: "text", text: `Result: ${param}` }],
  })),
);
```

---

## Bun Runtime Specifics

### Why Bun

- **Zero config TypeScript**: Run `.ts` files directly — no `tsc`, `tsx`, or `ts-node` needed
- **Built-in test runner**: `bun test` instead of Jest/Vitest
- **Fast package manager**: `bun install` is significantly faster than npm/yarn
- **Auto .env loading**: Bun loads `.env` automatically — no `dotenv` needed
- **Node.js compatible**: All `node:*` modules work in Bun

### Key Commands

```bash
bun install                 # Install dependencies (NOT npm install)
bun run src/index.ts        # Run server directly (NOT node/tsx)
bun --hot run src/index.ts  # Development with hot reload
bun test                    # Run tests (NOT jest/vitest)
bun build src/index.ts      # Build if needed
bunx <package>              # Execute packages (NOT npx)
```

### CRITICAL: MCP HTTP Transport

**Use `node:http` (Bun-compatible), NOT `Bun.serve()`.**

`StreamableHTTPServerTransport.handleRequest()` requires Node.js `IncomingMessage`/`ServerResponse`, which are incompatible with Bun's Web API `Request`/`Response`.

```typescript
// CORRECT: Use node:http (works in Bun)
import { createServer as createHttpServer } from "node:http";

const httpServer = createHttpServer(async (req, res) => {
  if (url.pathname === "/mcp") {
    await transport.handleRequest(req, res);
  }
});

// WRONG: Do NOT use Bun.serve() for MCP HTTP transport
// Bun.serve({ fetch(req) { ... } }); // INCOMPATIBLE!
```

---

## MCP TypeScript SDK

The official MCP TypeScript SDK provides:
- `McpServer` class for server initialization
- `server.tool()` method for tool registration
- `server.resource()` for resource registration
- `server.prompt()` for prompt registration
- Zod schema integration for runtime input validation

### Tool Registration API

Use `server.tool()` with name, description, schema, and handler:

```typescript
server.tool(
  "tool_name",              // snake_case name
  "Tool description",       // Shown to LLM
  { /* Zod schema */ },     // Input validation
  handler,                  // Async handler function
);
```

---

## Server Naming Convention

Bun MCP servers follow this naming pattern:
- **Format**: `{service}-mcp-server` (lowercase with hyphens)
- **Examples**: `github-mcp-server`, `pokeapi-mcp-server`, `stripe-mcp-server`

The name should be:
- General (not tied to specific features)
- Descriptive of the service/API being integrated
- Easy to infer from the task description
- Without version numbers or dates

## Project Structure

```
{service}-mcp-server/
├── src/
│   ├── index.ts              # Entry point — creates McpServer, selects transport
│   ├── logging.ts            # Stderr-only logging (NEVER use console.log in stdio mode)
│   ├── errors.ts             # toolError() helper + withErrorHandling() wrapper
│   ├── tools/                # Tool handlers (one file per tool or domain)
│   │   ├── index.ts          # registerTools() — add new tools here
│   │   └── {tool-name}.ts    # Individual tool implementation
│   ├── resources/            # Resources (read-only data exposed to clients)
│   │   ├── index.ts          # registerResources()
│   │   └── {resource}.ts     # Individual resource
│   └── prompts/              # Prompts (reusable prompt templates)
│       ├── index.ts          # registerPrompts()
│       └── {prompt}.ts       # Individual prompt
├── tests/
│   └── tools.test.ts         # Tests using InMemoryTransport
├── .env.example              # Environment variable documentation
├── .gitignore
├── Dockerfile                # Multi-stage Bun Dockerfile
├── package.json
├── tsconfig.json
└── README.md
```

## Tool Implementation

### Tool Naming

Use snake_case for tool names with service prefix:
- `pokeapi_get_pokemon` (not just `get_pokemon`)
- `github_create_issue` (not just `create_issue`)
- `slack_send_message` (not just `send_message`)

Format: `{service}_{action}_{resource}`

### Error Handling Pattern

**Tool handlers must NEVER throw.** Use `withErrorHandling()` wrapper:

```typescript
// src/errors.ts
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

export function toolError(message: string): CallToolResult {
  return {
    content: [{ type: "text", text: `Error: ${message}` }],
    isError: true,
  };
}

export function withErrorHandling<T>(
  handler: (args: T) => Promise<CallToolResult>,
): (args: T) => Promise<CallToolResult> {
  return async (args: T) => {
    try {
      return await handler(args);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return toolError(message);
    }
  };
}
```

### Logging

**NEVER use `console.log()` in MCP servers** — it writes to stdout and corrupts JSON-RPC protocol. Use `console.error()` (stderr):

```typescript
// src/logging.ts
type LogLevel = "debug" | "info" | "warn" | "error";

const LOG_LEVELS: Record<LogLevel, number> = {
  debug: 0, info: 1, warn: 2, error: 3,
};

const currentLevel = (process.env.LOG_LEVEL as LogLevel) ?? "info";

export function log(level: LogLevel, message: string, ...args: unknown[]) {
  if (LOG_LEVELS[level] < LOG_LEVELS[currentLevel]) return;
  const timestamp = new Date().toISOString();
  console.error(`[${timestamp}] [${level.toUpperCase()}]`, message, ...args);
}
```

### Complete Tool Example

```typescript
// src/tools/get-pokemon.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { withErrorHandling, toolError } from "../errors.ts";

const schema = {
  name: z.string().min(1).max(100).describe("Pokemon name or ID (e.g., 'pikachu', '25')"),
};

export function registerGetPokemonTool(server: McpServer) {
  server.tool(
    "pokeapi_get_pokemon",
    "Get detailed information about a Pokemon by name or ID. Returns stats, types, abilities, and sprites.",
    schema,
    withErrorHandling(async ({ name }) => {
      const response = await fetch(
        `https://pokeapi.co/api/v2/pokemon/${encodeURIComponent(name.toLowerCase())}`,
        { signal: AbortSignal.timeout(10_000) },
      );

      if (!response.ok) {
        if (response.status === 404) {
          return toolError(`Pokemon "${name}" not found. Check the name or ID.`);
        }
        return toolError(`API error: HTTP ${response.status}`);
      }

      const data = await response.json();

      const info = {
        id: data.id,
        name: data.name,
        height: data.height,
        weight: data.weight,
        types: data.types.map((t: any) => t.type.name),
        abilities: data.abilities.map((a: any) => ({
          name: a.ability.name,
          is_hidden: a.is_hidden,
        })),
        stats: Object.fromEntries(
          data.stats.map((s: any) => [s.stat.name, s.base_stat]),
        ),
        sprite: data.sprites.front_default,
      };

      return {
        content: [{ type: "text", text: JSON.stringify(info, null, 2) }],
      };
    }),
  );
}
```

### Tool Registration Index

```typescript
// src/tools/index.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerGetPokemonTool } from "./get-pokemon.ts";

export function registerTools(server: McpServer) {
  registerGetPokemonTool(server);
  // Add new tool registrations here
}
```

## Zod Schemas for Input Validation

```typescript
import { z } from "zod";

// Basic schema with constraints
const schema = {
  name: z.string()
    .min(1, "Name is required")
    .max(100, "Name too long")
    .describe("Pokemon name or ID"),
  limit: z.number()
    .int()
    .min(1)
    .max(100)
    .default(20)
    .describe("Maximum results to return"),
  offset: z.number()
    .int()
    .min(0)
    .default(0)
    .describe("Pagination offset"),
};

// Enum schema
const responseFormat = z.enum(["json", "markdown"])
  .default("markdown")
  .describe("Output format");
```

## Pagination Implementation

```typescript
const listSchema = {
  limit: z.number().int().min(1).max(100).default(20)
    .describe("Maximum results to return"),
  offset: z.number().int().min(0).default(0)
    .describe("Number of results to skip"),
};

server.tool(
  "pokeapi_list_pokemon",
  "List Pokemon with pagination",
  listSchema,
  withErrorHandling(async ({ limit, offset }) => {
    const response = await fetch(
      `https://pokeapi.co/api/v2/pokemon?limit=${limit}&offset=${offset}`,
      { signal: AbortSignal.timeout(10_000) },
    );

    if (!response.ok) {
      return toolError(`API error: HTTP ${response.status}`);
    }

    const data = await response.json();
    const result = {
      total: data.count,
      count: data.results.length,
      offset,
      pokemon: data.results.map((p: any) => p.name),
      has_more: data.next !== null,
      next_offset: data.next ? offset + limit : undefined,
    };

    return {
      content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
    };
  }),
);
```

## HTTP Client Best Practices

Use the global `fetch` API (built into Bun):

```typescript
// Simple GET request
const response = await fetch("https://api.example.com/data", {
  signal: AbortSignal.timeout(10_000),  // 10s timeout
});

// POST with JSON body
const response = await fetch("https://api.example.com/data", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ key: "value" }),
  signal: AbortSignal.timeout(10_000),
});

// With auth headers
const response = await fetch(url, {
  headers: {
    "Authorization": `Bearer ${process.env.API_KEY}`,
    "Accept": "application/json",
  },
  signal: AbortSignal.timeout(10_000),
});
```

**Do NOT use `axios`** — Bun's built-in `fetch` is faster and sufficient. No extra dependency needed.

### SSRF Protection

Block internal/private addresses:

```typescript
const BLOCKED_HOSTS = ["127.0.0.1", "localhost", "0.0.0.0", "169.254.169.254", "[::1]"];

function isSafeUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return !BLOCKED_HOSTS.some((h) => parsed.hostname === h);
  } catch {
    return false;
  }
}
```

## Entry Point with Transport Selection

```typescript
// src/index.ts
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { createServer as createHttpServer } from "node:http";
import { registerTools } from "./tools/index.ts";
import { registerResources } from "./resources/index.ts";
import { registerPrompts } from "./prompts/index.ts";
import { log } from "./logging.ts";

const SERVER_NAME = "my-mcp-server";
const SERVER_VERSION = "1.0.0";

export function createServer(): McpServer {
  const server = new McpServer({
    name: SERVER_NAME,
    version: SERVER_VERSION,
  });

  registerTools(server);
  registerResources(server);
  registerPrompts(server);

  return server;
}

async function startStdio(server: McpServer) {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  log("info", "MCP server running on stdio");
}

async function startHttp(server: McpServer) {
  const port = Number(process.env.MCP_PORT) || 3000;

  const transport = new StreamableHTTPServerTransport({
    sessionIdGenerator: () => crypto.randomUUID(),
  });

  await server.connect(transport);

  // MUST use node:http — Bun.serve() is incompatible with StreamableHTTPServerTransport
  const httpServer = createHttpServer(async (req, res) => {
    const url = new URL(req.url ?? "/", `http://localhost:${port}`);

    if (url.pathname === "/mcp") {
      await transport.handleRequest(req, res);
      return;
    }

    if (url.pathname === "/health") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ status: "ok", server: SERVER_NAME, version: SERVER_VERSION }));
      return;
    }

    res.writeHead(404);
    res.end("Not Found");
  });

  httpServer.listen(port, () => {
    log("info", `MCP server running on http://localhost:${port}/mcp`);
  });
}

async function main() {
  const server = createServer();
  const transport = process.env.MCP_TRANSPORT ?? "stdio";

  switch (transport) {
    case "http":
      await startHttp(server);
      break;
    case "stdio":
    default:
      await startStdio(server);
      break;
  }
}

main().catch((err) => {
  log("error", "Failed to start server", err);
  process.exit(1);
});
```

## Testing with InMemoryTransport

Tests use `bun test` with the built-in `InMemoryTransport` — no real stdio/HTTP needed:

```typescript
// tests/tools.test.ts
import { test, expect, describe } from "bun:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { createServer } from "../src/index.ts";

async function createTestClient() {
  const server = createServer();
  const client = new Client({ name: "test-client", version: "1.0.0" });
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  await Promise.all([client.connect(clientTransport), server.connect(serverTransport)]);
  return client;
}

describe("tools", () => {
  test("lists available tools", async () => {
    const client = await createTestClient();
    const { tools } = await client.listTools();
    const names = tools.map((t) => t.name);
    expect(names).toContain("pokeapi_get_pokemon");
  });

  test("get pokemon returns data", async () => {
    const client = await createTestClient();
    const result = await client.callTool({
      name: "pokeapi_get_pokemon",
      arguments: { name: "pikachu" },
    });
    const text = (result.content as Array<{ text: string }>)[0]?.text;
    expect(text).toContain("pikachu");
  });
});
```

Run tests:

```bash
bun test
```

## Package Configuration

### package.json

```json
{
  "name": "{service}-mcp-server",
  "version": "1.0.0",
  "module": "src/index.ts",
  "type": "module",
  "private": true,
  "scripts": {
    "start": "bun run src/index.ts",
    "dev": "bun --hot run src/index.ts",
    "start:http": "MCP_TRANSPORT=http bun run src/index.ts",
    "dev:http": "MCP_TRANSPORT=http bun --hot run src/index.ts",
    "test": "bun test",
    "inspect": "bunx @anthropic-ai/mcp-inspector bun run src/index.ts"
  },
  "dependencies": {
    "@modelcontextprotocol/sdk": "^1.12.1",
    "zod": "^3.25.67"
  },
  "devDependencies": {
    "@types/bun": "latest"
  },
  "peerDependencies": {
    "typescript": "^5"
  }
}
```

### tsconfig.json

```json
{
  "compilerOptions": {
    "lib": ["ESNext"],
    "target": "ESNext",
    "module": "Preserve",
    "moduleDetection": "force",
    "jsx": "react-jsx",
    "allowJs": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "verbatimModuleSyntax": true,
    "noEmit": true,
    "strict": true,
    "skipLibCheck": true,
    "noFallthroughCasesInSwitch": true,
    "noUncheckedIndexedAccess": true,
    "noImplicitOverride": true,
    "noUnusedLocals": false,
    "noUnusedParameters": false,
    "noPropertyAccessFromIndexSignature": false
  }
}
```

### .gitignore

```
node_modules
out
dist
*.tgz
coverage
*.lcov
logs
*.log
.env
.env.*.local
.eslintcache
.cache
*.tsbuildinfo
.idea
.DS_Store
```

## Dockerfile (Bun)

```dockerfile
FROM oven/bun:1-alpine AS base
WORKDIR /app

# Install dependencies
FROM base AS deps
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

# Final image
FROM base
COPY --from=deps /app/node_modules ./node_modules
COPY package.json bun.lock ./
COPY src ./src

ENV NODE_ENV=production
ENV MCP_TRANSPORT=http
ENV MCP_PORT=3000
EXPOSE 3000

CMD ["bun", "run", "src/index.ts"]
```

**Key differences from Node.js Dockerfile:**
- Base image: `oven/bun:1-alpine` (NOT `node:22-alpine`)
- No build step needed — Bun runs TypeScript directly
- Entry point: `bun run src/index.ts` (NOT `node dist/index.js`)
- No `dist/` directory needed

## Resource Registration

```typescript
// src/resources/server-info.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

export function registerServerInfoResource(server: McpServer) {
  server.resource(
    "server-info",
    "info://server",
    { description: "Server runtime information", mimeType: "application/json" },
    async () => ({
      contents: [{
        uri: "info://server",
        mimeType: "application/json",
        text: JSON.stringify({
          runtime: "bun",
          bunVersion: Bun.version,
          platform: process.platform,
          uptime: process.uptime(),
        }, null, 2),
      }],
    }),
  );
}
```

## Prompt Registration

```typescript
// src/prompts/code-review.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

export function registerCodeReviewPrompt(server: McpServer) {
  server.prompt(
    "code_review",
    "Generate a code review prompt",
    {
      code: z.string().describe("The code to review"),
      language: z.string().default("typescript").describe("Programming language"),
    },
    ({ code, language }) => ({
      messages: [{
        role: "user" as const,
        content: {
          type: "text" as const,
          text: `Please review the following ${language} code:\n\n\`\`\`${language}\n${code}\n\`\`\``,
        },
      }],
    }),
  );
}
```

## Transport Options

### stdio (Default — for local integrations)

```typescript
const transport = new StdioServerTransport();
await server.connect(transport);
```

Use when: Claude Desktop, Cursor, VS Code, single-user local setups.

### Streamable HTTP (for remote/networked deployments)

```typescript
const transport = new StreamableHTTPServerTransport({
  sessionIdGenerator: () => crypto.randomUUID(),
});
await server.connect(transport);

// MUST use node:http, NOT Bun.serve()
const httpServer = createHttpServer(async (req, res) => {
  if (url.pathname === "/mcp") await transport.handleRequest(req, res);
});
httpServer.listen(3000);
```

Use when: Kubernetes, remote servers, multiple concurrent clients.

| Criterion | stdio | Streamable HTTP |
|-----------|-------|-----------------|
| Deployment | Local | Remote/K8s |
| Clients | Single | Multiple |
| Complexity | Low | Medium |
| Command | `bun run src/index.ts` | `MCP_TRANSPORT=http bun run src/index.ts` |

## Client Configuration

### Claude Desktop (`claude_desktop_config.json`)

```json
{
  "mcpServers": {
    "my-mcp": {
      "command": "bun",
      "args": ["run", "/absolute/path/to/src/index.ts"]
    }
  }
}
```

### Cursor (`.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "my-mcp": {
      "command": "bun",
      "args": ["run", "/absolute/path/to/src/index.ts"]
    }
  }
}
```

## Code Best Practices

### Composability and Reusability

1. **Extract Common Functionality**: Shared API clients, error handling, response formatting
2. **Avoid Duplication**: NEVER copy-paste between tools — extract to shared functions
3. **Centralize patterns**: Pagination, filtering, auth should be shared utilities

### TypeScript Quality

- Enable strict mode in tsconfig.json
- Use proper types — avoid `any` where possible
- Use `unknown` instead of `any` for external data
- All async functions should handle errors via `withErrorHandling()`
- Use optional chaining (`?.`) and nullish coalescing (`??`)

### Security

- API keys via environment variables (Bun loads .env automatically)
- Input validation via Zod schemas with constraints
- SSRF protection for URL-fetching tools
- Never expose internal errors to clients
- Use `AbortSignal.timeout()` for all fetch calls

## Quality Checklist

Before finalizing your Bun MCP server:

### Implementation Quality
- [ ] All tools registered with `server.tool()` with name, description, schema, handler
- [ ] All tools wrapped with `withErrorHandling()`
- [ ] All Zod schemas have proper constraints and `.describe()` on every field
- [ ] Tool names use `{service}_{action}_{resource}` pattern
- [ ] Tool annotations set (readOnlyHint, destructiveHint, idempotentHint, openWorldHint)
- [ ] Error messages are actionable with suggestions

### Bun-Specific
- [ ] No `console.log()` — only `console.error()` or `log()` helper
- [ ] Uses `fetch` (built-in) — NOT axios
- [ ] Uses `node:http` for HTTP transport — NOT `Bun.serve()`
- [ ] `bun test` passes
- [ ] `bun run src/index.ts` starts without errors
- [ ] Dockerfile uses `oven/bun:1-alpine` base

### Project Structure
- [ ] package.json with correct scripts (bun-based)
- [ ] tsconfig.json with strict mode
- [ ] .env.example documents all env vars
- [ ] /health endpoint for HTTP transport
- [ ] README.md with setup instructions

### Code Quality
- [ ] No code duplication (DRY)
- [ ] Pagination with limit/offset/has_more
- [ ] AbortSignal.timeout() on all fetch calls
- [ ] SSRF protection for URL-fetching tools
- [ ] Common functionality in shared utilities
