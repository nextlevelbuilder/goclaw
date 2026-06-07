import { z } from "zod";

export const mcpFormSchema = z.object({
  name: z.string().min(1),
  displayName: z.string(),
  transport: z.enum(["stdio", "sse", "streamable-http"]),
  command: z.string(),
  args: z.string(),
  url: z.string(),
  headers: z.record(z.string(), z.string()),
  env: z.record(z.string(), z.string()),
  toolPrefix: z.string(),
  timeout: z.number().min(1),
  enabled: z.boolean(),
  requireUserCreds: z.boolean(),
  // Admin-authored description hints appended to MCP tool descriptions so the
  // LLM sees server-specific quirks. Persisted under settings.tool_hints.
  toolHintsGlobal: z.string(),
  toolHintsTools: z.record(z.string(), z.string()),
});

export type MCPFormData = z.infer<typeof mcpFormSchema>;
