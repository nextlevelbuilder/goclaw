This file is a merged representation of a subset of the codebase, containing files not matching ignore patterns, combined into a single document by Repomix.

<file_summary>
This section contains a summary of this file.

<purpose>
This file contains a packed representation of a subset of the repository's contents that is considered the most important context.
It is designed to be easily consumable by AI systems for analysis, code review,
or other automated processes.
</purpose>

<file_format>
The content is organized as follows:
1. This summary section
2. Repository information
3. Directory structure
4. Repository files (if enabled)
5. Multiple file entries, each consisting of:
  - File path as an attribute
  - Full contents of the file
</file_format>

<usage_guidelines>
- This file should be treated as read-only. Any changes should be made to the
  original repository files, not this packed version.
- When processing this file, use the file path to distinguish
  between different files in the repository.
- Be aware that this file may contain sensitive information. Handle it with
  the same level of security as you would the original repository.
</usage_guidelines>

<notes>
- Some files may have been excluded based on .gitignore rules and Repomix's configuration
- Binary files are not included in this packed representation. Please refer to the Repository Structure section for a complete list of file paths, including binary files
- Files matching these patterns are excluded: node_modules, bun.lockb, bun.lock, .git
- Files matching patterns in .gitignore are excluded
- Files matching default ignore patterns are excluded
- Files are sorted by Git change count (files with more changes are at the bottom)
</notes>

</file_summary>

<directory_structure>
src/
  prompts/
    code-review.ts
    index.ts
  resources/
    index.ts
    server-info.ts
  tools/
    fetch-url.ts
    greet.ts
    index.ts
  errors.ts
  index.ts
  logging.ts
tests/
  tools.test.ts
.dockerignore
.env.example
.gitignore
CLAUDE.md
Dockerfile
package.json
README.md
tsconfig.json
</directory_structure>

<files>
This section contains the contents of the repository's files.

<file path="src/prompts/code-review.ts">
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

const schema = {
  code: z.string().describe("The code to review"),
  language: z.string().default("typescript").describe("Programming language"),
};

export function registerCodeReviewPrompt(server: McpServer) {
  server.prompt(
    "code_review",
    "Generate a code review prompt for the given code",
    schema,
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
              "Focus on:",
              "1. Correctness and potential bugs",
              "2. Performance issues",
              "3. Security vulnerabilities",
              "4. Code style and readability",
              "5. Suggested improvements",
            ].join("\n"),
          },
        },
      ],
    }),
  );
}
</file>

<file path="src/prompts/index.ts">
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerCodeReviewPrompt } from "./code-review.ts";

/**
 * Register all prompts with the MCP server.
 * Add new prompt registrations here.
 */
export function registerPrompts(server: McpServer) {
  registerCodeReviewPrompt(server);
}
</file>

<file path="src/resources/index.ts">
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerServerInfoResource } from "./server-info.ts";

/**
 * Register all resources with the MCP server.
 * Add new resource registrations here.
 */
export function registerResources(server: McpServer) {
  registerServerInfoResource(server);
}
</file>

<file path="src/resources/server-info.ts">
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

export function registerServerInfoResource(server: McpServer) {
  server.resource(
    "server-info",
    "info://server",
    {
      description: "Server runtime information",
      mimeType: "application/json",
    },
    async () => ({
      contents: [
        {
          uri: "info://server",
          mimeType: "application/json",
          text: JSON.stringify(
            {
              runtime: "bun",
              bunVersion: Bun.version,
              platform: process.platform,
              arch: process.arch,
              uptime: process.uptime(),
              memoryUsage: process.memoryUsage(),
            },
            null,
            2,
          ),
        },
      ],
    }),
  );
}
</file>

<file path="src/tools/fetch-url.ts">
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { toolError, withErrorHandling } from "../errors.ts";

const BLOCKED_HOSTS = ["127.0.0.1", "localhost", "0.0.0.0", "169.254.169.254", "[::1]"];
const MAX_RESPONSE_SIZE = 1024 * 1024; // 1MB

const schema = {
  url: z.string().url().describe("URL to fetch"),
  method: z.enum(["GET", "POST"]).default("GET").describe("HTTP method"),
};

function isSafeUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return !BLOCKED_HOSTS.some((h) => parsed.hostname === h);
  } catch {
    return false;
  }
}

export function registerFetchUrlTool(server: McpServer) {
  server.tool(
    "fetch_url",
    "Fetch content from a URL (GET or POST). Returns the response body as text.",
    schema,
    withErrorHandling(async ({ url, method }) => {
      if (!isSafeUrl(url)) {
        return toolError("Blocked: internal/private addresses are not allowed");
      }

      const response = await fetch(url, {
        method,
        signal: AbortSignal.timeout(10_000),
      });

      if (!response.ok) {
        return toolError(`HTTP ${response.status}: ${response.statusText}`);
      }

      const contentLength = Number(response.headers.get("content-length") ?? 0);
      if (contentLength > MAX_RESPONSE_SIZE) {
        return toolError(`Response too large: ${contentLength} bytes (max ${MAX_RESPONSE_SIZE})`);
      }

      const text = await response.text();
      if (text.length > MAX_RESPONSE_SIZE) {
        return toolError(`Response too large: ${text.length} chars (max ${MAX_RESPONSE_SIZE})`);
      }

      return {
        content: [{ type: "text", text }],
      };
    }),
  );
}
</file>

<file path="src/tools/greet.ts">
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { withErrorHandling } from "../errors.ts";

const schema = {
  name: z.string().min(1).max(100).describe("Name of the person to greet"),
};

export function registerGreetTool(server: McpServer) {
  server.tool(
    "greet",
    "Greet a person by name",
    schema,
    withErrorHandling(async ({ name }) => ({
      content: [{ type: "text", text: `Hello, ${name}! Welcome to the MCP server.` }],
    })),
  );
}
</file>

<file path="src/tools/index.ts">
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerGreetTool } from "./greet.ts";
import { registerFetchUrlTool } from "./fetch-url.ts";

/**
 * Register all tools with the MCP server.
 * Add new tool registrations here.
 */
export function registerTools(server: McpServer) {
  registerGreetTool(server);
  registerFetchUrlTool(server);
}
</file>

<file path="src/errors.ts">
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";

/**
 * Create a standardized error result for tool handlers.
 * Tool handlers should never throw — always return an error result instead.
 */
export function toolError(message: string): CallToolResult {
  return {
    content: [{ type: "text", text: `Error: ${message}` }],
    isError: true,
  };
}

/**
 * Wrap a tool handler with automatic error catching.
 * Converts thrown exceptions into proper MCP error results.
 */
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
</file>

<file path="src/index.ts">
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { createServer as createHttpServer } from "node:http";
import { registerTools } from "./tools/index.ts";
import { registerResources } from "./resources/index.ts";
import { registerPrompts } from "./prompts/index.ts";
import { log } from "./logging.ts";

const SERVER_NAME = "bun-mcp-server";
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

  // StreamableHTTPServerTransport expects Node.js IncomingMessage/ServerResponse,
  // not Bun's Web API Request/Response. Use node:http (Bun-compatible) for HTTP transport.
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
</file>

<file path="src/logging.ts">
/**
 * Logging utility for MCP servers.
 *
 * CRITICAL: Never use console.log() in MCP stdio servers — it writes to stdout
 * and corrupts the JSON-RPC protocol. Always use console.error() (stderr).
 */

type LogLevel = "debug" | "info" | "warn" | "error";

const LOG_LEVELS: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
};

const currentLevel = (process.env.LOG_LEVEL as LogLevel) ?? "info";

export function log(level: LogLevel, message: string, ...args: unknown[]) {
  if (LOG_LEVELS[level] < LOG_LEVELS[currentLevel]) return;

  const timestamp = new Date().toISOString();
  const prefix = `[${timestamp}] [${level.toUpperCase()}]`;
  console.error(prefix, message, ...args);
}
</file>

