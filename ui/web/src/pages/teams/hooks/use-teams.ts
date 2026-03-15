import { useState, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import type { TeamData, TeamMemberData, TeamTaskData, TeamTaskComment, TeamTaskEvent, TeamTaskAttachment, TeamAccessSettings, ScopeEntry } from "@/types/team";

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
      await ws.call(Methods.TEAMS_CREATE, params);
      load();
    },
    [ws, load],
  );

  const deleteTeam = useCallback(
    async (teamId: string) => {
      await ws.call(Methods.TEAMS_DELETE, { teamId });
      load();
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

  const approveTask = useCallback(
    async (teamId: string, taskId: string, comment?: string) => {
      await ws.call(Methods.TEAMS_TASK_APPROVE, { teamId, taskId, comment });
    },
    [ws],
  );

  const rejectTask = useCallback(
    async (teamId: string, taskId: string, reason?: string) => {
      await ws.call(Methods.TEAMS_TASK_REJECT, { teamId, taskId, reason });
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
      const res = await ws.call<{ task: TeamTaskData }>(
        Methods.TEAMS_TASK_CREATE,
        { teamId, ...params },
      );
      return res.task;
    },
    [ws],
  );

  const deleteTask = useCallback(
    async (teamId: string, taskId: string) => {
      await ws.call(Methods.TEAMS_TASK_DELETE, { teamId, taskId });
    },
    [ws],
  );

  const assignTask = useCallback(
    async (teamId: string, taskId: string, agentId: string) => {
      await ws.call(Methods.TEAMS_TASK_ASSIGN, { teamId, taskId, agentId });
    },
    [ws],
  );

  const addMember = useCallback(
    async (teamId: string, agent: string, role?: string) => {
      await ws.call(Methods.TEAMS_MEMBERS_ADD, { teamId, agent, role });
    },
    [ws],
  );

  const removeMember = useCallback(
    async (teamId: string, agentId: string) => {
      await ws.call(Methods.TEAMS_MEMBERS_REMOVE, { teamId, agentId });
    },
    [ws],
  );

  const updateTeamSettings = useCallback(
    async (teamId: string, settings: TeamAccessSettings) => {
      await ws.call(Methods.TEAMS_UPDATE, { teamId, settings });
    },
    [ws],
  );

  const getKnownUsers = useCallback(
    async (teamId: string): Promise<string[]> => {
      const res = await ws.call<{ users: string[] }>(
        Methods.TEAMS_KNOWN_USERS,
        { teamId },
      );
      return res.users ?? [];
    },
    [ws],
  );

  return {
    teams, loading, load, createTeam, deleteTeam, getTeam, getTeamTasks, getTeamScopes,
    getTaskDetail, approveTask, rejectTask, addTaskComment, getTaskComments, getTaskEvents,
    createTask, deleteTask, assignTask,
    addMember, removeMember, updateTeamSettings, getKnownUsers,
  };
}
