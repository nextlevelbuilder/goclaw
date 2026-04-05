import { useState, useEffect, useCallback, useRef } from 'react'
import { getWsClient } from '../lib/ws'
import { toast } from '../stores/toast-store'
import type { TeamData, TeamTaskData, TeamMemberData, TeamTaskAttachment } from '../types/team'

/** Event payload shape from team.task.* WS events */
interface TaskEventPayload {
  team_id: string
  task_id: string
  status?: string
  progress_percent?: number
  progress_step?: string
}

export function useTeamTasks() {
  const [teams, setTeams] = useState<TeamData[]>([])
  const [tasks, setTasks] = useState<TeamTaskData[]>([])
  const [members, setMembers] = useState<TeamMemberData[]>([])
  const [loading, setLoading] = useState(true)
  const activeTeamRef = useRef<string | null>(null)

  // Debounce timers for fetch-one calls (300ms per task, matching web UI)
  const fetchTimersRef = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  // Progress debounce (1s global batch, matching web UI)
  const progressTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined)
  const pendingProgressRef = useRef(new Map<string, { percent: number; step: string }>())

  // Cleanup timers on unmount
  useEffect(() => {
    return () => {
      fetchTimersRef.current.forEach((t) => clearTimeout(t))
      clearTimeout(progressTimerRef.current)
    }
  }, [])

  const fetchTeams = useCallback(async () => {
    try {
      const ws = getWsClient()
      const res = await ws.call('teams.list') as { teams: TeamData[] | null }
      setTeams(res.teams ?? [])
      return res.teams ?? []
    } catch (err) {
      console.error('Failed to fetch teams:', err)
      return []
    }
  }, [])

  const fetchTasks = useCallback(async (teamId: string, statusFilter?: string) => {
    try {
      const ws = getWsClient()
      const params: Record<string, unknown> = { teamId }
      if (statusFilter) params.status = statusFilter
      const res = await ws.call('teams.tasks.list', params) as { tasks: TeamTaskData[] | null; members?: TeamMemberData[] | null }
      setTasks(res.tasks ?? [])
      if (res.members) setMembers(res.members)
      activeTeamRef.current = teamId
    } catch (err) {
      console.error('Failed to fetch tasks:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  /** Fetch single task via get-light and upsert into local state */
  const fetchOneTask = useCallback(async (taskId: string) => {
    const teamId = activeTeamRef.current
    if (!teamId) return
    try {
      const ws = getWsClient()
      const res = await ws.call('teams.tasks.get-light', { teamId, taskId }) as { task: TeamTaskData }
      if (!res.task) return
      setTasks((prev) => {
        const idx = prev.findIndex((t) => t.id === res.task.id)
        if (idx >= 0) return prev.map((t) => t.id === res.task.id ? res.task : t)
        return [res.task, ...prev]
      })
    } catch { /* task may have been deleted */ }
  }, [])

  /** Debounced fetch-one (300ms per task, matching web board-container) */
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

  const createTask = useCallback(async (teamId: string, params: { subject: string; description?: string; priority?: number; assignee?: string }) => {
    try {
      const ws = getWsClient()
      const task = await ws.call('teams.tasks.create', { teamId, ...params }) as TeamTaskData
      setTasks((prev) => [task, ...prev])
      toast.success('Task created', params.subject)
      return task
    } catch (err) {
      toast.error('Failed to create task', (err as Error).message)
      throw err
    }
  }, [])

  const assignTask = useCallback(async (taskId: string, agentKey: string) => {
    const teamId = activeTeamRef.current
    if (!teamId) return
    try {
      const ws = getWsClient()
      const task = await ws.call('teams.tasks.assign', { teamId, taskId, agentId: agentKey }) as TeamTaskData
      setTasks((prev) => prev.map((t) => t.id === taskId ? task : t))
      toast.success('Task assigned')
      return task
    } catch (err) {
      toast.error('Failed to assign task', (err as Error).message)
      throw err
    }
  }, [])

  const deleteTask = useCallback(async (taskId: string) => {
    const teamId = activeTeamRef.current
    if (!teamId) return
    try {
      const ws = getWsClient()
      await ws.call('teams.tasks.delete', { teamId, taskId })
      setTasks((prev) => prev.filter((t) => t.id !== taskId))
      toast.success('Task deleted')
    } catch (err) {
      toast.error('Failed to delete task', (err as Error).message)
      throw err
    }
  }, [])

  const deleteBulk = useCallback(async (taskIds: string[]) => {
    const teamId = activeTeamRef.current
    if (!teamId) return
    try {
      const ws = getWsClient()
      await ws.call('teams.tasks.delete-bulk', { teamId, taskIds })
      const idSet = new Set(taskIds)
      setTasks((prev) => prev.filter((t) => !idSet.has(t.id)))
      toast.success(`${taskIds.length} tasks deleted`)
    } catch (err) {
      toast.error('Failed to delete tasks', (err as Error).message)
      throw err
    }
  }, [])

  // Real-time WS event subscriptions (matching web board-container pattern)
  useEffect(() => {
    const ws = getWsClient()
    const unsubs: Array<() => void> = []

    // Progress events: local patch with 1s debounce
    const onProgress = (payload: unknown) => {
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
    }

    // Deletion: immediate remove
    const onDeleted = (payload: unknown) => {
      const p = payload as TaskEventPayload
      if (!p.task_id || p.team_id !== activeTeamRef.current) return
      setTasks((prev) => prev.filter((t) => t.id !== p.task_id))
    }

    // Status changes: debounced fetch-one via get-light
    const onFetchOne = (payload: unknown) => {
      const p = payload as TaskEventPayload
      if (!p.task_id || p.team_id !== activeTeamRef.current) return
      debouncedFetchTask(p.task_id)
    }

    unsubs.push(ws.on('team.task.progress', onProgress))
    unsubs.push(ws.on('team.task.deleted', onDeleted))

    for (const evt of [
      'team.task.created', 'team.task.completed', 'team.task.claimed',
      'team.task.cancelled', 'team.task.failed', 'team.task.assigned',
      'team.task.dispatched', 'team.task.updated',
    ]) {
      unsubs.push(ws.on(evt, onFetchOne))
    }

    return () => { for (const fn of unsubs) fn() }
  }, [debouncedFetchTask])

  /** Fetch full task detail including attachments (for modal view) */
  const fetchTaskDetail = useCallback(async (teamId: string, taskId: string) => {
    try {
      const ws = getWsClient()
      const res = await ws.call('teams.tasks.get', { teamId, taskId }) as {
        task: TeamTaskData
        attachments?: TeamTaskAttachment[]
      }
      return { task: res.task, attachments: res.attachments ?? [] }
    } catch (err) {
      console.error('Failed to fetch task detail:', err)
      return null
    }
  }, [])

  return { teams, tasks, members, loading, fetchTeams, fetchTasks, fetchTaskDetail, createTask, assignTask, deleteTask, deleteBulk }
}
