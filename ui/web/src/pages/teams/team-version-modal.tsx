import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Check, Minus } from "lucide-react";
import { useTranslation } from "react-i18next";

interface TeamVersionModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const FEATURES = [
  { key: "taskManagement", v1: true },
  { key: "sharedWorkspace", v1: true },
  { key: "executionLocking", v1: false },
  { key: "followupReminders", v1: false },
  { key: "reviewWorkflow", v1: false },
  { key: "progressTracking", v1: false },
  { key: "commentsAudit", v1: false },
  { key: "autoRecovery", v1: false },
  { key: "escalationPolicy", v1: false },
] as const;

export function TeamVersionModal({ open, onOpenChange }: TeamVersionModalProps) {
  const { t } = useTranslation("teams");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("settings.versionModal.title")}</DialogTitle>
        </DialogHeader>

        <div className="overflow-x-auto">
          <table className="w-full min-w-[400px] text-sm">
            <thead>
              <tr className="border-b">
                <th className="py-2 pr-4 text-left font-medium">{t("settings.versionModal.feature")}</th>
                <th className="w-16 py-2 text-center font-medium">V1</th>
                <th className="w-16 py-2 text-center font-medium">V2</th>
              </tr>
            </thead>
            <tbody>
              {FEATURES.map((f) => (
                <tr key={f.key} className="border-b last:border-0">
                  <td className="py-2.5 pr-4">
                    <div className="font-medium">{t(`settings.versionModal.${f.key}`)}</div>
                    <div className="text-xs text-muted-foreground">{t(`settings.versionModal.${f.key}Desc`)}</div>
                  </td>
                  <td className="py-2.5 text-center">
                    {f.v1 ? (
                      <Check className="mx-auto h-4 w-4 text-green-600 dark:text-green-400" />
                    ) : (
                      <Minus className="mx-auto h-4 w-4 text-muted-foreground/40" />
                    )}
                  </td>
                  <td className="py-2.5 text-center">
                    <Check className="mx-auto h-4 w-4 text-green-600 dark:text-green-400" />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="space-y-1 text-xs text-muted-foreground">
          <p>{t("settings.versionModal.downgradeNote")}</p>
          <p>{t("settings.versionModal.betaNote")}</p>
        </div>

        <div className="flex justify-end">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            {t("settings.versionModal.gotIt")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
