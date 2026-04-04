import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { IconClose, IconTrash } from '../common/Icons'
import { TERMINAL_STATUSES } from '../../types/team'
import { TaskDetailBody } from './task-detail-meta'
import type { TeamTaskData, TeamMemberData, TeamTaskAttachment } from '../../types/team'

interface TaskDetailModalProps {
  task: TeamTaskData
  members: TeamMemberData[]
  onClose: () => void
  onAssign: (taskId: string, agentKey: string) => Promise<unknown>
  onDelete: (taskId: string) => Promise<void>
  onFetchDetail?: (teamId: string, taskId: string) => Promise<{ task: TeamTaskData; attachments: TeamTaskAttachment[] } | null>
}

export function TaskDetailModal({ task, members, onClose, onAssign, onDelete, onFetchDetail }: TaskDetailModalProps) {
  const { t } = useTranslation('teams')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([])

  useEffect(() => {
    if (!onFetchDetail) return
    onFetchDetail(task.team_id, task.id).then((res) => {
      if (res) setAttachments(res.attachments)
    })
  }, [task.id, task.team_id, onFetchDetail])

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
        {/* Header */}
        <div className="px-6 pt-5 pb-4 border-b border-border shrink-0">
          <div className="flex items-start gap-3">
            <div className="flex-1 min-w-0">
              <h3 className="text-base font-semibold text-text-primary leading-snug sm:text-lg mt-1">{task.subject}</h3>
            </div>
            <button onClick={onClose} className="text-text-muted hover:text-text-primary p-1.5 cursor-pointer shrink-0 rounded-lg hover:bg-surface-tertiary">
              <IconClose />
            </button>
          </div>
        </div>

        {/* Scrollable body */}
        <div className="flex-1 overflow-y-auto overscroll-contain space-y-4 px-6 py-4">
          <TaskDetailBody task={task} members={members} attachments={attachments} />
        </div>

        {/* Footer */}
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
