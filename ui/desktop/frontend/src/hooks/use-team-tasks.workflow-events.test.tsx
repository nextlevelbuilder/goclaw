import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TeamTaskData } from '../types/team'

const handlers = vi.hoisted(() => new Map<string, (payload: unknown) => void>())
const listTasks = vi.hoisted(() => vi.fn())
const getTaskLight = vi.hoisted(() => vi.fn())

vi.mock('../lib/ws', () => ({
  getWsClient: () => ({ on: (event: string, handler: (payload: unknown) => void) => { handlers.set(event, handler); return () => handlers.delete(event) } }),
}))
vi.mock('../services/team-service', () => ({
  teamService: { list: vi.fn(), listTasks, getTaskLight },
}))

import { useTeamTasks } from './use-team-tasks'

const task = { id: 'task-1', team_id: 'team-1', subject: 'Task', status: 'blocked' } as TeamTaskData

afterEach(() => {
  cleanup()
  handlers.clear()
  vi.clearAllMocks()
  vi.useRealTimers()
})

describe('useTeamTasks workflow event refetching', () => {
  it('debounces matching team.workflow.updated events into one authoritative board refetch', async () => {
    vi.useFakeTimers()
    listTasks.mockResolvedValue({ tasks: [task], members: [] })
    const { result } = renderHook(() => useTeamTasks())
    await act(async () => { await result.current.fetchTasks('team-1') })
    expect(listTasks).toHaveBeenCalledTimes(1)

    act(() => {
      handlers.get('team.workflow.updated')?.({ team_id: 'other-team' })
      handlers.get('team.workflow.updated')?.({ team_id: 'team-1' })
      handlers.get('team.workflow.updated')?.({ team_id: 'team-1' })
      vi.advanceTimersByTime(299)
    })
    expect(listTasks).toHaveBeenCalledTimes(1)
    await act(async () => { vi.advanceTimersByTime(1); await Promise.resolve() })
    expect(listTasks).toHaveBeenCalledTimes(2)
    expect(listTasks).toHaveBeenLastCalledWith('team-1', undefined)
  })

  it('debounces matching team.task.blocked events into one exact task refetch', async () => {
    vi.useFakeTimers()
    listTasks.mockResolvedValue({ tasks: [], members: [] })
    getTaskLight.mockResolvedValue({ task })
    const { result } = renderHook(() => useTeamTasks())
    await act(async () => { await result.current.fetchTasks('team-1') })

    act(() => {
      handlers.get('team.task.blocked')?.({ team_id: 'other-team', task_id: 'task-1' })
      handlers.get('team.task.blocked')?.({ team_id: 'team-1', task_id: 'task-1' })
      handlers.get('team.task.blocked')?.({ team_id: 'team-1', task_id: 'task-1' })
      vi.advanceTimersByTime(299)
    })
    expect(getTaskLight).not.toHaveBeenCalled()
    await act(async () => { vi.advanceTimersByTime(1); await Promise.resolve() })
    expect(getTaskLight).toHaveBeenCalledOnce()
    expect(getTaskLight).toHaveBeenCalledWith('team-1', 'task-1')
  })
})
