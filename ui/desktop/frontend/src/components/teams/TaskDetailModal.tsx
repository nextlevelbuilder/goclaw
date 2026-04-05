import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { MarkdownRenderer } from '../chat/MarkdownRenderer'
import { Combobox } from '../common/Combobox'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { IconClose, IconChevronDown, IconDocument, IconCheckCircle, IconTrash } from '../common/Icons'
import { getApiClient } from '../../lib/api'
import { STATUS_BADGE, PRIORITY_BADGE, isTaskLocked, TERMINAL_STATUSES } from '../../types/team'
import type { TeamTaskData, TeamMemberData, TeamTaskAttachment } from '../../types/team'

interface TaskDetailModalProps {
  task: TeamTaskData
  members: TeamMemberData[]
  onClose: () => void
  onAssign: (taskId: string, agentKey: string) => Promise<unknown>
  onDelete: (taskId: string) => Promise<void>
  onFetchDetail?: (teamId: string, taskId: string) => Promise<{ task: TeamTaskData; attachments: TeamTaskAttachment[] } | null>
}

/** Metadata label + value pair */
function MetaItem({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-text-muted mb-0.5">{label}</dt>
      <dd className="text-sm font-medium text-text-primary">{children}</dd>
    </div>
  )
}

/** Collapsible bordered section — matches web UI TaskDetailContent pattern */
function CollapsibleSection({ title, icon, defaultOpen = true, children }: {
  title: string; icon: React.ReactNode; defaultOpen?: boolean; children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="rounded-lg border border-border">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-4 py-3 text-sm font-medium text-text-muted hover:text-text-primary transition-colors cursor-pointer"
      >
        {icon}
        <span>{title}</span>
        <IconChevronDown className={`ml-auto transition-transform ${open ? '' : '-rotate-90'}`} />
      </button>
      {open && (
        <div className="border-t border-border px-4 py-3">
          {children}
        </div>
      )}
    </div>
  )
}

