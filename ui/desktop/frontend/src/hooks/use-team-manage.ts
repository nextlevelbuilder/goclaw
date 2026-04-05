import { useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import { toast } from '../stores/toast-store'
import type { TeamData, TeamMemberData } from '../types/team'

interface TeamDetailResult {
  team: TeamData
  members: TeamMemberData[]
}

export function useTeamManage() {
  const fetchTeamDetail = useCallback(async (teamId: string): Promise<TeamDetailResult | null> => {
    try {
      const ws = getWsClient()
      const res = await ws.call('teams.get', { teamId }) as TeamDetailResult
      return res
    } catch (err) {
      console.error('Failed to fetch team detail:', err)
      return null
    }
  }, [])

  const updateTeam = useCallback(async (
    teamId: string,
    params: { name?: string; description?: string; settings?: Record<string, unknown> },
  ) => {
    try {
      const ws = getWsClient()
      await ws.call('teams.update', { teamId, ...params })
      toast.success('Team updated')
    } catch (err) {
      toast.error('Failed to update team', (err as Error).message)
      throw err
    }
  }, [])

  const addMember = useCallback(async (teamId: string, agentId: string, role?: string) => {
    try {
      const ws = getWsClient()
      await ws.call('teams.members.add', { teamId, agent: agentId, role: role || 'member' })
      toast.success('Member added')
    } catch (err) {
      toast.error('Failed to add member', (err as Error).message)
      throw err
    }
  }, [])

  const removeMember = useCallback(async (teamId: string, agentId: string) => {
    try {
      const ws = getWsClient()
      await ws.call('teams.members.remove', { teamId, agentId })
      toast.success('Member removed')
    } catch (err) {
      toast.error('Failed to remove member', (err as Error).message)
      throw err
    }
  }, [])

  return { fetchTeamDetail, updateTeam, addMember, removeMember }
}
