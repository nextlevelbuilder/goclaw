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
  Check,
  ListTodo, FolderOpen, Lock, Bell,
  ClipboardCheck, BarChart3, MessageSquare, RotateCcw, ShieldCheck,
} from "lucide-react";
import { useTranslation } from "react-i18next";

interface TeamFeaturesModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const FEATURES = [
  { key: "taskManagement", icon: ListTodo },
  { key: "sharedWorkspace", icon: FolderOpen },
  { key: "executionLocking", icon: Lock },
  { key: "followupReminders", icon: Bell },
  { key: "progressTracking", icon: BarChart3 },
  { key: "autoRecovery", icon: RotateCcw },
  { key: "commentsAudit", icon: MessageSquare },
  { key: "escalationPolicy", icon: ShieldCheck },
  { key: "reviewWorkflow", icon: ClipboardCheck },
] as const;

export function TeamFeaturesModal({ open, onOpenChange }: TeamFeaturesModalProps) {
  const { t } = useTranslation("teams");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] w-[95vw] flex flex-col sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {t("settings.versionModal.title")}
            <Badge className="bg-gradient-to-r from-orange-500 to-amber-500 text-[10px] px-2 py-0.5 text-white border-0 font-semibold">
              Beta
            </Badge>
          </DialogTitle>
          <DialogDescription className="sr-only">{t("settings.versionModal.title")}</DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 space-y-4 -mx-4 px-4 sm:-mx-6 sm:px-6">
          <div className="space-y-1">
            {FEATURES.map((f) => {
              const Icon = f.icon;
              return (
                <div
                  key={f.key}
                  className="group grid grid-cols-[1fr_40px] items-center gap-2 rounded-lg px-3 py-2.5 hover:bg-muted/30"
                >
                  <div className="flex items-start gap-3 min-w-0">
                    <Icon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                    <div className="min-w-0">
                      <p className="text-sm font-medium">
                        {String(t(`settings.versionModal.${f.key}`))}
                      </p>
                      <p className="text-xs text-muted-foreground leading-relaxed">
                        {String(t(`settings.versionModal.${f.key}Desc`))}
                      </p>
                    </div>
                  </div>
                  <div className="flex justify-center">
                    <Check className="h-4 w-4 text-green-600 dark:text-green-400" />
                  </div>
                </div>
              );
            })}
          </div>

          <div className="space-y-1 rounded-lg border bg-muted/20 px-4 py-3 text-xs text-muted-foreground">
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
