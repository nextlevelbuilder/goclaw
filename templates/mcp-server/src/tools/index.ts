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
