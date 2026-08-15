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
})

export type TeamSettingsFormData = z.infer<typeof teamSettingsSchema>
export type NotifyMode = z.infer<typeof notifyModeEnum>
