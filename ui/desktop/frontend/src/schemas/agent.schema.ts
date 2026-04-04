import { z } from 'zod'
import { isValidSlug } from '../lib/slug'

export const agentFormSchema = z.object({
  displayName: z.string().min(1, 'Required'),
  emoji: z.string().max(2),
  agentKey: z
    .string()
    .min(1, 'Required')
    .refine(isValidSlug, 'Only lowercase letters, numbers, and hyphens'),
  providerName: z.string().min(1, 'Required'),
  model: z.string().min(1, 'Required'),
  description: z.string().min(1, 'Personality description is required'),
  isDefault: z.boolean(),
})

export type AgentFormData = z.infer<typeof agentFormSchema>