<file path="tests/tools.test.ts">
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
  test("greet returns greeting message", async () => {
    const client = await createTestClient();
    const result = await client.callTool({ name: "greet", arguments: { name: "World" } });

    expect(result.content).toEqual([
      { type: "text", text: "Hello, World! Welcome to the MCP server." },
    ]);
  });

  test("fetch_url blocks internal addresses", async () => {
    const client = await createTestClient();
    const result = await client.callTool({
      name: "fetch_url",
      arguments: { url: "http://127.0.0.1/secret" },
    });

    expect(result.isError).toBe(true);
    expect((result.content as Array<{ text: string }>)[0]?.text).toContain("Blocked");
  });

  test("lists available tools", async () => {
    const client = await createTestClient();
    const { tools } = await client.listTools();

    const names = tools.map((t) => t.name);
    expect(names).toContain("greet");
    expect(names).toContain("fetch_url");
  });
});

describe("resources", () => {
  test("lists available resources", async () => {
    const client = await createTestClient();
    const { resources } = await client.listResources();

    expect(resources.length).toBeGreaterThan(0);
    expect(resources[0]?.uri).toBe("info://server");
  });

  test("reads server-info resource", async () => {
    const client = await createTestClient();
    const result = await client.readResource({ uri: "info://server" });

    const content = result.contents[0];
    expect(content?.mimeType).toBe("application/json");
    const data = JSON.parse(content?.text as string);
    expect(data.runtime).toBe("bun");
  });
});

describe("prompts", () => {
  test("lists available prompts", async () => {
    const client = await createTestClient();
    const { prompts } = await client.listPrompts();

    const names = prompts.map((p) => p.name);
    expect(names).toContain("code_review");
  });

  test("gets code_review prompt", async () => {
    const client = await createTestClient();
    const result = await client.getPrompt({
      name: "code_review",
      arguments: { code: "const x = 1;", language: "typescript" },
    });

    expect(result.messages.length).toBe(1);
    expect(result.messages[0]?.role).toBe("user");
  });
});
</file>

<file path=".dockerignore">
node_modules
tests
*.md
.git
.gitignore
tsconfig.json
</file>

<file path=".env.example">
# MCP Transport: "stdio" (default) or "http"
# MCP_TRANSPORT=stdio

# HTTP transport port (only used when MCP_TRANSPORT=http)
# MCP_PORT=3000

# Log level: debug, info, warn, error
# LOG_LEVEL=info
</file>

<file path=".gitignore">
# dependencies (bun install)
node_modules

# output
out
dist
*.tgz

# code coverage
coverage
*.lcov

# logs
logs
_.log
report.[0-9]_.[0-9]_.[0-9]_.[0-9]_.json

# dotenv environment variable files
.env
.env.development.local
.env.test.local
.env.production.local
.env.local

# caches
.eslintcache
.cache
*.tsbuildinfo

# IntelliJ based IDEs
.idea

# Finder (MacOS) folder config
.DS_Store
</file>

<file path="CLAUDE.md">
Default to using Bun instead of Node.js.

- Use `bun <file>` instead of `node <file>` or `ts-node <file>`
- Use `bun test` instead of `jest` or `vitest`
- Use `bun build <file.html|file.ts|file.css>` instead of `webpack` or `esbuild`
- Use `bun install` instead of `npm install` or `yarn install` or `pnpm install`
- Use `bun run <script>` instead of `npm run <script>` or `yarn run <script>` or `pnpm run <script>`
- Use `bunx <package> <command>` instead of `npx <package> <command>`
- Bun automatically loads .env, so don't use dotenv.

## APIs

- `Bun.serve()` supports WebSockets, HTTPS, and routes. Don't use `express`.
- **IMPORTANT for MCP HTTP transport:** Use `node:http` (Bun-compatible), NOT `Bun.serve()`. `StreamableHTTPServerTransport.handleRequest()` requires Node.js `IncomingMessage`/`ServerResponse`, which are incompatible with Bun's Web API `Request`/`Response`.
- `bun:sqlite` for SQLite. Don't use `better-sqlite3`.
- `Bun.redis` for Redis. Don't use `ioredis`.
- `Bun.sql` for Postgres. Don't use `pg` or `postgres.js`.
- `WebSocket` is built-in. Don't use `ws`.
- Prefer `Bun.file` over `node:fs`'s readFile/writeFile
- Bun.$`ls` instead of execa.

