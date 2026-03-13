import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
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

  Bun.serve({
    port,
    async fetch(req) {
      const url = new URL(req.url);

      if (url.pathname === "/mcp") {
        return transport.handleRequest(req);
      }

      if (url.pathname === "/health") {
        return Response.json({ status: "ok", server: SERVER_NAME, version: SERVER_VERSION });
      }

      return new Response("Not Found", { status: 404 });
    },
  });

  log("info", `MCP server running on http://localhost:${port}/mcp`);
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
