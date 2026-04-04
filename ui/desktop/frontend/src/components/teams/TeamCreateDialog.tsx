import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { teamService } from '../../services/team-service'
import { toast } from '../../stores/toast-store'
import { Combobox } from '../common/Combobox'
import { IconClose, IconCheck } from '../common/Icons'
import type { TeamData } from '../../types/team'

interface Agent {
  id: string
  key: string
  name: string
  emoji?: string
}

interface TeamCreateDialogProps {
  agents: Agent[]
  onClose: () => void
  onCreated: (team: TeamData) => void
}

export function TeamCreateDialog({ agents, onClose, onCreated }: TeamCreateDialogProps) {
  const { t } = useTranslation('teams')
  const [name, setName] = useState('')
  const [lead, setLead] = useState('')
  const [memberKeys, setMemberKeys] = useState<string[]>([])
  const [saving, setSaving] = useState(false)

  const agentOptions = agents.map((a) => ({ value: a.key, label: `${a.emoji || ''} ${a.name}`.trim() }))
  const memberOptions = agentOptions.filter((o) => o.value !== lead)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim() || !lead) return
    setSaving(true)
    try {
      const res = await teamService.create({
        name: name.trim(),
        lead,
        members: memberKeys.filter((k) => k !== lead),
      })
      toast.success(t('teamCreated', 'Team created'), name.trim())
      onCreated(res.team)
      onClose()
    } catch (err) {
      toast.error(t('createFailed', 'Failed to create team'), (err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const toggleMember = (key: string) => {
    setMemberKeys((prev) =>
      prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        onClick={(e) => e.stopPropagation()}
        className="bg-surface-primary border border-border rounded-xl shadow-xl w-full max-w-sm mx-4"
      >
        <div className="flex items-center justify-between p-4 border-b border-border">
          <h3 className="text-sm font-semibold text-text-primary">{t('createTeam', 'Create Team')}</h3>
          <button onClick={onClose} className="text-text-muted hover:text-text-primary cursor-pointer">
            <IconClose size={16} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="p-4 space-y-3">
          {/* Team name */}
          <div>
            <label className="text-xs text-text-muted mb-1 block">{t('teamName', 'Team name')} *</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('teamNamePlaceholder', 'My team...')}
              className="w-full bg-surface-secondary border border-border rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted text-base md:text-sm focus:outline-none focus:ring-1 focus:ring-accent"
              autoFocus
            />
          </div>

          {/* Lead agent */}
          <div>
            <label className="text-xs text-text-muted mb-1 block">{t('leadAgent', 'Lead agent')} *</label>
            <Combobox
              options={agentOptions}
              value={lead}
              onChange={setLead}
              placeholder={t('selectLead', 'Select lead agent...')}
            />
          </div>

          {/* Members — styled checkboxes, not native */}
          {lead && memberOptions.length > 0 && (
            <div>
              <label className="text-xs text-text-muted mb-1.5 block">{t('members', 'Members')}</label>
              <div className="space-y-1 max-h-[140px] overflow-y-auto overscroll-contain">
                {memberOptions.map((opt) => {
                  const checked = memberKeys.includes(opt.value)
                  return (
                    <button
                      key={opt.value}
                      type="button"
                      onClick={() => toggleMember(opt.value)}
                      className={[
                        'w-full flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-left transition-colors cursor-pointer',
                        checked
                          ? 'bg-accent/10 border border-accent/30'
                          : 'bg-surface-secondary border border-border hover:border-accent/20',
                      ].join(' ')}
                    >
                      {/* Custom checkbox */}
                      <div className={[
                        'w-4 h-4 rounded flex items-center justify-center shrink-0 transition-colors',
                        checked ? 'bg-accent' : 'border border-border bg-surface-tertiary',
                      ].join(' ')}>
                        {checked && <IconCheck size={10} className="text-white" />}
                      </div>
                      <span className={`text-xs font-medium ${checked ? 'text-accent' : 'text-text-secondary'}`}>
                        {opt.label}
                      </span>
                    </button>
                  )
                })}
              </div>
              {memberKeys.length > 0 && (
                <p className="text-[10px] text-text-muted mt-1 px-1">
                  {memberKeys.length} {t('selected', 'selected')}
                </p>
              )}
            </div>
          )}

          {/* Validation: need at least lead + 1 member */}
          {lead && memberKeys.length === 0 && memberOptions.length > 0 && (
            <p className="text-[10px] text-amber-500 px-1">
              {t('needMembers', 'Select at least 1 member for the team')}
            </p>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-2">
            <button type="button" onClick={onClose} className="text-xs text-text-muted hover:text-text-primary px-3 py-1.5 rounded cursor-pointer">
              {t('cancel', 'Cancel')}
            </button>
            <button
              type="submit"
              disabled={!name.trim() || !lead || memberKeys.length === 0 || saving}
              className="text-xs font-medium bg-accent text-white px-4 py-1.5 rounded-lg hover:bg-accent/90 transition-colors disabled:opacity-50 cursor-pointer"
            >
              {saving ? t('creating', 'Creating...') : t('create', 'Create')}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
