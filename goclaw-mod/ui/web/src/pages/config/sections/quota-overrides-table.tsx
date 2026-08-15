import { useTranslation } from "react-i18next";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { InfoLabel } from "@/components/shared/info-label";

export interface QuotaWindow {
  hour?: number;
  day?: number;
  week?: number;
}

interface QuotaWindowInputsProps {
  value: QuotaWindow;
  onChange: (v: QuotaWindow) => void;
}

export function QuotaWindowInputs({ value, onChange }: QuotaWindowInputsProps) {
  const { t } = useTranslation("config");
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <div className="grid gap-1.5">
        <InfoLabel tip={t("quota.hourTip")}>{t("quota.hour")}</InfoLabel>
        <Input
          type="number"
          min={0}
          value={value.hour ?? 0}
          onChange={(e) => onChange({ ...value, hour: Number(e.target.value) })}
        />
      </div>
      <div className="grid gap-1.5">
        <InfoLabel tip={t("quota.dayTip")}>{t("quota.day")}</InfoLabel>
        <Input
          type="number"
          min={0}
          value={value.day ?? 0}
          onChange={(e) => onChange({ ...value, day: Number(e.target.value) })}
        />
      </div>
      <div className="grid gap-1.5">
        <InfoLabel tip={t("quota.weekTip")}>{t("quota.week")}</InfoLabel>
        <Input
          type="number"
          min={0}
          value={value.week ?? 0}
          onChange={(e) => onChange({ ...value, week: Number(e.target.value) })}
        />
      </div>
    </div>
  );
}

interface OverridesTableProps {
  label: string;
  tip: string;
  entries: Record<string, QuotaWindow>;
  onChange: (v: Record<string, QuotaWindow>) => void;
  keyPlaceholder: string;
  options?: { value: string; label: string }[];
}

export function OverridesTable({
  label,
  tip,
  entries,
  onChange,
  keyPlaceholder,
  options,
}: OverridesTableProps) {
  const { t } = useTranslation("config");
  const keys = Object.keys(entries);
  const usedKeys = new Set(keys);
  const availableOptions = options?.filter((o) => !usedKeys.has(o.value));

  const addRow = (key?: string) => {
    const newKey = key ?? "";
    if (newKey in entries) return;
    onChange({ ...entries, [newKey]: { hour: 0, day: 0, week: 0 } });
  };

  const removeRow = (key: string) => {
    const next = { ...entries };
    delete next[key];
    onChange(next);
  };

  const updateKey = (oldKey: string, newKey: string) => {
    if (newKey !== oldKey && newKey in entries) return;
    const next: Record<string, QuotaWindow> = {};
    for (const [k, v] of Object.entries(entries)) {
      next[k === oldKey ? newKey : k] = v;
    }
    onChange(next);
  };

  const updateWindow = (key: string, window: QuotaWindow) => {
    onChange({ ...entries, [key]: window });
  };

  return (
    <div className="space-y-2">
      <InfoLabel tip={tip}>{label}</InfoLabel>
      {keys.map((key, i) => (
        <div key={i} className="overflow-x-auto">
          <div className="flex items-end gap-2 min-w-[420px]">
            <div className="grid gap-1.5 min-w-[180px]">
              {i === 0 && (
                <span className="text-xs text-muted-foreground">{t("quota.keyLabel")}</span>
              )}
              {options ? (
                <Select value={key} onValueChange={(v) => updateKey(key, v)}>
                  <SelectTrigger>
                    <SelectValue placeholder={keyPlaceholder} />
                  </SelectTrigger>
                  <SelectContent>
                    {key && (
                      <SelectItem value={key}>
                        {options.find((o) => o.value === key)?.label ?? key}
                      </SelectItem>
                    )}
                    {availableOptions
                      ?.filter((o) => o.value !== key)
                      .map((o) => (
                        <SelectItem key={o.value} value={o.value}>
                          {o.label}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  placeholder={keyPlaceholder}
                  value={key}
                  onChange={(e) => updateKey(key, e.target.value)}
                />
              )}
            </div>
            <div className="grid gap-1.5 w-20">
              {i === 0 && (
                <span className="text-xs text-muted-foreground">{t("quota.hour")}</span>
              )}
              <Input
                type="number"
                min={0}
                value={entries[key]?.hour ?? 0}
                onChange={(e) => updateWindow(key, { ...entries[key], hour: Number(e.target.value) })}
              />
            </div>
            <div className="grid gap-1.5 w-20">
              {i === 0 && (
                <span className="text-xs text-muted-foreground">{t("quota.day")}</span>
              )}
              <Input
                type="number"
                min={0}
                value={entries[key]?.day ?? 0}
                onChange={(e) => updateWindow(key, { ...entries[key], day: Number(e.target.value) })}
              />
            </div>
            <div className="grid gap-1.5 w-20">
              {i === 0 && (
                <span className="text-xs text-muted-foreground">{t("quota.week")}</span>
              )}
              <Input
                type="number"
                min={0}
                value={entries[key]?.week ?? 0}
                onChange={(e) => updateWindow(key, { ...entries[key], week: Number(e.target.value) })}
              />
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="shrink-0"
              onClick={() => removeRow(key)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        </div>
      ))}
      {options ? (
        <Select value="" onValueChange={(v) => addRow(v)}>
          <SelectTrigger className="w-auto gap-1.5" size="sm">
            <Plus className="h-3.5 w-3.5" />
            <SelectValue placeholder={t("quota.addOverride")} />
          </SelectTrigger>
          <SelectContent>
            {availableOptions && availableOptions.length > 0 ? (
              availableOptions.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))
            ) : (
              <SelectItem value="__none__" disabled>
                {t("quota.allOptionsAdded")}
              </SelectItem>
            )}
          </SelectContent>
        </Select>
      ) : (
        <Button variant="outline" size="sm" onClick={() => addRow()} className="gap-1.5">
          <Plus className="h-3.5 w-3.5" /> {t("quota.addOverride")}
        </Button>
      )}
    </div>
  );
}
