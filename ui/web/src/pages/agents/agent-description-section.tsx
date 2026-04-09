import { Controller } from "react-hook-form";
import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { AgentCreateFormData } from "@/schemas/agent.schema";
import type { AgentPreset } from "./agent-presets";

interface AgentDescriptionSectionProps {
  form: UseFormReturn<AgentCreateFormData>;
  agentPresets: AgentPreset[];
}

/**
 * Renders the description textarea with presets and self-evolution switch.
 * v3: always predefined — no agent type toggle.
 */
export function AgentDescriptionSection({ form, agentPresets }: AgentDescriptionSectionProps) {
  const { t } = useTranslation("agents");
  const { register, control, setValue } = form;

  return (
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
  );
}
