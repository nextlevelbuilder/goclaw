import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { FeatureSwitchGroup } from "@/components/shared/feature-switch-group";
import { FEATURE_GROUPS } from "@/lib/feature-registry";

interface FeaturesSectionProps {
  data: Record<string, boolean | undefined> | undefined;
  onSave: (features: Record<string, boolean>) => void;
  saving: boolean;
}

export function FeaturesSection({ data, onSave, saving }: FeaturesSectionProps) {
  const { t } = useTranslation("features");
  const [draft, setDraft] = useState<Record<string, boolean>>({});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (data) {
      const initial: Record<string, boolean> = {};
      for (const group of FEATURE_GROUPS) {
        for (const f of group.features) {
          initial[f.key] = data[f.key] !== false;
        }
      }
      setDraft(initial);
      setDirty(false);
    }
  }, [data]);

  const toggle = (key: string, val: boolean) => {
    setDraft((prev) => ({ ...prev, [key]: val }));
    setDirty(true);
  };

  const handleSave = () => {
    // Only send false values (true = default, omit)
    const result: Record<string, boolean> = {};
    for (const [k, v] of Object.entries(draft)) {
      if (!v) result[k] = false;
    }
    onSave(result);
    setDirty(false);
  };

  return (
    <div className="space-y-4">
      {FEATURE_GROUPS.map((group) => (
        <FeatureSwitchGroup
          key={group.key}
          title={t(group.labelKey.replace("features:", ""))}
          description={t(group.descKey.replace("features:", ""))}
          items={group.features.map((f) => ({
            label: t(f.labelKey.replace("features:", "")),
            hint: t(f.descKey.replace("features:", "")),
            checked: draft[f.key] ?? true,
            onCheckedChange: (v) => toggle(f.key, v),
          }))}
        />
      ))}

      {dirty && (
        <div className="flex justify-end">
          <Button onClick={handleSave} disabled={saving} size="sm">
            {saving ? t("common:saving", "Saving...") : t("common:save", "Save")}
          </Button>
        </div>
      )}
    </div>
  );
}
