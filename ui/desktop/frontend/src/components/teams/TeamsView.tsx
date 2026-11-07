import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { teamService } from '../../services/team-service'
import { useTeamManage } from '../../hooks/use-team-manage'
import { useAgentStore } from '../../stores/agent-store'
import { useUiStore } from '../../stores/ui-store'
import type { TeamData } from '../../types/team'
import { cn } from '../../lib/cn'
import { IconPlus, IconTrash, IconUsers } from '../common/Icons'
import { useGatewayStatus } from '../layout/use-gateway-status'
import {
  Button, ConfirmDialog, DataTable, EmptyState, ErrorState, Menu, PageHeader, SkeletonList, StatusChip, toast,
  type DataTableColumn,
} from '../ui'
import { TeamCreateDialog } from './TeamCreateDialog'
import { TeamSettingsModal } from './TeamSettingsModal'

const MAX_TEAMS_LITE = 1

/** Teams: list with lead, members and status. Opening a team goes to its board. */
export function TeamsView() {
  const { t } = useTranslation('teams')
  const { disconnected } = useGatewayStatus()
  const [teams, setTeams] = useState<TeamData[] | null>(null)
  const [failed, setFailed] = useState(false)
  const [retrying, setRetrying] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [editId, setEditId] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<TeamData | null>(null)
  const [busy, setBusy] = useState(false)
  const openTeamBoard = useUiStore((s) => s.openTeamBoard)
  const agents = useAgentStore((s) => s.agents)
  const { deleteTeam } = useTeamManage()

  const load = useCallback(async () => {
    try {
      const res = await teamService.list()
      setTeams(res.teams ?? [])
      setFailed(false)
    } catch {
      setFailed(true)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const retry = async () => {
    setRetrying(true)
    try { await load() } finally { setRetrying(false) }
  }

  const confirmDelete = async () => {
    if (!deleting) return
    setBusy(true)
    try {
      await deleteTeam(deleting.id)
      setTeams((prev) => (prev ?? []).filter((x) => x.id !== deleting.id))
      toast.success(t('screen.toast.deleted'))
      setDeleting(null)
    } catch {
      toast.error(t('screen.toast.deleteFailed'))
    } finally {
      setBusy(false)
    }
  }

  const atLimit = (teams?.length ?? 0) >= MAX_TEAMS_LITE
  const loading = teams === null && !failed

  const header = (
    <PageHeader
      title={t('title')}
      description={teams ? t('screen.summary', { count: teams.length, max: MAX_TEAMS_LITE }) : undefined}
      actions={(
        <Button
          variant="primary"
          icon={<IconPlus size={16} />}
          onClick={() => setCreateOpen(true)}
          disabled={atLimit || teams === null || disconnected}
        >
          {t('screen.newTeam')}
        </Button>
      )}
    />
  )

  const columns: DataTableColumn<TeamData>[] = [
    {
      id: 'team',
      header: t('screen.colTeam'),
      className: 'min-w-[220px]',
      cell: (team) => (
        <div className="min-w-0">
          <p className="truncate text-ui font-semibold text-ink">{team.name}</p>
          {team.description && <p className="max-w-md truncate text-caption font-normal text-ink-muted">{team.description}</p>}
        </div>
      ),
    },
    {
      id: 'lead',
      header: t('screen.colLead'),
      className: 'w-48',
      cell: (team) => (
        <span className="block truncate text-ui text-ink-secondary">
          {team.lead_display_name || team.lead_agent_key || '—'}
        </span>
      ),
    },
    {
      id: 'members',
      header: t('screen.colMembers'),
      className: 'w-32',
      cell: (team) => (
        <span className="text-ui text-ink-secondary">
          {team.member_count != null ? t('screen.membersCount', { count: team.member_count }) : '—'}
        </span>
      ),
    },
    {
      id: 'status',
      header: t('screen.colStatus'),
      className: 'w-36',
      cell: (team) => (
        <StatusChip
          tone={team.status === 'archived' ? 'idle' : 'success'}
          label={t(`screen.status.${team.status === 'archived' ? 'archived' : 'active'}`)}
        />
      ),
    },
    {
      id: 'actions',
      header: <span className="sr-only">{t('screen.colTeam')}</span>,
      className: 'w-14 text-right',
      cell: (team) => (
        <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
          <Menu
            label={t('screen.rowActions', { name: team.name })}
            items={[
              { label: t('screen.openBoard'), onSelect: () => openTeamBoard(team.id) },
              { label: t('screen.editTeam'), onSelect: () => setEditId(team.id), disabled: disconnected },
              'separator',
              {
                label: t('screen.deleteTeam'),
                icon: <IconTrash size={14} />,
                destructive: true,
                disabled: disconnected,
                onSelect: () => setDeleting(team),
              },
            ]}
          />
        </div>
      ),
    },
  ]

  let body
  if (failed && teams === null) {
    body = (
      <ErrorState
        title={t('screen.errorTitle')}
        description={t('screen.errorDescription')}
        onRetry={retry}
        retrying={retrying}
      />
    )
  } else if (loading) {
    body = <SkeletonList rows={3} label={t('screen.loading')} />
  } else if (teams && teams.length === 0) {
    body = (
      <EmptyState
        icon={<IconUsers size={20} />}
        title={t('screen.emptyTitle')}
        description={t('screen.emptyDescription')}
        action={(
          <Button variant="secondary" onClick={() => setCreateOpen(true)} disabled={disconnected}>
            {t('screen.emptyAction')}
          </Button>
        )}
      />
    )
  } else if (teams) {
    body = (
      <>
        {atLimit && <p className="mb-3 text-caption text-ink-muted">{t('screen.limitReached', { max: MAX_TEAMS_LITE })}</p>}
        <DataTable
          aria-label={t('title')}
          columns={columns}
          rows={teams}
          getRowId={(x) => x.id}
          onRowClick={(x) => openTeamBoard(x.id)}
        />
      </>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {header}
      <div className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>{body}</div>

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

      {editId && (
        <TeamSettingsModal
          teamId={editId}
          onClose={() => setEditId(null)}
          onSaved={() => void load()}
          onDeleted={() => { setEditId(null); void load() }}
        />
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => { if (!o) setDeleting(null) }}
        title={t('screen.delete.title', { name: deleting?.name ?? '' })}
        description={t('screen.delete.body')}
        confirmLabel={t('screen.delete.confirm')}
        cancelLabel={t('screen.create.cancel')}
        variant="destructive"
        loading={busy}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
