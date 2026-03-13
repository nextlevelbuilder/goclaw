# Bun/TypeScript MCP Server Implementation Guide

## Overview

This guide covers building MCP servers using **Bun + TypeScript** with the official `@modelcontextprotocol/sdk`. Based on the base template at `templates/mcp-server/`.

**Key advantages of Bun:**
- Native TypeScript execution — no build step, no `tsc`, no `dist/`
- Built-in test runner (`bun:test`)
- Built-in `.env` loading (no dotenv)
- Fast startup, low memory footprint
- Native `fetch()` API (no axios needed)

---

## Quick Reference

### Key Imports

```typescript
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
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
import { withErrorHandling } from "../errors.ts";

server.tool(
  "tool_name",
  "What this tool does (shown to LLM)",
  { param: z.string().describe("Parameter description for LLM") },
  withErrorHandling(async ({ param }) => ({
    content: [{ type: "text", text: `Result: ${param}` }],
  })),
);
```

### Commands

```bash
bun run start          # stdio transport (default)
bun run dev            # stdio with hot reload
bun run start:http     # HTTP transport on MCP_PORT
bun run dev:http       # HTTP with hot reload
bun test               # Run tests
bun run inspect        # MCP Inspector
```

---

## Project Structure

Scaffold from `templates/mcp-server/`:

```
{service}-mcp-server/
├── src/
│   ├── index.ts              # Entry point: McpServer + transport selection
│   ├── logging.ts            # Stderr-only logging (NEVER use console.log)
│   ├── errors.ts             # toolError() + withErrorHandling() wrapper
│   ├── tools/
│   │   ├── index.ts          # registerTools() — centralized hub
│   │   ├── greet.ts          # Example: simple tool
│   │   └── fetch-url.ts      # Example: tool with SSRF protection
│   ├── resources/
│   │   ├── index.ts          # registerResources() — centralized hub
│   │   └── server-info.ts    # Example: runtime info resource
│   └── prompts/
│       ├── index.ts          # registerPrompts() — centralized hub
│       └── code-review.ts    # Example: code review prompt template
├── tests/
│   └── tools.test.ts         # Tests using InMemoryTransport
├── package.json
├── tsconfig.json
├── Dockerfile
├── .env.example
├── .gitignore
└── README.md
```

## Package Configuration

### package.json

```json
{
  "name": "{service}-mcp-server",
  "module": "src/index.ts",
  "type": "module",
  "private": true,
  "scripts": {
    "start": "bun run src/index.ts",
    "dev": "bun --hot run src/index.ts",
    "start:http": "MCP_TRANSPORT=http bun run src/index.ts",
    "dev:http": "MCP_TRANSPORT=http bun --hot run src/index.ts",
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

**Important differences from Node.js:**
- `"module": "src/index.ts"` — entry point is TypeScript (no dist/)
- No build script — Bun runs TypeScript directly
- No axios — use native `fetch()`
- No express — use `Bun.serve()` for HTTP or SDK's built-in HTTP handler
- No dotenv — Bun loads `.env` automatically

### tsconfig.json

```json
{
  "compilerOptions": {
    "lib": ["ESNext"],
    "target": "ESNext",
    "module": "Preserve",
    "moduleDetection": "force",
    "jsx": "react-jsx",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "skipLibCheck": true,
    "noFallthroughCasesInSwitch": true,
    "noUnusedLocals": false,
    "noUnusedParameters": false,
    "noPropertyAccessFromIndexSignature": false,
    "verbatimModuleSyntax": true,
    "forceConsistentCasingInFileNames": true,
    "types": ["bun"]
  }
}
```

---

## Entry Point & Transport Selection

The entry point creates the server and selects transport based on `MCP_TRANSPORT` env var:

```typescript
// src/index.ts
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { log } from "./logging.ts";
import { registerTools } from "./tools/index.ts";
import { registerResources } from "./resources/index.ts";
import { registerPrompts } from "./prompts/index.ts";

const SERVER_NAME = "my-mcp-server";
const SERVER_VERSION = "1.0.0";

export function createServer(): McpServer {
  const server = new McpServer({ name: SERVER_NAME, version: SERVER_VERSION });
  registerTools(server);
  registerResources(server);
  registerPrompts(server);
  return server;
}

async function startStdio() {
  const server = createServer();
  const transport = new StdioServerTransport();
  await server.connect(transport);
  log("info", `${SERVER_NAME} running via stdio`);
}

