import { useCallback, useState } from "react";
import { Controller } from "react-hook-form";
import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { ChannelInstanceData } from "./hooks/use-channel-instances";
import type { AgentData } from "@/types/agent";
import { slugify } from "@/lib/slug";
import { credentialsSchema, configSchema, wizardConfig, type FieldDef } from "./channel-schemas";
import { ChannelFields } from "./channel-fields";
import { ChannelScopesInfo } from "./channel-scopes-info";
import { wizardEditConfigs } from "./channel-wizard-registry";
import { TelegramGroupOverrides, type GroupConfigWithTopics } from "./telegram-group-overrides";
import { CHANNEL_TYPES } from "@/constants/channels";
import type { ChannelInstanceFormData } from "@/schemas/channel.schema";

interface ChannelInstanceFormStepProps {
  form: UseFormReturn<ChannelInstanceFormData>;
  instance?: ChannelInstanceData | null;
  agents: AgentData[];
  credsValues: Record<string, unknown>;
  configValues: Record<string, unknown>;
  onCredsChange: (key: string, value: unknown) => void;
  onConfigChange: (key: string, value: unknown) => void;
  setConfigValues: React.Dispatch<React.SetStateAction<Record<string, unknown>>>;
  error: string;
  loading: boolean;
  onCancel: () => void;
  onSubmit: () => void;
  submitLabel: string;
}

