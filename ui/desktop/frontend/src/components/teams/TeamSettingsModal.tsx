import { useState, useEffect, useMemo, useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { useAgents } from '../../hooks/use-agents'
import { useTeamManage } from '../../hooks/use-team-manage'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { TeamMemberList } from './team-member-list'
import { teamSettingsSchema, type TeamSettingsFormData } from '../../schemas/team-settings.schema'
import type { TeamData, TeamMemberData, TeamNotifyConfig } from '../../types/team'
import {
  Button, ConfirmDialog, Dialog, DialogFooter, ErrorState, Field, Input, SegmentedControl, Skeleton, StatusChip, Switch,
  Textarea, toast,
} from '../ui'

interface TeamSettingsModalProps {
  teamId: string
  onClose: () => void
  onSaved?: () => void
  /** Called after the team was deleted from this dialog. */
  onDeleted?: () => void
}

const NOTIFY_KEYS = ['dispatched', 'progress', 'failed', 'completed', 'new_task'] as const
type NotifyKey = typeof NOTIFY_KEYS[number]

/** Edit team: name, description, members and notifications. Delete sits on the left, Cancel and Save on the right. */
export function TeamSettingsModal({ teamId, onClose, onSaved, onDeleted }: TeamSettingsModalProps) {
  const { t } = useTranslation('teams')
  const { disconnected } = useGatewayStatus()
  const { fetchTeamDetail, updateTeam, addMember, removeMember, deleteTeam } = useTeamManage()
  const { agents, refreshAgents } = useAgents()

  const [team, setTeam] = useState<TeamData | null>(null)
  const [members, setMembers] = useState<TeamMemberData[]>([])
  const [loadState, setLoadState] = useState<'loading' | 'ready' | 'error'>('loading')

  const [showAdd, setShowAdd] = useState(false)
  const [addAgent, setAddAgent] = useState('')
  const [adding, setAdding] = useState(false)
  const [removing, setRemoving] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const { watch, setValue, reset, handleSubmit, formState: { isSubmitting, errors } } = useForm<TeamSettingsFormData>({
    resolver: zodResolver(teamSettingsSchema),
    mode: 'onChange',
    defaultValues: { name: '', description: '', notify: { dispatched: true, progress: true, failed: true, completed: true, new_task: true }, notifyMode: 'direct' },
  })

  const load = useCallback(async () => {
    setLoadState('loading')
    const res = await fetchTeamDetail(teamId)
    if (!res) {
      setLoadState('error')
      return
    }
    setTeam(res.team)
    setMembers(res.members ?? [])
    const settings = (res.team.settings ?? {}) as Record<string, unknown>
    const n = (settings.notifications ?? {}) as Record<string, unknown>
    reset({
      name: res.team.name,
      description: res.team.description ?? '',
      notify: {
        dispatched: n.dispatched !== false,
        progress: n.progress !== false,
        failed: n.failed !== false,
        completed: n.completed !== false,
        new_task: n.new_task !== false,
      },
      notifyMode: (n.mode as 'direct' | 'leader') || 'direct',
    })
    setLoadState('ready')
  }, [fetchTeamDetail, reset, teamId])

  useEffect(() => {
    void refreshAgents()
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [teamId])

  const leadMember = members.find((m) => m.role === 'lead')
  const sorted = useMemo(() => [...members].sort((a, b) => {
    if (a.role === 'lead' && b.role !== 'lead') return -1
    if (b.role === 'lead' && a.role !== 'lead') return 1
    return (a.display_name || a.agent_key || '').localeCompare(b.display_name || b.agent_key || '')
  }), [members])

  const memberIds = useMemo(() => new Set(members.map((m) => m.agent_id)), [members])
  const availableAgents = useMemo(
    () => agents.filter((a) => !memberIds.has(a.id)).map((a) => ({ value: a.id, label: `${a.emoji || ''} ${a.name}`.trim() })),
    [agents, memberIds],
  )

  const toggleNotify = useCallback((key: NotifyKey, on: boolean) => {
    setValue(`notify.${key}`, on, { shouldDirty: true })
  }, [setValue])

  const handleAddMember = async () => {
    if (!addAgent) return
    setAdding(true)
    try {
      await addMember(teamId, addAgent)
      toast.success(t('screen.toast.memberAdded'))
      const res = await fetchTeamDetail(teamId)
      if (res) setMembers(res.members ?? [])
      setAddAgent('')
      setShowAdd(false)
    } catch {
      toast.error(t('screen.toast.memberFailed'))
    } finally {
      setAdding(false)
    }
  }

  const handleRemoveMember = async (agentId: string) => {
    setRemoving(agentId)
    try {
      await removeMember(teamId, agentId)
      toast.success(t('screen.toast.memberRemoved'))
      setMembers((prev) => prev.filter((m) => m.agent_id !== agentId))
    } catch {
      toast.error(t('screen.toast.memberFailed'))
    } finally {
      setRemoving(null)
    }
  }

  const onValid = async (data: TeamSettingsFormData) => {
    const notifications: TeamNotifyConfig = { ...data.notify, mode: data.notifyMode }
    const settings = { ...(team?.settings ?? {}), notifications } as Record<string, unknown>
    try {
      await updateTeam(teamId, {
        name: data.name !== team?.name ? data.name : undefined,
        description: data.description !== (team?.description ?? '') ? data.description : undefined,
        settings,
      })
      toast.success(t('screen.toast.updated'))
      onSaved?.()
      onClose()
    } catch (err) {
      toast.error(t('screen.toast.updateFailed'), err instanceof Error ? err.message : undefined)
    }
  }

  const doDelete = async () => {
    setDeleting(true)
    try {
      await deleteTeam(teamId)
      toast.success(t('screen.toast.deleted'))
      onDeleted?.()
    } catch {
      toast.error(t('screen.toast.deleteFailed'))
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  const notifyMode = watch('notifyMode')
  const notify = watch('notify')
  const name = watch('name')
  const ready = loadState === 'ready'

  return (
    <>
      <Dialog
        open
        onOpenChange={(o) => { if (!o) onClose() }}
        title={t('screen.edit.title')}
        size="lg"
        dismissible={!isSubmitting}
        footer={ready ? (
          <DialogFooter
            destructive={(
              <Button variant="danger" onClick={() => setConfirmDelete(true)} disabled={disconnected}>
                {t('screen.deleteTeam')}
              </Button>
            )}
          >
            <Button variant="secondary" onClick={onClose} disabled={isSubmitting}>{t('screen.edit.cancel')}</Button>
            <Button
              variant="primary"
              onClick={handleSubmit(onValid)}
              loading={isSubmitting}
              disabled={!name.trim() || disconnected}
            >
              {t('screen.edit.save')}
            </Button>
          </DialogFooter>
        ) : undefined}
      >
        {loadState === 'loading' && (
          <div role="status" aria-busy="true" className="flex flex-col gap-4">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        )}
        {loadState === 'error' && (
          <ErrorState
            title={t('screen.edit.loadFailed')}
            description={t('screen.errorDescription')}
            onRetry={() => void load()}
          />
        )}
        {ready && (
          <div className="flex flex-col gap-6">
            <section className="flex flex-col gap-3">
              <h3 className="text-ui font-semibold text-ink">{t('settings.teamInfo')}</h3>
              <Field label={t('teamName')} error={errors.name ? t('screen.create.nameRequired') : undefined}>
                <Input value={name} onChange={(e) => setValue('name', e.target.value, { shouldValidate: true, shouldDirty: true })} />
              </Field>
              <Field label={t('description')}>
                <Textarea
                  rows={2}
                  value={watch('description')}
                  onChange={(e) => setValue('description', e.target.value, { shouldDirty: true })}
                />
              </Field>
              <dl className="grid grid-cols-2 gap-3">
                <div>
                  <dt className="text-caption text-ink-muted">{t('status')}</dt>
                  <dd className="mt-1">
                    <StatusChip
                      tone={team?.status === 'archived' ? 'idle' : 'success'}
                      label={t(`screen.status.${team?.status === 'archived' ? 'archived' : 'active'}`)}
                    />
                  </dd>
                </div>
                <div>
                  <dt className="text-caption text-ink-muted">{t('leadAgent')}</dt>
                  <dd className="mt-1 text-ui text-ink">
                    {leadMember?.emoji && <span className="mr-1">{leadMember.emoji}</span>}
                    {leadMember?.display_name || leadMember?.agent_key || '—'}
                  </dd>
                </div>
              </dl>
            </section>

            <TeamMemberList
              members={members}
              sorted={sorted}
              availableAgents={availableAgents}
              showAdd={showAdd}
              addAgent={addAgent}
              adding={adding}
              removing={removing}
              readOnly={disconnected}
              onToggleAdd={() => setShowAdd(!showAdd)}
              onAddAgentChange={setAddAgent}
              onAddMember={handleAddMember}
              onRemoveMember={handleRemoveMember}
            />

            <section className="flex flex-col gap-3">
              <h3 className="text-ui font-semibold text-ink">{t('settings.notifications')}</h3>
              <ul className="flex flex-col">
                {NOTIFY_KEYS.map((key) => (
                  <li key={key} className="flex items-center justify-between py-1.5">
                    <span id={`notify-${key}`} className="text-ui text-ink-secondary">{t(`settings.notify_${key}`)}</span>
                    <Switch
                      checked={notify[key]}
                      onCheckedChange={(on) => toggleNotify(key, on)}
                      aria-labelledby={`notify-${key}`}
                    />
                  </li>
                ))}
              </ul>
              <div className="flex flex-col gap-2 border-t border-border-subtle pt-3">
                <span className="text-caption text-ink-muted">{t('settings.notifyMode')}</span>
                <SegmentedControl<'direct' | 'leader'>
                  value={notifyMode}
                  onValueChange={(m) => setValue('notifyMode', m, { shouldDirty: true })}
                  aria-label={t('settings.notifyMode')}
                  options={[
                    { value: 'direct', label: t('settings.notifyMode_direct') },
                    { value: 'leader', label: t('settings.notifyMode_leader') },
                  ]}
                />
                <p className="text-caption text-ink-muted">{t(`settings.notifyMode_${notifyMode}_desc`)}</p>
                {notifyMode === 'leader' && <p className="text-caption text-warning">{t('settings.notifyModeLeaderWarn')}</p>}
              </div>
            </section>
          </div>
        )}
      </Dialog>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={(o) => { if (!o && !deleting) setConfirmDelete(false) }}
        title={t('screen.delete.title', { name: team?.name ?? '' })}
        description={t('screen.delete.body')}
        confirmLabel={t('screen.delete.confirm')}
        cancelLabel={t('screen.edit.cancel')}
        variant="destructive"
        loading={deleting}
        onConfirm={() => void doDelete()}
      />
    </>
  )
}
