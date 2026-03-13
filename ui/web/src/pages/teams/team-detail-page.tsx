import { useState, useEffect, useCallback } from "react";
import { useSearchParams } from "react-router";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ArrowLeft, Users, Trash2 } from "lucide-react";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useTranslation } from "react-i18next";
import { useTeams } from "./hooks/use-teams";
import { TeamMembersTab } from "./team-members-tab";
import { TeamTasksTab } from "./team-tasks-tab";
import { TeamDelegationsTab } from "./team-delegations-tab";
import { TeamSettingsTab } from "./team-settings-tab";
import { TeamWorkspaceTab } from "./team-workspace-tab";
import type { TeamData, TeamMemberData, TeamAccessSettings, ScopeEntry } from "@/types/team";

interface TeamDetailPageProps {
  teamId: string;
  onBack: () => void;
}

export function TeamDetailPage({ teamId, onBack }: TeamDetailPageProps) {
  const { t } = useTranslation("teams");
  const {
    getTeam, getTeamTasks, getTeamScopes, addMember, removeMember, deleteTeam,
    getTaskDetail, approveTask, rejectTask, addTaskComment,
    createTask, assignTask,
  } = useTeams();
  const [team, setTeam] = useState<TeamData | null>(null);
  const [members, setMembers] = useState<TeamMemberData[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = searchParams.get("tab") || "members";
  const setActiveTab = useCallback((tab: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (tab === "members") {
        next.delete("tab");
      } else {
        next.set("tab", tab);
      }
      return next;
    }, { replace: true });
  }, [setSearchParams]);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [scopes, setScopes] = useState<ScopeEntry[]>([]);

  const reload = useCallback(async () => {
    try {
      const res = await getTeam(teamId);
      setTeam(res.team);
      setMembers(res.members ?? []);
    } catch {
      // ignore
    }
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
          setScopes(scopeList);
        }
      } catch {
        // ignore
      } finally {
        if (!cancelled) setLoading(false);
      }
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
    return <DetailPageSkeleton tabs={5} />;
  }

  return (
    <div className="p-4 sm:p-6">
      {/* Header */}
      <div className="mb-6 flex items-start gap-4">
        <Button variant="ghost" size="icon" onClick={onBack} className="mt-0.5 shrink-0">
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
          <Users className="h-6 w-6" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-xl font-semibold">{team.name}</h2>
            <Badge variant={team.status === "active" ? "success" : "secondary"}>
              {team.status}
            </Badge>
            {((team.settings ?? {}) as TeamAccessSettings).version != null &&
              ((team.settings ?? {}) as TeamAccessSettings).version! >= 2 && (
              <Badge variant="secondary" className="text-[10px] px-1.5 py-0">V2 Beta</Badge>
            )}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
            {team.lead_agent_key && (
              <>
                <span>{t("detail.lead")}: {team.lead_display_name || team.lead_agent_key}</span>
                <span className="text-border">|</span>
              </>
            )}
            <span>
              {members.length !== 1
                ? t("detail.memberCountPlural", { count: members.length })
                : t("detail.memberCount", { count: members.length })}
            </span>
          </div>
          {team.description && (
            <p className="mt-1 text-sm text-muted-foreground/70">{team.description}</p>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="shrink-0 text-muted-foreground hover:text-destructive"
          onClick={() => setDeleteOpen(true)}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>

      {/* Tabs */}
      <div className="max-w-4xl rounded-xl border bg-card p-3 shadow-sm sm:p-4">
        <Tabs value={activeTab} onValueChange={setActiveTab}>
          <TabsList className="w-full justify-start overflow-x-auto overflow-y-hidden">
            <TabsTrigger value="members">{t("detail.tabs.members")}</TabsTrigger>
            <TabsTrigger value="tasks">{t("detail.tabs.tasks")}</TabsTrigger>
            <TabsTrigger value="delegations">{t("detail.tabs.delegations")}</TabsTrigger>
            <TabsTrigger value="workspace">{t("detail.tabs.workspace")}</TabsTrigger>
            <TabsTrigger value="settings">{t("detail.tabs.settings")}</TabsTrigger>
          </TabsList>

          <TabsContent value="members" className="mt-4">
            <TeamMembersTab
              teamId={teamId}
              members={members}
              onAddMember={handleAddMember}
              onRemoveMember={handleRemoveMember}
            />
          </TabsContent>

          <TabsContent value="tasks" className="mt-4">
            <TeamTasksTab
              teamId={teamId}
              members={members}
              scopes={scopes}
              isTeamV2={((team.settings ?? {}) as TeamAccessSettings).version != null && ((team.settings ?? {}) as TeamAccessSettings).version! >= 2}
              getTeamTasks={getTeamTasks}
              getTaskDetail={getTaskDetail}
              approveTask={approveTask}
              rejectTask={rejectTask}
              addTaskComment={addTaskComment}
              createTask={createTask}
              assignTask={assignTask}
            />
          </TabsContent>

          <TabsContent value="delegations" className="mt-4">
            <TeamDelegationsTab teamId={teamId} />
          </TabsContent>

          <TabsContent value="workspace" className="mt-4">
            <TeamWorkspaceTab teamId={teamId} scopes={scopes} />
          </TabsContent>

          <TabsContent value="settings" className="mt-4">
            <TeamSettingsTab teamId={teamId} team={team} onSaved={reload} />
          </TabsContent>
        </Tabs>
      </div>

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