## Testing

Use `bun test` to run tests.

```ts#index.test.ts
import { test, expect } from "bun:test";

test("hello world", () => {
  expect(1).toBe(1);
});
```

## MCP Server Development

This template implements an MCP (Model Context Protocol) server using `@modelcontextprotocol/sdk` with Bun.

### Project Structure

```
src/
├── index.ts              # Entry point — creates McpServer, selects transport
├── logging.ts            # Stderr-only logging (NEVER use console.log in stdio mode)
├── errors.ts             # toolError() helper + withErrorHandling() wrapper
├── tools/                # Tool handlers (LLM-invocable actions)
│   ├── index.ts          # registerTools() — add new tools here
│   ├── greet.ts          # Example: simple tool
│   └── fetch-url.ts      # Example: tool with SSRF protection
├── resources/            # Resources (read-only data exposed to clients)
│   ├── index.ts          # registerResources() — add new resources here
│   └── server-info.ts    # Example: server runtime info
└── prompts/              # Prompts (reusable prompt templates)
    ├── index.ts          # registerPrompts() — add new prompts here
    └── code-review.ts    # Example: code review prompt
tests/
└── tools.test.ts         # Tests using InMemoryTransport (no real stdio needed)
```

### Running

```bash
# STDIO transport (default — for Claude Desktop, Cursor, etc.)
bun run start

# HTTP transport (for remote/networked deployments)
bun run start:http

# Development with hot reload
bun run dev
bun run dev:http

# Run tests
bun test

# Inspect with MCP Inspector
bun run inspect
```

### Transport Selection

- **STDIO** (default): For local integrations where the client spawns the server as a subprocess. Used by Claude Desktop, Cursor, VS Code extensions.
- **Streamable HTTP** (`MCP_TRANSPORT=http`): For remote/networked deployments with multiple concurrent clients. Server listens on `MCP_PORT` (default 3000) at `/mcp` endpoint.

### Adding a New Tool

1. Create `src/tools/my-tool.ts`:

```ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { withErrorHandling } from "../errors.ts";

const schema = {
  input: z.string().describe("Description for LLM"),
};

export function registerMyTool(server: McpServer) {
  server.tool(
    "my_tool",
    "What this tool does (shown to LLM)",
    schema,
    withErrorHandling(async ({ input }) => ({
      content: [{ type: "text", text: `Result: ${input}` }],
    })),
  );
}
```

2. Register in `src/tools/index.ts`:

```ts
import { registerMyTool } from "./my-tool.ts";

export function registerTools(server: McpServer) {
  // ... existing tools
  registerMyTool(server);
}
```

### Adding a New Resource

Create `src/resources/my-resource.ts` and register in `src/resources/index.ts`:

```ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

export function registerMyResource(server: McpServer) {
  server.resource(
    "my-resource",           // name
    "data://my-resource",    // URI
    { description: "...", mimeType: "application/json" },
    async () => ({
      contents: [{ uri: "data://my-resource", mimeType: "application/json", text: "{}" }],
    }),
  );
}
```

### Adding a New Prompt

Create `src/prompts/my-prompt.ts` and register in `src/prompts/index.ts`:

```ts
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

export function registerMyPrompt(server: McpServer) {
  server.prompt(
    "my_prompt",
    "Description",
    { variable: z.string() },
    ({ variable }) => ({
      messages: [{ role: "user" as const, content: { type: "text" as const, text: variable } }],
    }),
  );
}
```

### Key Rules

- **NEVER use `console.log()`** in MCP servers — it writes to stdout and corrupts JSON-RPC protocol. Use `console.error()` or the `log()` helper from `logging.ts`.
- **Tool handlers must never throw.** Use `withErrorHandling()` wrapper or return `toolError()` from `errors.ts`.
- **Validate inputs with Zod schemas.** The SDK auto-validates, but add `.describe()` to help LLMs understand parameters.
- **SSRF protection:** When tools access URLs, block internal/private addresses (see `fetch-url.ts`).
- **Test with `InMemoryTransport`** — no real stdio/HTTP needed. See `tests/tools.test.ts`.
- **Use `bun run inspect`** to interactively test tools via MCP Inspector.

