import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** Modal explaining v3 agent features compared to v2. */
export function AgentV3InfoModal({ open, onOpenChange }: Props) {
  const { t } = useTranslation("agents");

  const sections = [
    { icon: "🔄", titleKey: "v3Info.pipeline", descKey: "v3Info.pipelineDesc" },
    { icon: "🧠", titleKey: "v3Info.memory", descKey: "v3Info.memoryDesc" },
    { icon: "🔍", titleKey: "v3Info.retrieval", descKey: "v3Info.retrievalDesc" },
    { icon: "📊", titleKey: "v3Info.metrics", descKey: "v3Info.metricsDesc" },
    { icon: "💡", titleKey: "v3Info.suggestions", descKey: "v3Info.suggestionsDesc" },
  ];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {t("v3Info.title")}
            <Badge className="bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300">v3</Badge>
          </DialogTitle>
          <DialogDescription>{t("v3Info.subtitle")}</DialogDescription>
        </DialogHeader>

        <div className="space-y-3 pt-2">
          {sections.map((s) => (
            <div key={s.titleKey} className="flex gap-3 rounded-md border p-3">
              <span className="text-lg leading-none mt-0.5">{s.icon}</span>
              <div className="min-w-0">
                <p className="text-sm font-medium">{t(s.titleKey)}</p>
                <p className="text-xs text-muted-foreground mt-0.5">{t(s.descKey)}</p>
              </div>
            </div>
          ))}
        </div>

        <p className="text-xs text-muted-foreground pt-2">{t("v3Info.note")}</p>
      </DialogContent>
    </Dialog>
  );
}
