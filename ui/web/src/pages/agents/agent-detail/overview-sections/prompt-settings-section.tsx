import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Sparkles } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import type { AgentData } from "@/types/agent";

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

const PROMPT_MODES = ["full", "task", "minimal", "none"] as const;

function readPromptMode(agent: AgentData): string {
  const bag = (agent.other_config ?? {}) as Record<string, unknown>;
  return (bag.prompt_mode as string) || "full";
}

export function PromptSettingsSection({ agent, onUpdate }: Props) {
  const { t } = useTranslation("agents");
  const savedMode = readPromptMode(agent);
  const [mode, setMode] = useState(savedMode);
  const [saving, setSaving] = useState(false);

  useEffect(() => { setMode(readPromptMode(agent)); }, [agent.other_config]);

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
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center gap-2">
        <Sparkles className="h-4 w-4 text-amber-500 shrink-0" />
        <h3 className="text-sm font-medium">{t("detail.prompt.title", "Prompt Settings")}</h3>
      </div>

      <div className="space-y-1">
        <label className="text-xs text-muted-foreground">{t("detail.prompt.modeLabel", "System Prompt Mode")}</label>
        <Select value={mode} onValueChange={setMode}>
          <SelectTrigger className="w-[180px] text-base md:text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PROMPT_MODES.map((m) => (
              <SelectItem key={m} value={m}>
                {t(`detail.prompt.mode.${m}`, m)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-[11px] text-muted-foreground">{modeHints[mode]}</p>
      </div>

      {dirty && (
        <div className="flex justify-end">
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? t("saving", "Saving...") : t("save", "Save")}
          </Button>
        </div>
      )}
    </section>
  );
}

const modeHints: Record<string, string> = {
  full: "All sections — chatbot, main agent (default)",
  task: "Enterprise automation — lean prompt, keeps tools/safety/skills",
  minimal: "Subagent/cron — bare minimum sections",
  none: "Identity line only — API/webhook integrations",
};
