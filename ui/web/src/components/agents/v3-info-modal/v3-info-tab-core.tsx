import { useTranslation } from "react-i18next";
import { Workflow, Shield, BookOpen } from "lucide-react";
import { V3FeatureCard } from "./v3-feature-card";

const FEATURES = [
  { key: "pipeline", icon: Workflow, iconColor: "text-blue-500" },
  { key: "resilience", icon: Shield, iconColor: "text-emerald-500" },
  { key: "registry", icon: BookOpen, iconColor: "text-violet-500" },
] as const;

export function V3InfoTabCore() {
  const { t } = useTranslation("agents");
  return (
    <div className="space-y-3">
      {FEATURES.map(({ key, icon, iconColor }) => (
        <V3FeatureCard
          key={key}
          icon={icon}
          iconColor={iconColor}
          title={t(`v3Info.features.${key}.title`)}
          stat={t(`v3Info.features.${key}.stat`)}
          comparison={t(`v3Info.features.${key}.v2v3`)}
          description={t(`v3Info.features.${key}.desc`)}
        />
      ))}
    </div>
  );
}
