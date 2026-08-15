import { useState, useEffect, useCallback, useRef } from 'react'
import { getWsClient } from '../lib/ws'
import { teamService } from '../services/team-service'
import { toast } from '../stores/toast-store'
import { useTaskDebouncer } from './team-task-debouncer'
import type { TeamData, TeamTaskData, TeamMemberData } from '../types/team'

export function useTeamTasks() {
  const [teams, setTeams] = useState<TeamData[]>([])
  const [tasks, setTasks] = useState<TeamTaskData[]>([])
  const [members, setMembers] = useState<TeamMemberData[]>([])
  const [loading, setLoading] = useState(true)
  const activeTeamRef = useRef<string | null>(null)

  const fetchOneTask = useCallback(async (taskId: string) => {
    const teamId = activeTeamRef.current
    if (!teamId) return
    try {
      const res = await teamService.getTaskLight(teamId, taskId)
      if (!res.task) return
      setTasks((prev) => {
        const idx = prev.findIndex((t) => t.id === res.task.id)
        if (idx >= 0) return prev.map((t) => t.id === res.task.id ? res.task : t)
        return [res.task, ...prev]
      })
    } catch { /* task may have been deleted */ }
  }, [])

  const { clearAll, handleProgress, handleDeleted, handleFetchOne } = useTaskDebouncer({
    activeTeamRef,
    fetchOneTask,
    setTasks,
  })

  useEffect(() => () => clearAll(), [clearAll])

  const fetchTeams = useCallback(async () => {
    try {
      const res = await teamService.list()
      setTeams(res.teams ?? [])
      return res.teams ?? []
    } catch (err) {
      console.error('Failed to fetch teams:', err)
      return []
    }
  }, [])

  const fetchTasks = useCallback(async (teamId: string, statusFilter?: string) => {
    try {
      const res = await teamService.listTasks(teamId, statusFilter)
      setTasks(res.tasks ?? [])
      if (res.members) setMembers(res.members)
      activeTeamRef.current = teamId
    } catch (err) {
      console.error('Failed to fetch tasks:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  const createTask = useCallback(async (teamId: string, params: { subject: string; description?: string; priority?: number; assignee?: string }) => {
    try {
      const task = await teamService.createTask(teamId, params)
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
      const task = await teamService.assignTask(teamId, taskId, agentKey)
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
      await teamService.deleteTask(teamId, taskId)
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
      await teamService.deleteBulkTasks(teamId, taskIds)
      const idSet = new Set(taskIds)
      setTasks((prev) => prev.filter((t) => !idSet.has(t.id)))
      toast.success(`${taskIds.length} tasks deleted`)
    } catch (err) {
      toast.error('Failed to delete tasks', (err as Error).message)
      throw err
    }
  }, [])

  // Real-time WS event subscriptions — use getWsClient() directly (.on not .call)
  useEffect(() => {
    const ws = getWsClient()
    const unsubs: Array<() => void> = []

    unsubs.push(ws.on('team.task.progress', handleProgress))
    unsubs.push(ws.on('team.task.deleted', handleDeleted))

    for (const evt of [
      'team.task.created', 'team.task.completed', 'team.task.claimed',
      'team.task.cancelled', 'team.task.failed', 'team.task.assigned',
      'team.task.dispatched', 'team.task.updated',
    ]) {
      unsubs.push(ws.on(evt, handleFetchOne))
    }

    return () => { for (const fn of unsubs) fn() }
  }, [handleProgress, handleDeleted, handleFetchOne])

  const fetchTaskDetail = useCallback(async (teamId: string, taskId: string) => {
    try {
      const res = await teamService.getTask(teamId, taskId)
      return { task: res.task, attachments: res.attachments ?? [] }
    } catch (err) {
      console.error('Failed to fetch task detail:', err)
      return null
    }
  }, [])

  return { teams, tasks, members, loading, fetchTeams, fetchTasks, fetchTaskDetail, createTask, assignTask, deleteTask, deleteBulk }
}
