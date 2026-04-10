import { Controller } from "react-hook-form";
import type { UseFormRegister, FieldErrors, Control } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { PROVIDER_TYPES } from "@/constants/providers";
import type { ProviderCreateFormData } from "@/schemas/provider.schema";

interface ProviderStandardFormFieldsProps {
  register: UseFormRegister<ProviderCreateFormData>;
  errors: FieldErrors<ProviderCreateFormData>;
  providerType: string;
  control: Control<ProviderCreateFormData>;
}

/**
 * Standard (non-OAuth, non-CLI, non-ACP) provider form fields:
 * apiBase, apiKey, enabled toggle, and root error display.
 */
export function ProviderStandardFormFields({
  register,
  errors,
  providerType,
  control,
}: ProviderStandardFormFieldsProps) {
  const { t } = useTranslation("providers");

  return (
    <>
      <div className="space-y-2">
        <Label htmlFor="apiBase">{t("form.apiBase")}</Label>
        <Input
          id="apiBase"
          {...register("apiBase")}
          placeholder={
            PROVIDER_TYPES.find((pt) => pt.value === providerType)?.placeholder ||
            PROVIDER_TYPES.find((pt) => pt.value === providerType)?.apiBase ||
            "https://api.example.com/v1"
          }
          className="text-base md:text-sm"
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="apiKey">{t("form.apiKey")}</Label>
        <Input
          id="apiKey"
          type="password"
          {...register("apiKey")}
          placeholder={t("form.apiKeyPlaceholder")}
          className="text-base md:text-sm"
        />
      </div>

      <div className="flex items-center justify-between">
        <Label htmlFor="enabled">{t("form.enabled")}</Label>
        <Controller
          control={control}
          name="enabled"
          render={({ field }) => (
            <Switch id="enabled" checked={field.value} onCheckedChange={field.onChange} />
          )}
        />
      </div>

      {errors.root && (
        <p className="text-sm text-destructive">{errors.root.message}</p>
      )}
    </>
  );
}
