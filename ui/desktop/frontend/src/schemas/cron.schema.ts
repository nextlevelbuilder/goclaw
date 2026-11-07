import { z } from 'zod'
import { isValidSlug } from '../lib/slug'

const scheduleKindEnum = z.enum(['every', 'cron', 'at'])
const everyUnitEnum = z.enum(['seconds', 'minutes', 'hours'])

/**
 * Validation messages are i18n keys in the `cron` namespace (`form.errors.*`);
 * the form translates them next to each field.
 */
export const cronFormSchema = z
  .object({
    name: z
      .string()
      .min(1, 'form.errors.nameRequired')
      .refine(isValidSlug, 'form.errors.nameFormat'),
    agentId: z.string(),
    scheduleKind: scheduleKindEnum,
    everyValue: z.number().min(1, 'form.errors.everyMin'),
    everyUnit: everyUnitEnum,
    cronExpr: z.string(),
    message: z.string().trim().min(1, 'form.errors.messageRequired'),
    deleteAfterRun: z.boolean(),
  })
  .superRefine((data, ctx) => {
    if (data.scheduleKind === 'cron' && data.cronExpr.trim().split(/\s+/).filter(Boolean).length < 5) {
      ctx.addIssue({ code: 'custom', path: ['cronExpr'], message: 'form.errors.cronFields' })
    }
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

/** Largest whole unit that represents `ms` exactly, for pre-filling the edit form. */
export function fromEveryMs(ms: number): { value: number; unit: EveryUnit } {
  if (ms >= 3600_000 && ms % 3600_000 === 0) return { value: ms / 3600_000, unit: 'hours' }
  if (ms >= 60_000 && ms % 60_000 === 0) return { value: ms / 60_000, unit: 'minutes' }
  return { value: Math.max(1, Math.round(ms / 1000)), unit: 'seconds' }
}
