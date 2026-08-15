import { useRef, useCallback } from 'react'
import type { TeamTaskData } from '../types/team'

/** Event payload shape from team.task.* WS events */
export interface TaskEventPayload {
  team_id: string
  task_id: string
  status?: string
  progress_percent?: number
  progress_step?: string
}

interface UseDebouncerOptions {
  activeTeamRef: React.MutableRefObject<string | null>
  fetchOneTask: (taskId: string) => Promise<void>
  setTasks: React.Dispatch<React.SetStateAction<TeamTaskData[]>>
}

export function useTaskDebouncer({ activeTeamRef, fetchOneTask, setTasks }: UseDebouncerOptions) {
  const fetchTimersRef = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  const progressTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined)
  const pendingProgressRef = useRef(new Map<string, { percent: number; step: string }>())

  const clearAll = useCallback(() => {
    fetchTimersRef.current.forEach((t) => clearTimeout(t))
    clearTimeout(progressTimerRef.current)
  }, [])

  /** Debounced fetch-one (300ms per task) */
  const debouncedFetchTask = useCallback((taskId: string) => {
    const timers = fetchTimersRef.current
    const existing = timers.get(taskId)
    if (existing) clearTimeout(existing)
    pendingProgressRef.current.delete(taskId)
    timers.set(taskId, setTimeout(() => {
      timers.delete(taskId)
      fetchOneTask(taskId)
    }, 300))
  }, [fetchOneTask])

  /** Progress event handler — batches updates with 1s debounce */
  const handleProgress = useCallback((payload: unknown) => {
    const p = payload as TaskEventPayload
    if (!p.task_id || p.team_id !== activeTeamRef.current) return
    pendingProgressRef.current.set(p.task_id, {
      percent: p.progress_percent ?? 0,
      step: p.progress_step ?? '',
    })
    clearTimeout(progressTimerRef.current)
    progressTimerRef.current = setTimeout(() => {
      const patches = new Map(pendingProgressRef.current)
      pendingProgressRef.current.clear()
      setTasks((prev) => prev.map((t) => {
        const patch = patches.get(t.id)
        if (!patch) return t
        return { ...t, progress_percent: patch.percent, progress_step: patch.step }
      }))
    }, 1000)
  }, [activeTeamRef, setTasks])

  /** Deletion handler — immediate remove */
  const handleDeleted = useCallback((payload: unknown) => {
    const p = payload as TaskEventPayload
    if (!p.task_id || p.team_id !== activeTeamRef.current) return
    setTasks((prev) => prev.filter((t) => t.id !== p.task_id))
  }, [activeTeamRef, setTasks])

  /** Status-change handler — debounced fetch-one */
  const handleFetchOne = useCallback((payload: unknown) => {
    const p = payload as TaskEventPayload
    if (!p.task_id || p.team_id !== activeTeamRef.current) return
    debouncedFetchTask(p.task_id)
  }, [activeTeamRef, debouncedFetchTask])

  return { clearAll, debouncedFetchTask, handleProgress, handleDeleted, handleFetchOne }
}
