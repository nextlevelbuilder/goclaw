import { useTranslation } from "react-i18next";
import { Zap, Wrench, Package, CircleOff } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export const PROMPT_MODES = ["full", "task", "minimal", "none"] as const;
export type PromptMode = (typeof PROMPT_MODES)[number];

export const MODE_ICONS: Record<PromptMode, LucideIcon> = {
  full: Zap,
  task: Wrench,
  minimal: Package,
  none: CircleOff,
};

/**
 * Section tags per mode — accurate to systemprompt.go gating logic (v3).
 * Context files filtered by ModeAllowlist: full=all, task=AGENTS_TASK+TOOLS+CAPABILITIES+SOUL+IDENTITY,
 * minimal=AGENTS_CORE+CAPABILITIES, none=TOOLS only.
 */
export const MODE_SECTIONS: Record<PromptMode, string[]> = {
  full: ["persona", "tools", "execBias", "callStyle", "safety", "skills", "mcp", "memory", "sandbox", "evolution", "channelHints"],
  task: ["styleEcho", "tools", "execBias", "safetySm", "skillsHybrid", "mcpSearch", "memorySm"],
  minimal: ["tools", "pinnedSkills", "memoryMin", "domainCtx"],
  none: ["tools", "toolNotes", "pinnedSkills", "mcpSearch", "workspace"],
};

/** Token count per mode — estimated from systemprompt.go section sizes */
export const MODE_TOKENS: Record<PromptMode, string> = {
  full: "~4.8K",
  task: "~1.3K",
  minimal: "~570",
  none: "~640",
};

interface PromptModeCardsProps {
  value: PromptMode;
  onChange: (mode: PromptMode) => void;
  /** Hide section tags for compact layouts (e.g. create dialog) */
  compact?: boolean;
}

/** Reusable 4-mode card selector used in both create dialog and agent settings. */
export function PromptModeCards({ value, onChange, compact }: PromptModeCardsProps) {
  const { t } = useTranslation("agents");

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
      {PROMPT_MODES.map((m) => {
        const Icon = MODE_ICONS[m] as LucideIcon;
        const selected = value === m;
        const sections = MODE_SECTIONS[m];
        const tokens = MODE_TOKENS[m];
        return (
          <button
            key={m}
            type="button"
            onClick={() => onChange(m)}
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
                <span className="text-2xs text-muted-foreground/70 tabular-nums shrink-0">
                  {tokens}
                </span>
              </div>
              <p className="text-xs-plus text-muted-foreground">
                {t(`detail.prompt.mode.${m}Desc`)}
              </p>
              {!compact && sections.length > 0 && (
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
              )}
            </div>
          </button>
        );
      })}
    </div>
  );
}
