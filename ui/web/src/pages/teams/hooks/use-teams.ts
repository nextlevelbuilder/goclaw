import { useState, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import type { TeamData, TeamMemberData, TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment, TeamAccessSettings, ScopeEntry } from "@/types/team";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";

export function useTeams() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [teams, setTeams] = useState<TeamData[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!connected) return;
    setLoading(true);
    try {
      const res = await ws.call<{ teams: TeamData[]; count: number }>(
        Methods.TEAMS_LIST,
      );
      setTeams(res.teams ?? []);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [ws, connected]);

  const createTeam = useCallback(
    async (params: {
      name: string;
      lead: string;
      members: string[];
      description?: string;
    }) => {
      try {
        await ws.call(Methods.TEAMS_CREATE, params);
        load();
        toast.success(i18next.t("teams:toast.created"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedCreate"), userFriendlyError(err));
        throw err;
      }
    },
    [ws, load],
  );

  const deleteTeam = useCallback(
    async (teamId: string) => {
      try {
        await ws.call(Methods.TEAMS_DELETE, { teamId });
        load();
        toast.success(i18next.t("teams:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedDelete"), userFriendlyError(err));
        throw err;
      }
    },
    [ws, load],
  );

  const getTeam = useCallback(
    async (teamId: string) => {
      const res = await ws.call<{ team: TeamData; members: TeamMemberData[] }>(
        Methods.TEAMS_GET,
        { teamId },
      );
      return res;
    },
    [ws],
  );

  const getTeamTasks = useCallback(
    async (teamId: string, status?: string, channel?: string, chatId?: string) => {
      const res = await ws.call<{ tasks: TeamTaskData[]; count: number }>(
        Methods.TEAMS_TASK_LIST,
        { teamId, status, channel: channel ?? "", chatId: chatId ?? "" },
      );
      return res;
    },
    [ws],
  );

  const getTeamScopes = useCallback(
    async (teamId: string) => {
      const res = await ws.call<{ scopes: ScopeEntry[] }>(
        Methods.TEAMS_SCOPES,
        { teamId },
      );
      return res.scopes ?? [];
    },
    [ws],
  );

  const getTaskDetail = useCallback(
    async (teamId: string, taskId: string) => {
      const res = await ws.call<{
        task: TeamTaskData;
        comments: TeamTaskComment[];
        events: TeamTaskEvent[];
        attachments: TeamTaskAttachment[];
      }>(Methods.TEAMS_TASK_GET, { teamId, taskId });
      return res;
    },
    [ws],
  );

  const getTaskLight = useCallback(
    async (teamId: string, taskId: string) => {
      const res = await ws.call<{ task: TeamTaskData }>(
        Methods.TEAMS_TASK_GET_LIGHT,
        { teamId, taskId },
      );
      return res.task;
    },
    [ws],
  );

  const approveTask = useCallback(
    async (teamId: string, taskId: string, comment?: string) => {
      try {
        await ws.call(Methods.TEAMS_TASK_APPROVE, { teamId, taskId, comment });
        toast.success(i18next.t("teams:toast.taskApproved"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedApproveTask"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const rejectTask = useCallback(
    async (teamId: string, taskId: string, reason?: string) => {
      try {
        await ws.call(Methods.TEAMS_TASK_REJECT, { teamId, taskId, reason });
        toast.success(i18next.t("teams:toast.taskRejected"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedRejectTask"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const addTaskComment = useCallback(
    async (taskId: string, content: string, teamId?: string) => {
      await ws.call(Methods.TEAMS_TASK_COMMENT, { teamId, taskId, content });
    },
    [ws],
  );

  const getTaskComments = useCallback(
    async (teamId: string, taskId: string) => {
      const res = await ws.call<{ comments: TeamTaskComment[] }>(
        Methods.TEAMS_TASK_COMMENTS,
        { teamId, taskId },
      );
      return res.comments ?? [];
    },
    [ws],
  );

  const getTaskEvents = useCallback(
    async (teamId: string, taskId: string) => {
      const res = await ws.call<{ events: TeamTaskEvent[] }>(
        Methods.TEAMS_TASK_EVENTS,
        { teamId, taskId },
      );
      return res.events ?? [];
    },
    [ws],
  );

  const createTask = useCallback(
    async (teamId: string, params: { subject: string; description?: string; priority?: number; taskType?: string; assignTo?: string; channel?: string; chatId?: string }) => {
      try {
        const res = await ws.call<{ task: TeamTaskData }>(
          Methods.TEAMS_TASK_CREATE,
          { teamId, ...params },
        );
        toast.success(i18next.t("teams:toast.taskCreated"));
        return res.task;
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedCreateTask"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const deleteTask = useCallback(
    async (teamId: string, taskId: string) => {
      try {
        await ws.call(Methods.TEAMS_TASK_DELETE, { teamId, taskId });
        toast.success(i18next.t("teams:toast.taskDeleted"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedDeleteTask"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const deleteTasksBulk = useCallback(
    async (teamId: string, taskIds: string[]) => {
      try {
        const res = await ws.call<{ deleted: number }>(Methods.TEAMS_TASK_DELETE_BULK, { teamId, taskIds });
        toast.success(i18next.t("teams:toast.tasksBulkDeleted"));
        return res.deleted;
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedBulkDeleteTasks"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const assignTask = useCallback(
    async (teamId: string, taskId: string, agentId: string) => {
      try {
        await ws.call(Methods.TEAMS_TASK_ASSIGN, { teamId, taskId, agentId });
        toast.success(i18next.t("teams:toast.taskAssigned"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedAssignTask"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const addMember = useCallback(
    async (teamId: string, agent: string, role?: string) => {
      try {
        await ws.call(Methods.TEAMS_MEMBERS_ADD, { teamId, agent, role });
        toast.success(i18next.t("teams:toast.memberAdded"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedAddMember"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const removeMember = useCallback(
    async (teamId: string, agentId: string) => {
      try {
        await ws.call(Methods.TEAMS_MEMBERS_REMOVE, { teamId, agentId });
        toast.success(i18next.t("teams:toast.memberRemoved"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedRemoveMember"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const updateTeamSettings = useCallback(
    async (teamId: string, settings: TeamAccessSettings) => {
      try {
        await ws.call(Methods.TEAMS_UPDATE, { teamId, settings });
        toast.success(i18next.t("teams:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("teams:toast.failedUpdate"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  return {
    teams, loading, load, createTeam, deleteTeam, getTeam, getTeamTasks, getTeamScopes,
    getTaskDetail, getTaskLight, approveTask, rejectTask, addTaskComment, getTaskComments, getTaskEvents,
    createTask, deleteTask, deleteTasksBulk, assignTask,
    addMember, removeMember, updateTeamSettings,
  };
}
