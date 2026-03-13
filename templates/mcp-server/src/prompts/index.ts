import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { registerCodeReviewPrompt } from "./code-review.ts";

/**
 * Register all prompts with the MCP server.
 * Add new prompt registrations here.
 */
export function registerPrompts(server: McpServer) {
  registerCodeReviewPrompt(server);
}
