import { Controller } from "react-hook-form";
import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { ChevronRight } from "lucide-react";
import type { AgentCreateFormData } from "@/schemas/agent.schema";
import type { AgentPreset } from "./agent-presets";

interface AgentDescriptionSectionProps {
  form: UseFormReturn<AgentCreateFormData>;
  agentPresets: AgentPreset[];
}

/**
 * Renders the agent type toggle, description textarea with presets,
 * and self-evolution switch for predefined agents.
 */
export function AgentDescriptionSection({ form, agentPresets }: AgentDescriptionSectionProps) {
  const { t } = useTranslation("agents");
  const { register, control, watch, setValue } = form;
  const agentType = watch("agentType");

  return (
    <>
      {agentType === "predefined" ? (
        <div className="space-y-3">
          <Label>{t("create.describeAgent")}</Label>
          <div className="flex flex-wrap gap-1.5">
            {agentPresets.map((preset) => (
              <button
                key={preset.label}
                type="button"
                onClick={() => setValue("description", preset.prompt, { shouldValidate: true })}
                className="rounded-full border px-2.5 py-0.5 text-xs transition-colors hover:bg-accent"
              >
                {preset.label}
              </button>
            ))}
          </div>
          <Textarea
            {...register("description")}
            placeholder={t("create.descriptionPlaceholder")}
            className="min-h-[120px]"
          />
          <p className="text-xs text-muted-foreground">{t("create.descriptionHint")}</p>
          <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
            <div className="space-y-0.5">
              <Label htmlFor="create-self-evolve" className="text-sm font-normal">
                {t("create.selfEvolution")}
              </Label>
              <p className="text-xs text-muted-foreground">{t("create.selfEvolutionHint")}</p>
            </div>
            <Controller
              control={control}
              name="selfEvolve"
              render={({ field }) => (
                <Switch id="create-self-evolve" checked={field.value} onCheckedChange={field.onChange} />
              )}
            />
          </div>
        </div>
      ) : (
        <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2.5 space-y-2">
          <p className="text-xs text-amber-700 dark:text-amber-400">{t("create.openWarning")}</p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-7 text-xs"
            onClick={() => setValue("agentType", "predefined")}
          >
            {t("create.switchToPredefined")}
          </Button>
        </div>
      )}

      <button
        type="button"
        onClick={() => setValue("agentType", agentType === "open" ? "predefined" : "open")}
        className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        <ChevronRight className={`h-3 w-3 transition-transform ${agentType === "open" ? "rotate-90" : ""}`} />
        {t("create.useOpenAgent")}
      </button>
    </>
  );
}
