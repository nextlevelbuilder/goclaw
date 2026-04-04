import { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { getWsClient } from '../../lib/ws'
import { teamService } from '../../services/team-service'
import type { TeamTaskData } from '../../types/team'
import { PRIORITY_BADGE } from '../../types/team'

interface TaskPanelProps {
  sessionKey: string | null
  onTaskClick?: (task: TeamTaskData) => void
}

/** Lightweight task shape from active-by-session endpoint (camelCase) */
interface ActiveTaskResponse {
  taskId: string
  taskNumber?: number
  subject: string
  status: string
  ownerAgentKey?: string
  progressPercent?: number
  progressStep?: string
}

/** Event payload from team.task.* events */
interface TaskEventPayload {
  task_id: string
  team_id: string
  subject?: string
  status: string
  owner_agent_key?: string
  progress_percent?: number
  progress_step?: string
}

/** Normalize active-by-session response to TeamTaskData shape */
function normalizeActiveTask(t: ActiveTaskResponse): TeamTaskData {
  return {
    id: t.taskId,
    team_id: '',
    subject: t.subject,
    status: t.status as TeamTaskData['status'],
    priority: 2,
    task_number: t.taskNumber,
    owner_agent_key: t.ownerAgentKey,
    progress_percent: t.progressPercent,
    progress_step: t.progressStep,
  }
}

/** Compact task panel shown alongside chat when team tasks are active */
export function TaskPanel({ sessionKey, onTaskClick }: TaskPanelProps) {
  const { t } = useTranslation('teams')
  const [tasks, setTasks] = useState<TeamTaskData[]>([])
  const [collapsed, setCollapsed] = useState(false)

  const fetchActiveTasks = useCallback(async () => {
    if (!sessionKey) return
    try {
      const res = await teamService.activeTasksBySession(sessionKey)
      setTasks((res.tasks ?? []).map(normalizeActiveTask))
    } catch { /* session may not have team tasks */ }
  }, [sessionKey])

  useEffect(() => { fetchActiveTasks() }, [fetchActiveTasks])

  // Real-time updates
  useEffect(() => {
    const ws = getWsClient()
    const unsubs: Array<() => void> = []

    const handleEvent = (payload: unknown) => {
      const p = payload as TaskEventPayload
      if (!p.task_id) return
      setTasks((prev) => {
        const idx = prev.findIndex((t) => t.id === p.task_id)
        if (idx >= 0) {
          return prev.map((t) => t.id === p.task_id ? {
            ...t,
            status: (p.status || t.status) as TeamTaskData['status'],
            owner_agent_key: p.owner_agent_key ?? t.owner_agent_key,
            progress_percent: p.progress_percent ?? t.progress_percent,
            progress_step: p.progress_step ?? t.progress_step,
          } : t)
        }
        if (p.subject) {
          return [...prev, {
            id: p.task_id,
            team_id: p.team_id,
            subject: p.subject,
            status: (p.status || 'pending') as TeamTaskData['status'],
            priority: 2,
            owner_agent_key: p.owner_agent_key,
            progress_percent: p.progress_percent,
          } as TeamTaskData]
        }
        return prev
      })
    }

    const handleRemove = (payload: unknown) => {
      const p = payload as TaskEventPayload
      if (p.task_id) setTasks((prev) => prev.filter((t) => t.id !== p.task_id))
    }

    for (const evt of ['team.task.created', 'team.task.progress', 'team.task.assigned', 'team.task.completed', 'team.task.failed']) {
      unsubs.push(ws.on(evt, handleEvent))
    }
    unsubs.push(ws.on('team.task.deleted', handleRemove))

    return () => { for (const fn of unsubs) fn() }
  }, [])

  const activeTasks = tasks.filter((t) => t.status === 'pending' || t.status === 'blocked' || t.status === 'in_progress')

  if (activeTasks.length === 0) return null

  return (
    <div className="border-t border-border bg-surface-secondary/50">
      <button
        onClick={() => setCollapsed((v) => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 text-xs text-text-muted hover:text-text-primary transition-colors cursor-pointer"
      >
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className={`transition-transform ${collapsed ? '' : 'rotate-90'}`}>
          <polyline points="9 18 15 12 9 6" />
        </svg>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
          <circle cx="9" cy="7" r="4" />
          <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
          <path d="M16 3.13a4 4 0 0 1 0 7.75" />
        </svg>
        <span className="font-medium">
          {t('teamTasks', 'Team')}: {activeTasks.length} {t('tasksActive', 'active')}
        </span>
      </button>

      {!collapsed && (
        <div className="px-3 pb-2 space-y-1 max-h-[200px] overflow-y-auto overscroll-contain">
          {activeTasks.map((task) => {
            const prio = PRIORITY_BADGE[task.priority] ?? PRIORITY_BADGE[3]
            return (
              <button
                key={task.id}
                onClick={() => onTaskClick?.(task)}
                className="w-full flex items-center gap-2 px-2 py-1.5 rounded-lg bg-surface-secondary border border-border/50 hover:border-accent/30 transition-colors text-left cursor-pointer"
              >
                <span className={`text-[9px] font-medium px-1 py-0.5 rounded shrink-0 ${prio.cls}`}>{prio.label}</span>
                <span className="text-[11px] text-text-primary truncate flex-1">{task.subject}</span>
                {task.progress_percent != null && task.progress_percent > 0 && (
                  <div className="w-12 h-1 rounded-full bg-surface-tertiary overflow-hidden shrink-0">
                    <div className="h-full rounded-full bg-accent" style={{ width: `${task.progress_percent}%` }} />
                  </div>
                )}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
