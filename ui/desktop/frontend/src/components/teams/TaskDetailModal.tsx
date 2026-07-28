import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { IconClose, IconTrash } from '../common/Icons'
import { getWsClient } from '../../lib/ws'
import { teamService } from '../../services/team-service'
import { toast } from '../../stores/toast-store'
import { TERMINAL_STATUSES } from '../../types/team'
import { TaskDetailBody } from './task-detail-meta'
import { WorkflowActionDialog } from './WorkflowActionDialog'
import type {
  TeamTaskData, TeamMemberData, TeamTaskAttachment, TeamWorkflowDetailResponse,
  WorkflowAction,
} from '../../types/team'

interface TaskDetailModalProps {
  task: TeamTaskData
  members: TeamMemberData[]
  onClose: () => void
  onAssign: (taskId: string, agentKey: string) => Promise<unknown>
  onDelete: (taskId: string) => Promise<void>
  onFetchDetail?: (teamId: string, taskId: string) => Promise<{ task: TeamTaskData; attachments: TeamTaskAttachment[] } | null>
  onWorkflowChanged?: () => void
}

export function TaskDetailModal({ task, members, onClose, onAssign, onDelete, onFetchDetail, onWorkflowChanged }: TaskDetailModalProps) {
  const { t } = useTranslation('teams')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [attachments, setAttachments] = useState<TeamTaskAttachment[]>([])
  const [workflow, setWorkflow] = useState<TeamWorkflowDetailResponse | null>(null)
  const [selectedAction, setSelectedAction] = useState<WorkflowAction | null>(null)
  const [workflowBusy, setWorkflowBusy] = useState(false)
  const workflowRefreshTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  useEffect(() => {
    if (!onFetchDetail) return
    onFetchDetail(task.team_id, task.id).then((res) => {
      if (res) setAttachments(res.attachments)
    })
  }, [task.id, task.team_id, onFetchDetail])

  const loadWorkflow = useCallback(async () => {
    if (!task.workflow_id) {
      setWorkflow(null)
      return
    }
    try {
      setWorkflow(await teamService.getWorkflow(task.team_id, task.workflow_id))
    } catch {
      setWorkflow(null)
    }
  }, [task.team_id, task.workflow_id])

  useEffect(() => { loadWorkflow() }, [loadWorkflow])

  useEffect(() => {
    const ws = getWsClient()
    const handleWorkflowUpdated = (payload: unknown) => {
      const event = payload as { team_id?: string; workflow_id?: string }
      if (event.team_id !== task.team_id || event.workflow_id !== task.workflow_id) return
      clearTimeout(workflowRefreshTimerRef.current)
      workflowRefreshTimerRef.current = setTimeout(loadWorkflow, 300)
    }
    const unsubscribe = ws.on('team.workflow.updated', handleWorkflowUpdated)
    return () => {
      clearTimeout(workflowRefreshTimerRef.current)
      unsubscribe()
    }
  }, [task.team_id, task.workflow_id, loadWorkflow])

  const runWorkflowAction = async (reason: string) => {
    if (!workflow || !selectedAction) return
    const stepScoped = selectedAction === 'retry_blocked' || selectedAction === 'request_revision' || selectedAction === 'apply_replan'
    const blockedTask = stepScoped
      ? workflow.tasks.find((workflowTask) => (
        workflowTask.status === 'blocked' &&
        workflowTask.workflow_kind === 'work' &&
        workflowTask.plan_revision === workflow.workflow.plan_revision
      ))
      : undefined

    if (stepScoped && !blockedTask) {
      toast.error(t('workflow.actionUnavailable'))
      return
    }

    setWorkflowBusy(true)
    try {
      const response = await teamService.applyWorkflowAction({
        teamId: task.team_id,
        workflowId: workflow.workflow.id,
        action: selectedAction,
        expectedStatus: workflow.workflow.status,
        expectedPlanRevision: workflow.workflow.plan_revision,
        ...(blockedTask ? { taskId: blockedTask.id, expectedTaskStatus: blockedTask.status } : {}),
        reason,
      })
      // 1. Replace local detail from the authoritative action response.
      setWorkflow(response)
      setSelectedAction(null)
      // 2. Refetch the authoritative workflow detail.
      await loadWorkflow()
      // 3. Refetch the board.
      onWorkflowChanged?.()
      if (response.outcome === 'conflict') {
        toast.warning(t('workflow.outcomes.conflict'))
      } else {
        toast.success(t(`workflow.outcomes.${response.outcome}`))
      }
    } catch (error) {
      toast.error(t('workflow.actionFailed'), (error as Error).message)
    } finally {
      setWorkflowBusy(false)
    }
  }

  const isTerminal = TERMINAL_STATUSES.has(task.status)
  const blockerReason = workflow?.tasks.find((workflowTask) => (
    workflowTask.status === 'blocked' &&
    workflowTask.workflow_kind === 'work' &&
    workflowTask.plan_revision === workflow.workflow.plan_revision &&
    workflowTask.blocker_reason
  ))?.blocker_reason
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
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={(event) => { if (event.target === event.currentTarget) onClose() }}
    >
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

          {workflow && (
            <section className="space-y-3 rounded-lg border border-border bg-surface-tertiary/30 p-4">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-xs font-medium text-text-muted">{t('workflow.title')}</span>
                <span className="text-xs font-medium px-2 py-0.5 rounded border border-border text-text-secondary">
                  {t(`workflow.status.${workflow.workflow.status}`)}
                </span>
                <span className="text-xs font-medium px-2 py-0.5 rounded border border-border text-text-secondary">
                  {t('workflow.revision', { revision: workflow.workflow.plan_revision })}
                </span>
                <span className="text-xs text-text-muted">
                  {workflow.workflow.coordinator_display_name || workflow.workflow.coordinator_agent_key}
                </span>
              </div>
              <div className="grid grid-cols-2 gap-2 text-xs text-text-muted sm:grid-cols-3">
                <span>{t('workflow.attempts.expansion', { count: workflow.workflow.expansion_attempt_count })}</span>
                <span>{t('workflow.attempts.delivery', { count: workflow.workflow.delivery_attempt_count })}</span>
                <span>{t('workflow.delivery.label', { status: t(`workflow.delivery.${workflow.workflow.delivery_status}`) })}</span>
              </div>
              {blockerReason && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  {t('workflow.blockerReason', { reason: blockerReason })}
                </p>
              )}
              {[
                workflow.workflow.failure_summary,
                workflow.workflow.cancel_reason,
                workflow.workflow.last_expansion_error,
                workflow.workflow.last_delivery_error,
              ].filter(Boolean).map((error) => (
                <p key={error} className="text-xs text-error">{error}</p>
              ))}
              {workflow.allowed_actions.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {workflow.allowed_actions.map((action) => (
                    <button
                      key={action}
                      type="button"
                      disabled={workflowBusy}
                      onClick={() => setSelectedAction(action)}
                      className={`px-3 py-1.5 text-xs rounded-lg border transition-colors disabled:opacity-50 cursor-pointer ${
                        action === 'cancel_workflow' || action === 'fail_workflow'
                          ? 'border-error/30 text-error hover:bg-error/10'
                          : 'border-border text-text-secondary hover:bg-surface-secondary'
                      }`}
                    >
                      {t(`workflow.actions.${action}.label`)}
                    </button>
                  ))}
                </div>
              )}
            </section>
          )}
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

      <WorkflowActionDialog
        action={selectedAction}
        loading={workflowBusy}
        onClose={() => setSelectedAction(null)}
        onConfirm={runWorkflowAction}
      />
    </div>
  )
}
