import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/stores/use-auth-store";
import { useWsCall } from "@/hooks/use-ws-call";
import { Methods } from "@/api/protocol";
import type { HealthPayload } from "@/pages/overview/types";
import { cleanVersion } from "@/lib/clean-version";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";

interface AboutDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function AboutDialog({ open, onOpenChange }: AboutDialogProps) {
  const { t } = useTranslation("topbar");
  const serverInfo = useAuthStore((s) => s.serverInfo);
  const connected = useAuthStore((s) => s.connected);
  const { call: fetchHealth, data: health } =
    useWsCall<HealthPayload>(Methods.HEALTH);

  useEffect(() => {
    if (open && connected) {
      fetchHealth();
    }
  }, [open, connected, fetchHealth]);

  const rawVersion = health?.version || serverInfo?.version || "dev";
  const version = cleanVersion(rawVersion);
  const latestVersion = health?.latestVersion;
  const updateAvailable = health?.updateAvailable ?? false;
  const updateUrl = health?.updateUrl;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2.5">
            <img src="/goclaw-icon.svg" alt="GoClaw" className="h-7 w-7" />
            {t("about.title")}
          </DialogTitle>
        </DialogHeader>

        <div className="grid gap-3 py-2">
          {/* Version */}
          <div className="grid grid-cols-[140px_1fr] items-baseline gap-2 text-sm">
            <span className="text-muted-foreground">{t("about.version")}</span>
            <div className="flex items-center gap-2">
              <span className="font-medium">{version}</span>
              {!updateAvailable && latestVersion && (
                <span className="rounded-full bg-green-500/15 px-2 py-0.5 text-xs text-green-600 dark:text-green-400">
                  {t("about.upToDate")}
                </span>
              )}
            </div>
          </div>

          {/* Update available banner */}
          {updateAvailable && latestVersion && (
            <div className="rounded-lg border border-primary/30 bg-primary/5 p-3 text-sm">
              <div className="font-medium">
                {t("about.updateAvailable", { version: latestVersion })}
              </div>
              {updateUrl && (
                <a
                  href={updateUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-1 inline-block text-primary hover:underline"
                >
                  {t("about.viewRelease")}
                </a>
              )}
            </div>
          )}

          {/* Source Code */}
          <div className="grid grid-cols-[140px_1fr] items-baseline gap-2 text-sm">
            <span className="text-muted-foreground">{t("about.sourceCode")}</span>
            <a
              href="https://github.com/nextlevelbuilder/goclaw"
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary hover:underline break-all"
            >
              github.com/nextlevelbuilder/goclaw
            </a>
          </div>

          {/* License */}
          <div className="grid grid-cols-[140px_1fr] items-baseline gap-2 text-sm">
            <span className="text-muted-foreground">{t("about.license")}</span>
            <span className="font-medium">MIT</span>
          </div>

          {/* Documentation */}
          <div className="grid grid-cols-[140px_1fr] items-baseline gap-2 text-sm">
            <span className="text-muted-foreground">{t("about.documentation")}</span>
            <a
              href="https://docs.goclaw.sh"
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary hover:underline"
            >
              docs.goclaw.sh
            </a>
          </div>

          {/* Report Bug */}
          <div className="grid grid-cols-[140px_1fr] items-baseline gap-2 text-sm">
            <span className="text-muted-foreground">{t("about.reportBug")}</span>
            <a
              href="https://github.com/nextlevelbuilder/goclaw/issues"
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary hover:underline break-all"
            >
              github.com/.../issues
            </a>
          </div>
        </div>

        <DialogFooter showCloseButton />
      </DialogContent>
    </Dialog>
  );
}
