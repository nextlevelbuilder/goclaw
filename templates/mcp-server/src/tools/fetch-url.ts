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
