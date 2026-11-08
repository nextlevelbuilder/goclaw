import { useState, useEffect, useCallback, useRef } from 'react'
import { getWsClient } from '../lib/ws'
import { teamService } from '../services/team-service'
import { useTaskDebouncer } from './team-task-debouncer'
import type { TeamData, TeamTaskData, TeamMemberData } from '../types/team'

export function useTeamTasks() {
  const [teams, setTeams] = useState<TeamData[]>([])
  const [tasks, setTasks] = useState<TeamTaskData[]>([])
  const [members, setMembers] = useState<TeamMemberData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
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
      setError(false)
    } catch (err) {
      console.error('Failed to fetch tasks:', err)
      setError(true)
    } finally {
      setLoading(false)
    }
  }, [])

  const createTask = useCallback(async (teamId: string, params: { subject: string; description?: string; priority?: number; assignee?: string }) => {
    const task = await teamService.createTask(teamId, params)
    setTasks((prev) => [task, ...prev])
    return task
  }, [])

  const assignTask = useCallback(async (taskId: string, agentKey: string) => {
    const teamId = activeTeamRef.current
    if (!teamId) return
    const task = await teamService.assignTask(teamId, taskId, agentKey)
    setTasks((prev) => prev.map((t) => t.id === taskId ? task : t))
    return task
  }, [])

  const deleteTask = useCallback(async (taskId: string) => {
    const teamId = activeTeamRef.current
    if (!teamId) return
    await teamService.deleteTask(teamId, taskId)
    setTasks((prev) => prev.filter((t) => t.id !== taskId))
  }, [])

  const deleteBulk = useCallback(async (taskIds: string[]) => {
    const teamId = activeTeamRef.current
    if (!teamId) return
    await teamService.deleteBulkTasks(teamId, taskIds)
    const idSet = new Set(taskIds)
    setTasks((prev) => prev.filter((t) => !idSet.has(t.id)))
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

  return { teams, tasks, members, loading, error, fetchTeams, fetchTasks, fetchTaskDetail, createTask, assignTask, deleteTask, deleteBulk }
}