### Client Configuration

For Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "bun-mcp": {
      "command": "bun",
      "args": ["run", "/absolute/path/to/src/index.ts"]
    }
  }
}
```

For Cursor (`.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "bun-mcp": {
      "command": "bun",
      "args": ["run", "/absolute/path/to/src/index.ts"]
    }
  }
}
```

## Frontend

Use HTML imports with `Bun.serve()`. Don't use `vite`. HTML imports fully support React, CSS, Tailwind.

Server:

```ts#index.ts
import index from "./index.html"

Bun.serve({
  routes: {
    "/": index,
    "/api/users/:id": {
      GET: (req) => {
        return new Response(JSON.stringify({ id: req.params.id }));
      },
    },
  },
  // optional websocket support
  websocket: {
    open: (ws) => {
      ws.send("Hello, world!");
    },
    message: (ws, message) => {
      ws.send(message);
    },
    close: (ws) => {
      // handle close
    }
  },
  development: {
    hmr: true,
    console: true,
  }
})
```

HTML files can import .tsx, .jsx or .js files directly and Bun's bundler will transpile & bundle automatically. `<link>` tags can point to stylesheets and Bun's CSS bundler will bundle.

```html#index.html
<html>
  <body>
    <h1>Hello, world!</h1>
    <script type="module" src="./frontend.tsx"></script>
  </body>
</html>
```

With the following `frontend.tsx`:

```tsx#frontend.tsx
import React from "react";
import { createRoot } from "react-dom/client";

// import .css files directly and it works
import './index.css';

const root = createRoot(document.body);

export default function Frontend() {
  return <h1>Hello, world!</h1>;
}

root.render(<Frontend />);
```

Then, run index.ts

```sh
bun --hot ./index.ts
```

For more information, read the Bun API docs in `node_modules/bun-types/docs/**.mdx`.
</file>

<file path="Dockerfile">
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
</file>

<file path="package.json">
{
  "name": "mcp-server",
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
</file>

<file path="README.md">
# bun-mcp

MCP (Model Context Protocol) server template built with Bun and `@modelcontextprotocol/sdk`.

## Quick Start

```bash
bun install
bun run start        # stdio transport (default)
bun run start:http   # HTTP transport on port 3000
bun test             # run tests
bun run inspect      # interactive MCP Inspector
```

## Structure

```
src/
├── index.ts          # Server entry point + transport selection
├── tools/            # Tool handlers (greet, fetch_url)
├── resources/        # Resources (server-info)
├── prompts/          # Prompt templates (code_review)
├── errors.ts         # Error handling helpers
└── logging.ts        # Stderr logging (safe for stdio)
tests/
└── tools.test.ts     # Tests via InMemoryTransport
```

## Transports

| Transport | Command | Use Case |
|-----------|---------|----------|
| **stdio** | `bun run start` | Claude Desktop, Cursor, local integrations |
| **HTTP** | `bun run start:http` | Remote servers, multiple clients |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_TRANSPORT` | `stdio` | Transport: `stdio` or `http` |
| `MCP_PORT` | `3000` | HTTP transport port |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

## Client Config

**Claude Desktop** (`claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "bun-mcp": {
      "command": "bun",
      "args": ["run", "/path/to/src/index.ts"]
    }
  }
}
```

See `CLAUDE.md` for detailed development guide.
</file>

<file path="tsconfig.json">
{
  "compilerOptions": {
    // Environment setup & latest features
    "lib": ["ESNext"],
    "target": "ESNext",
    "module": "Preserve",
    "moduleDetection": "force",
    "jsx": "react-jsx",
    "allowJs": true,

    // Bundler mode
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "verbatimModuleSyntax": true,
    "noEmit": true,

    // Best practices
    "strict": true,
    "skipLibCheck": true,
    "noFallthroughCasesInSwitch": true,
    "noUncheckedIndexedAccess": true,
    "noImplicitOverride": true,

    // Some stricter flags (disabled by default)
    "noUnusedLocals": false,
    "noUnusedParameters": false,
    "noPropertyAccessFromIndexSignature": false
  }
}
</file>

</files>
