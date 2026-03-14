import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Save, Check, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
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
import type { ChannelInstanceData } from "@/types/channel";
import type { AgentData } from "@/types/agent";
import { channelTypeLabels } from "../channels-status-view";

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

  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      await onUpdate({
        display_name: displayName || null,
        agent_id: agentId,
        enabled,
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Read-only fields */}
      <div className="grid gap-1.5">
        <Label>{t("detail.general.name")}</Label>
        <Input value={instance.name} disabled />
        <p className="text-xs text-muted-foreground">{t("detail.general.nameHint")}</p>
      </div>

      <div className="grid gap-1.5">
        <Label>{t("detail.general.channelType")}</Label>
        <Input value={channelTypeLabels[instance.channel_type] || instance.channel_type} disabled />
      </div>

      {/* Editable fields */}
      <div className="grid gap-1.5">
        <Label htmlFor="cd-display">{t("detail.general.displayName")}</Label>
        <Input
          id="cd-display"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder={t("detail.general.displayNamePlaceholder")}
        />
      </div>

      <div className="grid gap-1.5">
        <Label>{t("detail.general.agent")}</Label>
        <Select value={agentId} onValueChange={setAgentId}>
          <SelectTrigger>
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

      {/* Save */}
      {saveError && (
        <div className="flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="h-4 w-4 shrink-0" />
          {saveError}
        </div>
      )}
      <div className="flex items-center justify-end gap-2">
        {saved && (
          <span className="flex items-center gap-1 text-sm text-success">
            <Check className="h-3.5 w-3.5" /> {t("detail.general.saved")}
          </span>
        )}
        <Button onClick={handleSave} disabled={saving}>
          {!saving && <Save className="h-4 w-4" />}
          {saving ? t("detail.general.saving") : t("detail.general.saveChanges")}
        </Button>
      </div>
    </div>
  );
}
