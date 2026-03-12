import { useState, useEffect } from "react";
import { Save, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { InfoLabel } from "@/components/shared/info-label";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";

interface QuotaWindow {
  hour?: number;
  day?: number;
  week?: number;
}

interface QuotaData {
  enabled: boolean;
  default: QuotaWindow;
  providers?: Record<string, QuotaWindow>;
  channels?: Record<string, QuotaWindow>;
  groups?: Record<string, QuotaWindow>;
}

const DEFAULT_QUOTA: QuotaData = {
  enabled: true,
  default: { hour: 40, day: 200, week: 1000 },
};

interface Props {
  data: { quota?: QuotaData } | undefined;
  onSave: (value: { quota: QuotaData }) => Promise<void>;
  saving: boolean;
}

function QuotaWindowInputs({
  value,
  onChange,
}: {
  value: QuotaWindow;
  onChange: (v: QuotaWindow) => void;
}) {
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

function OverridesTable({
  label,
  tip,
  entries,
  onChange,
  keyPlaceholder,
  options,
}: {
  label: string;
  tip: string;
  entries: Record<string, QuotaWindow>;
  onChange: (v: Record<string, QuotaWindow>) => void;
  keyPlaceholder: string;
  options?: { value: string; label: string }[];
}) {
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

  const { t } = useTranslation("config");

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
                <Select
                  value={key}
                  onValueChange={(v) => updateKey(key, v)}
                >
                  <SelectTrigger>
                    <SelectValue placeholder={keyPlaceholder} />
                  </SelectTrigger>
                  <SelectContent>
                    {/* Current value always shown */}
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
                onChange={(e) =>
                  updateWindow(key, {
                    ...entries[key],
                    hour: Number(e.target.value),
                  })
                }
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
                onChange={(e) =>
                  updateWindow(key, {
                    ...entries[key],
                    day: Number(e.target.value),
                  })
                }
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
                onChange={(e) =>
                  updateWindow(key, {
                    ...entries[key],
                    week: Number(e.target.value),
                  })
                }
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
        <Select
          value=""
          onValueChange={(v) => addRow(v)}
        >
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

export function QuotaSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<QuotaData>(
    data?.quota ?? DEFAULT_QUOTA
  );
  const [dirty, setDirty] = useState(false);

  const { providers } = useProviders();
  const { instances } = useChannelInstances();

  const providerOptions = providers.map((p) => ({
    value: p.name,
    label: p.display_name || p.name,
  }));

  // Deduplicate channel types
  const channelOptions = [
    ...new Map(
      instances.map((c) => [c.channel_type, c.channel_type])
    ).entries(),
  ].map(([value]) => ({ value, label: value }));

  // Build group options from channel instance configs (e.g., telegram groups)
  const groupOptions = instances.flatMap((inst) => {
    const groups = (inst.config as Record<string, unknown>)?.groups as
      | Record<string, unknown>
      | undefined;
    if (!groups) return [];
    return Object.keys(groups).map((gid) => ({
      value: `group:${inst.channel_type}:${gid}`,
      label: `${inst.channel_type} / ${gid}`,
    }));
  });

  useEffect(() => {
    setDraft(data?.quota ?? DEFAULT_QUOTA);
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<QuotaData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("quota.title")}</CardTitle>
        <CardDescription>{t("quota.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between">
          <InfoLabel tip={t("quota.enabledTip")}>{t("quota.enabled")}</InfoLabel>
          <Switch
            checked={draft.enabled}
            onCheckedChange={(v) => update({ enabled: v })}
          />
        </div>

        {draft.enabled && (
          <>
            <div className="space-y-2">
              <InfoLabel tip={t("quota.defaultLimitsTip")}>
                {t("quota.defaultLimits")}
              </InfoLabel>
              <QuotaWindowInputs
                value={draft.default}
                onChange={(v) => update({ default: v })}
              />
            </div>

            <OverridesTable
              label={t("quota.providerOverrides")}
              tip={t("quota.providerOverridesTip")}
              entries={draft.providers ?? {}}
              onChange={(v) => update({ providers: v })}
              keyPlaceholder={t("quota.selectProvider")}
              options={providerOptions}
            />

            <OverridesTable
              label={t("quota.channelOverrides")}
              tip={t("quota.channelOverridesTip")}
              entries={draft.channels ?? {}}
              onChange={(v) => update({ channels: v })}
              keyPlaceholder={t("quota.selectChannel")}
              options={channelOptions}
            />

            <OverridesTable
              label={t("quota.groupOverrides")}
              tip={t("quota.groupOverridesTip")}
              entries={draft.groups ?? {}}
              onChange={(v) => update({ groups: v })}
              keyPlaceholder={t("quota.selectGroup")}
              options={groupOptions.length > 0 ? groupOptions : undefined}
            />
          </>
        )}

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button
              size="sm"
              onClick={() => onSave({ quota: draft })}
              disabled={saving}
              className="gap-1.5"
            >
              <Save className="h-3.5 w-3.5" />{" "}
              {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
