import { z } from 'zod'

export const mediaChainEntrySchema = z.object({
  id: z.string(), // client-only UUID for DnD keys
  provider: z.string().min(1, 'Provider is required'),
  model: z.string().min(1, 'Model is required'),
  enabled: z.boolean(),
  timeout: z.number().min(1).max(600),
  max_retries: z.number().min(0).max(10),
  params: z.record(z.string(), z.unknown()).optional(),
})

export const mediaChainSchema = z.object({
  chain: z.array(mediaChainEntrySchema),
})

export type MediaChainEntry = z.infer<typeof mediaChainEntrySchema>
export type MediaChainFormData = z.infer<typeof mediaChainSchema>
