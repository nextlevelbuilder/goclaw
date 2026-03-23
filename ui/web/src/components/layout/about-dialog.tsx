import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/stores/use-auth-store";
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

  const version = serverInfo?.version || "dev";

  const rows = [
    { label: t("about.version"), value: version },
    {
      label: t("about.sourceCode"),
      href: "https://github.com/nextlevelbuilder/goclaw",
    },
    { label: t("about.license"), value: "MIT" },
    {
      label: t("about.documentation"),
      href: "https://docs.goclaw.sh",
    },
    {
      label: t("about.reportBug"),
      href: "https://github.com/nextlevelbuilder/goclaw/issues",
    },
  ];

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
          {rows.map((row) => (
            <div
              key={row.label}
              className="grid grid-cols-[140px_1fr] items-baseline gap-2 text-sm"
            >
              <span className="text-muted-foreground">{row.label}</span>
              {"href" in row && row.href ? (
                <a
                  href={row.href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary hover:underline break-all"
                >
                  {row.href}
                </a>
              ) : (
                <span className="font-medium">{row.value}</span>
              )}
            </div>
          ))}
        </div>

        <DialogFooter showCloseButton />
      </DialogContent>
    </Dialog>
  );
}
