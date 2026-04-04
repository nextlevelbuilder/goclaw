import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
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
import { QuotaWindowInputs, OverridesTable, type QuotaWindow } from "./quota-overrides-table";

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
