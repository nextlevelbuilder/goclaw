import { useCallback } from 'react'
import { teamService } from '../services/team-service'
import type { TeamData, TeamMemberData } from '../types/team'

interface TeamDetailResult {
  team: TeamData
  members: TeamMemberData[]
}

/** Team writes throw on failure and never toast: the screen reports success and failure in the user's language. */
export function useTeamManage() {
  const fetchTeamDetail = useCallback(async (teamId: string): Promise<TeamDetailResult | null> => {
    try {
      return await teamService.get(teamId)
    } catch (err) {
      console.error('Failed to fetch team detail:', err)
      return null
    }
  }, [])

  const updateTeam = useCallback(async (
    teamId: string,
    params: { name?: string; description?: string; settings?: Record<string, unknown> },
  ) => {
    await teamService.update(teamId, params)
  }, [])

  const addMember = useCallback(async (teamId: string, agentId: string, role?: string) => {
    await teamService.addMember(teamId, agentId, role)
  }, [])

  const removeMember = useCallback(async (teamId: string, agentId: string) => {
    await teamService.removeMember(teamId, agentId)
  }, [])

  const deleteTeam = useCallback(async (teamId: string) => {
    await teamService.delete(teamId)
  }, [])

  return { fetchTeamDetail, updateTeam, addMember, removeMember, deleteTeam }
}
