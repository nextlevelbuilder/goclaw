import { useCallback, useEffect, useRef } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useHttp, useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { toast } from "@/stores/use-toast-store";
import { queryKeys } from "@/lib/query-keys";

// --- Shape mirrors backend PackageUpdateInfo ---
export interface UpdateMeta {
  repo?: string;
  assetName?: string;
  assetURL?: string;
  assetSizeBytes?: number;
  assetSHA256?: string;
  prerelease?: boolean;
  [key: string]: unknown;
}

export interface UpdateInfo {
  source: "github" | "pip" | "npm" | string;
  name: string;
  currentVersion: string;
  latestVersion: string;
  checkedAt: string;
  meta?: UpdateMeta;
}

export interface UpdatesResponse {
  updates: UpdateInfo[];
  checkedAt: string;
  ageSeconds: number;
  ttlSeconds: number;
  stale: boolean;
  sources: string[];
  /** Map of source → available (false = runtime not present in container) */
  availability?: Record<string, boolean>;
}

interface UpdateResult {
  ok: boolean;
  fromVersion: string;
  toVersion: string;
  error?: string;
  manifestDesynced?: boolean;
}

interface ApplyAllSucceeded {
  package: string;
  fromVersion: string;
  toVersion: string;
}

interface ApplyAllFailed {
  package: string;
  reason: string;
}

export interface ApplyAllResult {
  succeeded: ApplyAllSucceeded[];
  failed: ApplyAllFailed[];
  durationMs: number;
}

// WS event payloads
interface WsUpdateChecked { count: number; checked_at: string }
interface WsUpdateStarted { source: string; name: string; from_version: string; to_version: string }
interface WsUpdateSucceeded { source: string; name: string; from_version: string; to_version: string; duration_ms: number }
interface WsUpdateFailed { source: string; name: string; reason: string }

export function useUpdates() {
  const http = useHttp();
  const ws = useWs();
  const qc = useQueryClient();
  const connected = useAuthStore((s) => s.connected);

  const { data, isFetching: loading, refetch } = useQuery<UpdatesResponse>({
    queryKey: queryKeys.packages.updates,
    queryFn: () => http.get<UpdatesResponse>("/v1/packages/updates"),
    staleTime: 60_000,
    enabled: connected,
  });

  // --- refresh mutation ---
  const refreshMutation = useMutation({
    mutationFn: () => http.post<void>("/v1/packages/updates/refresh"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.packages.updates });
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Refresh failed: ${msg}`);
    },
  });

  const refresh = useCallback(() => {
    refreshMutation.mutate();
  }, [refreshMutation]);

  // --- single package update mutation ---
  // Returns the mutation object so callers can track isPending per-spec
  const updatePackageMutation = useMutation({
    mutationFn: ({ spec, toVersion }: { spec: string; toVersion?: string }) =>
      http.post<UpdateResult>("/v1/packages/update", {
        package: spec,
        ...(toVersion ? { toVersion } : {}),
      }),
    onSuccess: (res) => {
      if (res.ok) {
        qc.invalidateQueries({ queryKey: queryKeys.packages.updates });
        qc.invalidateQueries({ queryKey: queryKeys.packages.all });
        if (res.manifestDesynced) {
          // Surface manifest desync as a warning toast — update still succeeded
          toast.warning(`Updated but manifest save failed (${res.toVersion}). Manual recovery may be required.`);
        }
      } else if (res.error) {
        toast.error(`Update failed: ${res.error}`);
      }
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Update failed: ${msg}`);
    },
  });

  const updatePackage = useCallback(
    (spec: string, toVersion?: string) => {
      updatePackageMutation.mutate({ spec, toVersion });
    },
    [updatePackageMutation],
  );

  // --- apply-all mutation ---
  const applyAllMutation = useMutation({
    mutationFn: (specs?: string[]) =>
      http.post<ApplyAllResult>("/v1/packages/updates/apply-all", {
        // Always send body; empty array = update all
        packages: specs ?? [],
      }),
    // apply-all always returns HTTP 200 — inspect failed.length for errors
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: queryKeys.packages.updates });
      qc.invalidateQueries({ queryKey: queryKeys.packages.all });
      if (res.failed.length === 0) {
        toast.success(`All ${res.succeeded.length} packages updated successfully`);
      } else if (res.succeeded.length === 0) {
        toast.error(`All updates failed (${res.failed.length} errors)`);
      } else {
        toast.warning(
          `${res.succeeded.length} succeeded, ${res.failed.length} failed`,
        );
      }
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Apply-all failed: ${msg}`);
    },
  });

  const applyAll = useCallback(
    (specs?: string[]) => applyAllMutation.mutateAsync(specs),
    [applyAllMutation],
  );

  // --- WS event subscriptions ---
  // Use a ref so the handler closure doesn't go stale
  const refetchRef = useRef(refetch);
  refetchRef.current = refetch;

  useEffect(() => {
    // Re-query when the server says updates have been refreshed
    const offChecked = ws.on("package.update.checked", (payload: unknown) => {
      // Payload: { count, checked_at } — we only need to re-read the list
      void (payload as WsUpdateChecked); // consumed by type annotation
      qc.invalidateQueries({ queryKey: queryKeys.packages.updates });
    });

    // Show toast when an individual update finishes
    const offSucceeded = ws.on("package.update.succeeded", (payload: unknown) => {
      const p = payload as WsUpdateSucceeded;
      qc.invalidateQueries({ queryKey: queryKeys.packages.updates });
      toast.success(`${p.name} updated to ${p.to_version}`);
    });

    const offFailed = ws.on("package.update.failed", (payload: unknown) => {
      const p = payload as WsUpdateFailed;
      toast.error(`Failed to update ${p.name}: ${p.reason}`);
    });

    // "started" event — UI state already reflects pending; no action needed
    const offStarted = ws.on("package.update.started", (_payload: unknown) => {
      void (_payload as WsUpdateStarted);
    });

    return () => {
      offChecked();
      offSucceeded();
      offFailed();
      offStarted();
    };
  }, [ws, qc]);

  return {
    updates: data?.updates ?? [],
    checkedAt: data?.checkedAt,
    ageSeconds: data?.ageSeconds,
    stale: data?.stale ?? false,
    availability: data?.availability,
    loading: loading || refreshMutation.isPending,
    refresh,
    updatePackage,
    updatePackagePending: updatePackageMutation.isPending,
    applyAll,
    applyAllPending: applyAllMutation.isPending,
    applyAllResult: applyAllMutation.data,
  };
}
