import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { ChannelFields } from "../channel-fields";
import { configSchema } from "../channel-schemas";
import type { ChannelInstanceData } from "@/types/channel";
import type { AgentData } from "@/types/agent";
import { channelTypeLabels } from "../channels-status-view";

const ESSENTIAL_CONFIG_KEYS = ["dm_policy", "group_policy", "require_mention"];

interface ChannelGeneralTabProps {
  instance: ChannelInstanceData;
  agents: AgentData[];
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function ChannelGeneralTab({ instance, agents, onUpdate }: ChannelGeneralTabProps) {
  const { t } = useTranslation("channels");

  const [displayName, setDisplayName] = useState(instance.display_name ?? "");
  const [agentId, setAgentId] = useState(instance.agent_id);
  const [enabled, setEnabled] = useState(instance.enabled);

  // Essential config fields (policies)
  const allConfigFields = configSchema[instance.channel_type] ?? [];
  const essentialFields = allConfigFields.filter((f) => ESSENTIAL_CONFIG_KEYS.includes(f.key));
  const existingConfig = (instance.config ?? {}) as Record<string, unknown>;
  const initialPolicyValues = Object.fromEntries(
    ESSENTIAL_CONFIG_KEYS
      .filter((k) => existingConfig[k] !== undefined)
      .map((k) => [k, existingConfig[k]]),
  );
  const [policyValues, setPolicyValues] = useState<Record<string, unknown>>(initialPolicyValues);

  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const handlePolicyChange = useCallback((key: string, value: unknown) => {
    setPolicyValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      // Merge policy values into existing config, preserving other keys (groups, advanced)
      const cleanPolicies = Object.fromEntries(
        Object.entries(policyValues).filter(([, v]) => v !== undefined && v !== "" && v !== null),
      );
      const mergedConfig = { ...existingConfig, ...cleanPolicies };
      await onUpdate({
        display_name: displayName || null,
        agent_id: agentId,
        enabled,
        config: mergedConfig,
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t("form.errors.failedSave"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      {/* Identity section */}
      <section className="space-y-4 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.general.identity")}</h3>

        <div className="grid gap-1.5">
          <Label>{t("detail.general.name")}</Label>
          <Input value={instance.name} disabled className="text-base md:text-sm" />
          <p className="text-xs text-muted-foreground">{t("detail.general.nameHint")}</p>
        </div>

        <div className="grid gap-1.5">
          <Label>{t("detail.general.channelType")}</Label>
          <Input
            value={channelTypeLabels[instance.channel_type] || instance.channel_type}
            disabled
            className="text-base md:text-sm"
          />
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="cd-display">{t("detail.general.displayName")}</Label>
          <Input
            id="cd-display"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={t("detail.general.displayNamePlaceholder")}
            className="text-base md:text-sm"
          />
        </div>

        <div className="grid gap-1.5">
          <Label>{t("detail.general.agent")}</Label>
          <Select value={agentId} onValueChange={setAgentId}>
            <SelectTrigger className="text-base md:text-sm">
              <SelectValue placeholder={t("detail.general.selectAgent")} />
            </SelectTrigger>
            <SelectContent>
              {agents.map((a) => (
                <SelectItem key={a.id} value={a.id}>
                  {a.display_name || a.agent_key}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex items-center gap-2">
          <Switch id="cd-enabled" checked={enabled} onCheckedChange={setEnabled} />
          <Label htmlFor="cd-enabled">{t("detail.general.enabled")}</Label>
        </div>
      </section>

      {/* Policies section — only shown if this channel type has essential config fields */}
      {essentialFields.length > 0 && (
        <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
          <h3 className="text-sm font-medium">{t("detail.policies")}</h3>
          <ChannelFields
            fields={essentialFields}
            values={policyValues}
            onChange={handlePolicyChange}
            idPrefix="cd-pol"
          />
        </section>
      )}

      <StickySaveBar
        onSave={handleSave}
        saving={saving}
        saved={saved}
        error={saveError}
        label={t("detail.general.saveChanges")}
        savingLabel={t("detail.general.saving")}
        savedLabel={t("detail.general.saved")}
      />
    </div>
  );
}
