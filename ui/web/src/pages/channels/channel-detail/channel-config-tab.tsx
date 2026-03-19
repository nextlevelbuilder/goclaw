import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Save, Check, AlertCircle, Info, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData } from "@/types/channel";
import { configSchema } from "../channel-schemas";
import { ChannelFields } from "../channel-fields";
import { ChannelScopesInfo } from "../channel-scopes-info";

interface ChannelConfigTabProps {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function ChannelConfigTab({ instance, onUpdate }: ChannelConfigTabProps) {
  const { t } = useTranslation("channels");
  const config = instance.config ?? {};
  // Filter out "groups" from config — managed in separate Groups tab
  const { groups: _groups, ...restConfig } = config as Record<string, unknown> & { groups?: unknown };

  const [values, setValues] = useState<Record<string, unknown>>(restConfig);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const fields = configSchema[instance.channel_type] ?? [];

  const handleChange = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleSave = async () => {
    const cleanConfig = Object.fromEntries(
      Object.entries(values).filter(([, v]) => v !== undefined && v !== "" && v !== null),
    );
    // Preserve existing groups when saving config
    const existingGroups = (instance.config as Record<string, unknown> | null)?.groups;
    const merged = existingGroups
      ? { ...cleanConfig, groups: existingGroups }
      : cleanConfig;

    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      await onUpdate({ config: Object.keys(merged).length > 0 ? merged : null });
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  };

  if (fields.length === 0) {
    return (
      <div className="max-w-2xl">
        <p className="text-sm text-muted-foreground">
          {t("detail.config.noSchema")}
        </p>
      </div>
    );
  }

  // Show webhook URL hint for Feishu/Lark in webhook mode
  const isFeishu = instance.channel_type === "feishu";
  const connectionMode = (values.connection_mode as string) ?? "webhook";
  const showWebhookUrl = isFeishu && connectionMode === "webhook";
  const webhookPath = (values.webhook_path as string) || "/feishu/events";
  const webhookUrl = `https://<your-gateway-domain>${webhookPath}`;

  const [copied, setCopied] = useState(false);
  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(webhookPath);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [webhookPath]);

  return (
    <div className="space-y-6">
      <ChannelFields
        fields={fields}
        values={values}
        onChange={handleChange}
        idPrefix="cd-cfg"
      />

      <ChannelScopesInfo channelType={instance.channel_type} />

      {showWebhookUrl && (
        <div className="flex items-start gap-2 rounded-md border border-blue-200 bg-blue-50 px-3 py-2 text-sm dark:border-blue-800 dark:bg-blue-950">
          <Info className="h-4 w-4 shrink-0 mt-0.5 text-blue-600 dark:text-blue-400" />
          <div className="flex-1 min-w-0">
            <p className="text-blue-800 dark:text-blue-200">{t("detail.config.webhookUrlLabel")}</p>
            <div className="flex items-center gap-1.5 mt-1">
              <code className="text-xs bg-blue-100 dark:bg-blue-900 px-1.5 py-0.5 rounded break-all">
                {webhookUrl}
              </code>
              <button
                type="button"
                onClick={handleCopy}
                className="shrink-0 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-200"
                title={t("detail.config.copyPath")}
              >
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              </button>
            </div>
          </div>
        </div>
      )}

      {saveError && (
        <div className="flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="h-4 w-4 shrink-0" />
          {saveError}
        </div>
      )}
      <div className="flex items-center justify-end gap-2">
        {saved && (
          <span className="flex items-center gap-1 text-sm text-success">
            <Check className="h-3.5 w-3.5" /> {t("detail.config.saved")}
          </span>
        )}
        <Button onClick={handleSave} disabled={saving}>
          {!saving && <Save className="h-4 w-4" />}
          {saving ? t("detail.config.saving") : t("detail.config.saveConfig")}
        </Button>
      </div>
    </div>
  );
}
