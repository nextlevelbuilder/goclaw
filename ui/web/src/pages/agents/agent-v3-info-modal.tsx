import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const FEATURES = [
  { key: "pipeline",   icon: "🔄", stat: "8 stages" },
  { key: "memory",     icon: "🧠", stat: "3 tiers" },
  { key: "retrieval",  icon: "🔍", stat: "Auto-inject" },
  { key: "vault",      icon: "📚", stat: "NEW" },
  { key: "evolution",  icon: "📊", stat: "3 stages" },
  { key: "orchestration", icon: "🤝", stat: "3 modes" },
  { key: "resilience", icon: "🛡️", stat: "9 error types" },
  { key: "registry",   icon: "🗂️", stat: "Auto-resolve" },
] as const;

/** Modal explaining v3 agent features compared to v2. */
export function AgentV3InfoModal({ open, onOpenChange }: Props) {
  const { t } = useTranslation("agents");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[85dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {t("v3Info.title")}
            <Badge className="bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300">v3</Badge>
          </DialogTitle>
          <DialogDescription>{t("v3Info.subtitle")}</DialogDescription>
        </DialogHeader>

        <div className="space-y-2.5 pt-1">
          {FEATURES.map(({ key, icon, stat }) => (
            <div key={key} className="flex gap-3 rounded-lg border p-3">
              <span className="text-lg leading-none mt-0.5 shrink-0">{icon}</span>
              <div className="min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <p className="text-sm font-medium">
                    {t(`v3Info.features.${key}.title`)}
                  </p>
                  <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                    {t(`v3Info.features.${key}.stat`, stat)}
                  </Badge>
                </div>
                <p className="text-xs text-orange-500/80 mt-0.5">
                  {t(`v3Info.features.${key}.v2v3`)}
                </p>
                <p className="text-xs text-muted-foreground mt-1">
                  {t(`v3Info.features.${key}.desc`)}
                </p>
              </div>
            </div>
          ))}
        </div>

        <p className="text-xs text-muted-foreground pt-1">{t("v3Info.note")}</p>
      </DialogContent>
    </Dialog>
  );
}
