import { z } from 'zod'

export const channelFormSchema = z.object({
  displayName: z.string(),
  channelType: z.string().min(1, 'Channel type is required'),
  agentId: z.string().min(1, 'Agent is required'),
  enabled: z.boolean(),
  credentials: z.record(z.string(), z.string()),
})

export type ChannelFormData = z.infer<typeof channelFormSchema>
