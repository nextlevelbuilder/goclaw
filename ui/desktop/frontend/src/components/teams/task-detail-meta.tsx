import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { MarkdownRenderer } from '../chat/MarkdownRenderer'
import { IconChevronDown, IconDocument, IconCheckCircle } from '../common/Icons'
import { Badge, StatusChip } from '../ui'
import { cn } from '../../lib/cn'
import { getApiClient } from '../../lib/api'
import { PRIORITY_BADGE, STATUS_TONE, isTaskLocked, TERMINAL_STATUSES } from '../../types/team'
import type { TeamTaskData, TeamMemberData, TeamTaskAttachment } from '../../types/team'

/** Human-readable file size */
export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/** Metadata label + value pair */
export function MetaItem({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="mb-0.5 text-caption text-ink-muted">{label}</dt>
      <dd className="text-ui font-medium text-ink">{children}</dd>
    </div>
  )
}

/** Collapsible bordered section */
export function CollapsibleSection({ title, icon, defaultOpen = true, children }: {
  title: string; icon: React.ReactNode; defaultOpen?: boolean; children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="rounded-md border border-border-subtle">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex w-full cursor-pointer items-center gap-2 px-4 py-3 text-ui font-medium text-ink-secondary transition-colors hover:text-ink focus-visible:outline-2 focus-visible:outline-focus-ring"
      >
        {icon}
        <span>{title}</span>
        <IconChevronDown className={cn('ml-auto transition-transform', !open && '-rotate-90')} />
      </button>
      {open && <div className="border-t border-border-subtle px-4 py-3">{children}</div>}
    </div>
  )
}

interface TaskDetailBodyProps {
  task: TeamTaskData
  members: TeamMemberData[]
  attachments: TeamTaskAttachment[]
}

/** Status, metadata, blockers, description, result and attachments of one task. */
export function TaskDetailBody({ task, members, attachments }: TaskDetailBodyProps) {
  const { t } = useTranslation('teams')

  const prio = PRIORITY_BADGE[task.priority] ?? PRIORITY_BADGE[3]
  const locked = isTaskLocked(task)
  const isTerminal = TERMINAL_STATUSES.has(task.status)
  const member = task.owner_agent_id ? members.find((m) => m.agent_id === task.owner_agent_id) : undefined

  return (
    <>
      {task.progress_percent != null && task.progress_percent > 0 && !isTerminal && (() => {
        const pct = Math.min(100, Math.max(0, task.progress_percent))
        return (
          <div className="space-y-1">
            <div className="flex justify-between text-caption text-ink-muted">
              <span>{t('progress')}</span>
              <span>{pct}%</span>
            </div>
            <div
              role="progressbar"
              aria-valuenow={pct}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-label={t('progress')}
              className="h-2 w-full overflow-hidden rounded-pill bg-surface-tertiary"
            >
              <div className="h-2 rounded-pill bg-accent transition-all duration-500" style={{ width: `${pct}%` }} />
            </div>
            {task.progress_step && <p className="text-caption text-ink-muted">{task.progress_step}</p>}
          </div>
        )
      })()}

      <div className="flex flex-wrap items-center gap-2">
        {task.identifier && <Badge tone="neutral">{task.identifier}</Badge>}
        <StatusChip tone={STATUS_TONE[task.status]} label={t(`board.status.${task.status}`)} />
        {locked && <StatusChip tone="success" label={t('board.running')} />}
      </div>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 rounded-md bg-surface-secondary p-4 sm:grid-cols-3">
        <MetaItem label={t('priority')}>
          <span className={cn('rounded-sm px-1.5 py-0.5 text-caption font-medium', prio.cls)}>{prio.label}</span>
        </MetaItem>
        <MetaItem label={t('owner')}>
          {member ? (
            <span className="flex items-center gap-1.5">
              {member.emoji && <span aria-hidden="true">{member.emoji}</span>}
              {member.display_name || member.agent_key}
            </span>
          ) : task.owner_agent_key || '—'}
        </MetaItem>
        {task.task_type && task.task_type !== 'general' && (
          <MetaItem label={t('board.type')}><Badge tone="neutral">{task.task_type}</Badge></MetaItem>
        )}
        {task.created_at && <MetaItem label={t('created')}>{new Date(task.created_at).toLocaleString()}</MetaItem>}
        {task.updated_at && <MetaItem label={t('updated')}>{new Date(task.updated_at).toLocaleString()}</MetaItem>}
      </dl>

      {task.blocked_by && task.blocked_by.length > 0 && (
        <div>
          <span className="text-caption text-ink-muted">{t('blockedBy')}</span>
          <div className="mt-1 flex flex-wrap gap-1.5">
            {task.blocked_by.map((id) => (
              <span key={id} className="rounded-sm bg-warning-subtle px-2 py-0.5 font-mono text-caption text-warning-text">
                {id.slice(0, 8)}
              </span>
            ))}
          </div>
        </div>
      )}

      {task.description && (
        <CollapsibleSection title={t('description')} icon={<IconDocument />}>
          <div className="prose prose-sm max-h-60 max-w-none overflow-y-auto text-ui text-ink-secondary">
            <MarkdownRenderer content={task.description} />
          </div>
        </CollapsibleSection>
      )}

      {task.result && (
        <CollapsibleSection title={t('result')} icon={<IconCheckCircle />}>
          <div className="prose prose-sm max-h-[40vh] max-w-none overflow-y-auto text-ui text-ink-secondary">
            <MarkdownRenderer content={task.result} />
          </div>
        </CollapsibleSection>
      )}

      {attachments.length > 0 && (
        <CollapsibleSection title={`${t('attachments')} (${attachments.length})`} icon={<IconDocument />}>
          <ul className="space-y-2">
            {attachments.map((a) => {
              const fileName = a.path?.split('/').pop() || 'file'
              const baseUrl = getApiClient()?.getBaseUrl() || ''
              const fullUrl = a.download_url?.startsWith('http') ? a.download_url : `${baseUrl}${a.download_url}`
              return (
                <li key={a.id} className="flex items-center gap-3 rounded-md border border-border-subtle bg-surface-secondary p-3">
                  <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-accent-subtle">
                    <IconDocument className="text-accent-subtle-ink" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-ui font-medium text-ink">{fileName}</p>
                    {a.file_size > 0 && <p className="text-caption text-ink-muted">{formatFileSize(a.file_size)}</p>}
                  </div>
                  {a.download_url && (
                    <a
                      href={fullUrl}
                      download
                      onClick={(e) => e.stopPropagation()}
                      className="shrink-0 rounded-button border border-border-control px-3 py-1.5 text-caption text-ink transition-colors hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-focus-ring"
                    >
                      {t('download')}
                    </a>
                  )}
                </li>
              )
            })}
          </ul>
        </CollapsibleSection>
      )}
    </>
  )
}
