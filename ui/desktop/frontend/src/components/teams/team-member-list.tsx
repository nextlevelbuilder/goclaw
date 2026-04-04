import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { IconClose, IconPlus, IconUser, IconSpinner } from '../common/Icons'
import type { TeamMemberData } from '../../types/team'

const ROLE_COLORS: Record<string, string> = {
  lead: 'bg-amber-500/15 text-amber-600 dark:text-amber-400',
  reviewer: 'bg-orange-500/15 text-orange-600 dark:text-orange-400',
  member: 'bg-surface-tertiary text-text-muted',
}

interface TeamMemberListProps {
  members: TeamMemberData[]
  sorted: TeamMemberData[]
  availableAgents: { value: string; label: string }[]
  showAdd: boolean
  addAgent: string
  adding: boolean
  removing: string | null
  onToggleAdd: () => void
  onAddAgentChange: (id: string) => void
  onAddMember: () => void
  onRemoveMember: (agentId: string) => void
}

export function TeamMemberList({
  members, sorted, availableAgents,
  showAdd, addAgent, adding, removing,
  onToggleAdd, onAddAgentChange, onAddMember, onRemoveMember,
}: TeamMemberListProps) {
  const { t } = useTranslation('teams')

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-text-primary">{t('members', 'Members')} ({members.length})</h3>
        <button onClick={onToggleAdd} className="text-xs text-accent hover:text-accent/80 cursor-pointer flex items-center gap-1">
          <IconPlus size={12} />{t('settings.addMember', 'Add member')}
        </button>
      </div>

      {showAdd && (
        <div className="flex gap-2">
          <div className="flex-1 min-w-0">
            <Combobox value={addAgent} onChange={onAddAgentChange} options={availableAgents} placeholder={t('settings.searchAgent', 'Search agent...')} allowCustom={false} />
          </div>
          <button
            onClick={onAddMember}
            disabled={!addAgent || adding}
            className="shrink-0 px-3 py-1.5 text-xs font-medium bg-accent text-white rounded-lg hover:bg-accent/90 disabled:opacity-50 cursor-pointer"
          >
            {adding ? '...' : t('settings.add', 'Add')}
          </button>
        </div>
      )}

      <div className="rounded-lg border border-border divide-y divide-border max-h-[200px] overflow-y-auto">
        {sorted.map((m) => (
          <div key={m.agent_id} className="group flex items-center gap-3 px-3 py-2.5 hover:bg-surface-tertiary/50">
            {m.emoji ? <span className="text-base shrink-0">{m.emoji}</span> : <IconUser className="text-text-muted shrink-0" />}
            <span className="text-sm text-text-primary truncate flex-1">{m.display_name || m.agent_key || m.agent_id.slice(0, 8)}</span>
            <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded ${ROLE_COLORS[m.role] ?? ''}`}>{m.role}</span>
            {m.role !== 'lead' && members.filter((x) => x.role !== 'lead').length > 1 && (
              <button
                onClick={() => onRemoveMember(m.agent_id)}
                disabled={removing === m.agent_id}
                className="opacity-0 group-hover:opacity-100 text-text-muted hover:text-error cursor-pointer transition-opacity disabled:opacity-50"
              >
                {removing === m.agent_id ? <IconSpinner size={14} className="border-text-muted" /> : <IconClose size={14} />}
              </button>
            )}
          </div>
        ))}
        {members.length === 0 && (
          <div className="px-3 py-4 text-center text-xs text-text-muted">{t('settings.noMembers', 'No members')}</div>
        )}
      </div>
    </section>
  )
}
