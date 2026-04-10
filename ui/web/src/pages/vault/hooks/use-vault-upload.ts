import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type { VaultDocument } from "@/types/vault";

const VAULT_KEY = "vault";

interface UploadResult {
  documents: Array<{ document?: VaultDocument; error?: string }>;
  count: number;
}

/** Upload files to vault via multipart POST /v1/vault/upload. */
export function useVaultUpload() {
  const http = useHttp();
  const queryClient = useQueryClient();
  const [isPending, setIsPending] = useState(false);

  const upload = useCallback(
    async (files: File[], opts?: { agentId?: string; teamId?: string }) => {
      setIsPending(true);
      try {
        const form = new FormData();
        if (opts?.agentId) form.append("agent_id", opts.agentId);
        if (opts?.teamId) form.append("team_id", opts.teamId);
        for (const f of files) form.append("files", f);

        const result = await http.upload<UploadResult>("/v1/vault/upload", form);
        await queryClient.invalidateQueries({ queryKey: [VAULT_KEY] });
        toast.success(i18n.t("vault:toast.uploadSuccess", { count: result.count }));
        return result;
      } catch (err) {
        toast.error(i18n.t("vault:toast.uploadFailed"), err instanceof Error ? err.message : "");
        throw err;
      } finally {
        setIsPending(false);
      }
    },
    [http, queryClient],
  );

  return { upload, isPending };
}
