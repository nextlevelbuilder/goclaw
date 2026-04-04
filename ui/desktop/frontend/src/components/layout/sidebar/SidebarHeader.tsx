import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAgents } from '../../../hooks/use-agents'
import { useUiStore } from '../../../stores/ui-store'
import { AgentAvatar } from '../../agents/AgentAvatar'
import { EditionCompareModal } from '../../common/EditionCompareModal'
import { wails } from '../../../lib/wails'

const MAX_AGENTS_LITE = 5

export function SidebarHeader() {
  const { t } = useTranslation('agents')
  const { agents, selectedAgent, selectAgent } = useAgents()
  const openSettings = useUiStore((s) => s.openSettings)
  const closeSettings = useUiStore((s) => s.closeSettings)
  const [editionOpen, setEditionOpen] = useState(false)
  const [version, setVersion] = useState('')

  useEffect(() => {
    wails.getVersion().then(setVersion).catch(() => {})
  }, [])

  const atLimit = agents.length >= MAX_AGENTS_LITE

  return (
    <div className="pt-6 px-3 pb-2 space-y-2">
      {/* Logo + version */}
      <div className="flex items-center gap-2.5 px-1">
        <img src="/goclaw-icon.svg" alt="GoClaw" className="h-7 w-7" />
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-base font-semibold text-text-primary">GoClaw</span>
            <button
              onClick={() => setEditionOpen(true)}
              className="text-[10px] font-medium bg-accent/15 text-accent px-1.5 py-0.5 rounded hover:bg-accent/25 transition-colors cursor-pointer"
            >
              Lite{version ? ` - v${version}` : ''}
            </button>
          </div>
        </div>
      </div>

      {/* Section title + add button */}
      <div className="flex items-center justify-between px-1 pt-1">
        <span className="text-[10px] text-text-muted font-medium tracking-wide">
          {t('title', 'Agents')} ({agents.length}/{MAX_AGENTS_LITE})
        </span>
        <button
          onClick={() => openSettings('agents')}
          disabled={atLimit}
          className="w-5 h-5 flex items-center justify-center rounded text-text-muted hover:text-accent hover:bg-surface-tertiary transition-colors disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
          title={atLimit ? t('limitReached', 'Agent limit reached ({{max}})', { max: MAX_AGENTS_LITE }) : t('createAgent', 'New agent')}
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round">
            <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
          </svg>
        </button>
      </div>

      {/* Agent list */}
      <div className="space-y-1">
        {agents.map((agent) => (
          <button
            key={agent.id}
            onClick={() => { selectAgent(agent.id); closeSettings() }}
            className={[
              'w-full flex items-center gap-2 px-2 py-1.5 rounded-lg text-left transition-colors',
              selectedAgent?.id === agent.id
                ? 'bg-accent/10 text-accent'
                : 'text-text-secondary hover:bg-surface-tertiary hover:text-text-primary',
            ].join(' ')}
          >
            <AgentAvatar name={agent.name} status={agent.status} size="sm" emoji={agent.emoji} />
            <span className="truncate flex-1 text-xs font-medium">{agent.name}</span>
          </button>
        ))}
      </div>

      {/* Edition comparison modal */}
      <EditionCompareModal open={editionOpen} onClose={() => setEditionOpen(false)} />
    </div>
  )
}
