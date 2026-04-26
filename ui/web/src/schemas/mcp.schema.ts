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
  healthFailThreshold: z.number().min(0),
  healthCheckInterval: z.number().min(0),
  maxReconnectAttempts: z.number().min(0),
  reconnectCooldown: z.number().min(0),
  idleTimeout: z.number().min(0),
});

export type MCPFormData = z.infer<typeof mcpFormSchema>;
