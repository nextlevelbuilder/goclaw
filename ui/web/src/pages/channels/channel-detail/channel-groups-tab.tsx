import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Save, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData } from "@/types/channel";
import { TelegramGroupOverrides } from "../telegram-group-overrides";
import type { TelegramGroupConfigValues } from "../telegram-group-fields";
import type { TelegramTopicConfigValues } from "../telegram-topic-overrides";
import type { GroupManagerGroupInfo } from "../hooks/use-channel-detail";

interface GroupConfigWithTopics extends TelegramGroupConfigValues {
  topics?: Record<string, TelegramTopicConfigValues>;
}

interface ChannelGroupsTabProps {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  listManagerGroups: () => Promise<GroupManagerGroupInfo[]>;
}

export function ChannelGroupsTab({ instance, onUpdate, listManagerGroups }: ChannelGroupsTabProps) {
  const { t } = useTranslation("channels");
  const config = (instance.config ?? {}) as Record<string, unknown>;
  const [groups, setGroups] = useState<Record<string, GroupConfigWithTopics>>(
    (config.groups as Record<string, GroupConfigWithTopics>) ?? {},
  );
  const [saving, setSaving] = useState(false);
  const [knownGroups, setKnownGroups] = useState<GroupManagerGroupInfo[]>([]);

  const loadKnownGroups = useCallback(async () => {
    try {
      const g = await listManagerGroups();
      setKnownGroups(g);
    } catch { /* handled by http hook */ }
  }, [listManagerGroups]);

  useEffect(() => { loadKnownGroups(); }, [loadKnownGroups]);

  const handleSave = async () => {
    const hasGroups = Object.keys(groups).length > 0;
    const updatedConfig = { ...config, groups: hasGroups ? groups : undefined };
    // Clean undefined entries
    const cleanConfig = Object.fromEntries(
      Object.entries(updatedConfig).filter(([, v]) => v !== undefined),
    );

    setSaving(true);
    try {
      await onUpdate({ config: Object.keys(cleanConfig).length > 0 ? cleanConfig : null });
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <TelegramGroupOverrides groups={groups} onChange={(g) => setGroups(g)} knownGroups={knownGroups} />

      <div className="flex items-center justify-end gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {saving ? t("detail.groups.saving") : t("detail.groups.saveGroups")}
        </Button>
      </div>
    </div>
  );
}