/** Format bytes to human-readable size */
function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function TaskDetailModal({ task, members, onClose, onAssign, onDelete, onFetchDetail }: TaskDetailModalProps) {
  const { t } = useTranslation('teams')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([])

  // Fetch full detail (with attachments) on mount
  useEffect(() => {
    if (!onFetchDetail) return
    onFetchDetail(task.team_id, task.id).then((res) => {
      if (res) setAttachments(res.attachments)
    })
  }, [task.id, task.team_id, onFetchDetail])

  const prio = PRIORITY_BADGE[task.priority] ?? PRIORITY_BADGE[3]
  const statusCls = STATUS_BADGE[task.status] ?? ''
  const locked = isTaskLocked(task)
  const isTerminal = TERMINAL_STATUSES.has(task.status)
  const member = task.owner_agent_id ? members.find((m) => m.agent_id === task.owner_agent_id) : undefined

  const memberOptions = members.map((m) => ({
    value: m.agent_key || m.agent_id,
    label: `${m.emoji || ''} ${m.display_name || m.agent_key || m.agent_id}`.trim(),
  }))

  const handleDelete = async () => {
    try {
      await onDelete(task.id)
      onClose()
    } finally {
      setConfirmDelete(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        onClick={(e) => e.stopPropagation()}
        className="bg-surface-primary border border-border rounded-xl shadow-xl w-[95vw] max-w-4xl max-h-[85vh] flex flex-col mx-4"
      >
        {/* ── Header: badges + title ── */}
        <div className="px-6 pt-5 pb-4 border-b border-border shrink-0">
          <div className="flex items-start gap-3">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-2">
                {task.identifier && (
                  <span className="text-xs font-mono text-text-muted bg-surface-tertiary px-2 py-0.5 rounded border border-border">
                    {task.identifier}
                  </span>
                )}
                <span className={`text-xs font-medium px-2 py-0.5 rounded capitalize ${statusCls}`}>
                  {task.status.replace(/_/g, ' ')}
                </span>
                {locked && (
                  <span className="text-xs font-medium px-2 py-0.5 rounded bg-green-500/15 text-green-600 dark:text-green-400 animate-pulse">
                    Running
                  </span>
                )}
              </div>
              <h3 className="text-base font-semibold text-text-primary leading-snug sm:text-lg">{task.subject}</h3>
            </div>
            <button onClick={onClose} className="text-text-muted hover:text-text-primary p-1.5 cursor-pointer shrink-0 rounded-lg hover:bg-surface-tertiary">
              <IconClose />
            </button>
          </div>
        </div>

        {/* ── Scrollable body ── */}
        <div className="flex-1 overflow-y-auto overscroll-contain space-y-4 px-6 py-4">

          {/* Progress bar */}
          {task.progress_percent != null && task.progress_percent > 0 && !isTerminal && (() => {
            const pct = Math.min(100, Math.max(0, task.progress_percent))
            return (
              <div className="space-y-1">
                <div className="flex justify-between text-xs text-text-muted">
                  <span>{t('progress', 'Progress')}</span>
                  <span>{pct}%</span>
                </div>
                <div className="h-2 w-full rounded-full bg-surface-tertiary overflow-hidden">
                  <div className="h-2 rounded-full bg-accent transition-all duration-500" style={{ width: `${pct}%` }} />
                </div>
                {task.progress_step && <p className="text-xs text-text-muted">{task.progress_step}</p>}
              </div>
            )
          })()}

          {/* Metadata grid */}
          <dl className="grid grid-cols-2 sm:grid-cols-3 gap-x-6 gap-y-3 rounded-lg bg-surface-tertiary/30 p-4">
            <MetaItem label={t('priority', 'Priority')}>
              <span className={`inline-flex items-center gap-1.5 capitalize`}>
                <span className={`px-1.5 py-0.5 rounded text-xs font-medium ${prio.cls}`}>{prio.label}</span>
              </span>
            </MetaItem>
            <MetaItem label={t('owner', 'Owner')}>
              {member ? (
                <span className="flex items-center gap-1.5">
                  {member.emoji && <span className="text-base">{member.emoji}</span>}
                  {member.display_name || member.agent_key}
                </span>
              ) : task.owner_agent_key || '—'}
            </MetaItem>
            {task.task_type && task.task_type !== 'general' && (
              <MetaItem label={t('type', 'Type')}>
                <span className="text-xs bg-surface-tertiary border border-border px-2 py-0.5 rounded">{task.task_type}</span>
              </MetaItem>
            )}
            {task.created_at && (
              <MetaItem label={t('created', 'Created')}>
                {new Date(task.created_at).toLocaleString()}
              </MetaItem>
            )}
            {task.updated_at && (
              <MetaItem label={t('updated', 'Updated')}>
                {new Date(task.updated_at).toLocaleString()}
              </MetaItem>
            )}
          </dl>

          {/* Blocked by */}
          {task.blocked_by && task.blocked_by.length > 0 && (
            <div>
              <span className="text-xs text-text-muted">{t('blockedBy', 'Blocked by')}</span>
              <div className="mt-1 flex flex-wrap gap-1.5">
                {task.blocked_by.map((id) => (
                  <span key={id} className="text-xs font-mono bg-amber-500/15 text-amber-600 dark:text-amber-400 px-2 py-0.5 rounded border border-amber-500/20">
                    {id.slice(0, 8)}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Description section */}
          {task.description && (
            <CollapsibleSection
              title={t('description', 'Description')}
              icon={<IconDocument />}
            >
              <div className="text-sm text-text-secondary prose prose-sm dark:prose-invert max-w-none max-h-60 overflow-y-auto">
                <MarkdownRenderer content={task.description} />
              </div>
            </CollapsibleSection>
          )}

          {/* Result section */}
          {task.result && (
            <CollapsibleSection
              title={t('result', 'Result')}
              icon={<IconCheckCircle />}
            >
              <div className="text-sm text-text-secondary prose prose-sm dark:prose-invert max-w-none max-h-[40vh] overflow-y-auto">
                <MarkdownRenderer content={task.result} />
              </div>
            </CollapsibleSection>
          )}

          {/* Attachments section */}
          {attachments.length > 0 && (
            <CollapsibleSection
              title={`${t('attachments', 'Attachments')} (${attachments.length})`}
              icon={<IconDocument />}
            >
              <div className="space-y-2">
                {attachments.map((a) => {
                  const fileName = a.path?.split('/').pop() || 'file'
                  const baseUrl = getApiClient()?.getBaseUrl() || ''
                  const fullUrl = a.download_url?.startsWith('http') ? a.download_url : `${baseUrl}${a.download_url}`
                  return (
                    <div key={a.id} className="flex items-center gap-3 rounded-lg border border-border bg-surface-tertiary/30 p-3">
                      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent/10">
                        <IconDocument className="text-accent" />
                      </div>
                      <div className="flex-1 min-w-0">
                        <p className="truncate text-sm font-medium text-text-primary">{fileName}</p>
                        {a.file_size > 0 && (
                          <p className="text-xs text-text-muted">{formatFileSize(a.file_size)}</p>
                        )}
                      </div>
                      {a.download_url && (
                        <a
                          href={fullUrl}
                          download
                          onClick={(e) => e.stopPropagation()}
                          className="shrink-0 text-xs text-accent hover:text-accent/80 px-3 py-1.5 rounded-lg border border-accent/30 hover:bg-accent/10 transition-colors cursor-pointer"
                        >
                          {t('download', 'Download')}
                        </a>
                      )}
                    </div>
                  )
                })}
              </div>
            </CollapsibleSection>
          )}
        </div>

        {/* ── Footer ── */}
        <div className="flex items-center gap-3 px-6 py-3 border-t border-border shrink-0">
          {!isTerminal && (
            <div className="max-w-[240px]">
              <Combobox
                options={memberOptions}
                value={member?.agent_key || task.owner_agent_key || ''}
                onChange={(key) => onAssign(task.id, key)}
                placeholder={t('assignTo', 'Assign to...')}
              />
            </div>
          )}
          <div className="flex-1" />
          {isTerminal && (
            <button
              onClick={() => setConfirmDelete(true)}
              className="flex items-center gap-1.5 text-sm text-error hover:text-error/80 px-4 py-2 rounded-lg border border-error/30 hover:bg-error/10 transition-colors cursor-pointer"
            >
              <IconTrash />
              {t('delete', 'Delete')}
            </button>
          )}
        </div>

        <ConfirmDialog
          open={confirmDelete}
          onOpenChange={setConfirmDelete}
          title={t('deleteTask', 'Delete task?')}
          description={t('deleteTaskConfirm', 'This action cannot be undone.')}
          confirmLabel={t('delete', 'Delete')}
          variant="destructive"
          onConfirm={handleDelete}
        />
      </div>
    </div>
  )
}
