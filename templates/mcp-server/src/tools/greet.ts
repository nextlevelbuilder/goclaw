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
