import { Controller } from "react-hook-form";
import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import type { CliCredentialFormData } from "@/schemas/credential.schema";

interface CliCredentialScopeFieldsProps {
  form: UseFormReturn<CliCredentialFormData>;
}

/** Renders is_global toggle and enabled switch for a CLI credential. */
export function CliCredentialScopeFields({ form }: CliCredentialScopeFieldsProps) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");
  const { control } = form;

  return (
    <>
      <div className="flex items-center justify-between rounded-md border p-3">
        <div className="space-y-0.5">
          <Label htmlFor="cc-global">{t("form.isGlobal")}</Label>
          <p className="text-xs text-muted-foreground">{t("form.isGlobalHint")}</p>
        </div>
        <Controller
          control={control}
          name="isGlobal"
          render={({ field }) => (
            <Switch id="cc-global" checked={field.value} onCheckedChange={field.onChange} />
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
