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
