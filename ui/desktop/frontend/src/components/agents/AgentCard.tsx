import { useTranslation } from 'react-i18next'
import type { AgentData } from '../../types/agent'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

interface AgentCardProps {
  agent: AgentData
  onEdit: (agent: AgentData) => void
  onDelete: (agent: AgentData) => void
  onResummon: (agent: AgentData) => void
}

export function AgentCard({ agent, onEdit, onDelete, onResummon }: AgentCardProps) {
  const { t } = useTranslation('agents')
  const displayName = agent.display_name
    || (UUID_RE.test(agent.agent_key) ? 'Unnamed Agent' : agent.agent_key)
  const otherCfg = (agent.other_config ?? {}) as Record<string, unknown>
  const selfEvolve = agent.agent_type === 'predefined' && Boolean(otherCfg.self_evolve)
  const emoji = typeof otherCfg.emoji === 'string' ? otherCfg.emoji : ''
  const showSubtitle = agent.display_name && !UUID_RE.test(agent.agent_key)
  const isSummoning = agent.status === 'summoning'
  const isFailed = agent.status === 'summon_failed'

  return (
    <button
      type="button"
      onClick={() => { if (!isSummoning) onEdit(agent) }}
      className="flex cursor-pointer flex-col gap-3 rounded-lg border border-border bg-surface-secondary p-4 text-left transition-all hover:border-accent/30 hover:shadow-md"
    >
      {/* Top row: icon + name + status */}
      <div className="flex items-center gap-3">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent/10 text-accent">
          {emoji ? <span className="text-lg leading-none">{emoji}</span> : (
            <svg className="h-4.5 w-4.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 8V4H8" /><rect width="16" height="12" x="4" y="8" rx="2" /><path d="M2 14h2" /><path d="M20 14h2" /><path d="M15 13v2" /><path d="M9 13v2" />
            </svg>
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-semibold text-text-primary">{displayName}</span>
            {agent.is_default && (
              <svg className="h-3.5 w-3.5 shrink-0 fill-amber-400 text-amber-400" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" />
              </svg>
            )}
          </div>
          {showSubtitle && (
            <div className="truncate text-xs text-text-muted">{agent.agent_key}</div>
          )}
        </div>
        {/* Status badge — colors match web UI badge variants */}
        {isSummoning ? (
          <span className="shrink-0 animate-pulse rounded-full border border-orange-500/25 bg-orange-500/15 px-2 py-0.5 text-[11px] font-medium text-orange-700 dark:text-orange-400 dark:bg-orange-500/10 dark:border-orange-500/20">
            Summoning
          </span>
        ) : isFailed ? (
          <span className="shrink-0 rounded-full bg-error px-2 py-0.5 text-[11px] font-medium text-white dark:bg-error/60">
            Failed
          </span>
        ) : (
          <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium ${
            agent.status === 'active'
              ? 'bg-emerald-500/15 text-emerald-700 border border-emerald-500/25 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/20'
              : 'bg-surface-tertiary text-text-secondary'
          }`}>
            {agent.status}
          </span>
        )}
      </div>

      {/* Model info */}
      {agent.provider || agent.model ? (
        <div className="truncate text-xs text-text-muted">
          {[agent.provider, agent.model].filter(Boolean).join(' / ')}
        </div>
      ) : null}

      {/* Expertise/frontmatter */}
      {(agent.frontmatter || otherCfg.description) ? (
        <div className="line-clamp-3 text-xs text-text-muted/70">
          {String(agent.frontmatter || otherCfg.description || '')}
        </div>
      ) : null}

      {/* Bottom badges + actions */}
      <div className="flex items-center gap-1.5 flex-wrap">
        <span className="rounded-full border border-border px-2 py-0.5 text-[11px] text-text-primary">
          {agent.agent_type}
        </span>
        {agent.agent_type === 'predefined' && (
          <span className={`rounded-full px-2 py-0.5 text-[11px] flex items-center gap-0.5 ${
            selfEvolve
              ? 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-300'
              : 'border border-border text-text-muted'
          }`}>
            <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z" />
            </svg>
            {selfEvolve ? t('card.evolving') : t('card.static')}
          </span>
        )}
        {agent.context_window > 0 && (
          <span className="text-[11px] text-text-muted">
            {(agent.context_window / 1000).toFixed(0)}K ctx
          </span>
        )}

        {/* Actions */}
        {isFailed && (
          <button
            onClick={(e) => { e.stopPropagation(); onResummon(agent) }}
            className="ml-auto rounded border border-border px-2 py-0.5 text-[11px] text-text-secondary hover:bg-surface-tertiary transition-colors flex items-center gap-1"
          >
            <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" /><path d="M21 3v5h-5" />
              <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" /><path d="M3 21v-5h5" />
            </svg>
            Resummon
          </button>
        )}
        <button
          onClick={(e) => { e.stopPropagation(); onDelete(agent) }}
          className={`rounded px-2 py-0.5 text-[11px] text-text-muted hover:text-error transition-colors flex items-center gap-1 ${isFailed ? '' : 'ml-auto'}`}
        >
          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
          </svg>
          Delete
        </button>
      </div>
    </button>
  )
}
