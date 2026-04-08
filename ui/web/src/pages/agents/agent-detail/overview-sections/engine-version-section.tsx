import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Cpu, Workflow, Brain, Search } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { useV3Flags } from "@/hooks/use-v3-flags";
import { V3InfoModal } from "@/components/agents/v3-info-modal/v3-info-modal";
import { cn } from "@/lib/utils";

interface EngineVersionSectionProps {
  agentId: string;
}

export function EngineVersionSection({ agentId }: EngineVersionSectionProps) {
  const { t } = useTranslation("agents");
  const { flags, loading, toggleFlag, batchUpdate } = useV3Flags(agentId);
  const [infoOpen, setInfoOpen] = useState(false);

  if (loading || !flags) return null;

  const isV3 = flags.v3_pipeline_enabled;

  const handleSelectV2 = () => {
    if (!isV3 || loading) return;
    batchUpdate({ v3_pipeline_enabled: false, v3_memory_enabled: false, v3_retrieval_enabled: false });
  };

  const handleSelectV3 = () => {
    if (isV3 || loading) return;
    toggleFlag("v3_pipeline_enabled", true);
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Cpu className="h-4 w-4 text-blue-500 shrink-0" />
          <h3 className="text-sm font-medium">{t("detail.engine.title")}</h3>
        </div>
        <button
          onClick={() => setInfoOpen(true)}
          className="text-xs text-blue-600 hover:underline dark:text-blue-400 cursor-pointer py-1 px-2 -mr-2"
        >
          {t("detail.engine.learnMore")} &rarr;
        </button>
      </div>

      {/* Version cards */}
      <div className="grid grid-cols-2 gap-2">
        <VersionCard
          active={!isV3}
          label={t("detail.engine.classicLabel")}
          version="v2"
          desc={t("detail.engine.classicDesc")}
          onClick={handleSelectV2}
        />
        <VersionCard
          active={isV3}
          label={t("detail.engine.nextGenLabel")}
          version="v3"
          desc={t("detail.engine.nextGenDesc")}
          onClick={handleSelectV3}
        />
      </div>

      {/* Feature mini-cards (v3 only) */}
      {isV3 && (
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
      )}

      <V3InfoModal open={infoOpen} onOpenChange={setInfoOpen} />
    </section>
  );
}

function VersionCard({ active, label, version, desc, onClick }: {
  active: boolean; label: string; version: string; desc: string; onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "flex flex-col items-start gap-1 rounded-lg border-2 p-3 text-left transition-colors cursor-pointer",
        active
          ? "border-blue-500 bg-blue-50/50 dark:bg-blue-950/20"
          : "border-transparent bg-muted/50 hover:bg-muted",
      )}
    >
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium">{label}</span>
        <Badge variant="outline" className={cn(
          "text-[10px]",
          active && version === "v3" && "border-blue-300 text-blue-700 dark:border-blue-700 dark:text-blue-300",
        )}>
          {version}
        </Badge>
      </div>
      <p className="text-xs text-muted-foreground">{desc}</p>
    </button>
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
