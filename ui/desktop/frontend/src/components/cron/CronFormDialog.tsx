import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { cronFormSchema, fromEveryMs, toEveryMs, type CronFormData, type EveryUnit, type ScheduleKind } from '../../schemas/cron.schema'
import { slugify } from '../../lib/slug'
import { IconAlertTriangle } from '../common/Icons'
import { Button, Dialog, DialogFooter, Field, Input, SegmentedControl, Select, Switch, Textarea } from '../ui'
import { formatWhen } from './cron-format'
import type { CronJobPatch } from '../../services/cron-service'
import type { CronJob, CronPayload, CronSchedule } from '../../types/cron'

interface CronFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The job being edited; null creates a new one. */
  job?: CronJob | null
  onCreate: (params: {
    name: string
    agentId?: string
    schedule: CronSchedule
    payload: CronPayload
    deleteAfterRun?: boolean
  }) => Promise<void>
  onUpdate: (id: string, patch: CronJobPatch) => Promise<void>
  /** Destructive action shown on the left of the edit footer. */
  onDelete: (job: CronJob) => void
  /** Opens the run log of the edited job. */
  onViewLog: (job: CronJob) => void
}

const DEFAULTS: CronFormData = { name: '', agentId: '', scheduleKind: 'every', everyValue: 5, everyUnit: 'minutes', cronExpr: '', message: '', deleteAfterRun: false }

function valuesFor(job: CronJob | null | undefined): CronFormData {
  if (!job) return DEFAULTS
  const s = job.schedule
  const every = s.kind === 'every' && s.everyMs ? fromEveryMs(s.everyMs) : { value: 5, unit: 'minutes' as EveryUnit }
  return {
    name: job.name,
    agentId: job.agentId ?? '',
    scheduleKind: s.kind,
    everyValue: every.value,
    everyUnit: every.unit,
    cronExpr: s.kind === 'cron' ? s.expr ?? '' : '',
    message: job.payload?.message ?? '',
    deleteAfterRun: job.deleteAfterRun ?? false,
  }
}

