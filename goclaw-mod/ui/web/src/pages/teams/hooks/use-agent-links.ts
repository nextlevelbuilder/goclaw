import { useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";

export interface AgentLinkData {
  id: string;
  source_agent_id: string;
  target_agent_id: string;
  source_agent_key: string;
  source_display_name: string;
  source_emoji: string;
  target_agent_key: string;
  target_display_name: string;
  target_emoji: string;
  direction: string;
  description: string;
  max_concurrent: number;
  status: string;
  created_at: string;
}

export interface CreateLinkParams {
  sourceAgent: string;
  targetAgent: string;
  direction: string;
  description?: string;
  maxConcurrent?: number;
}

export interface UpdateLinkParams {
  linkId: string;
  direction?: string;
  description?: string;
  maxConcurrent?: number;
  status?: string;
}

export function useAgentLinks() {
  const ws = useWs();

  const listLinks = useCallback(
    async (agentId: string, direction = "all") => {
      const res = await ws.call<{ links: AgentLinkData[]; count: number }>(
        Methods.AGENTS_LINKS_LIST,
        { agentId, direction },
      );
      return res.links ?? [];
    },
    [ws],
  );

  const createLink = useCallback(
    async (params: CreateLinkParams) => {
      try {
        const res = await ws.call<{ link: AgentLinkData }>(
          Methods.AGENTS_LINKS_CREATE,
          {
            sourceAgent: params.sourceAgent,
            targetAgent: params.targetAgent,
            direction: params.direction,
            description: params.description ?? "",
            maxConcurrent: params.maxConcurrent ?? 5,
          },
        );
        toast.success(i18next.t("teams:links.created"));
        return res.link;
      } catch (err) {
        toast.error(i18next.t("teams:links.createFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const updateLink = useCallback(
    async (params: UpdateLinkParams) => {
      const body: Record<string, unknown> = { linkId: params.linkId };
      if (params.direction !== undefined) body.direction = params.direction;
      if (params.description !== undefined) body.description = params.description;
      if (params.maxConcurrent !== undefined) body.maxConcurrent = params.maxConcurrent;
      if (params.status !== undefined) body.status = params.status;
      try {
        await ws.call(Methods.AGENTS_LINKS_UPDATE, body);
        toast.success(i18next.t("teams:links.updated"));
      } catch (err) {
        toast.error(i18next.t("teams:links.updateFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  const deleteLink = useCallback(
    async (linkId: string) => {
      try {
        await ws.call(Methods.AGENTS_LINKS_DELETE, { linkId });
        toast.success(i18next.t("teams:links.deleted"));
      } catch (err) {
        toast.error(i18next.t("teams:links.deleteFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [ws],
  );

  return { listLinks, createLink, updateLink, deleteLink };
}
