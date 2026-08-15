import { useTranslation } from "react-i18next";
import { Users, TrendingUp } from "lucide-react";
import { CapabilityCard } from "./capability-card";

export function OrchestrationTab() {
  const { t } = useTranslation("v3-capabilities");

  return (
    <div className="space-y-3 pt-2">
      <CapabilityCard
        icon={Users}
        title={t("orchestration.delegateTitle")}
        description={t("orchestration.delegateDesc")}
      />
      <CapabilityCard
        icon={TrendingUp}
        title={t("orchestration.evolutionTitle")}
        description={t("orchestration.evolutionDesc")}
      />
    </div>
  );
}
