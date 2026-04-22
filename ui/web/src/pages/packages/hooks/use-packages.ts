import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { toast } from "@/stores/use-toast-store";
import { queryKeys } from "@/lib/query-keys";

export interface PackageInfo {
  name: string;
  version: string;
}

// Viewer-safe projection — mirrors GitHubPackageListEntry on the Go side.
// asset_url / sha256 / asset_name are deliberately stripped from the list
// response and are not exposed to viewer-level callers.
export interface GitHubPackageInfo {
  name: string;
  repo: string;
  tag: string;
  binaries: string[];
  installed_at: string;
}

export interface InstalledPackages {
  system: PackageInfo[] | null;
  pip: PackageInfo[] | null;
  npm: PackageInfo[] | null;
  github?: GitHubPackageInfo[] | null;
}

interface InstallResult {
  ok: boolean;
  error: string;
}


export function usePackages() {
  const http = useHttp();
  const qc = useQueryClient();
  const connected = useAuthStore((s) => s.connected);

  const { data, isFetching: loading, refetch } = useQuery({
    queryKey: queryKeys.packages.all,
    queryFn: () => http.get<InstalledPackages>("/v1/packages"),
    staleTime: 60_000,
    enabled: connected,
  });

  const refresh = useCallback(() => { refetch(); }, [refetch]);

  const installPackage = useCallback(async (pkg: string, t: (key: string, opts?: Record<string, string>) => string) => {
    try {
      const res = await http.post<InstallResult>("/v1/packages/install", { package: pkg });
      if (res.ok) {
        toast.success(t("messages.installSuccess", { name: pkg }));
        qc.invalidateQueries({ queryKey: queryKeys.packages.all });
        qc.invalidateQueries({ queryKey: queryKeys.packages.runtimes });
      } else {
        toast.error(t("messages.installError", { name: pkg }) + (res.error ? `: ${res.error}` : ""));
      }
      return res;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t("messages.installError", { name: pkg }) + (msg ? `: ${msg}` : ""));
      return { ok: false, error: msg } as InstallResult;
    }
  }, [http, qc]);

  const uninstallPackage = useCallback(async (pkg: string, t: (key: string, opts?: Record<string, string>) => string) => {
    try {
      const res = await http.post<InstallResult>("/v1/packages/uninstall", { package: pkg });
      if (res.ok) {
        toast.success(t("messages.uninstallSuccess", { name: pkg }));
        qc.invalidateQueries({ queryKey: queryKeys.packages.all });
        qc.invalidateQueries({ queryKey: queryKeys.packages.runtimes });
      } else {
        toast.error(t("messages.uninstallError", { name: pkg }) + (res.error ? `: ${res.error}` : ""));
      }
      return res;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t("messages.uninstallError", { name: pkg }) + (msg ? `: ${msg}` : ""));
      return { ok: false, error: msg } as InstallResult;
    }
  }, [http, qc]);

  return { packages: data, loading, refresh, installPackage, uninstallPackage };
}
