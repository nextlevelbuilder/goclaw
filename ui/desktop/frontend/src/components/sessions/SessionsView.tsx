import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSessionStore, type Session } from '../../stores/session-store'
import { useAgentStore } from '../../stores/agent-store'
import { deleteSessionByKey, renameSessionByKey } from '../../hooks/chat-session-actions'
import { dayGroup, relativeTime, type DayGroup } from '../../utils/chat-time'
import { cn } from '../../lib/cn'
import { IconChat, IconPlus, IconSearch } from '../common/Icons'
import { openSession, startNewChat } from '../layout/shell-actions'
import { useGatewayStatus } from '../layout/use-gateway-status'
import {
  Button, ConfirmDialog, Dialog, DialogFooter, EmptyState, ErrorState, Field, Input, Menu, PageHeader,
  Select, SkeletonList, toast,
} from '../ui'
import { SessionPreview } from './SessionPreview'

const GROUPS: DayGroup[] = ['today', 'yesterday', 'earlier']

/** Sessions: the selected agent's conversations, with a read-only preview and row actions. */
export function SessionsView() {
  const { t, i18n } = useTranslation('sessions')
  const sessions = useSessionStore((s) => s.sessions)
  const status = useSessionStore((s) => s.status)
  const loadError = useSessionStore((s) => s.error)
  const reload = useSessionStore((s) => s.reload)
  const agents = useAgentStore((s) => s.agents)
  const selectedAgentId = useAgentStore((s) => s.selectedAgentId)
  const selectAgent = useAgentStore((s) => s.selectAgent)
  const { disconnected } = useGatewayStatus()

  const [query, setQuery] = useState('')
  const [previewKey, setPreviewKey] = useState<string | null>(null)
  const [previewNonce, setPreviewNonce] = useState(0)
  const [toDelete, setToDelete] = useState<Session | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [toRename, setToRename] = useState<Session | null>(null)
  const [name, setName] = useState('')
  const [nameError, setNameError] = useState(false)
  const [saving, setSaving] = useState(false)

  const filtered = useMemo(() => {
    const q = query.trim().toLocaleLowerCase()
    return [...sessions]
      .filter((s) => !q || s.title.toLocaleLowerCase().includes(q))
      .sort((a, b) => b.lastMessageAt - a.lastMessageAt)
  }, [sessions, query])

  const grouped = useMemo(() => {
    const now = Date.now()
    return GROUPS.map((g) => ({ group: g, items: filtered.filter((s) => dayGroup(s.lastMessageAt, now) === g) })).filter(
      (g) => g.items.length > 0,
    )
  }, [filtered])

  const selected = sessions.find((s) => s.key === previewKey) ?? null
  const loading = status === 'idle' || status === 'loading'

  const confirmDelete = async () => {
    if (!toDelete) return
    setDeleting(true)
    try {
      await deleteSessionByKey(toDelete.key)
      if (previewKey === toDelete.key) setPreviewKey(null)
      toast.success(t('toast.deleted'))
      setToDelete(null)
    } catch {
      toast.error(t('toast.deleteFailed'))
    } finally {
      setDeleting(false)
    }
  }

  const startRename = (s: Session) => {
    setToRename(s)
    setName(s.title)
    setNameError(false)
  }

  const saveRename = async () => {
    if (!toRename) return
    const label = name.trim()
    if (!label) {
      setNameError(true)
      return
    }
    setSaving(true)
    try {
      await renameSessionByKey(toRename.key, label)
      toast.success(t('toast.updated'))
      setToRename(null)
    } catch {
      toast.error(t('toast.updateFailed'))
    } finally {
      setSaving(false)
    }
  }

  const header = (
    <PageHeader
      title={t('title')}
      description={status === 'ready' ? (sessions.length > 0 ? t('count', { count: sessions.length }) : t('countNone')) : undefined}
      actions={(
        <>
          {agents.length > 0 && (
            <div className="w-44">
              <Select
                aria-label={t('filterAgent')}
                value={selectedAgentId ?? ''}
                onChange={(e) => selectAgent(e.target.value)}
                options={agents.map((a) => ({ value: a.id, label: a.name }))}
              />
            </div>
          )}
          <Button variant="primary" icon={<IconPlus size={16} />} onClick={startNewChat}>
            {t('newChat')}
          </Button>
        </>
      )}
    />
  )

  if (status === 'error') {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {header}
        <ErrorState title={t('loadErrorTitle')} description={t('loadErrorHint')} code={loadError ?? undefined} onRetry={reload} />
      </div>
    )
  }

  if (!loading && sessions.length === 0) {
    return (
      <div className={cn('flex min-h-0 flex-1 flex-col', disconnected && 'opacity-60')}>
        {header}
        <div className="flex flex-1 items-center justify-center">
          <EmptyState
            icon={<IconChat size={20} />}
            title={t('firstTitle')}
            description={`${t('firstBody')} ${t('localHint')}`}
            action={<Button variant="secondary" onClick={startNewChat}>{agents.length > 0 ? t('newChat') : t('chooseAgent')}</Button>}
          />
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {header}
      <div className={cn('flex min-h-0 flex-1', disconnected && 'opacity-60')}>
        <div className="flex w-80 shrink-0 flex-col border-r border-border-subtle">
          <div className="relative p-3">
            <IconSearch size={14} className="pointer-events-none absolute left-5 top-1/2 -translate-y-1/2 text-ink-muted" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('search')}
              aria-label={t('search')}
              className="pl-8"
            />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-3">
            {loading ? (
              <SkeletonList rows={6} avatar={false} />
            ) : filtered.length === 0 ? (
              <EmptyState title={t('noMatchTitle')} description={t('noMatchDescription')} />
            ) : (
              grouped.map(({ group, items }) => (
                <section key={group} className="mb-2">
                  <h2 className="px-2 py-1.5 text-overline text-ink-muted">{t(`group.${group}`)}</h2>
                  <ul>
                    {items.map((s) => (
                      <li key={s.key} className="flex items-center gap-1">
                        <button
                          type="button"
                          aria-current={s.key === previewKey ? 'true' : undefined}
                          onClick={() => setPreviewKey(s.key)}
                          onDoubleClick={() => openSession(s.key)}
                          className={cn(
                            'flex min-w-0 flex-1 flex-col gap-0.5 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-surface-hover',
                            s.key === previewKey && 'bg-surface-hover',
                          )}
                        >
                          <span className="truncate text-ui text-ink">{s.title}</span>
                          <span className="truncate text-caption text-ink-muted">
                            {t('messages', { count: s.messageCount })} · {relativeTime(s.lastMessageAt, i18n.language)}
                          </span>
                        </button>
                        <Menu
                          label={t('openMenu')}
                          items={[
                            { label: t('continueChat'), onSelect: () => openSession(s.key) },
                            { label: t('rename'), onSelect: () => startRename(s), disabled: disconnected },
                            'separator',
                            { label: t('delete'), destructive: true, onSelect: () => setToDelete(s), disabled: disconnected },
                          ]}
                        />
                      </li>
                    ))}
                  </ul>
                </section>
              ))
            )}
          </div>
        </div>

        <div className="flex min-w-0 flex-1 flex-col">
          {selected && (
            <div className="flex shrink-0 items-center justify-between gap-3 border-b border-border-subtle px-6 py-2">
              <p className="min-w-0 truncate text-ui font-semibold text-ink">{selected.title}</p>
              <div className="flex shrink-0 gap-2">
                <Button size="sm" variant="secondary" onClick={() => startRename(selected)} disabled={disconnected}>
                  {t('rename')}
                </Button>
                <Button size="sm" variant="primary" onClick={() => openSession(selected.key)}>
                  {t('continueChat')}
                </Button>
              </div>
            </div>
          )}
          <div className="flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain">
            <SessionPreview
              sessionKey={selected ? selected.key : null}
              nonce={previewNonce}
              onRetry={() => setPreviewNonce((n) => n + 1)}
            />
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={toDelete !== null}
        onOpenChange={(o) => { if (!o) setToDelete(null) }}
        title={t('deleteConfirmTitle', { title: toDelete?.title ?? '' })}
        description={t('deleteConfirmBody')}
        confirmLabel={t('delete')}
        cancelLabel={t('cancel')}
        variant="destructive"
        loading={deleting}
        onConfirm={confirmDelete}
      />

      <Dialog
        open={toRename !== null}
        onOpenChange={(o) => { if (!o) setToRename(null) }}
        title={t('renameTitle')}
        size="sm"
        dismissible={!saving}
        footer={(
          <DialogFooter>
            <Button variant="secondary" onClick={() => setToRename(null)} disabled={saving}>{t('cancel')}</Button>
            <Button variant="primary" onClick={saveRename} loading={saving}>{t('save')}</Button>
          </DialogFooter>
        )}
      >
        <Field label={t('nameLabel')} error={nameError ? t('nameRequired') : undefined}>
          <Input
            value={name}
            onChange={(e) => { setName(e.target.value); setNameError(false) }}
            onKeyDown={(e) => { if (e.key === 'Enter') void saveRename() }}
            autoFocus
          />
        </Field>
      </Dialog>
    </div>
  )
}
