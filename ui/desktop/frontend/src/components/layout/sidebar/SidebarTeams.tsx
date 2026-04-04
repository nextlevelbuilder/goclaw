import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../../stores/ui-store'
import { useAgentStore } from '../../../stores/agent-store'
import { teamService } from '../../../services/team-service'
import { TeamCreateDialog } from '../../teams/TeamCreateDialog'
import type { TeamData } from '../../../types/team'

const MAX_TEAMS_LITE = 1

export function SidebarTeams() {
  const { t } = useTranslation('teams')
  const [teams, setTeams] = useState<TeamData[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const activeView = useUiStore((s) => s.activeView)
  const activeTeamId = useUiStore((s) => s.activeTeamId)
  const openTeamBoard = useUiStore((s) => s.openTeamBoard)
  const agents = useAgentStore((s) => s.agents)

  useEffect(() => {
    teamService.list().then((res) => {
      setTeams(res.teams ?? [])
    }).catch(() => {})
  }, [])

  const atLimit = teams.length >= MAX_TEAMS_LITE

  return (
    <div className="px-3 py-2 space-y-1">
      {/* Section header + add button */}
      <div className="flex items-center justify-between px-1">
        <span className="text-[10px] text-text-muted font-medium tracking-wide">
          {t('title', 'Teams')} ({teams.length}/{MAX_TEAMS_LITE})
        </span>
        <button
          onClick={() => setCreateOpen(true)}
          disabled={atLimit}
          className="w-5 h-5 flex items-center justify-center rounded text-text-muted hover:text-accent hover:bg-surface-tertiary transition-colors disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
          title={atLimit ? t('limitReached', 'Team limit reached') : t('createTeam', 'New team')}
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round">
            <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
          </svg>
        </button>
      </div>

      {/* Team list */}
      {teams.map((team) => (
        <button
          key={team.id}
          onClick={() => openTeamBoard(team.id)}
          className={[
            'w-full flex items-center gap-2 px-2 py-1.5 rounded-lg text-left transition-colors cursor-pointer',
            activeView === 'team-board' && activeTeamId === team.id
              ? 'bg-accent/10 text-accent'
              : 'text-text-secondary hover:bg-surface-tertiary hover:text-text-primary',
          ].join(' ')}
        >
          <div className="w-5 h-5 flex items-center justify-center rounded bg-accent/10 text-accent shrink-0">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
              <circle cx="9" cy="7" r="4" />
              <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
              <path d="M16 3.13a4 4 0 0 1 0 7.75" />
            </svg>
          </div>
          <span className="truncate flex-1 text-xs font-medium">{team.name}</span>
          {team.member_count != null && (
            <span className="text-[10px] text-text-muted">{team.member_count}</span>
          )}
        </button>
      ))}

      {/* Empty state */}
      {teams.length === 0 && (
        <p className="text-[10px] text-text-muted px-1">{t('noTeams', 'No teams yet')}</p>
      )}

      {/* Create team dialog */}
      {createOpen && (
        <TeamCreateDialog
          agents={agents.map((a) => ({ id: a.id, key: a.key, name: a.name, emoji: a.emoji }))}
          onClose={() => setCreateOpen(false)}
          onCreated={(team) => {
            setTeams((prev) => [...prev, team])
            openTeamBoard(team.id)
          }}
        />
      )}
    </div>
  )
}
