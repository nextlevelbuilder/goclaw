import { useTranslation } from 'react-i18next'
import { Badge, Menu, StatusChip, type MenuEntry } from '../ui'
import { IconChat, IconRefresh, IconTrash } from '../common/Icons'
import { AgentBadge } from './agent-badge'
import { agentDisplayName, agentEmoji, agentStatusView, agentSummary } from './agent-display'
import type { AgentData } from '../../types/agent'

interface AgentCardProps {
  agent: AgentData
  readOnly?: boolean
  onOpen: (agent: AgentData) => void
  onChat: (agent: AgentData) => void
  onResummon: (agent: AgentData) => void
  onDelete: (agent: AgentData) => void
}

/** Grid-view tile for one agent. */
export function AgentCard({ agent, readOnly = false, onOpen, onChat, onResummon, onDelete }: AgentCardProps) {
  const { t } = useTranslation('agents')
  const name = agentDisplayName(agent) ?? t('screen.list.unnamed')
  const status = agentStatusView(agent.status)
  const summary = agentSummary(agent)
  const selfEvolve = agent.agent_type === 'predefined' && Boolean(agent.self_evolve ?? agent.other_config?.self_evolve)

  const items: MenuEntry[] = [
    { label: t('screen.menu.open'), onSelect: () => onOpen(agent) },
    { label: t('screen.menu.chat'), icon: <IconChat size={14} />, onSelect: () => onChat(agent), disabled: agent.status !== 'active' },
  ]
  if (agent.status === 'summon_failed') {
    items.push({ label: t('screen.menu.resummon'), icon: <IconRefresh size={14} />, onSelect: () => onResummon(agent), disabled: readOnly })
  }
  items.push('separator', {
    label: t('screen.menu.delete'),
    icon: <IconTrash size={14} />,
    destructive: true,
    disabled: readOnly,
    onSelect: () => onDelete(agent),
  })

  return (
    <div className="relative flex flex-col gap-3 rounded-lg border border-border-subtle bg-surface-raised p-4 transition-colors hover:bg-surface-hover">
      <div className="flex items-center gap-3">
        <AgentBadge emoji={agentEmoji(agent)} />
        <div className="min-w-0 flex-1">
          <button
            type="button"
            onClick={() => onOpen(agent)}
            className="block max-w-full truncate text-left text-ui font-semibold text-ink after:absolute after:inset-0 after:content-['']"
          >
            {name}
          </button>
          <p className="truncate text-code text-ink-muted">{agent.model || '—'}</p>
        </div>
        <div className="relative z-10">
          <Menu items={items} label={t('screen.list.rowActions', { name })} />
        </div>
      </div>
      {summary && <p className="line-clamp-2 text-caption text-ink-muted">{summary}</p>}
      <div className="mt-auto flex flex-wrap items-center gap-1.5">
        <StatusChip tone={status.tone} label={t(`screen.status.${status.key}`)} />
        {agent.is_default && <Badge tone="accent">{t('screen.list.defaultBadge')}</Badge>}
        {agent.agent_type === 'predefined' && <Badge>{selfEvolve ? t('card.evolving') : t('card.static')}</Badge>}
      </div>
    </div>
  )
}
