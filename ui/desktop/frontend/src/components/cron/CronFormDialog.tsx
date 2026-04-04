import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { Switch } from '../common/Switch'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { cronFormSchema, toEveryMs, type CronFormData, type ScheduleKind, type EveryUnit } from '../../schemas/cron.schema'
import { slugify } from '../../lib/slug'
import type { CronSchedule, CronPayload } from '../../types/cron'

interface CronFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (params: {
    name: string
    agentId?: string
    schedule: CronSchedule
    payload: CronPayload
    deleteAfterRun?: boolean
  }) => Promise<void>
}

export function CronFormDialog({ open, onOpenChange, onSubmit }: CronFormDialogProps) {
  const { t } = useTranslation('cron')
  const { agents } = useAgentCrud()

  const { register, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting } } = useForm<CronFormData>({
    resolver: zodResolver(cronFormSchema),
    mode: 'onChange',
    defaultValues: { name: '', agentId: '', scheduleKind: 'every', everyValue: 5, everyUnit: 'minutes', cronExpr: '', message: '', deleteAfterRun: false },
  })

  useEffect(() => {
    if (open) reset({ name: '', agentId: '', scheduleKind: 'every', everyValue: 5, everyUnit: 'minutes', cronExpr: '', message: '', deleteAfterRun: false })
  }, [open, reset])

  if (!open) return null

  const scheduleKind = watch('scheduleKind')
  const everyValue = watch('everyValue')
  const everyUnit = watch('everyUnit')
  const cronExpr = watch('cronExpr')

  const agentOptions = agents.map((a) => ({ value: a.id, label: a.display_name || a.agent_key }))

  const canSubmit = watch('name').trim() && watch('message').trim()
    && (scheduleKind === 'at' || scheduleKind === 'every' ? everyValue > 0 : cronExpr.trim())

  function buildSchedule(): CronSchedule {
    if (scheduleKind === 'every') return { kind: 'every', everyMs: toEveryMs(everyValue, everyUnit) }
    if (scheduleKind === 'cron') return { kind: 'cron', expr: cronExpr.trim(), tz: Intl.DateTimeFormat().resolvedOptions().timeZone }
    return { kind: 'at', atMs: Date.now() + 60_000 }
  }

  const onValid = async (data: CronFormData) => {
    await onSubmit({
      name: data.name,
      agentId: data.agentId || undefined,
      schedule: buildSchedule(),
      payload: { kind: 'message', message: data.message },
      deleteAfterRun: data.scheduleKind === 'at' ? data.deleteAfterRun : undefined,
    })
    onOpenChange(false)
  }

  const SCHEDULE_KINDS: ScheduleKind[] = ['every', 'cron', 'at']
  const UNITS: { value: EveryUnit; label: string }[] = [
    { value: 'seconds', label: 'seconds' },
    { value: 'minutes', label: 'minutes' },
    { value: 'hours', label: 'hours' },
  ]

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={() => onOpenChange(false)} />
      <div className="relative w-full max-w-lg bg-surface-secondary rounded-xl border border-border overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <span className="text-sm font-semibold text-text-primary">{t('create.title')}</span>
          <button onClick={() => onOpenChange(false)} className="p-1 text-text-muted hover:text-text-primary transition-colors">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Form */}
        <div className="max-h-[70vh] overflow-y-auto p-5 space-y-4">
          {/* Name */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('create.name')}</label>
            <input
              value={watch('name')}
              onChange={(e) => setValue('name', slugify(e.target.value), { shouldValidate: true })}
              placeholder={t('create.namePlaceholder')}
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent font-mono"
            />
            <p className="text-[11px] text-text-muted">{t('create.nameHint')}</p>
            {errors.name && <p className="text-xs text-error">{errors.name.message}</p>}
          </div>

          {/* Agent */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('create.agentId')}</label>
            <Combobox value={watch('agentId')} onChange={(v) => setValue('agentId', v)} options={agentOptions} placeholder={t('create.agentIdPlaceholder')} allowCustom={false} />
          </div>

          {/* Schedule type */}
          <div className="space-y-2">
            <label className="text-xs font-medium text-text-secondary">{t('create.scheduleType')}</label>
            <div className="grid grid-cols-3 gap-2">
              {SCHEDULE_KINDS.map((kind) => (
                <button
                  key={kind}
                  type="button"
                  onClick={() => setValue('scheduleKind', kind)}
                  className={`border rounded-lg px-3 py-2 text-xs text-center transition-colors ${
                    scheduleKind === kind
                      ? 'border-accent bg-accent/10 text-accent font-medium'
                      : 'border-border text-text-secondary hover:bg-surface-tertiary/30'
                  }`}
                >
                  {t(`create.${kind === 'at' ? 'once' : kind}`)}
                </button>
              ))}
            </div>

            {/* Every fields */}
            {scheduleKind === 'every' && (
              <div className="flex items-center gap-2 pt-1">
                <input
                  type="number"
                  min={1}
                  value={everyValue}
                  onChange={(e) => setValue('everyValue', Math.max(1, Number(e.target.value)))}
                  className="w-20 bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
                />
                <div className="flex gap-1">
                  {UNITS.map((u) => (
                    <button
                      key={u.value}
                      type="button"
                      onClick={() => setValue('everyUnit', u.value)}
                      className={`border rounded-lg px-2.5 py-1.5 text-xs transition-colors ${
                        everyUnit === u.value
                          ? 'border-accent bg-accent/10 text-accent font-medium'
                          : 'border-border text-text-secondary hover:bg-surface-tertiary/30'
                      }`}
                    >
                      {u.label}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {/* Cron expression */}
            {scheduleKind === 'cron' && (
              <div className="space-y-1 pt-1">
                <input
                  {...register('cronExpr')}
                  placeholder="0 * * * *"
                  className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm font-mono text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
                />
                <p className="text-[11px] text-text-muted">
                  {t('create.cronHint')} — TZ: {Intl.DateTimeFormat().resolvedOptions().timeZone}
                </p>
              </div>
            )}

            {scheduleKind === 'at' && <p className="text-xs text-text-muted pt-1">{t('create.onceDesc')}</p>}
          </div>

          {/* Message */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('create.message')}</label>
            <textarea
              {...register('message')}
              placeholder={t('create.messagePlaceholder')}
              rows={3}
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent resize-none"
            />
            {errors.message && <p className="text-xs text-error">{errors.message.message}</p>}
          </div>

          {/* Delete after run */}
          {scheduleKind === 'at' && (
            <div className="flex items-center justify-between rounded-lg border border-border p-3">
              <div>
                <p className="text-xs font-medium text-text-primary">{t('detail.deleteAfterRun')}</p>
                <p className="text-[11px] text-text-muted">{t('detail.deleteAfterRunDesc')}</p>
              </div>
              <Switch checked={watch('deleteAfterRun')} onCheckedChange={(v) => setValue('deleteAfterRun', v)} />
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="border-t border-border px-5 py-4 flex items-center justify-end gap-2">
          <button type="button" onClick={() => onOpenChange(false)} className="border border-border rounded-lg px-4 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary transition-colors">
            {t('create.cancel')}
          </button>
          <button type="button" onClick={handleSubmit(onValid)} disabled={!canSubmit || isSubmitting} className="bg-accent rounded-lg px-4 py-1.5 text-sm text-white hover:bg-accent-hover disabled:opacity-50 transition-colors">
            {isSubmitting ? t('create.creating') : t('create.create')}
          </button>
        </div>
      </div>
    </div>
  )
}
