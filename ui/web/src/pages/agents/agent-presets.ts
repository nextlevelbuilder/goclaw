import { useTranslation } from "react-i18next";

export interface AgentPreset {
  label: string;
  prompt: string;
  emoji: string;
}

export function useAgentPresets(): AgentPreset[] {
  const { t } = useTranslation("agents");
  return [
    {
      label: t("presets.foxSpirit.label"),
      prompt: t("presets.foxSpirit.prompt"),
      emoji: "🦊",
    },
    {
      label: t("presets.coder.label"),
      prompt: t("presets.coder.prompt"),
      emoji: "💻",
    },
    {
      label: t("presets.support.label"),
      prompt: t("presets.support.prompt"),
      emoji: "🎧",
    },
    {
      label: t("presets.writer.label"),
      prompt: t("presets.writer.prompt"),
      emoji: "✍️",
    },
    {
      label: t("presets.translator.label"),
      prompt: t("presets.translator.prompt"),
      emoji: "🌐",
    },
    {
      label: t("presets.artisan.label"),
      prompt: t("presets.artisan.prompt"),
      emoji: "🎨",
    },
    {
      label: t("presets.astrologer.label"),
      prompt: t("presets.astrologer.prompt"),
      emoji: "🔮",
    },
  ];
}
