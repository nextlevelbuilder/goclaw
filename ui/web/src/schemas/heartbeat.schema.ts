import { z } from "zod";

export const heartbeatConfigSchema = z.object({
  enabled: z.boolean(),
  intervalMin: z.number().min(5),
  ackMaxChars: z.number().min(0),
  maxRetries: z.number().min(0),
  isolatedSession: z.boolean(),
  lightContext: z.boolean(),
  activeHoursStart: z.string().optional(),
  activeHoursEnd: z.string().optional(),
  timezone: z.string().optional(),
  channel: z.string().optional(),
  chatId: z.string().optional(),
  hbProvider: z.string().optional(),
  hbModel: z.string().optional(),
  checklist: z.string().optional(),
});

export type HeartbeatConfigFormData = z.infer<typeof heartbeatConfigSchema>;
