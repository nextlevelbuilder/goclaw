import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs } from "@/hooks/use-ws";
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
  deliver: boolean;
  channel: string;
  to: string;
}

export interface CronJob {
  id: string;
  name: string;
  agentId?: string;
  enabled: boolean;
  schedule: CronSchedule;
  payload: CronPayload;
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
}

export function useCron() {
  const ws = useWs();
  const queryClient = useQueryClient();

  const { data: jobs = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.cron.all,
    queryFn: async () => {
      if (!ws.isConnected) return [];
      const res = await ws.call<{ jobs: CronJob[] }>(Methods.CRON_LIST, {
        includeDisabled: true,
      });
      return res.jobs ?? [];
    },
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
        toast.success("Cron job created", `${params.name} has been added`);
      } catch (err) {
        toast.error("Failed to create cron job", err instanceof Error ? err.message : "Unknown error");
        throw err;
      }
    },
    [ws, invalidate],
  );

  const toggleJob = useCallback(
    async (jobId: string, enabled: boolean) => {
      try {
        await ws.call(Methods.CRON_TOGGLE, { jobId, enabled });
        await invalidate();
        toast.success(enabled ? "Cron job enabled" : "Cron job disabled");
      } catch (err) {
        toast.error("Failed to toggle cron job", err instanceof Error ? err.message : "Unknown error");
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
        toast.success("Cron job deleted");
      } catch (err) {
        toast.error("Failed to delete cron job", err instanceof Error ? err.message : "Unknown error");
        throw err;
      }
    },
    [ws, invalidate],
  );

  const runJob = useCallback(
    async (jobId: string) => {
      try {
        await ws.call(Methods.CRON_RUN, { jobId, mode: "force" });
        toast.success("Cron job triggered");
      } catch (err) {
        toast.error("Failed to run cron job", err instanceof Error ? err.message : "Unknown error");
        throw err;
      }
    },
    [ws],
  );

  const getRunLog = useCallback(
    async (jobId: string, limit = 20): Promise<CronRunLogEntry[]> => {
      if (!ws.isConnected) return [];
      const res = await ws.call<{ entries: CronRunLogEntry[] }>(Methods.CRON_RUNS, {
        jobId,
        limit,
      });
      return res.entries ?? [];
    },
    [ws],
  );

  return { jobs, loading, refresh: invalidate, createJob, toggleJob, deleteJob, runJob, getRunLog };
}
