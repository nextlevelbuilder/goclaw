import { Eye, MessageSquareText, Brain } from "lucide-react";
import { useTranslation } from "react-i18next";
import { FeatureSwitchGroup } from "@/components/shared/feature-switch-group";
import type { FeatureSwitchItem } from "@/components/shared/feature-switch-group";

interface UxValues {
  tool_status: boolean;
  block_reply: boolean;
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
      icon: Eye,
      iconClass: "text-blue-500",
      label: t("gateway.toolStatus"),
      hint: t("behavior.toolStatusHint"),
      checked: value.tool_status !== false,
      onCheckedChange: (v) => onChange({ ...value, tool_status: v }),
      infoWhenOn: t("behavior.toolStatusInfo"),
      infoClass: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-300",
    },
    {
      icon: MessageSquareText,
      iconClass: "text-emerald-500",
      label: t("gateway.blockReply"),
      hint: t("behavior.blockReplyHint"),
      checked: value.block_reply ?? false,
      onCheckedChange: (v) => onChange({ ...value, block_reply: v }),
      infoWhenOn: t("behavior.blockReplyInfo"),
      infoClass: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/30 dark:text-emerald-300",
    },
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
