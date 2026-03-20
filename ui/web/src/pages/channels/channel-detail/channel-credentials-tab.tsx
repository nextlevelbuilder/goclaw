import { useState, useCallback } from "react";
import { Save, Loader2 } from "lucide-react";
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

  const fields = credentialsSchema[instance.channel_type] ?? [];

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

      <div className="flex items-center justify-end gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {saving ? t("detail.credentials.saving") : t("detail.credentials.updateCredentials")}
        </Button>
      </div>
    </div>
  );
}
