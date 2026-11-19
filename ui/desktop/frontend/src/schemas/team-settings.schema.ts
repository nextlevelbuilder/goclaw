import { z } from 'zod'

const notifyModeEnum = z.enum(['direct', 'leader'])

export const teamSettingsSchema = z.object({
  name: z.string().min(1, 'Team name is required'),
  description: z.string(),
  notify: z.object({
    dispatched: z.boolean(),
    progress: z.boolean(),
    failed: z.boolean(),
    completed: z.boolean(),
    new_task: z.boolean(),
  }),
  notifyMode: notifyModeEnum,
  routingEnabled: z.boolean(),
  routingFallbackToSelf: z.boolean(),
  routingConfidenceThreshold: z.number().min(0.01).max(1),
  routingRules: z.array(z.object({
    agent_key: z.string().min(1),
    keywords: z.array(z.string()),
    file_patterns: z.array(z.string()),
    criteria: z.string(),
    enabled: z.boolean(),
  })),
})

export type TeamSettingsFormData = z.infer<typeof teamSettingsSchema>
export type NotifyMode = z.infer<typeof notifyModeEnum>
