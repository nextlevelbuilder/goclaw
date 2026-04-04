import { Controller } from "react-hook-form";
import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { AgentData } from "@/types/agent";
import type { CliCredentialFormData } from "@/schemas/credential.schema";

const GLOBAL_AGENT = "__global__";

interface CliCredentialScopeFieldsProps {
  form: UseFormReturn<CliCredentialFormData>;
  agents: AgentData[];
}

/** Renders agent scope selector and enabled toggle for a CLI credential. */
export function CliCredentialScopeFields({ form, agents }: CliCredentialScopeFieldsProps) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");
  const { control } = form;

  return (
    <>
      <div className="grid gap-1.5">
        <Label>
          {t("form.agentId")}{" "}
          <span className="text-xs text-muted-foreground">({t("form.agentIdHint")})</span>
        </Label>
        <Controller
          control={control}
          name="agentId"
          render={({ field }) => (
            <Select
              value={field.value || GLOBAL_AGENT}
              onValueChange={(v) => field.onChange(v === GLOBAL_AGENT ? "" : v)}
            >
              <SelectTrigger className="text-base md:text-sm">
                <SelectValue placeholder={t("placeholders.agentId")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={GLOBAL_AGENT}>{t("placeholders.agentId")}</SelectItem>
                {agents.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.display_name || a.agent_key || a.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        />
      </div>

      <div className="flex items-center gap-2">
        <Controller
          control={control}
          name="enabled"
          render={({ field }) => (
            <Switch id="cc-enabled" checked={field.value} onCheckedChange={field.onChange} />
          )}
        />
        <Label htmlFor="cc-enabled">{tc("enabled")}</Label>
      </div>
    </>
  );
}
