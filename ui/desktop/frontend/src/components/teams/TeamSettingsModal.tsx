import { useState, useEffect, useMemo, useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { IconClose, IconSpinner } from '../common/Icons'
import { useAgents } from '../../hooks/use-agents'
import { useTeamManage } from '../../hooks/use-team-manage'
import { TeamMemberList } from './team-member-list'
import { teamSettingsSchema, type TeamSettingsFormData } from '../../schemas/team-settings.schema'
import type { TeamData, TeamMemberData, TeamNotifyConfig, TeamAccessSettings, RoutingRule } from '../../types/team'

interface TeamSettingsModalProps {
  teamId: string
  onClose: () => void
  onSaved?: () => void
}

const NOTIFY_KEYS = ['dispatched', 'progress', 'failed', 'completed', 'new_task'] as const
type NotifyKey = typeof NOTIFY_KEYS[number]

export function TeamSettingsModal({ teamId, onClose, onSaved }: TeamSettingsModalProps) {
  const { t } = useTranslation('teams')
  const { fetchTeamDetail, updateTeam, addMember, removeMember } = useTeamManage()
  const { agents, refreshAgents } = useAgents()

  const [team, setTeam] = useState<TeamData | null>(null)
  const [members, setMembers] = useState<TeamMemberData[]>([])
  const [loading, setLoading] = useState(true)

  const [showAdd, setShowAdd] = useState(false)
  const [addAgent, setAddAgent] = useState('')
  const [adding, setAdding] = useState(false)
  const [removing, setRemoving] = useState<string | null>(null)

  const { watch, setValue, reset, handleSubmit, formState: { isSubmitting } } = useForm<TeamSettingsFormData>({
    resolver: zodResolver(teamSettingsSchema),
    mode: 'onChange',
    defaultValues: { name: '', description: '', notify: { dispatched: true, progress: true, failed: true, completed: true, new_task: true }, notifyMode: 'direct',
      routingEnabled: false, routingFallbackToSelf: true, routingConfidenceThreshold: 0.65, routingRules: [] },
  })

  useEffect(() => {
    refreshAgents()
    fetchTeamDetail(teamId).then((res) => {
      if (!res) return
      setTeam(res.team)
      setMembers(res.members ?? [])
      const settings = (res.team.settings ?? {}) as TeamAccessSettings
      const n = (settings.notifications ?? {}) as Record<string, unknown>
      const sr = settings.routing ?? {}
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
        routingEnabled: settings.routing !== undefined && sr.enabled !== false,
        routingFallbackToSelf: sr.fallback_to_self !== false,
        routingConfidenceThreshold: sr.confidence_threshold ?? 0.65,
        routingRules: (sr.rules ?? []).filter((r) => r.agent_key).map((r) => ({
          agent_key: r.agent_key,
          keywords: r.keywords ?? [],
          file_patterns: r.file_patterns ?? [],
          criteria: r.criteria ?? '',
          enabled: r.enabled !== false,
        })),
      })
      setLoading(false)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [teamId])

  const leadMember = members.find((m) => m.role === 'lead')
  const sorted = useMemo(() => [...members].sort((a, b) => {
    if (a.role === 'lead' && b.role !== 'lead') return -1
    if (b.role === 'lead' && a.role !== 'lead') return 1
    return (a.display_name || a.agent_key || '').localeCompare(b.display_name || b.agent_key || '')
  }), [members])

  const memberIds = useMemo(() => new Set(members.map((m) => m.agent_id)), [members])
  const availableAgents = useMemo(() => agents.filter((a) => !memberIds.has(a.id)).map((a) => ({ value: a.id, label: `${a.emoji || ''} ${a.name}`.trim() })), [agents, memberIds])

  const toggleNotify = useCallback((key: NotifyKey) => {
    setValue(`notify.${key}`, !watch(`notify.${key}`))
  }, [setValue, watch])

  const handleAddMember = async () => {
    if (!addAgent) return
    setAdding(true)
    try {
      await addMember(teamId, addAgent)
      const res = await fetchTeamDetail(teamId)
      if (res) setMembers(res.members ?? [])
      setAddAgent('')
      setShowAdd(false)
    } catch { /* toast in hook */ } finally { setAdding(false) }
  }

  const handleRemoveMember = async (agentId: string) => {
    setRemoving(agentId)
    try {
      await removeMember(teamId, agentId)
      setMembers((prev) => prev.filter((m) => m.agent_id !== agentId))
    } catch { /* toast in hook */ } finally { setRemoving(null) }
  }

  const onValid = async (data: TeamSettingsFormData) => {
    const notifications: TeamNotifyConfig = { ...data.notify, mode: data.notifyMode }
    const existing = (team?.settings ?? {}) as TeamAccessSettings
    // Backend rejects rules whose agent_key is not a current member; drop stale ones.
    const memberKeys = new Set(members.map((m) => m.agent_key))
    const routing = data.routingEnabled || data.routingRules.length > 0
      ? {
          enabled: data.routingEnabled,
          fallback_to_self: data.routingFallbackToSelf,
          confidence_threshold: data.routingConfidenceThreshold,
          rules: data.routingRules.filter((r) => memberKeys.has(r.agent_key)),
        }
      : existing.routing
    const settings = { ...existing, notifications, routing } as Record<string, unknown>
    await updateTeam(teamId, {
      name: data.name !== team?.name ? data.name : undefined,
      description: data.description !== (team?.description ?? '') ? data.description : undefined,
      settings,
    })
    onSaved?.()
  }

  if (loading) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
        <div className="bg-surface-primary rounded-xl border border-border p-8">
          <IconSpinner size={24} className="border-accent mx-auto" />
        </div>
      </div>
    )
  }

  const notifyMode = watch('notifyMode')
  const notify = watch('notify')
  const routingEnabled = watch('routingEnabled')
  const routingFallbackToSelf = watch('routingFallbackToSelf')
  const routingConfidenceThreshold = watch('routingConfidenceThreshold')
  const routingRules = watch('routingRules')

  const updateRule = (agentKey: string, patch: Partial<RoutingRule>) => {
    const next = routingRules.some((r) => r.agent_key === agentKey)
      ? routingRules.map((r) => (r.agent_key === agentKey ? { ...r, ...patch } : r))
      : [...routingRules, { agent_key: agentKey, keywords: [], file_patterns: [], criteria: '', enabled: true, ...patch }]
    setValue('routingRules', next)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} className="bg-surface-primary border border-border rounded-xl shadow-xl w-[95vw] max-w-2xl max-h-[85vh] flex flex-col mx-4">
        {/* Header */}
        <div className="px-6 pt-5 pb-4 border-b border-border shrink-0 flex items-center justify-between">
          <h2 className="text-base font-semibold text-text-primary">{t('teamSettings', 'Team Settings')}</h2>
          <button onClick={onClose} className="text-text-muted hover:text-text-primary p-1.5 cursor-pointer rounded-lg hover:bg-surface-tertiary"><IconClose /></button>
        </div>

        {/* Scrollable body */}
        <div className="flex-1 overflow-y-auto overscroll-contain space-y-5 px-6 py-4">
          {/* Team Info */}
          <section className="space-y-3">
            <h3 className="text-sm font-medium text-text-primary">{t('settings.teamInfo', 'Team Info')}</h3>
            <div className="space-y-2">
              <label className="block text-xs text-text-muted">{t('teamName', 'Team name')}</label>
              <input value={watch('name')} onChange={(e) => setValue('name', e.target.value, { shouldValidate: true })} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent/50" />
            </div>
            <div className="space-y-2">
              <label className="block text-xs text-text-muted">{t('description', 'Description')}</label>
              <textarea value={watch('description')} onChange={(e) => setValue('description', e.target.value)} rows={2} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent/50 resize-none" />
            </div>
            <div className="grid grid-cols-2 gap-3 text-sm">
              <div><span className="text-xs text-text-muted">{t('status', 'Status')}</span><p className="mt-0.5 font-medium capitalize text-text-primary">{team?.status || 'active'}</p></div>
              <div><span className="text-xs text-text-muted">{t('leadAgent', 'Lead agent')}</span><p className="mt-0.5 font-medium text-text-primary">{leadMember?.emoji && <span className="mr-1">{leadMember.emoji}</span>}{leadMember?.display_name || leadMember?.agent_key || '—'}</p></div>
            </div>
          </section>

          {/* Members */}
          <TeamMemberList
            members={members}
            sorted={sorted}
            availableAgents={availableAgents}
            showAdd={showAdd}
            addAgent={addAgent}
            adding={adding}
            removing={removing}
            onToggleAdd={() => setShowAdd(!showAdd)}
            onAddAgentChange={setAddAgent}
            onAddMember={handleAddMember}
            onRemoveMember={handleRemoveMember}
          />

          {/* Notifications */}
          <section className="space-y-3">
            <h3 className="text-sm font-medium text-text-primary">{t('settings.notifications', 'Notifications')}</h3>
            <div className="space-y-2">
              {NOTIFY_KEYS.map((key) => (
                <div key={key} className="flex items-center justify-between py-1.5">
                  <span className="text-sm text-text-secondary">{t(`settings.notify_${key}`, key)}</span>
                  <button type="button" onClick={() => toggleNotify(key)} className={`relative w-9 h-5 rounded-full transition-colors cursor-pointer ${notify[key] ? 'bg-accent' : 'bg-surface-tertiary border border-border'}`}>
                    <span className={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform ${notify[key] ? 'translate-x-4' : ''}`} />
                  </button>
                </div>
              ))}
            </div>
            <div className="space-y-2 pt-2 border-t border-border">
              <span className="text-xs text-text-muted">{t('settings.notifyMode', 'Notification mode')}</span>
              <div className="grid grid-cols-2 gap-2">
                {(['direct', 'leader'] as const).map((mode) => (
                  <button type="button" key={mode} onClick={() => setValue('notifyMode', mode)}
                    className={`text-left rounded-lg border p-3 transition-colors cursor-pointer ${notifyMode === mode ? 'border-accent bg-accent/5' : 'border-border hover:border-accent/30'}`}>
                    <div className="text-sm font-medium text-text-primary">{t(`settings.notifyMode_${mode}`, mode)}</div>
                    <div className="text-[11px] text-text-muted mt-0.5">{t(`settings.notifyMode_${mode}_desc`)}</div>
                  </button>
                ))}
              </div>
              {notifyMode === 'leader' && <p className="text-xs text-amber-500">{t('settings.notifyModeLeaderWarn', 'Only the lead agent will receive notifications.')}</p>}
            </div>
          </section>

          {/* Routing */}
          <section className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-text-primary">{t('settings.routing', 'Routing Rules')}</h3>
              <button type="button" onClick={() => setValue('routingEnabled', !routingEnabled)} className={`relative w-9 h-5 rounded-full transition-colors cursor-pointer ${routingEnabled ? 'bg-accent' : 'bg-surface-tertiary border border-border'}`}>
                <span className={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform ${routingEnabled ? 'translate-x-4' : ''}`} />
              </button>
            </div>
            {routingEnabled && (
              <>
                <div className="flex items-center justify-between py-1.5">
                  <span className="text-sm text-text-secondary">{t('settings.routingFallbackToSelf', 'Fallback to self')}</span>
                  <button type="button" onClick={() => setValue('routingFallbackToSelf', !routingFallbackToSelf)} className={`relative w-9 h-5 rounded-full transition-colors cursor-pointer ${routingFallbackToSelf ? 'bg-accent' : 'bg-surface-tertiary border border-border'}`}>
                    <span className={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform ${routingFallbackToSelf ? 'translate-x-4' : ''}`} />
                  </button>
                </div>
                <div className="space-y-2">
                  <label className="block text-xs text-text-muted">{t('settings.routingConfidence', 'Confidence threshold')}</label>
                  <input type="number" min={0.01} max={1} step={0.01} value={routingConfidenceThreshold}
                    onChange={(e) => setValue('routingConfidenceThreshold', Number(e.target.value), { shouldValidate: true })}
                    className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent/50" />
                </div>
                {members.map((m) => {
                  if (!m.agent_key) return null
                  const r = routingRules.find((x) => x.agent_key === m.agent_key) ?? { agent_key: m.agent_key, keywords: [], file_patterns: [], criteria: '', enabled: true }
                  return (
                    <div key={m.agent_id} className="rounded-lg border border-border p-3 space-y-2">
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-medium text-text-primary">{m.emoji} {m.display_name || m.agent_key}</span>
                        <button type="button" onClick={() => updateRule(m.agent_key!, { enabled: r.enabled === false })} className={`relative w-9 h-5 rounded-full transition-colors cursor-pointer ${r.enabled !== false ? 'bg-accent' : 'bg-surface-tertiary border border-border'}`}>
                          <span className={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform ${r.enabled !== false ? 'translate-x-4' : ''}`} />
                        </button>
                      </div>
                      <input value={(r.keywords ?? []).join(', ')} onChange={(e) => updateRule(m.agent_key!, { keywords: e.target.value.split(',').map((x) => x.trim()).filter(Boolean) })} placeholder={t('settings.routingKeywordsHint', 'Keywords (e.g. golang, deploy)…')} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text-primary" />
                      <input value={(r.file_patterns ?? []).join(', ')} onChange={(e) => updateRule(m.agent_key!, { file_patterns: e.target.value.split(',').map((x) => x.trim()).filter(Boolean) })} placeholder={t('settings.routingFilePatternsHint', 'File patterns (e.g. .go, dockerfile)…')} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text-primary" />
                      <textarea value={r.criteria ?? ''} onChange={(e) => updateRule(m.agent_key!, { criteria: e.target.value })} rows={2} placeholder={t('settings.routingCriteriaHint', 'Decision criteria (optional)…')} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text-primary resize-none" />
                    </div>
                  )
                })}
              </>
            )}
          </section>
        </div>

        {/* Footer */}
        <div className="px-6 py-3 border-t border-border shrink-0 flex justify-end">
          <button onClick={handleSubmit(onValid)} disabled={isSubmitting || !watch('name').trim()} className="px-4 py-2 text-sm font-medium bg-accent text-white rounded-lg hover:bg-accent/90 disabled:opacity-50 cursor-pointer flex items-center gap-2">
            {isSubmitting && <IconSpinner size={14} className="border-white" />}
            {t('settings.save', 'Save')}
          </button>
        </div>
      </div>
    </div>
  )
}
