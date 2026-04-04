// Team service — wraps WS teams.* calls
import { getWsClient } from '../lib/ws'
import type { TeamData, TeamMemberData, TeamTaskData, TeamTaskAttachment } from '../types/team'

export interface TeamCreateParams {
  name: string
  lead: string
  members: string[]
}

export interface TeamDetailResult {
  team: TeamData
  members: TeamMemberData[]
}

export interface TaskCreateParams {
  subject: string
  description?: string
  priority?: number
  assignee?: string
}

export interface ActiveTaskResponse {
  taskId: string
  taskNumber?: number
  subject: string
  status: string
  ownerAgentKey?: string
  progressPercent?: number
  progressStep?: string
}

export const teamService = {
  list(): Promise<{ teams: TeamData[] | null }> {
    return getWsClient().call('teams.list') as Promise<{ teams: TeamData[] | null }>
  },

  create(params: TeamCreateParams): Promise<{ team: TeamData }> {
    return getWsClient().call('teams.create', params as unknown as Record<string, unknown>) as Promise<{ team: TeamData }>
  },

  get(teamId: string): Promise<TeamDetailResult> {
    return getWsClient().call('teams.get', { teamId }) as Promise<TeamDetailResult>
  },

  update(teamId: string, params: { name?: string; description?: string; settings?: Record<string, unknown> }): Promise<unknown> {
    return getWsClient().call('teams.update', { teamId, ...params })
  },

  addMember(teamId: string, agentId: string, role = 'member'): Promise<unknown> {
    return getWsClient().call('teams.members.add', { teamId, agent: agentId, role })
  },

  removeMember(teamId: string, agentId: string): Promise<unknown> {
    return getWsClient().call('teams.members.remove', { teamId, agentId })
  },

  listTasks(teamId: string, statusFilter?: string): Promise<{ tasks: TeamTaskData[] | null; members?: TeamMemberData[] | null }> {
    const params: Record<string, unknown> = { teamId }
    if (statusFilter) params.status = statusFilter
    return getWsClient().call('teams.tasks.list', params) as Promise<{ tasks: TeamTaskData[] | null; members?: TeamMemberData[] | null }>
  },

  getTask(teamId: string, taskId: string): Promise<{ task: TeamTaskData; attachments?: TeamTaskAttachment[] }> {
    return getWsClient().call('teams.tasks.get', { teamId, taskId }) as Promise<{ task: TeamTaskData; attachments?: TeamTaskAttachment[] }>
  },

  getTaskLight(teamId: string, taskId: string): Promise<{ task: TeamTaskData }> {
    return getWsClient().call('teams.tasks.get-light', { teamId, taskId }) as Promise<{ task: TeamTaskData }>
  },

  createTask(teamId: string, params: TaskCreateParams): Promise<TeamTaskData> {
    return getWsClient().call('teams.tasks.create', { teamId, ...params }) as Promise<TeamTaskData>
  },

  assignTask(teamId: string, taskId: string, agentKey: string): Promise<TeamTaskData> {
    return getWsClient().call('teams.tasks.assign', { teamId, taskId, agentId: agentKey }) as Promise<TeamTaskData>
  },

  deleteTask(teamId: string, taskId: string): Promise<unknown> {
    return getWsClient().call('teams.tasks.delete', { teamId, taskId })
  },

  deleteBulkTasks(teamId: string, taskIds: string[]): Promise<unknown> {
    return getWsClient().call('teams.tasks.delete-bulk', { teamId, taskIds })
  },

  activeTasksBySession(sessionKey: string): Promise<{ tasks: ActiveTaskResponse[] | null }> {
    return getWsClient().call('teams.tasks.active-by-session', { sessionKey }) as Promise<{ tasks: ActiveTaskResponse[] | null }>
  },
}
