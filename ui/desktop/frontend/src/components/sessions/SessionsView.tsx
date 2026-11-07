import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useSessionStore } from '../../stores/session-store'
import { IconChat, IconPlus } from '../common/Icons'
import { openSession, startNewChat } from '../layout/shell-actions'
import { Button, EmptyState, PageHeader } from '../ui'

/** Interim Sessions view: the selected agent's conversations from the session store. */
export function SessionsView() {
  const { t } = useTranslation('desktop')
  const sessions = useSessionStore((s) => s.sessions)
  const sorted = useMemo(() => [...sessions].sort((a, b) => b.lastMessageAt - a.lastMessageAt), [sessions])

  const newChat = (
    <Button variant="primary" icon={<IconPlus size={16} />} onClick={startNewChat}>
      {t('nav.newChat')}
    </Button>
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader title={t('nav.views.sessions')} description={t('stub.sessionsDesc')} actions={newChat} />
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4">
        {sorted.length === 0 ? (
          <EmptyState icon={<IconChat size={20} />} title={t('nav.noSessions')} description={t('stub.sessionsEmpty')} />
        ) : (
          <ul className="flex flex-col">
            {sorted.map((s) => (
              <li key={s.key} className="border-b border-border-subtle last:border-b-0">
                <button
                  type="button"
                  onClick={() => openSession(s.key)}
                  className="flex h-row w-full items-center gap-3 rounded-sm px-2 text-left text-ui text-ink transition-colors hover:bg-surface-hover"
                >
                  <IconChat size={16} className="shrink-0 text-ink-muted" />
                  <span className="min-w-0 flex-1 truncate">{s.title}</span>
                  <span className="shrink-0 text-caption text-ink-muted">{new Date(s.lastMessageAt).toLocaleString()}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
