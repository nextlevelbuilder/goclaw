import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, Save, Settings, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import { ChannelFields } from "../channel-fields";
import { configSchema } from "../channel-schemas";
import {
  buildAdvancedConfigUpdate,
  deriveAdvancedInitialValues,
  ESSENTIAL_CONFIG_KEYS,
} from "./channel-advanced-config";
import type { ChannelInstanceData } from "@/types/channel";

interface ChannelAdvancedDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  onRefreshDiscordMetadata: () => Promise<void>;
}

const NETWORK_KEYS = new Set(["api_server", "proxy", "domain", "connection_mode", "webhook_port", "webhook_path", "webhook_url"]);
const LIMITS_KEYS = new Set(["history_limit", "media_max_mb", "text_chunk_limit"]);
const STREAMING_KEYS = new Set(["dm_stream", "group_stream", "draft_transport", "reasoning_delivery", "native_stream", "debounce_delay", "thread_ttl"]);
const BEHAVIOR_KEYS = new Set(["reaction_level", "link_preview", "render_mode", "topic_session_mode"]);
const ACCESS_KEYS = new Set(["allow_from", "group_allow_from"]);
const TELEGRAM_MANAGEMENT_KEYS = new Set(["telegram_manager.enabled", "telegram_manager.allowed_actions"]);

export function getAdvancedFields(channelType: string) {
  const allFields = configSchema[channelType] ?? [];
  const advanced = allFields.filter((f) => !ESSENTIAL_CONFIG_KEYS.has(f.key));
  return {
    network: advanced.filter((f) => NETWORK_KEYS.has(f.key)),
    limits: advanced.filter((f) => LIMITS_KEYS.has(f.key)),
    streaming: advanced.filter((f) => STREAMING_KEYS.has(f.key)),
    behavior: advanced.filter((f) => BEHAVIOR_KEYS.has(f.key) || f.key.startsWith("chat_behavior.")),
    access: advanced.filter((f) => ACCESS_KEYS.has(f.key)),
    telegramManagement: advanced.filter((f) => TELEGRAM_MANAGEMENT_KEYS.has(f.key)),
  };
}

export function ChannelAdvancedDialog({
  open,
  onOpenChange,
  instance,
  onUpdate,
  onRefreshDiscordMetadata,
}: ChannelAdvancedDialogProps) {
  const { t } = useTranslation("channels");
  const groups = getAdvancedFields(instance.channel_type);

  const [values, setValues] = useState<Record<string, unknown>>(() => deriveAdvancedInitialValues(instance.config));
  const [saving, setSaving] = useState(false);
  const [refreshingMetadata, setRefreshingMetadata] = useState(false);

  // Re-sync local state when dialog opens
  useEffect(() => {
    if (!open) return;
    setValues(deriveAdvancedInitialValues(instance.config));
  }, [open, instance]);

  const handleChange = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onUpdate({ config: buildAdvancedConfigUpdate(instance.config, values) });
      onOpenChange(false);
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  const handleRefreshMetadata = async () => {
    setRefreshingMetadata(true);
    try {
      await onRefreshDiscordMetadata();
    } catch {
      // toast shown by hook
    } finally {
      setRefreshingMetadata(false);
    }
  };

  const hasAnyGroup = Object.values(groups).some((g) => g.length > 0);
  const showDiscordMetadataRefresh = instance.channel_type === "discord";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings className="h-4 w-4" />
            {t("detail.advancedTitle")}
          </DialogTitle>
        </DialogHeader>

        {/* Scrollable body */}
        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">
          {!hasAnyGroup && (
            <p className="text-sm text-muted-foreground">{t("detail.config.noSchema")}</p>
          )}

          {showDiscordMetadataRefresh && (
            <>
              <ConfigGroupHeader
                title={t("detail.discordMetadata")}
                description={t("detail.discordMetadataDesc")}
              />
              <Button
                type="button"
                variant="outline"
                onClick={handleRefreshMetadata}
                disabled={refreshingMetadata}
              >
                {refreshingMetadata ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                {refreshingMetadata
                  ? t("detail.refreshingDiscordMetadata")
                  : t("detail.refreshDiscordMetadata")}
              </Button>
            </>
          )}

          {groups.network.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.network")}
                description={t("detail.networkDesc")}
              />
              <ChannelFields
                fields={groups.network}
                values={values}
                onChange={handleChange}
                idPrefix="adv-net"
                contextValues={values}
              />
            </>
          )}

          {groups.limits.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.limits")}
                description={t("detail.limitsDesc")}
              />
              <ChannelFields
                fields={groups.limits}
                values={values}
                onChange={handleChange}
                idPrefix="adv-lim"
              />
            </>
          )}

          {groups.streaming.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.streaming")}
                description={t("detail.streamingDesc")}
              />
              <ChannelFields
                fields={groups.streaming}
                values={values}
                onChange={handleChange}
                idPrefix="adv-str"
              />
            </>
          )}

          {groups.behavior.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.behavior")}
                description={t("detail.behaviorDesc")}
              />
              <ChannelFields
                fields={groups.behavior}
                values={values}
                onChange={handleChange}
                idPrefix="adv-beh"
              />
            </>
          )}

          {groups.access.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.accessControl")}
                description={t("detail.accessControlDesc")}
              />
              <ChannelFields
                fields={groups.access}
                values={values}
                onChange={handleChange}
                idPrefix="adv-acc"
              />
            </>
          )}

          {groups.telegramManagement.length > 0 && (
            <>
              <ConfigGroupHeader
                title={t("detail.telegramManagement")}
                description={t("detail.telegramManagementDesc")}
              />
              <ChannelFields
                fields={groups.telegramManagement}
                values={values}
                onChange={handleChange}
                idPrefix="adv-tgm"
              />
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 pt-4 border-t shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving || refreshingMetadata}>
            {t("form.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving || refreshingMetadata}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {saving ? t("form.saving") : t("detail.config.saveConfig")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
