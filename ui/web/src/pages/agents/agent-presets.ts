import { useTranslation } from "react-i18next";

export interface AgentPreset {
  label: string;
  prompt: string;
}

export function useAgentPresets(): AgentPreset[] {
  const { t } = useTranslation("agents");
  return [
    {
      label: t("presets.foxSpirit.label"),
      prompt: t("presets.foxSpirit.prompt"),
    },
    {
      label: t("presets.artisan.label"),
      prompt: t("presets.artisan.prompt"),
    },
    {
      label: t("presets.astrologer.label"),
      prompt: t("presets.astrologer.prompt"),
    },
  ];
}
