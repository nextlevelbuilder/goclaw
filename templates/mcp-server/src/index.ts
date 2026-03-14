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
