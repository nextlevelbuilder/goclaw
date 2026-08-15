import { z } from 'zod'

export const providerFormSchema = z.object({
  providerType: z.string().min(1, 'Provider type is required'),
  displayName: z.string(),
  apiBase: z.string(),
  apiKey: z.string(),
  enabled: z.boolean(),
})

export type ProviderFormData = z.infer<typeof providerFormSchema>
