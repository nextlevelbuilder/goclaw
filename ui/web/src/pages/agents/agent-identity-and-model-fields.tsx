import { Controller } from "react-hook-form";
import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { slugify } from "@/lib/slug";
import type { AgentCreateFormData } from "@/schemas/agent.schema";
import type { ProviderData } from "@/pages/providers/hooks/use-providers";
import type { ModelInfo } from "@/pages/providers/hooks/use-provider-models";

interface AgentIdentityAndModelFieldsProps {
  form: UseFormReturn<AgentCreateFormData>;
  enabledProviders: ProviderData[];
  models: ModelInfo[];
  modelsLoading: boolean;
  verifying: boolean;
  verifyResult: { valid: boolean; error?: string } | null;
  onProviderChange: (value: string) => void;
  onVerify: () => void;
}

/**
 * Renders agent identity fields (emoji, displayName, agentKey) and
 * provider/model selector with inline verify button.
 */
export function AgentIdentityAndModelFields({
  form,
  enabledProviders,
  models,
  modelsLoading,
  verifying,
  verifyResult,
  onProviderChange,
  onVerify,
}: AgentIdentityAndModelFieldsProps) {
  const { t } = useTranslation("agents");
  const { register, control, watch, setValue, formState: { errors } } = form;
  const provider = watch("provider");
  const model = watch("model");

  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="displayName">{t("create.displayName")}</Label>
          <div className="flex gap-2">
            <Input
              id="emoji"
              {...register("emoji")}
              placeholder="🤖"
              className="w-14 shrink-0 text-center text-lg"
              maxLength={2}
              title={t("create.emojiHint")}
            />
            <Input
              id="displayName"
              {...register("displayName")}
              onBlur={(e) => {
                register("displayName").onBlur(e);
                const name = e.target.value.trim();
                if (name && !form.getFieldState("agentKey").isDirty) {
                  setValue("agentKey", slugify(name), { shouldValidate: true });
                }
              }}
              placeholder={t("create.displayNamePlaceholder")}
            />
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor="agentKey">{t("create.agentKey")}</Label>
          <Input
            id="agentKey"
            {...register("agentKey")}
            onBlur={(e) => {
              setValue("agentKey", slugify(e.target.value), { shouldValidate: true });
            }}
            placeholder={t("create.agentKeyPlaceholder")}
          />
          {errors.agentKey ? (
            <p className="text-xs text-destructive">{errors.agentKey.message}</p>
          ) : (
            <p className="text-xs text-muted-foreground">{t("create.agentKeyHint")}</p>
          )}
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label>{t("create.provider")}</Label>
          {enabledProviders.length > 0 ? (
            <Controller
              control={control}
              name="provider"
              render={({ field }) => (
                <Select value={field.value} onValueChange={onProviderChange}>
                  <SelectTrigger>
                    <SelectValue placeholder={t("create.selectProvider")} />
                  </SelectTrigger>
                  <SelectContent>
                    {enabledProviders.map((p) => (
                      <SelectItem key={p.name} value={p.name}>
                        {p.display_name || p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          ) : (
            <Input {...register("provider")} placeholder="openrouter" />
          )}
        </div>

        <div className="space-y-2">
          <Label>{t("create.model")}</Label>
          <div className="flex gap-2">
            <div className="flex-1">
              <Controller
                control={control}
                name="model"
                render={({ field }) => (
                  <Combobox
                    value={field.value}
                    onChange={(v) => setValue("model", v, { shouldValidate: true })}
                    options={models.map((m) => ({ value: m.id, label: m.name ?? m.id }))}
                    placeholder={modelsLoading ? t("create.loadingModels") : t("create.enterOrSelectModel")}
                  />
                )}
              />
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-9 px-3"
              disabled={!provider || !model.trim() || verifying}
              onClick={onVerify}
            >
              {verifying ? "..." : t("create.check")}
            </Button>
          </div>
          {verifyResult && (
            <p className={`text-xs ${verifyResult.valid ? "text-success" : "text-destructive"}`}>
              {verifyResult.valid
                ? t("create.modelVerified")
                : verifyResult.error || t("create.verificationFailed")}
            </p>
          )}
          {!verifyResult && provider && !modelsLoading && models.length === 0 && (
            <p className="text-xs text-muted-foreground">{t("create.noModelsHint")}</p>
          )}
        </div>
      </div>
    </>
  );
}
