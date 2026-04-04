import { z } from "zod";

export const mcpUserCredentialsSchema = z.object({
  apiKey: z.string(),
  headers: z.record(z.string(), z.string()),
  env: z.record(z.string(), z.string()),
});

export type MCPUserCredentialsFormData = z.infer<typeof mcpUserCredentialsSchema>;
