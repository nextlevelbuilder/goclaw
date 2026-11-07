import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../../lib/cn'
import { useSessionStore } from '../../../stores/session-store'
import { useUiStore } from '../../../stores/ui-store'
import { IconClose } from '../../common/Icons'
import { ConfirmDialog, IconButton } from '../../ui'
import { openSession } from '../shell-actions'

/** How many recent conversations the sidebar lists; the Sessions view has the rest. */
export const RECENT_SESSION_LIMIT = 20

export interface SessionListProps {
  /** Delete handler from the shell's single useSessions() instance. */
  onDelete: (key: string) => Promise<void> | void
  onNavigate?: () => void
}

/** Recent conversations of the selected agent, read from the session store. */
export function SessionList({ onDelete, onNavigate }: SessionListProps) {
  const { t } = useTranslation('desktop')
  const sessions = useSessionStore((s) => s.sessions)
  const activeSessionKey = useSessionStore((s) => s.activeSessionKey)
  const activeView = useUiStore((s) => s.activeView)
  const [confirm, setConfirm] = useState<{ key: string; title: string } | null>(null)

  const recent = useMemo(
    () => [...sessions].sort((a, b) => b.lastMessageAt - a.lastMessageAt).slice(0, RECENT_SESSION_LIMIT),
    [sessions],
  )

  if (recent.length === 0) {
    return <p className="px-2 py-1.5 text-caption text-ink-muted">{t('nav.noSessions')}</p>
  }

  return (
    <>
      <ul className="flex flex-col gap-0.5">
        {recent.map((session) => {
          const active = activeView === 'chat' && activeSessionKey === session.key
          return (
            <li
              key={session.key}
              className={cn(
                'group flex h-[30px] items-center rounded-sm transition-colors',
                active ? 'bg-accent-subtle' : 'hover:bg-surface-hover',
              )}
            >
              <button
                type="button"
                onClick={() => { openSession(session.key); onNavigate?.() }}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'h-full min-w-0 flex-1 truncate rounded-sm px-2 text-left text-ui font-normal',
                  active ? 'text-accent-subtle-ink' : 'text-ink-secondary hover:text-ink',
                )}
              >
                {session.title}
              </button>
              <IconButton
                aria-label={t('nav.deleteSession')}
                icon={<IconClose size={12} />}
                size="sm"
                tooltipSide="right"
                onClick={() => setConfirm({ key: session.key, title: session.title })}
                className="mr-0.5 size-6 opacity-0 hover:text-danger focus-visible:opacity-100 group-hover:opacity-100"
              />
            </li>
          )
        })}
      </ul>
      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(open) => { if (!open) setConfirm(null) }}
        title={t('nav.deleteSessionTitle', { title: confirm?.title ?? '' })}
        description={t('nav.deleteSessionDesc')}
        confirmLabel={t('nav.deleteSession')}
        variant="destructive"
        onConfirm={() => {
          if (confirm) void onDelete(confirm.key)
          setConfirm(null)
        }}
      />
    </>
  )
}
