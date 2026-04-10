import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";

export interface PreflightResult {
  pg_dump_available: boolean;
  disk_space_ok: boolean;
  db_size_bytes: number;
  db_size_human: string;
  free_disk_bytes: number;
  free_disk_human: string;
  data_dir_size_bytes: number;
  data_dir_size_human: string;
  workspace_size_bytes: number;
  workspace_size_human: string;
  warnings: string[];
}

export function useBackupPreflight() {
  const http = useHttp();
  return useQuery({
    queryKey: ["backup-preflight"],
    queryFn: () => http.get<PreflightResult>("/v1/system/backup/preflight"),
    staleTime: 60_000,
  });
}
