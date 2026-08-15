import { z } from 'zod'

export const mcpFormSchema = z.object({
  name: z.string().min(1, 'Required'),
  displayName: z.string(),
  transport: z.enum(['stdio', 'sse', 'streamable-http']),
  command: z.string(),
  args: z.string(), // space-separated, split on submit
  url: z.string(),
  headers: z.record(z.string(), z.string()),
  env: z.record(z.string(), z.string()),
  toolPrefix: z.string(),
  timeoutSec: z.number().min(1),
  enabled: z.boolean(),
})

export type MCPFormData = z.infer<typeof mcpFormSchema>