async function startHttp() {
  const server = createServer();
  const port = parseInt(process.env.MCP_PORT || "3000", 10);

  const httpTransport = new StreamableHTTPServerTransport({
    sessionIdGenerator: () => crypto.randomUUID(),
  });
  await server.connect(httpTransport);

  Bun.serve({
    port,
    routes: {
      "/mcp": {
        POST: async (req) => httpTransport.handleRequest(req),
      },
      "/health": {
        GET: () =>
          Response.json({ status: "ok", server: SERVER_NAME, version: SERVER_VERSION }),
      },
    },
  });

  log("info", `${SERVER_NAME} running on http://localhost:${port}/mcp`);
}

async function main() {
  const transport = process.env.MCP_TRANSPORT || "stdio";
  if (transport === "http") {
    await startHttp();
  } else {
    await startStdio();
  }
}

main().catch((error) => {
  log("error", "Fatal error:", error);
  process.exit(1);
});
```

**Transport selection:**
| Transport | Env Var | Use Case |
|-----------|---------|----------|
| **stdio** (default) | `MCP_TRANSPORT=stdio` | Claude Desktop, Cursor, VS Code |
| **Streamable HTTP** | `MCP_TRANSPORT=http` | Remote deployments, multi-client |

---

## Logging — CRITICAL RULE

**NEVER use `console.log()` in MCP servers.** It writes to stdout and corrupts the JSON-RPC protocol in stdio mode. Always use `console.error()` or the `log()` helper:

```typescript
// src/logging.ts
type LogLevel = "debug" | "info" | "warn" | "error";

const LEVELS: Record<LogLevel, number> = { debug: 0, info: 1, warn: 2, error: 3 };

const currentLevel: LogLevel = (process.env.LOG_LEVEL as LogLevel) || "info";

export function log(level: LogLevel, message: string, ...args: unknown[]): void {
  if (LEVELS[level] >= LEVELS[currentLevel]) {
    const timestamp = new Date().toISOString();
    console.error(`[${timestamp}] [${level.toUpperCase()}] ${message}`, ...args);
  }
}
```

---

## Error Handling

Tool handlers **must never throw**. Use `withErrorHandling()` wrapper or return `toolError()`:

```typescript
// src/errors.ts
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

export function toolError(message: string): CallToolResult {
  return {
    content: [{ type: "text", text: `Error: ${message}` }],
    isError: true,
  };
}

export function withErrorHandling(
  handler: (...args: any[]) => Promise<CallToolResult>,
): (...args: any[]) => Promise<CallToolResult> {
  return async (...args) => {
    try {
      return await handler(...args);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return toolError(message);
    }
  };
}
```

**Usage in tools:**
```typescript
// Option 1: Wrap entire handler (recommended for most tools)
server.tool("my_tool", "Description", schema,
  withErrorHandling(async ({ param }) => ({
    content: [{ type: "text", text: result }],
  })),
);

// Option 2: Return toolError for specific cases
server.tool("my_tool", "Description", schema,
  withErrorHandling(async ({ url }) => {
    if (!isSafeUrl(url)) {
      return toolError("URL is blocked for security reasons");
    }
    const data = await fetch(url);
    return { content: [{ type: "text", text: await data.text() }] };
  }),
);
```

---

## Tool Implementation

### Tool Registration

Each tool lives in its own file under `src/tools/` and exports a register function:

```typescript
// src/tools/greet.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { withErrorHandling } from "../errors.ts";

export function registerGreetTool(server: McpServer) {
  server.tool(
    "greet",
    "Greet a person by name",
    {
      name: z.string().min(1).max(100).describe("Name of the person to greet"),
    },
    withErrorHandling(async ({ name }) => ({
      content: [
        {
          type: "text",
          text: `Hello, ${name}! Welcome to the MCP server.`,
        },
      ],
    })),
  );
}
```

### Tool Registration Hub

```typescript
// src/tools/index.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerGreetTool } from "./greet.ts";
import { registerFetchUrlTool } from "./fetch-url.ts";

