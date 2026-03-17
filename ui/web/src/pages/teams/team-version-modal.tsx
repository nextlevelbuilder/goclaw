import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Check, Minus,
  ListTodo, FolderOpen, Lock, Bell,
  ClipboardCheck, BarChart3, MessageSquare, RotateCcw, ShieldCheck,
} from "lucide-react";
import { useTranslation } from "react-i18next";

interface TeamVersionModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const FEATURES = [
  { key: "taskManagement", icon: ListTodo, v1: true },
  { key: "sharedWorkspace", icon: FolderOpen, v1: true },
  { key: "executionLocking", icon: Lock, v1: false },
  { key: "followupReminders", icon: Bell, v1: false },
  { key: "progressTracking", icon: BarChart3, v1: false },
  { key: "autoRecovery", icon: RotateCcw, v1: false },
  { key: "commentsAudit", icon: MessageSquare, v1: false, comingSoon: true },
  { key: "escalationPolicy", icon: ShieldCheck, v1: false, comingSoon: true },
  { key: "reviewWorkflow", icon: ClipboardCheck, v1: false, comingSoon: true },
] as const;

export function TeamVersionModal({ open, onOpenChange }: TeamVersionModalProps) {
  const { t } = useTranslation("teams");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] w-[95vw] flex flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {t("settings.versionModal.title")}
            <Badge className="bg-gradient-to-r from-violet-500 to-indigo-500 text-[10px] px-2 py-0.5 text-white border-0 font-semibold">
              Beta
            </Badge>
          </DialogTitle>
          <DialogDescription className="sr-only">{t("settings.versionModal.title")}</DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 space-y-4 -mx-4 px-4 sm:-mx-6 sm:px-6">
        {/* Version columns header */}
        <div className="grid grid-cols-2 gap-3">
          <div className="rounded-lg border bg-muted/30 p-3 text-center">
            <p className="text-lg font-bold">V1</p>
            <p className="text-xs text-muted-foreground">{t("settings.versionBasicDesc")}</p>
          </div>
          <div className="rounded-lg border border-violet-500/30 bg-gradient-to-br from-violet-500/5 to-indigo-500/5 p-3 text-center">
            <p className="text-lg font-bold bg-gradient-to-r from-violet-600 to-indigo-600 bg-clip-text text-transparent">V2 Super Team</p>
            <p className="text-xs text-muted-foreground">{t("settings.versionAdvancedDesc")}</p>
          </div>
        </div>

        {/* Feature list */}
        <div className="space-y-1">
          {FEATURES.map((f) => {
            const Icon = f.icon;
            const dimmed = "comingSoon" in f && f.comingSoon;
            return (
              <div
                key={f.key}
                className={`group grid grid-cols-[1fr_60px_60px] items-center gap-2 rounded-lg px-3 py-2.5 hover:bg-muted/30 ${dimmed ? "opacity-40" : ""}`}
              >
                <div className="flex items-start gap-3 min-w-0">
                  <Icon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0">
                    <p className="text-sm font-medium flex items-center gap-2">
                      {t(`settings.versionModal.${f.key}`)}
                      {dimmed && (
                        <Badge variant="outline" className="text-[9px] px-1.5 py-0 font-normal">
                          {t("settings.versionModal.comingSoon")}
                        </Badge>
                      )}
                    </p>
                    <p className="text-xs text-muted-foreground leading-relaxed">
                      {t(`settings.versionModal.${f.key}Desc`)}
                    </p>
                  </div>
                </div>
                <div className="flex justify-center">
                  {f.v1 ? (
                    <Check className="h-4 w-4 text-green-600 dark:text-green-400" />
                  ) : (
                    <Minus className="h-4 w-4 text-muted-foreground/30" />
                  )}
                </div>
                <div className="flex justify-center">
                  {dimmed ? (
                    <Minus className="h-4 w-4 text-muted-foreground/30" />
                  ) : (
                    <Check className="h-4 w-4 text-green-600 dark:text-green-400" />
                  )}
                </div>
              </div>
            );
          })}
        </div>

        {/* Column labels for V1/V2 */}
        <div className="grid grid-cols-[1fr_60px_60px] gap-2 px-3 text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
          <span />
          <span className="text-center">V1</span>
          <span className="text-center">V2</span>
        </div>

        {/* Notes */}
        <div className="space-y-1 rounded-lg border bg-muted/20 px-4 py-3 text-xs text-muted-foreground">
          <p>{t("settings.versionModal.downgradeNote")}</p>
          <p>{t("settings.versionModal.betaNote")}</p>
        </div>

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
