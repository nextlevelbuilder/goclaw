import { useState, useEffect, useCallback } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData, ChannelInstanceInput } from "./hooks/use-channel-instances";
import type { AgentData } from "@/types/agent";
import { credentialsSchema, configSchema, wizardConfig, type FieldDef } from "./channel-schemas";
import { wizardAuthSteps, wizardConfigSteps } from "./channel-wizard-registry";
import { CHANNEL_TYPES } from "@/constants/channels";
import { channelInstanceSchema, type ChannelInstanceFormData } from "@/schemas/channel.schema";
import { ChannelInstanceFormStep } from "./channel-instance-form-step";

type WizardStep = "form" | "auth" | "config";

interface ChannelInstanceFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  instance?: ChannelInstanceData | null;
  agents: AgentData[];
  onSubmit: (data: ChannelInstanceInput) => Promise<unknown>;
  onUpdate?: (id: string, data: Partial<ChannelInstanceInput>) => Promise<unknown>;
}

export function ChannelInstanceFormDialog({
  open,
  onOpenChange,
  instance,
  agents,
  onSubmit,
  onUpdate,
}: ChannelInstanceFormDialogProps) {
  const { t } = useTranslation("channels");

  const [credsValues, setCredsValues] = useState<Record<string, unknown>>({});
  const [configValues, setConfigValues] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [step, setStep] = useState<WizardStep>("form");
  const [createdInstanceId, setCreatedInstanceId] = useState<string | null>(null);
  const [authCompleted, setAuthCompleted] = useState(false);

  const form = useForm<ChannelInstanceFormData>({
    resolver: zodResolver(channelInstanceSchema),
    mode: "onChange",
    defaultValues: {
      name: "",
      displayName: "",
      channelType: "telegram",
      agentId: "",
      enabled: true,
    },
  });

  const channelType = form.watch("channelType");
  const wizard = wizardConfig[channelType];
  const hasWizard = !instance && !!wizard;
  const channelLabel = CHANNEL_TYPES.find((ct) => ct.value === channelType)?.label ?? channelType;

  const totalSteps = hasWizard ? 1 + wizard!.steps.length : 1;
  const currentStepNum = step === "form" ? 1 : (wizard?.steps.indexOf(step as "auth" | "config") ?? 0) + 2;

  const getNextWizardStep = useCallback((current: WizardStep): "auth" | "config" | null => {
    if (!wizard) return null;
    if (current === "form") return wizard.steps[0] ?? null;
    const idx = wizard.steps.indexOf(current as "auth" | "config");
    return idx >= 0 ? wizard.steps[idx + 1] ?? null : null;
  }, [wizard]);

  useEffect(() => {
    if (open) {
      form.reset({
        name: instance?.name ?? "",
        displayName: instance?.display_name ?? "",
        channelType: instance?.channel_type ?? "telegram",
        agentId: instance?.agent_id ?? (agents[0]?.id ?? ""),
        enabled: instance?.enabled ?? true,
      });
      setCredsValues({});

      const ct = instance?.channel_type ?? "telegram";
      const schema = configSchema[ct] ?? [];
      const defaults: Record<string, unknown> = {};
      for (const f of schema) {
        if (f.defaultValue !== undefined) defaults[f.key] = f.defaultValue;
      }
      const merged: Record<string, unknown> = { ...defaults, ...(instance?.config ?? {}) };
      const boolSelectKeys = new Set(
        schema.filter((f: FieldDef) => f.type === "select" && f.options?.some((o) => o.value === "true")).map((f: FieldDef) => f.key),
      );
      for (const key of boolSelectKeys) {
        if (typeof merged[key] === "boolean") merged[key] = String(merged[key]);
        else if (merged[key] === undefined || merged[key] === null) merged[key] = "inherit";
      }
      setConfigValues(merged);
      setError("");
      setStep("form");
      setCreatedInstanceId(null);
      setAuthCompleted(false);
    }
  }, [open, instance, agents, form]);

  useEffect(() => {
    if (step !== "auth" || !authCompleted) return;
    const next = getNextWizardStep("auth");
    const id = setTimeout(() => {
      if (next) setStep(next);
      else onOpenChange(false);
    }, 1200);
    return () => clearTimeout(id);
  }, [step, authCompleted, getNextWizardStep, onOpenChange]);

  const handleCredsChange = useCallback((key: string, value: unknown) => {
    setCredsValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleConfigChange = useCallback((key: string, value: unknown) => {
    setConfigValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const coerceBoolSelects = (cfg: Record<string, unknown>, schema: FieldDef[]) => {
    const boolSelectKeys = new Set(
      schema.filter((f) => f.type === "select" && f.options?.some((o) => o.value === "true")).map((f) => f.key),
    );
    for (const key of boolSelectKeys) {
      const v = cfg[key];
      if (v === "true") cfg[key] = true;
      else if (v === "false") cfg[key] = false;
      else delete cfg[key];
    }
  };

  const handleSubmit = form.handleSubmit(async (values) => {
    if (!instance) {
      const schema = credentialsSchema[values.channelType] ?? [];
      const missing = schema.filter((f: FieldDef) => f.required && !credsValues[f.key]);
      if (missing.length > 0) {
        setError(t("form.errors.requiredFields", { fields: missing.map((f: FieldDef) => f.label).join(", ") }));
        return;
      }
    }

    const cleanConfig = Object.fromEntries(
      Object.entries(configValues).filter(([, v]) => v !== undefined && v !== "" && v !== null),
    );
    coerceBoolSelects(cleanConfig, configSchema[values.channelType] ?? []);
    const cleanCreds = Object.fromEntries(
      Object.entries(credsValues).filter(([, v]) => v !== undefined && v !== "" && v !== null),
    );

    setLoading(true);
    setError("");
    try {
      const data: ChannelInstanceInput = {
        name: values.name,
        display_name: values.displayName?.trim() || undefined,
        channel_type: values.channelType,
        agent_id: values.agentId,
        config: Object.keys(cleanConfig).length > 0 ? cleanConfig : undefined,
        enabled: values.enabled,
      };
      if (Object.keys(cleanCreds).length > 0) data.credentials = cleanCreds;

      const result = await onSubmit(data);

      if (hasWizard && wizard) {
        const res = result as Record<string, unknown> | undefined;
        const firstStep = wizard.steps[0];
        if (typeof res?.id === "string" && firstStep) {
          setCreatedInstanceId(res.id);
          setStep(firstStep);
        } else {
          onOpenChange(false);
        }
      } else {
        onOpenChange(false);
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("form.errors.failedSave"));
    } finally {
      setLoading(false);
    }
  });

  const handleConfigDone = async () => {
    if (!createdInstanceId || !onUpdate) { onOpenChange(false); return; }
    const cleanConfig = Object.fromEntries(
      Object.entries(configValues).filter(([, v]) => v !== undefined && v !== "" && v !== null),
    );
    coerceBoolSelects(cleanConfig, configSchema[channelType] ?? []);
    setLoading(true);
    setError("");
    try {
      await onUpdate(createdInstanceId, { config: cleanConfig });
      onOpenChange(false);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("form.errors.failedSaveConfig"));
    } finally {
      setLoading(false);
    }
  };

  const handleSkipAuth = () => {
    const next = getNextWizardStep("auth");
    if (next) setStep(next);
    else onOpenChange(false);
  };

  const canClose = step !== "auth";
  const AuthStep = wizardAuthSteps[channelType];
  const ConfigStep = wizardConfigSteps[channelType];

  const submitLabel = loading
    ? t("form.saving")
    : instance
      ? t("form.update")
      : (wizard?.createLabel ? t(wizard.createLabel) : t("form.create"));

  const dialogTitle = instance
    ? t("form.editTitle")
    : step === "form"
      ? t("form.createTitle")
      : step === "auth"
        ? t("form.authenticate", { label: channelLabel })
        : t("form.configure", { label: channelLabel });

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!loading && canClose) onOpenChange(v); }}>
      <DialogContent className="max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{dialogTitle}</DialogTitle>
          {hasWizard && (
            <p className="text-xs text-muted-foreground">
              {t("form.step", { current: currentStepNum, total: totalSteps })}
            </p>
          )}
        </DialogHeader>

        {step === "form" && (
          <ChannelInstanceFormStep
            form={form}
            instance={instance}
            agents={agents}
            credsValues={credsValues}
            configValues={configValues}
            onCredsChange={handleCredsChange}
            onConfigChange={handleConfigChange}
            setConfigValues={setConfigValues}
            error={error}
            loading={loading}
            onCancel={() => onOpenChange(false)}
            onSubmit={handleSubmit}
            submitLabel={submitLabel}
          />
        )}

        {step === "auth" && createdInstanceId && AuthStep && (
          <AuthStep
            instanceId={createdInstanceId}
            onComplete={() => setAuthCompleted(true)}
            onSkip={handleSkipAuth}
          />
        )}

        {step === "config" && createdInstanceId && ConfigStep && (
          <>
            <div className="py-2 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
              <ConfigStep
                instanceId={createdInstanceId}
                authCompleted={authCompleted}
                configValues={configValues}
                onConfigChange={handleConfigChange}
              />
              {error && <p className="text-sm text-destructive mt-2">{error}</p>}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>{t("form.skip")}</Button>
              <Button onClick={handleConfigDone} disabled={loading}>{loading ? t("form.saving") : t("form.done")}</Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
