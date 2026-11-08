import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { IconTrash } from '../common/Icons'
import { Button, ConfirmDialog, Dialog, DialogFooter, Select } from '../ui'
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
  /** Disables writes while the gateway is unreachable. */
  readOnly?: boolean
}

/** Task detail: read-only body, reassign while open, delete once finished (destructive action on the left). */
export function TaskDetailModal({ task, members, onClose, onAssign, onDelete, onFetchDetail, readOnly = false }: TaskDetailModalProps) {
  const { t } = useTranslation('teams')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([])

  useEffect(() => {
    if (!onFetchDetail) return
    void onFetchDetail(task.team_id, task.id).then((res) => {
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
    setDeleting(true)
    try {
      await onDelete(task.id)
      onClose()
    } catch {
      /* the caller reports the failure */
    } finally {
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  return (
    <>
      <Dialog
        open
        onOpenChange={(o) => { if (!o) onClose() }}
        title={task.subject}
        size="xl"
        footer={(
          <DialogFooter
            destructive={isTerminal ? (
              <Button variant="danger" icon={<IconTrash size={14} />} onClick={() => setConfirmDelete(true)} disabled={readOnly}>
                {t('delete')}
              </Button>
            ) : undefined}
          >
            {!isTerminal && (
              <div className="w-60">
                <Select
                  aria-label={t('assignTo')}
                  value={member?.agent_key || task.owner_agent_key || ''}
                  placeholder={t('board.assignPlaceholder')}
                  options={memberOptions}
                  disabled={readOnly}
                  onChange={(e) => { if (e.target.value) void onAssign(task.id, e.target.value).catch(() => undefined) }}
                />
              </div>
            )}
            <Button variant="secondary" onClick={onClose}>{t('board.close')}</Button>
          </DialogFooter>
        )}
      >
        <div className="space-y-4">
          <TaskDetailBody task={task} members={members} attachments={attachments} />
        </div>
      </Dialog>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={(o) => { if (!o && !deleting) setConfirmDelete(false) }}
        title={t('board.deleteTaskTitle')}
        description={t('board.deleteTaskBody')}
        confirmLabel={t('delete')}
        cancelLabel={t('cancel')}
        variant="destructive"
        loading={deleting}
        onConfirm={() => void handleDelete()}
      />
    </>
  )
}
