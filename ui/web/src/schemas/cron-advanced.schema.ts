import { z } from "zod";

export const cronAdvancedSchema = z.object({
  timezone: z.string(),
  deliver: z.boolean(),
  channel: z.string(),
  to: z.string(),
  wakeHeartbeat: z.boolean(),
  deleteAfterRun: z.boolean(),
  stateless: z.boolean(),
});

export type CronAdvancedFormData = z.infer<typeof cronAdvancedSchema>;
