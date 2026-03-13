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
