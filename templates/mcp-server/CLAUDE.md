
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
