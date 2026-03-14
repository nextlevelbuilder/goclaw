import { useState, useCallback } from "react";
import { Save, Check, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData } from "@/types/channel";
import { credentialsSchema } from "../channel-schemas";
import { ChannelFields } from "../channel-fields";
import { useTranslation } from "react-i18next";

interface ChannelCredentialsTabProps {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function ChannelCredentialsTab({ instance, onUpdate }: ChannelCredentialsTabProps) {
  const { t } = useTranslation("channels");
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const fields = credentialsSchema[instance.channel_type] ?? [];

  const handleChange = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleSave = async () => {
    const cleanCreds = Object.fromEntries(
      Object.entries(values).filter(([, v]) => v !== undefined && v !== "" && v !== null),
    );
    if (Object.keys(cleanCreds).length === 0) {
      setSaveError(t("detail.credentials.noCredentials"));
      return;
    }
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      await onUpdate({ credentials: cleanCreds });
      setSaved(true);
      setValues({});
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t("detail.credentials.failedSave"));
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
      <p className="text-sm text-muted-foreground">
        {t("detail.credentials.hint")}
      </p>

      <ChannelFields
        fields={fields}
        values={values}
        onChange={handleChange}
        idPrefix="cd-cred"
        isEdit
      />

      {saveError && (
        <div className="flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="h-4 w-4 shrink-0" />
          {saveError}
        </div>
      )}
      <div className="flex items-center justify-end gap-2">
        {saved && (
          <span className="flex items-center gap-1 text-sm text-success">
            <Check className="h-3.5 w-3.5" /> {t("detail.credentials.saved")}
          </span>
        )}
        <Button onClick={handleSave} disabled={saving}>
          {!saving && <Save className="h-4 w-4" />}
          {saving ? t("detail.credentials.saving") : t("detail.credentials.updateCredentials")}
        </Button>
      </div>
    </div>
  );
}
