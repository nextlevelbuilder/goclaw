import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";

export interface S3BackupEntry {
  key: string;
  size: number;
  last_modified: string;
}

export function useS3Backups(enabled: boolean) {
  const http = useHttp();
  return useQuery({
    queryKey: ["s3-backups"],
    queryFn: () => http.get<{ backups: S3BackupEntry[] }>("/v1/system/backup/s3/list"),
    enabled,
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  });
}
