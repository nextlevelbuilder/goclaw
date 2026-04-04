import { z } from 'zod'

/** Extractor entry for web_fetch tool chain */
export const extractorEntrySchema = z.object({
  name: z.string(),
  enabled: z.boolean(),
  base_url: z.string().optional(),
  timeout: z.number().min(0).optional(),
  max_retries: z.number().min(0).max(10).optional(),
})

export const extractorChainSchema = z.object({
  extractors: z.array(extractorEntrySchema),
})

/** Generic JSON settings — validated as a record */
export const jsonSettingsSchema = z.record(z.string(), z.unknown())

export type ExtractorEntry = z.infer<typeof extractorEntrySchema>
export type ExtractorChainData = z.infer<typeof extractorChainSchema>
