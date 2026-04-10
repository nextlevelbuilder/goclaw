import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { toast } from "@/stores/use-toast-store";
import type { AgentData } from "@/types/agent";
import { readPromptMode } from "../agent-display-utils";
import { PromptModeCards, type PromptMode } from "../../prompt-mode-cards";

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function PromptSettingsSection({ agent, onUpdate }: Props) {
  const { t } = useTranslation("agents");
  const savedMode = readPromptMode(agent) as PromptMode;
  const [mode, setMode] = useState<PromptMode>(savedMode);
  const [saving, setSaving] = useState(false);

  useEffect(() => { setMode(readPromptMode(agent) as PromptMode); }, [agent.other_config]);

  const dirty = mode !== savedMode;

  const handleSave = async () => {
    setSaving(true);
    try {
      const bag = { ...((agent.other_config ?? {}) as Record<string, unknown>) };
      if (mode && mode !== "full") {
        bag.prompt_mode = mode;
      } else {
        delete bag.prompt_mode;
      }
      await onUpdate({ other_config: bag });
      const modeRank: Record<string, number> = { none: 0, minimal: 1, task: 2, full: 3 };
      if ((modeRank[mode] ?? 3) > (modeRank[savedMode] ?? 3)) {
        toast.info(t("detail.prompt.upgradeWarning", "Mode upgraded. Some files may need regeneration — use Resummon or Edit with AI in the Files tab."));
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">{t("detail.prompt.title")}</h3>
        {dirty && (
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? t("saving", "Saving...") : t("save", "Save")}
          </Button>
        )}
      </div>

      <PromptModeCards value={mode} onChange={setMode} />
    </section>
  );
}