export function registerTools(server: McpServer) {
  registerGreetTool(server);
  registerFetchUrlTool(server);
  // Add new tools here
}
```

### Tool Naming Convention

Use snake_case with service prefix to avoid conflicts:
- `github_create_issue` (not `create_issue`)
- `slack_send_message` (not `send_message`)
- `stripe_create_payment` (not `create_payment`)

### Zod Schemas for Input Validation

Always use `.describe()` on every field — this is what the LLM sees:

```typescript
const SearchSchema = {
  query: z.string()
    .min(2, "Query must be at least 2 characters")
    .max(200)
    .describe("Search string to match against names or emails"),
  limit: z.number()
    .int().min(1).max(100).default(20)
    .describe("Maximum results to return (1-100)"),
  offset: z.number()
    .int().min(0).default(0)
    .describe("Number of results to skip for pagination"),
};
```

### SSRF Protection

When tools access URLs, block internal/private addresses:

```typescript
// src/tools/fetch-url.ts
const BLOCKED_HOSTS = ["127.0.0.1", "localhost", "0.0.0.0", "169.254.169.254", "[::1]"];
const MAX_RESPONSE_SIZE = 1_000_000; // 1MB

function isSafeUrl(urlString: string): boolean {
  try {
    const url = new URL(urlString);
    return !BLOCKED_HOSTS.includes(url.hostname);
  } catch {
    return false;
  }
}

export function registerFetchUrlTool(server: McpServer) {
  server.tool(
    "fetch_url",
    "Fetch content from a URL. Blocks internal addresses for security.",
    {
      url: z.string().url().describe("URL to fetch"),
      method: z.enum(["GET", "POST"]).default("GET").describe("HTTP method"),
    },
    withErrorHandling(async ({ url, method }) => {
      if (!isSafeUrl(url)) {
        return toolError("Access to internal addresses is blocked");
      }

      const response = await fetch(url, {
        method,
        signal: AbortSignal.timeout(10_000), // 10s timeout
      });

      if (!response.ok) {
        return toolError(`HTTP ${response.status}: ${response.statusText}`);
      }

      let text = await response.text();
      if (text.length > MAX_RESPONSE_SIZE) {
        text = text.slice(0, MAX_RESPONSE_SIZE) + "\n... (truncated)";
      }

      return { content: [{ type: "text", text }] };
    }),
  );
}
```

### HTTP Requests — Use Native fetch()

Bun has native `fetch()`. Do NOT use axios:

```typescript
// Good: native fetch
const response = await fetch(`${API_BASE_URL}/users`, {
  method: "GET",
  headers: {
    "Authorization": `Bearer ${process.env.API_KEY}`,
    "Content-Type": "application/json",
  },
  signal: AbortSignal.timeout(30_000),
});

if (!response.ok) {
  return toolError(`API error: ${response.status} ${response.statusText}`);
}

