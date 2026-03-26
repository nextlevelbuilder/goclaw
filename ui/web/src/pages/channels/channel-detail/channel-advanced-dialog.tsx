import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Save, Settings, Loader2 } from "lucide-react";
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
import type { ChannelInstanceData } from "@/types/channel";

interface ChannelAdvancedDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

const ESSENTIAL_CONFIG_KEYS = new Set(["dm_policy", "group_policy", "require_mention", "mention_mode"]);

const NETWORK_KEYS = new Set(["api_server", "proxy", "domain", "connection_mode", "webhook_port", "webhook_path", "webhook_url"]);
const LIMITS_KEYS = new Set(["history_limit", "media_max_mb", "text_chunk_limit"]);
const STREAMING_KEYS = new Set(["dm_stream", "group_stream", "draft_transport", "reasoning_stream", "native_stream", "debounce_delay", "thread_ttl"]);
const BEHAVIOR_KEYS = new Set(["reaction_level", "link_preview", "block_reply", "render_mode", "topic_session_mode"]);
const ACCESS_KEYS = new Set(["allow_from", "group_allow_from"]);

function getAdvancedFields(channelType: string) {
  const allFields = configSchema[channelType] ?? [];
  const advanced = allFields.filter((f) => !ESSENTIAL_CONFIG_KEYS.has(f.key));
  return {
    network: advanced.filter((f) => NETWORK_KEYS.has(f.key)),
    limits: advanced.filter((f) => LIMITS_KEYS.has(f.key)),
    streaming: advanced.filter((f) => STREAMING_KEYS.has(f.key)),
    behavior: advanced.filter((f) => BEHAVIOR_KEYS.has(f.key)),
    access: advanced.filter((f) => ACCESS_KEYS.has(f.key)),
  };
}

function deriveInitialValues(instance: ChannelInstanceData): Record<string, unknown> {
  const config = (instance.config ?? {}) as Record<string, unknown>;
  const { groups: _groups, ...rest } = config as Record<string, unknown> & { groups?: unknown };
  // Only keep advanced keys (exclude essential + groups)
  return Object.fromEntries(
    Object.entries(rest).filter(([k]) => !ESSENTIAL_CONFIG_KEYS.has(k)),
  );
}

export function ChannelAdvancedDialog({
  open,
  onOpenChange,
  instance,
  onUpdate,
}: ChannelAdvancedDialogProps) {
  const { t } = useTranslation("channels");
  const groups = getAdvancedFields(instance.channel_type);

  const [values, setValues] = useState<Record<string, unknown>>(() => deriveInitialValues(instance));
  const [saving, setSaving] = useState(false);

  // Re-sync local state when dialog opens
  useEffect(() => {
    if (!open) return;
    setValues(deriveInitialValues(instance));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const handleChange = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const existingConfig = (instance.config ?? {}) as Record<string, unknown>;
      const cleanAdvanced = Object.fromEntries(
        Object.entries(values).filter(([, v]) => v !== undefined && v !== "" && v !== null),
      );
      // Merge: preserve essential keys and groups from existing, overwrite advanced keys
      const merged = { ...existingConfig, ...cleanAdvanced };
      await onUpdate({ config: merged });
      onOpenChange(false);
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  const hasAnyGroup = Object.values(groups).some((g) => g.length > 0);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-2xl">
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
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 pt-4 border-t shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("form.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {saving ? t("form.saving") : t("detail.config.saveConfig")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
