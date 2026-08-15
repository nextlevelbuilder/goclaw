import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";

export interface S3Config {
  configured: boolean;
  bucket?: string;
  region?: string;
  endpoint?: string;
  prefix?: string;
  access_key_id?: string;
}

export interface S3ConfigInput {
  access_key_id: string;
  secret_access_key: string;
  bucket: string;
  region: string;
  endpoint: string;
  prefix: string;
}

export function useS3Config() {
  const http = useHttp();
  return useQuery({
    queryKey: ["s3-config"],
    queryFn: () => http.get<S3Config>("/v1/system/backup/s3/config"),
    staleTime: 60_000,
  });
}

export function useSaveS3Config() {
  const http = useHttp();
  const queryClient = useQueryClient();
  const { t } = useTranslation("backup");

  return useMutation({
    mutationFn: (input: S3ConfigInput) =>
      http.put<{ status: string; bucket: string }>("/v1/system/backup/s3/config", input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["s3-config"] });
      toast.success(t("s3.saveSuccess"));
    },
    onError: (err: Error) => {
      toast.error(t("s3.saveError"), err.message);
    },
  });
}
