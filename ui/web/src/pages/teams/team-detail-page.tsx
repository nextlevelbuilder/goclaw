import { useState, useEffect, useCallback } from "react";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useTranslation } from "react-i18next";
import { useTeams } from "./hooks/use-teams";
import { BoardHeader } from "./board/board-header";
import { BoardContainer } from "./board/board-container";
import { TeamInfoDialog } from "./board/team-info-dialog";
import { TeamMembersDialog } from "./board/team-members-dialog";
import { TeamWorkspaceDialog } from "./board/team-workspace-dialog";
import { TeamVersionModal } from "./team-version-modal";
import type { TeamData, TeamMemberData, TeamAccessSettings, ScopeEntry } from "@/types/team";

interface TeamDetailPageProps {
  teamId: string;
  onBack: () => void;
}

export function TeamDetailPage({ teamId, onBack }: TeamDetailPageProps) {
  const { t } = useTranslation("teams");
  const {
    getTeam, getTeamTasks, getTeamScopes, addMember, removeMember, deleteTeam,
    getTaskDetail, deleteTask, deleteTasksBulk,
  } = useTeams();

  const [team, setTeam] = useState<TeamData | null>(null);
  const [members, setMembers] = useState<TeamMemberData[]>([]);
  const [scopes, setScopes] = useState<ScopeEntry[]>([]);
  const [loading, setLoading] = useState(true);

  // Dialog states
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [infoOpen, setInfoOpen] = useState(false);
  const [membersOpen, setMembersOpen] = useState(false);
  const [workspaceOpen, setWorkspaceOpen] = useState(false);
  const [versionModalOpen, setVersionModalOpen] = useState(false);

  const reload = useCallback(async () => {
    try {
      const res = await getTeam(teamId);
      setTeam(res.team);
      setMembers(res.members ?? []);
    } catch { /* ignore */ }
  }, [teamId, getTeam]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const [res, scopeList] = await Promise.all([
          getTeam(teamId),
          getTeamScopes(teamId).catch(() => [] as ScopeEntry[]),
        ]);
        if (!cancelled) {
          setTeam(res.team);
          setMembers(res.members ?? []);
          setScopes(scopeList.filter((s) => s.chat_id));
        }
      } catch { /* ignore */ }
      finally { if (!cancelled) setLoading(false); }
    })();
    return () => { cancelled = true; };
  }, [teamId, getTeam, getTeamScopes]);

  const handleAddMember = useCallback(async (agentId: string, role?: string) => {
    await addMember(teamId, agentId, role);
    await reload();
  }, [teamId, addMember, reload]);

  const handleRemoveMember = useCallback(async (agentId: string) => {
    await removeMember(teamId, agentId);
    await reload();
  }, [teamId, removeMember, reload]);

  if (loading || !team) {
    return <DetailPageSkeleton tabs={3} />;
  }

  const settings = (team.settings ?? {}) as TeamAccessSettings;
  const isTeamV2 = (settings.version ?? 1) >= 2;

  return (
    <div className="flex h-full flex-col">
      <BoardHeader
        team={team}
        members={members}
        onBack={onBack}
        onDelete={() => setDeleteOpen(true)}
        onSettings={() => setInfoOpen(true)}
        onMembers={() => setMembersOpen(true)}
        onV2Click={() => setVersionModalOpen(true)}
      />

      <BoardContainer
        teamId={teamId}
        members={members}
        scopes={scopes}
        isTeamV2={isTeamV2}
        getTeamTasks={getTeamTasks}
        getTaskDetail={getTaskDetail}
        deleteTask={deleteTask}
        deleteTasksBulk={deleteTasksBulk}
        onWorkspace={() => setWorkspaceOpen(true)}
      />

      {/* Team info + settings + members dialog */}
      <TeamInfoDialog
        open={infoOpen}
        onOpenChange={setInfoOpen}
        team={team}
        teamId={teamId}
        members={members}
        onSaved={reload}
      />

      {/* Members dialog */}
      <TeamMembersDialog
        open={membersOpen}
        onOpenChange={setMembersOpen}
        members={members}
        onAddMember={handleAddMember}
        onRemoveMember={handleRemoveMember}
      />

      {/* Workspace dialog */}
      <TeamWorkspaceDialog
        open={workspaceOpen}
        onOpenChange={setWorkspaceOpen}
        teamId={teamId}
        scopes={scopes}
      />

      {/* V2 version comparison modal */}
      <TeamVersionModal open={versionModalOpen} onOpenChange={setVersionModalOpen} />

      {/* Delete confirmation */}
      <ConfirmDeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("delete.title")}
        description={t("detail.deleteDescription", { name: team.name })}
        confirmValue={team.name}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={async () => {
          await deleteTeam(teamId);
          setDeleteOpen(false);
          onBack();
        }}
      />
    </div>
  );
}
