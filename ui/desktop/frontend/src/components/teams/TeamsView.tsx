import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { teamService } from '../../services/team-service'
import { useAgentStore } from '../../stores/agent-store'
import { useUiStore } from '../../stores/ui-store'
import type { TeamData } from '../../types/team'
import { IconPlus, IconUsers } from '../common/Icons'
import { Button, EmptyState, ErrorState, PageHeader, SkeletonList } from '../ui'
import { TeamCreateDialog } from './TeamCreateDialog'

const MAX_TEAMS_LITE = 1

/** Interim Teams view: list teams, open a team board, create a team (Lite allows one). */
export function TeamsView() {
  const { t } = useTranslation('teams')
  const { t: td } = useTranslation('desktop')
  const [teams, setTeams] = useState<TeamData[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const openTeamBoard = useUiStore((s) => s.openTeamBoard)
  const agents = useAgentStore((s) => s.agents)

  const load = useCallback(() => {
    setError(null)
    teamService.list()
      .then((res) => setTeams(res.teams ?? []))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  useEffect(() => { load() }, [load])

  const atLimit = (teams?.length ?? 0) >= MAX_TEAMS_LITE
  const create = (
    <Button
      variant="primary"
      icon={<IconPlus size={16} />}
      onClick={() => setCreateOpen(true)}
      disabled={atLimit || teams === null}
      title={atLimit ? t('limitReached', 'Team limit reached') : undefined}
    >
      {t('createTeam', 'New team')}
    </Button>
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title={td('nav.views.teams')}
        description={teams ? `${teams.length}/${MAX_TEAMS_LITE}` : td('stub.teamsDesc')}
        actions={create}
      />
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4">
        {error ? (
          <ErrorState code={error} onRetry={load} />
        ) : teams === null ? (
          <SkeletonList rows={2} />
        ) : teams.length === 0 ? (
          <EmptyState icon={<IconUsers size={20} />} title={t('noTeams', 'No teams yet')} description={td('stub.teamsEmpty')} />
        ) : (
          <ul className="flex flex-col">
            {teams.map((team) => (
              <li key={team.id} className="border-b border-border-subtle last:border-b-0">
                <button
                  type="button"
                  onClick={() => openTeamBoard(team.id)}
                  className="flex h-row w-full items-center gap-3 rounded-sm px-2 text-left text-ui text-ink transition-colors hover:bg-surface-hover"
                >
                  <IconUsers size={16} className="shrink-0 text-ink-muted" />
                  <span className="min-w-0 flex-1 truncate">{team.name}</span>
                  {team.member_count != null && <span className="shrink-0 text-caption text-ink-muted">{team.member_count}</span>}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
      {createOpen && (
        <TeamCreateDialog
          agents={agents.map((a) => ({ id: a.id, key: a.key, name: a.name, emoji: a.emoji }))}
          onClose={() => setCreateOpen(false)}
          onCreated={(team) => {
            setTeams((prev) => [...(prev ?? []), team])
            openTeamBoard(team.id)
          }}
        />
      )}
    </div>
  )
}
