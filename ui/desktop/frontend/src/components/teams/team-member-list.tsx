import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { IconClose, IconPlus, IconUser } from '../common/Icons'
import { Badge, Button, IconButton } from '../ui'
import type { TeamMemberData } from '../../types/team'

interface TeamMemberListProps {
  members: TeamMemberData[]
  sorted: TeamMemberData[]
  availableAgents: { value: string; label: string }[]
  showAdd: boolean
  addAgent: string
  adding: boolean
  removing: string | null
  /** Disables writes while the gateway is unreachable. */
  readOnly?: boolean
  onToggleAdd: () => void
  onAddAgentChange: (id: string) => void
  onAddMember: () => void
  onRemoveMember: (agentId: string) => void
}

/** Members of a team: role badge, add and remove (the lead and the last member cannot be removed). */
export function TeamMemberList({
  members, sorted, availableAgents,
  showAdd, addAgent, adding, removing, readOnly = false,
  onToggleAdd, onAddAgentChange, onAddMember, onRemoveMember,
}: TeamMemberListProps) {
  const { t } = useTranslation('teams')
  const nonLead = members.filter((x) => x.role !== 'lead').length

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h3 className="text-ui font-semibold text-ink">{t('members')} ({members.length})</h3>
        <Button size="sm" variant="ghost" icon={<IconPlus size={14} />} onClick={onToggleAdd} disabled={readOnly}>
          {t('settings.addMember')}
        </Button>
      </div>

      {showAdd && (
        <div className="flex gap-2">
          <div className="min-w-0 flex-1">
            <Combobox value={addAgent} onChange={onAddAgentChange} options={availableAgents} placeholder={t('settings.searchAgent')} allowCustom={false} />
          </div>
          <Button variant="secondary" onClick={onAddMember} disabled={!addAgent || readOnly} loading={adding}>
            {t('settings.add')}
          </Button>
        </div>
      )}

      <ul className="max-h-52 divide-y divide-border-subtle overflow-y-auto rounded-md border border-border-subtle">
        {sorted.map((m) => (
          <li key={m.agent_id} className="flex min-h-row items-center gap-3 px-3">
            {m.emoji ? <span aria-hidden="true" className="shrink-0 text-body">{m.emoji}</span> : <IconUser className="shrink-0 text-ink-muted" />}
            <span className="min-w-0 flex-1 truncate text-ui text-ink">{m.display_name || m.agent_key || m.agent_id.slice(0, 8)}</span>
            <Badge tone={m.role === 'lead' ? 'accent' : 'neutral'}>{m.role}</Badge>
            {m.role !== 'lead' && nonLead > 1 && (
              <IconButton
                size="sm"
                aria-label={t('common:remove')}
                icon={<IconClose size={14} />}
                onClick={() => onRemoveMember(m.agent_id)}
                loading={removing === m.agent_id}
                disabled={readOnly}
              />
            )}
          </li>
        ))}
        {members.length === 0 && (
          <li className="px-3 py-4 text-center text-caption text-ink-muted">{t('settings.noMembers')}</li>
        )}
      </ul>
    </section>
  )
}
