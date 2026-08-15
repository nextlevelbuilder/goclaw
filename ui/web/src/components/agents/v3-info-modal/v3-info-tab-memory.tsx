import { useTranslation } from "react-i18next";
import { Brain, Search, Library } from "lucide-react";
import { V3FeatureCard } from "./v3-feature-card";

const FEATURES = [
  { key: "memory", icon: Brain, iconColor: "text-pink-500" },
  { key: "retrieval", icon: Search, iconColor: "text-cyan-500" },
  { key: "vault", icon: Library, iconColor: "text-amber-500" },
] as const;

export function V3InfoTabMemory() {
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
