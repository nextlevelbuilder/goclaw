import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Sparkles, Pin } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import type { AgentData } from "@/types/agent";

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

const PROMPT_MODES = ["full", "task", "minimal", "none"] as const;
const MAX_PINNED = 10;

/** Extract prompt_mode and pinned_skills from agent.other_config bag. */
function readOtherConfig(agent: AgentData) {
  const bag = (agent.other_config ?? {}) as Record<string, unknown>;
  return {
    promptMode: (bag.prompt_mode as string) || "full",
    pinnedSkills: (bag.pinned_skills as string[]) || [],
  };
}

export function PromptSettingsSection({ agent, onUpdate }: Props) {
  const { t } = useTranslation("agents");
  const { promptMode: savedMode, pinnedSkills: savedPinned } = readOtherConfig(agent);

  const [mode, setMode] = useState(savedMode);
  const [pinned, setPinned] = useState<string[]>(savedPinned);
  const [newSkill, setNewSkill] = useState("");
  const [saving, setSaving] = useState(false);

  // Sync from props when agent data refreshes
  useEffect(() => {
    const { promptMode, pinnedSkills } = readOtherConfig(agent);
    setMode(promptMode);
    setPinned(pinnedSkills);
  }, [agent.other_config]);

  const dirty = mode !== savedMode || JSON.stringify(pinned) !== JSON.stringify(savedPinned);

  const handleSave = async () => {
    setSaving(true);
    try {
      const bag = { ...((agent.other_config ?? {}) as Record<string, unknown>) };
      // Only set prompt_mode if not default
      if (mode && mode !== "full") {
        bag.prompt_mode = mode;
      } else {
        delete bag.prompt_mode;
      }
      // Only set pinned_skills if non-empty
      if (pinned.length > 0) {
        bag.pinned_skills = pinned;
      } else {
        delete bag.pinned_skills;
      }
      await onUpdate({ other_config: bag });
    } finally {
      setSaving(false);
    }
  };

  const addPinned = () => {
    const name = newSkill.trim().toLowerCase();
    if (name && !pinned.includes(name) && pinned.length < MAX_PINNED) {
      setPinned([...pinned, name]);
      setNewSkill("");
    }
  };

  const removePinned = (name: string) => {
    setPinned(pinned.filter((s) => s !== name));
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center gap-2">
        <Sparkles className="h-4 w-4 text-amber-500 shrink-0" />
        <h3 className="text-sm font-medium">{t("detail.prompt.title", "Prompt Settings")}</h3>
      </div>

      {/* Prompt Mode */}
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
        <p className="text-[11px] text-muted-foreground">
          {modeHints[mode]}
        </p>
      </div>

      {/* Pinned Skills */}
      <div className="space-y-1.5">
        <div className="flex items-center gap-1.5">
          <Pin className="h-3.5 w-3.5 text-muted-foreground" />
          <label className="text-xs text-muted-foreground">
            {t("detail.prompt.pinnedLabel", "Pinned Skills")} ({pinned.length}/{MAX_PINNED})
          </label>
        </div>
        {pinned.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {pinned.map((name) => (
              <Badge
                key={name} variant="secondary"
                className="text-xs cursor-pointer hover:bg-destructive/20"
                onClick={() => removePinned(name)}
              >
                {name} &times;
              </Badge>
            ))}
          </div>
        )}
        {pinned.length < MAX_PINNED && (
          <div className="flex items-center gap-1.5">
            <Input
              value={newSkill}
              onChange={(e) => setNewSkill(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), addPinned())}
              placeholder={t("detail.prompt.pinnedPlaceholder", "Skill name (e.g. github)")}
              className="text-base md:text-sm h-8 max-w-[200px]"
            />
            <Button size="xs" variant="outline" onClick={addPinned} disabled={!newSkill.trim()}>
              {t("detail.prompt.addSkill", "Add")}
            </Button>
          </div>
        )}
        <p className="text-[11px] text-muted-foreground">
          {t("detail.prompt.pinnedHint", "Pinned skills are always inlined in the prompt. Others use skill_search.")}
        </p>
      </div>

      {/* Save */}
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
