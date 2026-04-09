import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Cpu, Workflow, Brain, Search } from "lucide-react";
import { V3CapabilitiesModal } from "@/components/agents/v3-capabilities-modal/v3-capabilities-modal";

interface EngineVersionSectionProps {
  agentId: string;
}

export function EngineVersionSection({ agentId: _agentId }: EngineVersionSectionProps) {
  const { t } = useTranslation("agents");
  const [infoOpen, setInfoOpen] = useState(false);

  return (
    <section className="rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <Cpu className="h-4 w-4 text-blue-500 shrink-0" />
          <h3 className="text-sm font-medium">{t("detail.engine.title")}</h3>

          <div className="flex items-center gap-1.5">
            <FeatureBadge icon={Workflow} label={t("detail.engine.pipelineTitle")} />
            <FeatureBadge icon={Brain} label={t("detail.engine.memoryTitle")} />
            <FeatureBadge icon={Search} label={t("detail.engine.retrievalTitle")} />
          </div>
        </div>

        <button
          onClick={() => setInfoOpen(true)}
          className="text-xs text-blue-600 hover:underline dark:text-blue-400 cursor-pointer py-1 px-2 -mr-2 shrink-0"
        >
          {t("detail.engine.learnMore")} &rarr;
        </button>
      </div>

      <V3CapabilitiesModal open={infoOpen} onOpenChange={setInfoOpen} />
    </section>
  );
}

function FeatureBadge({ icon: Icon, label }: { icon: typeof Workflow; label: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-blue-50 px-2 py-0.5 text-xs-plus font-medium text-blue-700 dark:bg-blue-900/30 dark:text-blue-300">
      <Icon className="h-3 w-3" />
      {label}
    </span>
  );
}
