import { useMemo, useState } from 'react'
import { useSessions } from '../../../hooks/use-sessions'
import { useUiStore } from '../../../stores/ui-store'
import { ConfirmDialog } from '../../common/ConfirmDialog'

function groupByDate(sessions: Array<{ key: string; title: string; lastMessageAt: number }>) {
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const yesterday = today - 86400000

  const groups: { label: string; items: typeof sessions }[] = [
    { label: 'Today', items: [] },
    { label: 'Yesterday', items: [] },
    { label: 'Older', items: [] },
  ]

  for (const s of sessions) {
    if (s.lastMessageAt >= today) groups[0].items.push(s)
    else if (s.lastMessageAt >= yesterday) groups[1].items.push(s)
    else groups[2].items.push(s)
  }

  return groups.filter((g) => g.items.length > 0)
}

export function SessionList() {
  const { sessions, activeSessionKey, setActiveSession, deleteSession } = useSessions()
  const activeView = useUiStore((s) => s.activeView)
  const closeSettings = useUiStore((s) => s.closeSettings)
  const [confirmKey, setConfirmKey] = useState<string | null>(null)

  const groups = useMemo(() => groupByDate(sessions), [sessions])

  if (sessions.length === 0) {
    return (
      <div className="px-3 py-6 text-center">
        <p className="text-xs text-text-muted">No conversations yet</p>
      </div>
    )
  }

  return (
    <>
      <ConfirmDialog
        open={!!confirmKey}
        onOpenChange={(open) => { if (!open) setConfirmKey(null) }}
        title="Delete conversation?"
        description="This action cannot be undone."
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={() => { if (confirmKey) { deleteSession(confirmKey); setConfirmKey(null) } }}
      />
      <div className="px-2 space-y-3">
        {groups.map((group) => (
          <div key={group.label}>
            <p className="text-[10px] uppercase tracking-wider text-text-muted px-1 mb-1">
              {group.label}
            </p>
            <div className="space-y-0.5">
              {group.items.map((session) => (
                <div
                  key={session.key}
                  className={[
                    'group flex items-center rounded-lg transition-colors',
                    activeSessionKey === session.key
                      ? 'bg-accent/10'
                      : 'hover:bg-surface-tertiary',
                  ].join(' ')}
                >
                  <button
                    onClick={() => { setActiveSession(session.key); if (activeView !== 'chat') closeSettings() }}
                    className={[
                      'flex-1 text-left px-2 py-1.5 text-xs truncate min-w-0',
                      activeSessionKey === session.key
                        ? 'text-accent font-medium'
                        : 'text-text-secondary hover:text-text-primary',
                    ].join(' ')}
                  >
                    {session.title}
                  </button>
                  <button
                    onClick={(e) => { e.stopPropagation(); setConfirmKey(session.key) }}
                    className="shrink-0 p-1 mr-1 rounded text-text-muted hover:text-error opacity-0 group-hover:opacity-100 transition-opacity"
                    title="Delete conversation"
                  >
                    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                      <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                    </svg>
                  </button>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  )
}
