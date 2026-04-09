import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Zap, Wrench, Package, CircleOff } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { AgentData } from "@/types/agent";
import { readPromptMode } from "../agent-display-utils";

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

const PROMPT_MODES = ["full", "task", "minimal", "none"] as const;

const MODE_ICONS: Record<(typeof PROMPT_MODES)[number], LucideIcon> = {
  full: Zap,
  task: Wrench,
  minimal: Package,
  none: CircleOff,
};

/** Section tag keys per mode — maps to i18n detail.prompt.section.* */
const MODE_SECTIONS: Record<(typeof PROMPT_MODES)[number], string[]> = {
  full: ["persona", "tools", "safety", "skills", "mcp", "memory", "sandbox", "evolution"],
  task: ["styleEcho", "tools", "safetySm", "skills", "mcp", "memorySm"],
  minimal: ["tools", "safety", "workspace"],
  none: ["identityOnly"],
};

/** Token range per mode measured via tiktoken (cl100k) across bare→heavy configs */
const MODE_TOKENS: Record<(typeof PROMPT_MODES)[number], string> = {
  full: "~500–2.9K",
  task: "~350–1.2K",
  minimal: "~350–820",
  none: "~6",
};

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
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">{t("detail.prompt.title")}</h3>
        {dirty && (
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? t("saving", "Saving...") : t("save", "Save")}
          </Button>
        )}
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        {PROMPT_MODES.map((m) => {
          const Icon = MODE_ICONS[m] as LucideIcon;
          const selected = mode === m;
          const sections = MODE_SECTIONS[m];
          return (
            <button
              key={m}
              type="button"
              onClick={() => setMode(m)}
              className={cn(
                "flex items-start gap-2.5 rounded-lg border p-2.5 text-left transition-all min-h-[64px] cursor-pointer",
                selected
                  ? "ring-2 ring-primary border-primary bg-primary/5"
                  : "hover:border-primary/30",
              )}
            >
              <Icon className={cn(
                "h-4 w-4 shrink-0 mt-0.5",
                selected ? "text-primary" : "text-muted-foreground",
              )} />
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex items-center justify-between gap-1">
                  <span className={cn(
                    "text-xs font-medium",
                    selected && "text-primary",
                  )}>
                    {t(`detail.prompt.mode.${m}`)}
                  </span>
                  <span className="text-[10px] text-muted-foreground/70 tabular-nums shrink-0">
                    {MODE_TOKENS[m]}
                  </span>
                </div>
                <p className="text-[11px] text-muted-foreground">
                  {t(`detail.prompt.mode.${m}Desc`)}
                </p>
                <div className="flex flex-wrap gap-1">
                  {sections.map((s) => (
                    <span
                      key={s}
                      className="inline-block rounded bg-muted px-1.5 py-0.5 text-[9px] leading-tight text-muted-foreground"
                    >
                      {t(`detail.prompt.section.${s}`)}
                    </span>
                  ))}
                </div>
              </div>
            </button>
          );
        })}
      </div>
    </section>
  );
}
