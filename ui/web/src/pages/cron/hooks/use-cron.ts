import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";

export interface CronSchedule {
  kind: "at" | "every" | "cron";
  atMs?: number;
  everyMs?: number;
  expr?: string;
  tz?: string;
}

export interface CronPayload {
  kind: string;
  message: string;
  command?: string;
}

export interface CronJobPatch {
  name?: string;
  agentId?: string;
  enabled?: boolean;
  schedule?: CronSchedule;
  message?: string;
  deliver?: boolean;
  deliverChannel?: string;
  deliverTo?: string;
  deleteAfterRun?: boolean;
  wakeHeartbeat?: boolean;
  stateless?: boolean;
}

export interface CronJob {
  id: string;
  name: string;
  agentId?: string;
  enabled: boolean;
  schedule: CronSchedule;
  payload: CronPayload;
  deliver?: boolean;
  deliverChannel?: string;
  deliverTo?: string;
  wakeHeartbeat?: boolean;
  stateless?: boolean;
  createdAtMs: number;
  updatedAtMs: number;
  deleteAfterRun?: boolean;
  state?: {
    nextRunAtMs?: number;
    lastRunAtMs?: number;
    lastStatus?: string;
    lastError?: string;
  };
}

export interface CronRunLogEntry {
  ts: number;
  jobId: string;
  status?: string;
  error?: string;
  summary?: string;
  durationMs?: number;
  inputTokens?: number;
  outputTokens?: number;
}

export function useCron() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();

  const { data: jobs = [], isPending: loading, isFetching: refreshing } = useQuery({
    queryKey: queryKeys.cron.all,
    queryFn: async () => {
      const res = await ws.call<{ jobs: CronJob[] }>(Methods.CRON_LIST, {
        includeDisabled: true,
      });
      return res.jobs ?? [];
    },
    staleTime: 60_000,
    enabled: connected,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.cron.all }),
    [queryClient],
  );

  const createJob = useCallback(
    async (params: {
      name: string;
      schedule: CronSchedule;
      message: string;
      agentId?: string;
      deliver?: boolean;
      channel?: string;
      to?: string;
    }) => {
      try {
        await ws.call(Methods.CRON_CREATE, params);
        await invalidate();
        toast.success(i18next.t("cron:toast.created"), i18next.t("cron:toast.createdDesc", { name: params.name }));
      } catch (err) {
        toast.error(i18next.t("cron:toast.failedCreate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, invalidate],
  );

  const toggleJob = useCallback(
    async (jobId: string, enabled: boolean) => {
      try {
        queryClient.setQueryData<CronJob[]>(queryKeys.cron.all, (old) =>
          old?.map((j) => (j.id === jobId ? { ...j, enabled } : j)),
        );
        await ws.call(Methods.CRON_TOGGLE, { jobId, enabled });
        await invalidate();
        toast.success(enabled ? i18next.t("cron:toast.enabled") : i18next.t("cron:toast.disabled"));
      } catch (err) {
        toast.error(i18next.t("cron:toast.failedToggle"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, invalidate],
  );

  const deleteJob = useCallback(
    async (jobId: string) => {
      try {
        await ws.call(Methods.CRON_DELETE, { jobId });
        await invalidate();
        toast.success(i18next.t("cron:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("cron:toast.failedDelete"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, invalidate],
  );

  const runJob = useCallback(
    async (jobId: string) => {
      try {
        await ws.call(Methods.CRON_RUN, { jobId, mode: "force" });
        toast.success(i18next.t("cron:toast.triggered"));
      } catch (err) {
        toast.error(i18next.t("cron:toast.failedRun"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws],
  );

  const getRunLog = useCallback(
    async (jobId: string, limit = 20, offset = 0): Promise<{ entries: CronRunLogEntry[]; total: number }> => {
      if (!ws.isConnected) return { entries: [], total: 0 };
      const res = await ws.call<{ entries: CronRunLogEntry[]; total: number }>(Methods.CRON_RUNS, {
        jobId,
        limit,
        offset,
      });
      return { entries: res.entries ?? [], total: res.total ?? 0 };
    },
    [ws],
  );

  const updateJob = useCallback(
    async (jobId: string, params: CronJobPatch) => {
      try {
        await ws.call(Methods.CRON_UPDATE, { jobId, patch: params });
        await invalidate();
        toast.success(i18next.t("cron:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("cron:toast.failedUpdate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [ws, invalidate],
  );

  return { jobs, loading, refreshing, refresh: invalidate, createJob, toggleJob, deleteJob, runJob, getRunLog, updateJob };
}
