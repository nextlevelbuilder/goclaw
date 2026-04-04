import { z } from 'zod'
import { isValidSlug } from '../lib/slug'

const scheduleKindEnum = z.enum(['every', 'cron', 'at'])
const everyUnitEnum = z.enum(['seconds', 'minutes', 'hours'])

export const cronFormSchema = z.object({
  name: z
    .string()
    .min(1, 'Required')
    .refine(isValidSlug, 'Only lowercase letters, numbers, and hyphens'),
  agentId: z.string(),
  scheduleKind: scheduleKindEnum,
  everyValue: z.number().min(1),
  everyUnit: everyUnitEnum,
  cronExpr: z.string(),
  message: z.string().min(1, 'Message is required'),
  deleteAfterRun: z.boolean(),
})

export type CronFormData = z.infer<typeof cronFormSchema>
export type ScheduleKind = z.infer<typeof scheduleKindEnum>
export type EveryUnit = z.infer<typeof everyUnitEnum>

/** Convert everyValue + everyUnit to milliseconds */
export function toEveryMs(value: number, unit: z.infer<typeof everyUnitEnum>): number {
  if (unit === 'minutes') return value * 60 * 1000
  if (unit === 'hours') return value * 3600 * 1000
  return value * 1000
}
