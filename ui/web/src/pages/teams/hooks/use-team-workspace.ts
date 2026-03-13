import { useState, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import type { TeamWorkspaceFile } from "@/types/team";

export function useTeamWorkspace() {
  const ws = useWs();
  const [files, setFiles] = useState<TeamWorkspaceFile[]>([]);
  const [loading, setLoading] = useState(false);

  const listFiles = useCallback(
    async (teamId: string, channel?: string, chatId?: string) => {
      setLoading(true);
      try {
        const res = await ws.call<{ files: TeamWorkspaceFile[]; count: number }>(
          Methods.TEAMS_WORKSPACE_LIST,
          { team_id: teamId, channel: channel ?? "", chat_id: chatId ?? "" },
        );
        setFiles(res.files ?? []);
        return res.files ?? [];
      } catch {
        return [];
      } finally {
        setLoading(false);
      }
    },
    [ws],
  );

  const readFile = useCallback(
    async (teamId: string, fileName: string, channel?: string, chatId?: string) => {
      const res = await ws.call<{ file: TeamWorkspaceFile; content: string }>(
        Methods.TEAMS_WORKSPACE_READ,
        { team_id: teamId, file_name: fileName, channel: channel ?? "", chat_id: chatId ?? "" },
      );
      return res;
    },
    [ws],
  );

  const deleteFile = useCallback(
    async (teamId: string, fileName: string, channel?: string, chatId?: string) => {
      await ws.call(Methods.TEAMS_WORKSPACE_DELETE, {
        team_id: teamId,
        file_name: fileName,
        channel: channel ?? "",
        chat_id: chatId ?? "",
      });
    },
    [ws],
  );

  return { files, loading, listFiles, readFile, deleteFile };
}
