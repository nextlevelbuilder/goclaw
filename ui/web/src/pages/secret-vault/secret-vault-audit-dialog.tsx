import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { useSecretVaultAudit } from "./hooks/use-secret-vault";
import type { SecretVaultEntry } from "@/types/secret-vault";

interface Props {
  entry: SecretVaultEntry | null;
  onClose: () => void;
}

export function SecretVaultAuditDialog({ entry, onClose }: Props) {
  const { t } = useTranslation("secret-vault");
  const { rows, loading } = useSecretVaultAudit(entry?.id ?? "", !!entry);

  return (
    <Dialog open={!!entry} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("audit.title")}</DialogTitle>
          <DialogDescription>{t("audit.description", { name: entry?.name })}</DialogDescription>
        </DialogHeader>

        {rows.length === 0 && !loading ? (
          <p className="text-sm text-muted-foreground">{t("audit.empty")}</p>
        ) : (
          <div className="max-h-96 overflow-auto overscroll-contain rounded-md border">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-3 py-2 text-left font-medium">{t("audit.columns.time")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("audit.columns.action")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("audit.columns.host")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("audit.columns.outcome")}</th>
                  <th className="px-3 py-2 text-left font-medium">{t("audit.columns.user")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} className="border-b last:border-0">
                    <td className="px-3 py-2 whitespace-nowrap text-muted-foreground">
                      {new Date(r.created_at).toLocaleString()}
                    </td>
                    <td className="px-3 py-2">{r.action}</td>
                    <td className="px-3 py-2 font-mono text-xs">{r.target_host || "-"}</td>
                    <td className="px-3 py-2">{r.outcome}</td>
                    <td className="px-3 py-2">{r.user_id || "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