const data = await response.json();
```

### Pagination

```typescript
server.tool(
  "service_list_items",
  "List items with pagination",
  {
    limit: z.number().int().min(1).max(100).default(20).describe("Max results"),
    offset: z.number().int().min(0).default(0).describe("Skip N results"),
  },
  withErrorHandling(async ({ limit, offset }) => {
    const data = await fetchItems(limit, offset);
    const result = {
      total: data.total,
      count: data.items.length,
      offset,
      items: data.items,
      has_more: data.total > offset + data.items.length,
      next_offset: data.total > offset + data.items.length
        ? offset + data.items.length
        : undefined,
    };
    return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
  }),
);
```

### Tool Annotations

```typescript
server.tool(
  "service_delete_item",
  "Delete an item by ID",
  { id: z.string().describe("Item ID to delete") },
  withErrorHandling(async ({ id }) => {
    await deleteItem(id);
    return { content: [{ type: "text", text: `Deleted item ${id}` }] };
  }),
  // Tool annotations help clients understand behavior
  {
    annotations: {
      readOnlyHint: false,
      destructiveHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
);
```

---

## Resource Implementation

Resources expose read-only data to MCP clients:

```typescript
// src/resources/server-info.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

export function registerServerInfoResource(server: McpServer) {
  server.resource(
    "server-info",
    "info://server",
    { description: "Server runtime information", mimeType: "application/json" },
    async () => ({
      contents: [
        {
          uri: "info://server",
          mimeType: "application/json",
          text: JSON.stringify({
            runtime: "bun",
            bunVersion: Bun.version,
            platform: process.platform,
            arch: process.arch,
            uptime: process.uptime(),
            memoryUsage: process.memoryUsage(),
          }, null, 2),
        },
      ],
    }),
  );
}
```

**When to use Resources vs Tools:**
- **Resources**: Static/semi-static data, URI-based access, no side effects
- **Tools**: Complex operations, validation, business logic, side effects

---

## Prompt Implementation

Prompts are reusable prompt templates with parameters:

```typescript
// src/prompts/code-review.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

export function registerCodeReviewPrompt(server: McpServer) {
  server.prompt(
    "code_review",
    "Generate a code review prompt for the given code",
    {
      code: z.string().describe("The code to review"),
      language: z.string().default("typescript").describe("Programming language"),
    },
    ({ code, language }) => ({
      messages: [
        {
          role: "user" as const,
          content: {
            type: "text" as const,
            text: [
              `Please review the following ${language} code:`,
              "",
              "```" + language,
              code,
              "```",
              "",
              "Analyze for: correctness, performance, security, readability, and best practices.",
            ].join("\n"),
          },
        },
      ],
    }),
  );
}
```

---

## Testing

Use `bun:test` with `InMemoryTransport` — no real stdio/HTTP needed:

```typescript
// tests/tools.test.ts
import { describe, test, expect, beforeAll } from "bun:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { createServer } from "../src/index.ts";

async function createTestClient(): Promise<Client> {
  const server = createServer();
  const client = new Client({ name: "test-client", version: "1.0.0" });
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  await Promise.all([
    server.connect(serverTransport),
    client.connect(clientTransport),
  ]);
  return client;
}

describe("Tools", () => {
  let client: Client;

  beforeAll(async () => {
    client = await createTestClient();
  });

  test("greet returns greeting message", async () => {
    const result = await client.callTool({
      name: "greet",
      arguments: { name: "World" },
    });
    expect(result.content).toEqual([
      { type: "text", text: expect.stringContaining("World") },
    ]);
  });

  test("fetch_url blocks internal addresses", async () => {
    const result = await client.callTool({
      name: "fetch_url",
      arguments: { url: "http://127.0.0.1/secret" },
    });
    expect(result.isError).toBe(true);
  });

  test("lists available tools", async () => {
    const tools = await client.listTools();
    expect(tools.tools.length).toBeGreaterThan(0);
    const names = tools.tools.map((t) => t.name);
    expect(names).toContain("greet");
  });
});

describe("Resources", () => {
  let client: Client;

  beforeAll(async () => {
    client = await createTestClient();
  });

  test("reads server-info resource", async () => {
    const result = await client.readResource({ uri: "info://server" });
    const text = result.contents[0]?.text as string;
    const info = JSON.parse(text);
    expect(info.runtime).toBe("bun");
  });
});

describe("Prompts", () => {
  let client: Client;

  beforeAll(async () => {
    client = await createTestClient();
  });

  test("gets code_review prompt", async () => {
    const result = await client.getPrompt({
      name: "code_review",
      arguments: { code: "const x = 1;", language: "typescript" },
    });
    expect(result.messages.length).toBeGreaterThan(0);
    expect(result.messages[0].role).toBe("user");
  });
});
```

Run tests:
```bash
bun test
```

---

## Docker Containerization

Multi-stage build with `oven/bun:1-alpine`:

```dockerfile
# Stage 1: Install dependencies
FROM oven/bun:1-alpine AS deps
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

# Stage 2: Final image
FROM oven/bun:1-alpine
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY package.json bun.lock ./
COPY src/ ./src/
ENV NODE_ENV=production
ENV MCP_TRANSPORT=http
EXPOSE 3000
CMD ["bun", "run", "src/index.ts"]
```

**.dockerignore:**
```
node_modules
tests
*.md
.env*
.git
```

Build and run:
```bash
docker build -t {service}-mcp-server .
docker run -p 3000:3000 -e MCP_TRANSPORT=http -e API_KEY=xxx {service}-mcp-server
```

---

## Client Configuration

### Claude Desktop (`claude_desktop_config.json`)

```json
{
  "mcpServers": {
    "my-service": {
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
    "my-service": {
      "command": "bun",
      "args": ["run", "/absolute/path/to/src/index.ts"]
    }
  }
}
```

### Remote HTTP

```json
{
  "mcpServers": {
    "my-service": {
      "url": "http://localhost:3000/mcp"
    }
  }
}
```

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_TRANSPORT` | `stdio` | Transport type: `stdio` or `http` |
| `MCP_PORT` | `3000` | HTTP server port (HTTP mode only) |
| `LOG_LEVEL` | `info` | Logging level: `debug`, `info`, `warn`, `error` |

Service-specific variables (e.g., `API_KEY`, `API_BASE_URL`) are loaded from `.env` automatically by Bun.

---

## Bun-Specific APIs

Prefer Bun-native APIs over Node.js equivalents:

| Instead of | Use |
|------------|-----|
| `axios` / `node-fetch` | `fetch()` (built-in) |
| `express` | `Bun.serve()` |
| `dotenv` | Automatic `.env` loading |
| `jest` / `vitest` | `bun:test` |
| `fs.readFile()` | `Bun.file(path).text()` |
| `fs.writeFile()` | `Bun.write(path, data)` |
| `crypto.randomUUID()` | `crypto.randomUUID()` (built-in) |
| `ws` | `WebSocket` (built-in) |
| `npx` | `bunx` |

---

## Complete Tool Example with API Integration

```typescript
// src/tools/users.ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { withErrorHandling, toolError } from "../errors.ts";
import { log } from "../logging.ts";

const API_BASE_URL = process.env.API_BASE_URL || "https://api.example.com/v1";
const API_KEY = process.env.API_KEY;

async function apiRequest<T>(endpoint: string, params?: Record<string, string>): Promise<T> {
  const url = new URL(`${API_BASE_URL}/${endpoint}`);
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      url.searchParams.set(key, value);
    }
  }

  const response = await fetch(url.toString(), {
    headers: {
      "Authorization": `Bearer ${API_KEY}`,
      "Accept": "application/json",
    },
    signal: AbortSignal.timeout(30_000),
  });

  if (!response.ok) {
    throw new Error(`API error: ${response.status} ${response.statusText}`);
  }

  return response.json() as Promise<T>;
}

export function registerUserTools(server: McpServer) {
  server.tool(
    "service_search_users",
    "Search for users by name or email",
    {
      query: z.string().min(2).max(200).describe("Search query"),
      limit: z.number().int().min(1).max(100).default(20).describe("Max results"),
      offset: z.number().int().min(0).default(0).describe("Pagination offset"),
    },
    withErrorHandling(async ({ query, limit, offset }) => {
      if (!API_KEY) {
        return toolError("API_KEY environment variable is required");
      }

      const data = await apiRequest<{ users: any[]; total: number }>("users/search", {
        q: query,
        limit: String(limit),
        offset: String(offset),
      });

      if (data.users.length === 0) {
        return { content: [{ type: "text", text: `No users found matching '${query}'` }] };
      }

      const result = {
        total: data.total,
        count: data.users.length,
        offset,
        users: data.users,
        has_more: data.total > offset + data.users.length,
      };

      log("debug", `Found ${data.total} users for query "${query}"`);

      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    }),
  );
}
```

---

## Quality Checklist

Before finalizing your Bun/TypeScript MCP server:

### Architecture
- [ ] Project scaffolded from `templates/mcp-server/` base template
- [ ] Tools organized in `src/tools/` with one file per tool/domain
- [ ] Resources in `src/resources/`, prompts in `src/prompts/`
- [ ] Centralized registration hubs (`index.ts` in each directory)
- [ ] Shared utilities extracted (API client, formatters, etc.)

### Tool Quality
- [ ] All tools use `server.tool()` API with `withErrorHandling()` wrapper
- [ ] All Zod schemas have `.describe()` on every field
- [ ] Tool names use snake_case with service prefix
- [ ] Tool descriptions are clear and comprehensive
- [ ] SSRF protection on tools that access URLs
- [ ] Pagination support for list operations
- [ ] Annotations set correctly (readOnlyHint, destructiveHint, etc.)

### Safety & Correctness
- [ ] NO `console.log()` usage — only `console.error()` or `log()`
- [ ] Tool handlers never throw — wrapped with `withErrorHandling()`
- [ ] HTTP requests use native `fetch()` with `AbortSignal.timeout()`
- [ ] Large responses truncated with clear messages
- [ ] Sensitive data (API keys) read from environment variables

### Testing
- [ ] Tests use `bun:test` + `InMemoryTransport`
- [ ] All tools have at least one happy-path test
- [ ] Security-sensitive tools tested for edge cases (SSRF, etc.)
- [ ] `bun test` passes cleanly

### Configuration
- [ ] `package.json` uses Bun conventions (no build step, `"module": "src/index.ts"`)
- [ ] `tsconfig.json` has strict mode enabled
- [ ] `.env.example` documents all required env vars
- [ ] Dockerfile uses `oven/bun:1-alpine` multi-stage build
- [ ] Transport selection via `MCP_TRANSPORT` env var

### Code Quality
- [ ] No duplicated code (DRY)
- [ ] No `any` types — use `unknown` or proper types
- [ ] Consistent error handling across all tools
- [ ] Native Bun APIs used (fetch, Bun.file, etc.)
- [ ] TypeScript strict mode passes: `bunx tsc --noEmit`
