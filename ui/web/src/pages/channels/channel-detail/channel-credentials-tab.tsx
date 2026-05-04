import { useState, useCallback, useEffect, useMemo } from "react";
import { Save, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData, ChannelRuntimeStatus } from "@/types/channel";
import { credentialsSchema, configSchema, type FieldDef } from "../channel-schemas";
import { ChannelFields } from "../channel-fields";
import { ZaloWebhookURLSection } from "../zalo/zalo-webhook-url-section";
import { useTranslation } from "react-i18next";

interface ChannelCredentialsTabProps {
  instance: ChannelInstanceData;
  status?: ChannelRuntimeStatus | null;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

// Backend masks secrets as "***" and leaves non-secret keys (per channel-type
// allowlist) plain. Pre-populate non-password fields so users can see and
// edit values like redirect_uri without retyping.
function initialCredsValues(
  fields: FieldDef[],
  raw: Record<string, unknown> | undefined,
): Record<string, unknown> {
  if (!raw) return {};
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    if (f.type === "password") continue;
    const v = raw[f.key];
    if (v !== undefined && v !== null && v !== "***" && v !== "") out[f.key] = v;
  }
  return out;
}

// Merge config defaults with instance.config so credential fields' showWhen
// can resolve config keys (e.g. "transport") even when the saved config
// relied on schema defaults.
function buildConfigContext(channelType: string, cfg: Record<string, unknown> | null): Record<string, unknown> {
  const schema = configSchema[channelType] ?? [];
  const ctx: Record<string, unknown> = {};
  for (const f of schema) {
    if (f.defaultValue !== undefined) ctx[f.key] = f.defaultValue;
  }
  if (cfg) Object.assign(ctx, cfg);
  return ctx;
}

export function ChannelCredentialsTab({ instance, status, onUpdate }: ChannelCredentialsTabProps) {
  const { t } = useTranslation("channels");
  const fields = useMemo(
    () => credentialsSchema[instance.channel_type] ?? [],
    [instance.channel_type],
  );
  const ctx = useMemo(
    () => buildConfigContext(instance.channel_type, instance.config),
    [instance.channel_type, instance.config],
  );
  const [values, setValues] = useState<Record<string, unknown>>(() =>
    initialCredsValues(fields, instance.credentials),
  );
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setValues(initialCredsValues(fields, instance.credentials));
  }, [fields, instance.credentials]);

  const isZaloOABootstrap =
    instance.channel_type === "zalo_oa" &&
    status?.state === "degraded" &&
    status?.bootstrap_state === "awaiting_secret";

  const handleChange = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleSave = async () => {
    const cleanCreds = Object.fromEntries(
      Object.entries(values).filter(([, v]) => v !== undefined && v !== "" && v !== null),
    );
    if (Object.keys(cleanCreds).length === 0) return;
    setSaving(true);
    try {
      await onUpdate({ credentials: cleanCreds });
      setValues({});
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  if (fields.length === 0) {
    return (
      <div className="">
        <p className="text-sm text-muted-foreground">
          {t("detail.credentials.noSchema")}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {isZaloOABootstrap && (
        <div className="rounded-md border border-amber-300 bg-amber-50 p-4 space-y-3 dark:bg-amber-950/30 dark:border-amber-700">
          <p className="font-medium">{t("detail.credentials.bootstrapBanner.title")}</p>
          <ol className="list-decimal list-inside text-sm space-y-1">
            <li>{t("detail.credentials.bootstrapBanner.step1")}</li>
            <li>{t("detail.credentials.bootstrapBanner.step2")}</li>
            <li>{t("detail.credentials.bootstrapBanner.step3")}</li>
            <li>{t("detail.credentials.bootstrapBanner.step4")}</li>
          </ol>
          <ZaloWebhookURLSection instanceId={instance.id} channelType={instance.channel_type} />
          <p className="text-xs text-muted-foreground">{t("detail.credentials.bootstrapBanner.note")}</p>
        </div>
      )}

      <p className="text-sm text-muted-foreground">
        {t("detail.credentials.hint")}
      </p>

      <ChannelFields
        fields={fields}
        values={values}
        onChange={handleChange}
        idPrefix="cd-cred"
        isEdit
        contextValues={ctx}
      />

      <div className="flex items-center justify-end gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {saving ? t("detail.credentials.saving") : t("detail.credentials.updateCredentials")}
        </Button>
      </div>
    </div>
  );
}
