import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerServerInfoResource } from "./server-info.ts";

/**
 * Register all resources with the MCP server.
 * Add new resource registrations here.
 */
export function registerResources(server: McpServer) {
  registerServerInfoResource(server);
}