export function ChannelInstanceFormStep({
  form, instance, agents, credsValues, configValues,
  onCredsChange, onConfigChange, setConfigValues,
  error, loading, onCancel, onSubmit, submitLabel,
}: ChannelInstanceFormStepProps) {
  const { t } = useTranslation("channels");
  const { register, control, formState: { errors } } = form;

  const channelType = form.watch("channelType");
  const wizard = wizardConfig[channelType];
  const EditConfig = wizardEditConfigs[channelType];
  const credsFields = credentialsSchema[channelType] ?? [];
  const excludeSet = new Set(wizard?.excludeConfigFields ?? []);
  const cfgFields = configSchema[channelType] ?? [];
  const formCfgFields = excludeSet.size > 0 ? cfgFields.filter((f: FieldDef) => !excludeSet.has(f.key)) : cfgFields;
  const hasWizard = !instance && !!wizard;
  const normalCfgFields = formCfgFields.filter((f: FieldDef) => !f.advanced);
  const advancedCfgFields = formCfgFields.filter((f: FieldDef) => f.advanced);
  const [showAdvanced, setShowAdvanced] = useState(
    () => advancedCfgFields.some((f) => configValues[f.key] !== undefined && configValues[f.key] !== ""),
  );

  const handleTelegramGroupsChange = useCallback((groups: Record<string, GroupConfigWithTopics>) => {
    setConfigValues((prev) => ({
      ...prev,
      groups: Object.keys(groups).length > 0 ? groups : undefined,
    }));
  }, [setConfigValues]);

  return (
    <>
      <div className="grid gap-4 py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
        <div className="grid gap-1.5">
          <Label htmlFor="ci-name">{t("form.key")}</Label>
          <Input
            id="ci-name"
            {...register("name", { setValueAs: (v: string) => slugify(v) })}
            onChange={(e) => form.setValue("name", slugify(e.target.value), { shouldValidate: true })}
            value={form.watch("name")}
            placeholder={t("form.keyPlaceholder")}
            disabled={!!instance}
          />
          {errors.name && <p className="text-xs text-destructive">{errors.name.message}</p>}
          <p className="text-xs text-muted-foreground">{t("form.keyHint")}</p>
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="ci-display">{t("form.displayName")}</Label>
          <Input id="ci-display" {...register("displayName")} placeholder={t("form.displayNamePlaceholder")} />
        </div>

        <div className="grid gap-1.5">
          <Label>{t("form.channelType")}</Label>
          <Controller
            control={control}
            name="channelType"
            render={({ field }) => (
              <Select value={field.value} onValueChange={field.onChange} disabled={!!instance}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {CHANNEL_TYPES.map((ct) => (
                    <SelectItem key={ct.value} value={ct.value}>{ct.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          />
        </div>

        <div className="grid gap-1.5">
          <Label>{t("form.agent")}</Label>
          <Controller
            control={control}
            name="agentId"
            render={({ field }) => (
              <Select value={field.value} onValueChange={field.onChange}>
                <SelectTrigger><SelectValue placeholder={t("form.selectAgent")} /></SelectTrigger>
                <SelectContent>
                  {agents.map((a) => (
                    <SelectItem key={a.id} value={a.id}>{a.display_name || a.agent_key}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          />
          {errors.agentId && <p className="text-xs text-destructive">{errors.agentId.message}</p>}
        </div>

        {credsFields.length > 0 && (
          <fieldset className="rounded-md border p-3 space-y-3">
            <legend className="px-1 text-sm font-medium">
              {t("form.credentials")}
              {instance && <span className="text-xs font-normal text-muted-foreground ml-1">{t("form.credentialsHint")}</span>}
            </legend>
            <ChannelFields fields={credsFields} values={credsValues} onChange={onCredsChange} idPrefix="ci-cred" isEdit={!!instance} contextValues={configValues} />
            <p className="text-xs text-muted-foreground">{t("form.credentialsEncrypted")}</p>
          </fieldset>
        )}

        <ChannelScopesInfo channelType={channelType} />

        {instance && wizard?.steps.includes("auth") && (
          <div className="rounded-md border border-blue-200 bg-blue-50 dark:border-blue-900 dark:bg-blue-950 p-3">
            <div className="flex items-center gap-2">
              <span className={`h-2 w-2 rounded-full ${instance.has_credentials ? "bg-green-500" : "bg-amber-500"}`} />
              <span className="text-sm">
                {instance.has_credentials ? t("form.authStatus.authenticated") : t("form.authStatus.notAuthenticated")}
              </span>
              {!instance.has_credentials && (
                <span className="text-xs text-muted-foreground ml-1">{t("form.authStatus.useQrHint")}</span>
              )}
            </div>
          </div>
        )}

        {hasWizard && wizard?.formBanner && (
          <div className="rounded-md border border-blue-200 bg-blue-50 dark:border-blue-900 dark:bg-blue-950 p-3">
            <p className="text-sm text-muted-foreground">{t(wizard.formBanner)}</p>
          </div>
        )}

        {formCfgFields.length > 0 && (
          <fieldset className="rounded-md border p-3 space-y-3">
            <legend className="px-1 text-sm font-medium">{t("form.configuration")}</legend>
            <ChannelFields fields={normalCfgFields} values={configValues} onChange={onConfigChange} idPrefix="ci-cfg" />
            {instance && EditConfig && <EditConfig instance={instance} configValues={configValues} onConfigChange={onConfigChange} />}
            {advancedCfgFields.length > 0 && (
              <div className="pt-1">
                <button
                  type="button"
                  onClick={() => setShowAdvanced((v) => !v)}
                  className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
                >
                  {showAdvanced ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                  {t("form.advanced", { defaultValue: "Advanced" })}
                </button>
                {showAdvanced && (
                  <div className="mt-3">
                    <ChannelFields fields={advancedCfgFields} values={configValues} onChange={onConfigChange} idPrefix="ci-cfg-adv" />
                  </div>
                )}
              </div>
            )}
          </fieldset>
        )}

        {channelType === "telegram" && (
          <TelegramGroupOverrides
            groups={(configValues.groups as Record<string, Record<string, unknown>>) ?? {}}
            onChange={handleTelegramGroupsChange}
          />
        )}

        <div className="flex items-center gap-2">
          <Controller
            control={control}
            name="enabled"
            render={({ field }) => (
              <Switch id="ci-enabled" checked={field.value} onCheckedChange={field.onChange} />
            )}
          />
          <Label htmlFor="ci-enabled">{t("form.enabled")}</Label>
        </div>

        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>

      <div className="flex justify-end gap-2 pt-2">
        <Button variant="outline" onClick={onCancel} disabled={loading}>{t("form.cancel")}</Button>
        <Button onClick={onSubmit} disabled={loading}>
          {loading ? t("form.saving") : submitLabel}
        </Button>
      </div>
    </>
  );
}
