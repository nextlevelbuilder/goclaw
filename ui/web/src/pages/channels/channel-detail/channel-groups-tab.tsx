import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Save, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData } from "@/types/channel";
import type { ChannelContact } from "@/types/contact";
import type { AgentData } from "@/types/agent";
import { TelegramGroupOverrides } from "../telegram-group-overrides";
import type { TelegramGroupConfigValues } from "../telegram-group-fields";
import type { TelegramTopicConfigValues } from "../telegram-topic-overrides";
import { WhatsAppGroupOverrides } from "../whatsapp-group-overrides";
import type { GroupManagerGroupInfo } from "../hooks/use-channel-detail";

interface GroupConfigWithTopics extends TelegramGroupConfigValues {
  topics?: Record<string, TelegramTopicConfigValues>;
}

interface ChannelGroupsTabProps {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  listManagerGroups: () => Promise<GroupManagerGroupInfo[]>;
  listContacts: (search: string, channelType?: string) => Promise<ChannelContact[]>;
  agents: AgentData[];
}

export function ChannelGroupsTab({
  instance,
  onUpdate,
  listManagerGroups,
  listContacts,
  agents,
}: ChannelGroupsTabProps) {
  if (instance.channel_type === "whatsapp") {
    return (
      <WhatsAppGroupsContent
        instance={instance}
        onUpdate={onUpdate}
        listContacts={listContacts}
        agents={agents}
      />
    );
  }

  // Telegram (default)
  return (
    <TelegramGroupsContent
      instance={instance}
      onUpdate={onUpdate}
      listManagerGroups={listManagerGroups}
    />
  );
}

// --- Telegram groups content (existing logic) ---

function TelegramGroupsContent({
  instance,
  onUpdate,
  listManagerGroups,
}: {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  listManagerGroups: () => Promise<GroupManagerGroupInfo[]>;
}) {
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
    } catch {
      /* handled by http hook */
    }
  }, [listManagerGroups]);

  useEffect(() => {
    loadKnownGroups();
  }, [loadKnownGroups]);

  const handleSave = async () => {
    const hasGroups = Object.keys(groups).length > 0;
    const updatedConfig = { ...config, groups: hasGroups ? groups : undefined };
    // Clean undefined entries
    const cleanConfig = Object.fromEntries(
      Object.entries(updatedConfig).filter(([, v]) => v !== undefined),
    );

    setSaving(true);
    try {
      await onUpdate({
        config: Object.keys(cleanConfig).length > 0 ? cleanConfig : null,
      });
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <TelegramGroupOverrides
        groups={groups}
        onChange={(g) => setGroups(g)}
        knownGroups={knownGroups}
      />

      <div className="flex items-center justify-end gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Save className="h-4 w-4" />
          )}
          {saving ? t("detail.groups.saving") : t("detail.groups.saveGroups")}
        </Button>
      </div>
    </div>
  );
}

// --- WhatsApp groups content ---

function WhatsAppGroupsContent({
  instance,
  onUpdate,
  listContacts,
  agents,
}: {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  listContacts: (search: string, channelType?: string) => Promise<ChannelContact[]>;
  agents: AgentData[];
}) {
  const { t } = useTranslation("channels");
  const config = (instance.config ?? {}) as Record<string, unknown>;
  const [groups, setGroups] = useState<
    Record<string, { name?: string; agent_id?: string; enabled?: boolean }>
  >((config.groups as Record<string, { name?: string; agent_id?: string; enabled?: boolean }>) ?? {});
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    const hasGroups = Object.keys(groups).length > 0;
    const updatedConfig = { ...config, groups: hasGroups ? groups : undefined };
    const cleanConfig = Object.fromEntries(
      Object.entries(updatedConfig).filter(([, v]) => v !== undefined),
    );

    setSaving(true);
    try {
      await onUpdate({
        config: Object.keys(cleanConfig).length > 0 ? cleanConfig : null,
      });
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <WhatsAppGroupOverrides
        groups={groups}
        onChange={setGroups}
        listContacts={listContacts}
        agents={agents}
      />

      <div className="flex items-center justify-end gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Save className="h-4 w-4" />
          )}
          {saving
            ? t("whatsappGroupOverrides.saving")
            : t("whatsappGroupOverrides.saveGroups")}
        </Button>
      </div>
    </div>
  );
}
