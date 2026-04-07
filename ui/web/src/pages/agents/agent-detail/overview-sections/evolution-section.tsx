import { useTranslation } from "react-i18next";
import { GraduationCap, Info, Sparkles, BarChart3, Lightbulb } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useV3Flags } from "@/hooks/use-v3-flags";

interface EvolutionSectionProps {
  agentId: string;
  selfEvolve: boolean;
  onSelfEvolveChange: (v: boolean) => void;
  skillEvolve: boolean;
  onSkillEvolveChange: (v: boolean) => void;
  skillNudgeInterval: number;
  onSkillNudgeIntervalChange: (v: number) => void;
}

export function EvolutionSection({
  agentId,
  selfEvolve, onSelfEvolveChange,
  skillEvolve, onSkillEvolveChange,
  skillNudgeInterval, onSkillNudgeIntervalChange,
}: EvolutionSectionProps) {
  const { t } = useTranslation("agents");
  const { flags, toggleFlag } = useV3Flags(agentId);

  return (
    <section className="space-y-4 rounded-lg border p-3 sm:p-4">
      <h3 className="text-sm font-medium">{t("detail.evolution.title")}</h3>

      {/* Self-Evolve */}
      <div className="space-y-2">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-2">
            <Sparkles className="h-4 w-4 text-orange-500 shrink-0" />
            <div className="space-y-0.5">
              <Label htmlFor="self-evolve" className="text-sm font-normal cursor-pointer">
                {t("general.selfEvolutionLabel")}
              </Label>
              <p className="text-xs text-muted-foreground">{t("general.selfEvolutionHint")}</p>
            </div>
          </div>
          <Switch id="self-evolve" checked={selfEvolve} onCheckedChange={onSelfEvolveChange} />
        </div>
        {selfEvolve && (
          <div className="flex items-start gap-2 rounded-md border border-orange-200 bg-orange-50 px-3 py-2 text-xs text-orange-700 dark:border-orange-800 dark:bg-orange-950/30 dark:text-orange-300">
            <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span>{t("general.selfEvolutionInfo")}</span>
          </div>
        )}
      </div>

      {/* Skill Learning */}
      <div className="space-y-2">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-2">
            <GraduationCap className="h-4 w-4 text-amber-500 shrink-0" />
            <div className="space-y-0.5">
              <Label htmlFor="skill-evolve" className="text-sm font-normal cursor-pointer">
                {t("general.skillLearningLabel")}
              </Label>
              <p className="text-xs text-muted-foreground">{t("general.skillLearningHint")}</p>
            </div>
          </div>
          <Switch id="skill-evolve" checked={skillEvolve} onCheckedChange={onSkillEvolveChange} />
        </div>
        {skillEvolve && (
          <>
            <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-300">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{t("general.skillLearningInfo")}</span>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="nudge-interval" className="text-xs font-normal text-muted-foreground">
                {t("general.skillNudgeIntervalLabel")}
              </Label>
              <Input
                id="nudge-interval"
                type="number"
                min={0}
                max={100}
                value={skillNudgeInterval}
                onChange={(e) => onSkillNudgeIntervalChange(Number(e.target.value) || 0)}
                className="max-w-[120px] text-base md:text-sm"
              />
              <p className="text-xs text-muted-foreground">{t("general.skillNudgeIntervalHint")}</p>
            </div>
          </>
        )}
      </div>

      {/* V3 Evolution Metrics (stored in other_config via v3-flags API) */}
      {flags && (
        <>
          <div className="border-t pt-3 mt-3" />

          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-2">
              <BarChart3 className="h-4 w-4 text-cyan-500 shrink-0" />
              <div className="space-y-0.5">
                <Label htmlFor="evo-metrics" className="text-sm font-normal cursor-pointer">
                  {t("detail.evolution.metricsLabel")}
                </Label>
                <p className="text-xs text-muted-foreground">{t("detail.evolution.metricsHint")}</p>
              </div>
            </div>
            <Switch
              id="evo-metrics"
              checked={flags.self_evolution_metrics}
              onCheckedChange={(v) => toggleFlag("self_evolution_metrics", v)}
            />
          </div>

          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-2">
              <Lightbulb className="h-4 w-4 text-yellow-500 shrink-0" />
              <div className="space-y-0.5">
                <Label htmlFor="evo-suggestions" className="text-sm font-normal cursor-pointer">
                  {t("detail.evolution.suggestionsLabel")}
                </Label>
                <p className="text-xs text-muted-foreground">{t("detail.evolution.suggestionsHint")}</p>
              </div>
            </div>
            <Switch
              id="evo-suggestions"
              checked={flags.self_evolution_suggestions}
              onCheckedChange={(v) => toggleFlag("self_evolution_suggestions", v)}
            />
          </div>
        </>
      )}
    </section>
  );
}
