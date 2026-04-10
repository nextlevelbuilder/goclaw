import { useTranslation } from "react-i18next";
import { GitFork, Library, Moon } from "lucide-react";
import { CapabilityCard } from "./capability-card";

export function KnowledgeTab() {
  const { t } = useTranslation("v3-capabilities");

  return (
    <div className="space-y-3 pt-2">
      <CapabilityCard
        icon={GitFork}
        title={t("knowledge.kgTitle")}
        description={t("knowledge.kgDesc")}
      />
      <CapabilityCard
        icon={Library}
        title={t("knowledge.vaultTitle")}
        description={t("knowledge.vaultDesc")}
      />
      <CapabilityCard
        icon={Moon}
        title={t("knowledge.dreamingTitle")}
        description={t("knowledge.dreamingDesc")}
      />
    </div>
  );
}
