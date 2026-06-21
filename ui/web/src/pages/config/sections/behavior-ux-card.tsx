import { Brain } from "lucide-react";
import { useTranslation } from "react-i18next";
import { FeatureSwitchGroup } from "@/components/shared/feature-switch-group";
import type { FeatureSwitchItem } from "@/components/shared/feature-switch-group";

interface UxValues {
  intent_classify: boolean;
}

interface Props {
  value: UxValues;
  onChange: (v: UxValues) => void;
}

/** High-impact UX toggles with icon, hint, and contextual info. */
export function BehaviorUxCard({ value, onChange }: Props) {
  const { t } = useTranslation("config");

  const items: FeatureSwitchItem[] = [
    {
      icon: Brain,
      iconClass: "text-orange-500",
      label: t("agents.intentClassify"),
      hint: t("behavior.intentClassifyHint"),
      checked: value.intent_classify !== false,
      onCheckedChange: (v) => onChange({ ...value, intent_classify: v }),
      infoWhenOn: t("behavior.intentClassifyInfo"),
      infoClass: "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-800 dark:bg-orange-950/30 dark:text-orange-300",
    },
  ];

  return (
    <FeatureSwitchGroup
      title={t("behavior.uxTitle")}
      description={t("behavior.uxDescription")}
      items={items}
      highlight
    />
  );
}
