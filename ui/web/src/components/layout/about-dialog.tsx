import { lazy, Suspense, useEffect, useState } from "react";
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
import {
  ChevronDown,
  ChevronRight,
  ExternalLink,
} from "lucide-react";

const ReactMarkdown = lazy(() => import("react-markdown"));

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

  const [notesExpanded, setNotesExpanded] = useState(false);

  useEffect(() => {
    if (open && connected) {
      fetchHealth();
    }
    if (!open) {
      setNotesExpanded(false);
    }
  }, [open, connected, fetchHealth]);

  const rawVersion = health?.version || serverInfo?.version || "dev";
  const version = cleanVersion(rawVersion);
  const latestVersion = health?.latestVersion;
  const updateAvailable = health?.updateAvailable ?? false;
  const updateUrl = health?.updateUrl;
  const releaseNotes = health?.releaseNotes;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2.5">
            <img src="/goclaw-icon.svg" alt="GoClaw" className="h-7 w-7" />
            {t("about.title")}
            {updateAvailable && latestVersion && (
              <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
                {latestVersion}
              </span>
            )}
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

              {/* Release notes (collapsible, markdown) */}
              {releaseNotes && (
                <div className="mt-2">
                  <button
                    onClick={() => setNotesExpanded((v) => !v)}
                    className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  >
                    {notesExpanded ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
                    {t("about.releaseNotes")}
                  </button>
                  {notesExpanded && (
                    <div className="mt-1.5 max-h-48 overflow-y-auto rounded border bg-muted/50 p-2.5 text-xs prose prose-xs dark:prose-invert prose-headings:text-sm prose-headings:font-semibold prose-headings:mt-2 prose-headings:mb-1 prose-p:my-1 prose-ul:my-1 prose-li:my-0">
                      <Suspense fallback={<span>{releaseNotes}</span>}>
                        <ReactMarkdown>{releaseNotes}</ReactMarkdown>
                      </Suspense>
                    </div>
                  )}
                </div>
              )}

              {updateUrl && (
                <a
                  href={updateUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-2 inline-flex items-center gap-1 text-xs text-primary hover:underline"
                >
                  {t("about.viewRelease")}
                  <ExternalLink className="size-3" />
                </a>
              )}
            </div>
          )}

          {/* Links */}
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
            {[
              { label: t("about.sourceCode"), href: "https://github.com/nextlevelbuilder/goclaw" },
              { label: t("about.license"), href: "https://creativecommons.org/licenses/by-nc/4.0/" },
              { label: t("about.documentation"), href: "https://docs.goclaw.sh" },
              { label: t("about.reportBug"), href: "https://github.com/nextlevelbuilder/goclaw/issues" },
            ].map(({ label, href }) => (
              <a
                key={label}
                href={href}
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary hover:underline"
              >
                {label}
              </a>
            ))}
          </div>
        </div>

        <DialogFooter showCloseButton />
      </DialogContent>
    </Dialog>
  );
}
