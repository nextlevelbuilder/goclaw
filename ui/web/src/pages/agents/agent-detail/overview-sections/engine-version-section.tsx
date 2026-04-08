import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Cpu, Workflow, Brain, Search } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { useV3Flags } from "@/hooks/use-v3-flags";
import { V3InfoModal } from "@/components/agents/v3-info-modal/v3-info-modal";

interface EngineVersionSectionProps {
  agentId: string;
}

export function EngineVersionSection({ agentId }: EngineVersionSectionProps) {
  const { t } = useTranslation("agents");
  const { flags, loading, toggleFlag } = useV3Flags(agentId);
  const [infoOpen, setInfoOpen] = useState(false);

  if (loading || !flags) return null;

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Cpu className="h-4 w-4 text-blue-500 shrink-0" />
          <h3 className="text-sm font-medium">{t("detail.engine.title")}</h3>
          <Badge variant="outline" className="text-[10px] border-blue-300 text-blue-700 dark:border-blue-700 dark:text-blue-300">
            v3
          </Badge>
        </div>
        <button
          onClick={() => setInfoOpen(true)}
          className="text-xs text-blue-600 hover:underline dark:text-blue-400 cursor-pointer py-1 px-2 -mr-2"
        >
          {t("detail.engine.learnMore")} &rarr;
        </button>
      </div>

      {/* Feature mini-cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
        <FeatureMiniCard
          icon={Workflow}
          title={t("detail.engine.pipelineTitle")}
          hint={t("detail.engine.pipelineHint")}
          checked={true}
          onChange={() => {}}
          disabled
        />
        <FeatureMiniCard
          icon={Brain}
          title={t("detail.engine.memoryTitle")}
          hint={t("detail.engine.memoryHint")}
          checked={flags.v3_memory_enabled}
          onChange={(v) => toggleFlag("v3_memory_enabled", v)}
        />
        <FeatureMiniCard
          icon={Search}
          title={t("detail.engine.retrievalTitle")}
          hint={t("detail.engine.retrievalHint")}
          checked={flags.v3_retrieval_enabled}
          onChange={(v) => toggleFlag("v3_retrieval_enabled", v)}
        />
      </div>

      <V3InfoModal open={infoOpen} onOpenChange={setInfoOpen} />
    </section>
  );
}

function FeatureMiniCard({ icon: Icon, title, hint, checked, onChange, disabled }: {
  icon: LucideIcon; title: string; hint: string;
  checked: boolean; onChange: (v: boolean) => void; disabled?: boolean;
}) {
  return (
    <div className="flex items-start gap-2.5 rounded-lg border p-2.5">
      <Icon className="h-4 w-4 shrink-0 text-blue-500 mt-0.5" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-medium">{title}</span>
          <Switch size="sm" checked={checked} onCheckedChange={onChange} disabled={disabled} />
        </div>
        <p className="text-[11px] text-muted-foreground mt-0.5">{hint}</p>
      </div>
    </div>
  );
}