export function CronFormDialog({ open, onOpenChange, job, onCreate, onUpdate, onDelete, onViewLog }: CronFormDialogProps) {
  const { t, i18n } = useTranslation(['cron', 'common'])
  const { agents } = useAgentCrud()
  const isEditing = !!job
  const [submitError, setSubmitError] = useState<string | null>(null)
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone

  const { register, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting, isValid } } = useForm<CronFormData>({
    resolver: zodResolver(cronFormSchema),
    mode: 'onChange',
    defaultValues: DEFAULTS,
  })

  useEffect(() => {
    if (!open) return
    setSubmitError(null)
    reset(valuesFor(job))
  }, [open, job, reset])

  const scheduleKind = watch('scheduleKind')
  const everyValue = watch('everyValue')
  const everyUnit = watch('everyUnit')
  const deleteAfterRun = watch('deleteAfterRun')

  const msg = (key?: string) => (key ? t(`cron:${key}`) : undefined)

  function buildSchedule(data: CronFormData): CronSchedule | undefined {
    if (data.scheduleKind === 'every') return { kind: 'every', everyMs: toEveryMs(data.everyValue, data.everyUnit) }
    if (data.scheduleKind === 'cron') return { kind: 'cron', expr: data.cronExpr.trim(), tz }
    // An existing one-time job keeps its run time unless the schedule type changed.
    if (job?.schedule.kind === 'at') return undefined
    return { kind: 'at', atMs: Date.now() + 60_000 }
  }

  const onValid = async (data: CronFormData) => {
    setSubmitError(null)
    try {
      const schedule = buildSchedule(data)
      const deleteFlag = data.scheduleKind === 'at' ? data.deleteAfterRun : undefined
      if (job) {
        await onUpdate(job.id, {
          name: data.name,
          agentId: data.agentId,
          ...(schedule ? { schedule } : {}),
          message: data.message,
          ...(deleteFlag !== undefined ? { deleteAfterRun: deleteFlag } : {}),
        })
      } else {
        await onCreate({
          name: data.name,
          agentId: data.agentId || undefined,
          schedule: schedule as CronSchedule,
          payload: { kind: 'message', message: data.message },
          deleteAfterRun: deleteFlag,
        })
      }
      onOpenChange(false)
    } catch (err) {
      setSubmitError((err as Error).message || t('cron:form.errors.saveFailed'))
    }
  }

  const kindOptions: { value: ScheduleKind; label: string }[] = [
    { value: 'every', label: t('cron:create.every') },
    { value: 'cron', label: t('cron:create.cron') },
    { value: 'at', label: t('cron:create.once') },
  ]
  const unitOptions: { value: EveryUnit; label: string }[] = [
    { value: 'seconds', label: t('cron:form.units.seconds') },
    { value: 'minutes', label: t('cron:form.units.minutes') },
    { value: 'hours', label: t('cron:form.units.hours') },
  ]
  const agentOptions = [
    { value: '', label: t('cron:defaultAgent') },
    ...agents.map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
  ]
  const lastFailed = job?.state?.lastStatus && ['error', 'failed'].includes(job.state.lastStatus.toLowerCase())

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title={isEditing ? t('cron:edit.title') : t('cron:create.title')}
      dismissible={!isSubmitting}
      footer={(
        <DialogFooter
          destructive={job ? (
            <Button variant="danger" onClick={() => onDelete(job)} disabled={isSubmitting}>{t('cron:edit.delete')}</Button>
          ) : undefined}
        >
          <Button variant="secondary" onClick={() => onOpenChange(false)} disabled={isSubmitting}>{t('cron:create.cancel')}</Button>
          <Button variant="primary" onClick={handleSubmit(onValid)} loading={isSubmitting} disabled={!isValid}>
            {isEditing ? t('cron:edit.save') : t('cron:create.create')}
          </Button>
        </DialogFooter>
      )}
    >
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void handleSubmit(onValid)() }} noValidate>
        {submitError && (
          <div role="alert" className="flex items-start gap-2 rounded-md bg-danger-subtle px-3 py-2.5 text-caption text-danger">
            <IconAlertTriangle size={16} className="mt-px shrink-0" />
            <span>{submitError}</span>
          </div>
        )}

        <Field label={t('cron:create.name')} helper={t('cron:create.nameHint')} error={msg(errors.name?.message)} required>
          <Input
            value={watch('name')}
            onChange={(e) => setValue('name', slugify(e.target.value), { shouldValidate: true })}
            placeholder={t('cron:create.namePlaceholder')}
            className="text-code"
          />
        </Field>

        <Field label={t('cron:create.agentId')}>
          <Select value={watch('agentId')} onChange={(e) => setValue('agentId', e.target.value)} options={agentOptions} />
        </Field>

        <div className="flex flex-col gap-1.5">
          <span id="cron-schedule-label" className="text-ui text-ink">{t('cron:create.scheduleType')}</span>
          <SegmentedControl
            aria-label={t('cron:create.scheduleType')}
            value={scheduleKind}
            onValueChange={(v) => setValue('scheduleKind', v, { shouldValidate: true })}
            options={kindOptions}
          />

          {scheduleKind === 'every' && (
            <div className="flex items-start gap-2 pt-1">
              <Field label={t('cron:form.interval')} error={msg(errors.everyValue?.message)} className="w-28">
                <Input
                  type="number"
                  min={1}
                  value={Number.isFinite(everyValue) ? everyValue : ''}
                  onChange={(e) => setValue('everyValue', Math.max(0, Number(e.target.value)), { shouldValidate: true })}
                  className="text-base md:text-ui"
                />
              </Field>
              <Field label={t('cron:form.unit')} className="w-36">
                <Select
                  value={everyUnit}
                  onChange={(e) => setValue('everyUnit', e.target.value as EveryUnit)}
                  options={unitOptions}
                />
              </Field>
            </div>
          )}

          {scheduleKind === 'cron' && (
            <div className="pt-1">
              <Field
                label={t('cron:create.cronExpression')}
                helper={`${t('cron:create.cronHint')} · ${t('cron:form.timezone', { tz })}`}
                error={msg(errors.cronExpr?.message)}
              >
                <Input {...register('cronExpr')} placeholder="0 * * * *" className="text-code" />
              </Field>
              {job?.state?.nextRunAtMs && job.schedule.kind === 'cron' && (
                <p className="mt-1 text-caption text-ink-muted">{t('cron:form.nextRun', { time: formatWhen(job.state.nextRunAtMs, i18n.language) })}</p>
              )}
            </div>
          )}

          {scheduleKind === 'at' && (
            <p className="pt-1 text-caption text-ink-muted">
              {job?.schedule.kind === 'at' && job.schedule.atMs
                ? t('cron:form.onceAt', { time: formatWhen(job.schedule.atMs, i18n.language) })
                : t('cron:create.onceDesc')}
            </p>
          )}
        </div>

        <Field label={t('cron:create.message')} error={msg(errors.message?.message)} required>
          <Textarea {...register('message')} placeholder={t('cron:create.messagePlaceholder')} rows={3} />
        </Field>

        {scheduleKind === 'at' && (
          <div className="flex items-center justify-between gap-3 rounded-md border border-border-subtle p-3">
            <div>
              <p id="cron-delete-after" className="text-ui text-ink">{t('cron:detail.deleteAfterRun')}</p>
              <p className="text-caption text-ink-muted">{t('cron:detail.deleteAfterRunDesc')}</p>
            </div>
            <Switch checked={deleteAfterRun} onCheckedChange={(v) => setValue('deleteAfterRun', v)} aria-labelledby="cron-delete-after" />
          </div>
        )}

        {job && lastFailed && (
          <div className="flex items-start justify-between gap-3 rounded-md bg-danger-subtle px-3 py-2 text-caption text-danger">
            <span>{t('cron:form.lastFailed', { error: job.state.lastError ?? '' })}</span>
            <button type="button" onClick={() => onViewLog(job)} className="shrink-0 rounded-sm font-medium underline">
              {t('cron:form.viewLog')}
            </button>
          </div>
        )}
      </form>
    </Dialog>
  )
}
