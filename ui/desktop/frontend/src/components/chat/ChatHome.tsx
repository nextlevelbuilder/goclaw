import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAgents } from '../../hooks/use-agents'
import { useSessionStore } from '../../stores/session-store'
import { useUiStore } from '../../stores/ui-store'
import { relativeTime } from '../../utils/chat-time'
import { IconBot, IconChat, IconChevronDown } from '../common/Icons'
import { Button, EmptyState, Menu, Skeleton } from '../ui'
import { chatWithAgent, openSession } from '../layout/shell-actions'
import { InputBar, type AttachedFile } from './InputBar'
import { ModelPicker } from './ModelPicker'

const SHORTCUT_LIMIT = 3
/** How long to wait for the agent list before concluding there are none. */
const AGENTS_SETTLE_MS = 1500

interface ChatHomeProps {
  onSend: (text: string, files?: AttachedFile[]) => void
  disabled?: boolean
}

/** Home: greeting, composer, then recent conversations and agents as quiet shortcuts. */
export function ChatHome({ onSend, disabled }: ChatHomeProps) {
  const { t, i18n } = useTranslation('chat')
  const { agents, selectedAgent, selectAgent } = useAgents()
  const sessions = useSessionStore((s) => s.sessions)
  const setActiveView = useUiStore((s) => s.setActiveView)
  const [settled, setSettled] = useState(false)

  useEffect(() => {
    const id = setTimeout(() => setSettled(true), AGENTS_SETTLE_MS)
    return () => clearTimeout(id)
  }, [])

  const recent = useMemo(
    () => [...sessions].sort((a, b) => b.lastMessageAt - a.lastMessageAt).slice(0, SHORTCUT_LIMIT),
    [sessions],
  )
  const shortcutAgents = useMemo(() => {
    const rest = agents.filter((a) => a.id !== selectedAgent?.id)
    return (selectedAgent ? [selectedAgent, ...rest] : rest).slice(0, SHORTCUT_LIMIT)
  }, [agents, selectedAgent])

  if (agents.length === 0 && settled) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <EmptyState
          icon={<IconBot size={20} />}
          title={t('state.noAgents')}
          description={t('state.noAgentsHint')}
          action={<Button variant="primary" onClick={() => setActiveView('agents')}>{t('state.openAgents')}</Button>}
        />
      </div>
    )
  }

  const agentPill = selectedAgent && (
    <Menu
      align="start"
      side="top"
      items={agents.map((a) => ({ label: a.name, onSelect: () => selectAgent(a.id) }))}
      trigger={
        <button
          type="button"
          className="inline-flex h-7 max-w-44 items-center gap-1.5 rounded-pill border border-border-control px-3 text-caption font-medium text-ink-secondary transition-colors hover:bg-surface-hover"
        >
          <span className="truncate">{selectedAgent.name}</span>
          <IconChevronDown size={12} className="shrink-0 text-ink-muted" />
        </button>
      }
    />
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-8 overflow-y-auto overscroll-contain px-6 pb-12">
      <h1 className="text-center text-display text-ink">{t('home.greeting')}</h1>

      <div className="w-full max-w-reading">
        <InputBar
          autoFocus
          onSend={onSend}
          disabled={disabled || !selectedAgent}
          placeholder={selectedAgent ? t('home.placeholder') : t('composer.placeholderNoAgent')}
          pills={
            <>
              {agentPill}
              <ModelPicker agentModel={selectedAgent?.model} disabled={disabled || !selectedAgent} />
            </>
          }
        />
      </div>

      {agents.length === 0 && !settled && (
        <div className="w-full max-w-reading" role="status" aria-label={t('state.loadingAgents')}>
          <Skeleton className="h-16 w-full rounded-lg" />
        </div>
      )}

      {recent.length > 0 && (
        <ShortcutGroup label={t('home.recent')}>
          {recent.map((s) => (
            <Shortcut
              key={s.key}
              icon={<IconChat size={16} />}
              title={s.title}
              detail={`${t('home.messages', { count: s.messageCount })} · ${relativeTime(s.lastMessageAt, i18n.language)}`}
              onClick={() => openSession(s.key)}
            />
          ))}
        </ShortcutGroup>
      )}

      {shortcutAgents.length > 0 && (
        <ShortcutGroup label={t('home.agents')}>
          {shortcutAgents.map((a) => (
            <Shortcut key={a.id} icon={<IconBot size={16} />} title={a.name} detail={a.model} mono onClick={() => chatWithAgent(a.id)} />
          ))}
        </ShortcutGroup>
      )}
    </div>
  )
}

function ShortcutGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section className="w-full max-w-reading">
      <h2 className="mb-2 text-overline text-ink-muted">{label}</h2>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">{children}</div>
    </section>
  )
}

function Shortcut({ icon, title, detail, mono, onClick }: { icon: React.ReactNode; title: string; detail: string; mono?: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex min-w-0 flex-col gap-1 rounded-lg border border-border-subtle bg-surface-raised p-4 text-left transition-colors hover:bg-surface-hover"
    >
      <span className="flex min-w-0 items-center gap-2 text-ui font-semibold text-ink">
        <span className="shrink-0 text-ink-muted">{icon}</span>
        <span className="truncate">{title}</span>
      </span>
      <span className={mono ? 'truncate font-mono text-caption text-ink-muted' : 'truncate text-caption text-ink-muted'}>{detail}</span>
    </button>
  )
}
