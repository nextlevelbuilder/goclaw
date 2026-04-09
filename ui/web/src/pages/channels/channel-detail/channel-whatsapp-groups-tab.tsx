import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Save, Loader2, Trash2, Users, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { ChannelInstanceData } from "@/types/channel";
import { useWhatsAppGroups } from "../hooks/use-whatsapp-groups";
import { useAgents } from "@/pages/agents/hooks/use-agents";

interface ChannelWhatsAppGroupsTabProps {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

// Parse groups config from instance config JSON.
function parseGroupsConfig(config: Record<string, unknown> | null | undefined): Record<string, { agent_id?: string; display_name?: string; enabled?: boolean | null }> {
  if (!config) return {};
  const groups = config.groups as Record<string, { agent_id?: string; display_name?: string; enabled?: boolean | null }> | undefined;
  return groups ?? {};
}

export function ChannelWhatsAppGroupsTab({ instance, onUpdate }: ChannelWhatsAppGroupsTabProps) {
  const { t } = useTranslation("channels");
  const { agents } = useAgents();
  const { groups: discoveredGroups, loading, refreshGroups } = useWhatsAppGroups(instance.id);

  const config = (instance.config ?? {}) as Record<string, unknown>;
  const [groupConfigs, setGroupConfigs] = useState<Record<string, { agent_id?: string; display_name?: string; enabled?: boolean | null }>>(
    parseGroupsConfig(instance.config as Record<string, unknown>),
  );
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [newJid, setNewJid] = useState("");

  // Merge discovered groups with configured groups.
  const allGroupJids = new Set<string>();
  discoveredGroups.forEach((g) => allGroupJids.add(g.jid));
  Object.keys(groupConfigs).forEach((jid) => allGroupJids.add(jid));

  const sortedJids = Array.from(allGroupJids).sort();

  const getGroupName = (jid: string): string => {
    const discovered = discoveredGroups.find((g) => g.jid === jid);
    if (discovered?.name) return discovered.name;
    const configured = groupConfigs[jid];
    return configured?.display_name || jid;
  };

  const getMemberCount = (jid: string): number => {
    const discovered = discoveredGroups.find((g) => g.jid === jid);
    return discovered?.member_count ?? 0;
  };

  const isDiscovered = (jid: string): boolean => {
    return discoveredGroups.some((g) => g.jid === jid);
  };

  const addGroup = () => {
    const jid = newJid.trim();
    if (!jid || groupConfigs[jid]) return;
    setGroupConfigs((prev) => ({ ...prev, [jid]: {} }));
    setNewJid("");
  };

  const removeGroup = (jid: string) => {
    setGroupConfigs((prev) => {
      const next = { ...prev };
      delete next[jid];
      return next;
    });
  };

  const updateGroupConfig = (jid: string, updates: Partial<{ agent_id?: string; display_name?: string; enabled?: boolean | null }>) => {
    setGroupConfigs((prev) => ({
      ...prev,
      [jid]: { ...prev[jid], ...updates },
    }));
  };

  const handleSave = async () => {
    const newConfig = { ...config, groups: Object.keys(groupConfigs).length > 0 ? groupConfigs : undefined };
    // Clean undefined entries
    const cleanConfig = Object.fromEntries(
      Object.entries(newConfig).filter(([, v]) => v !== undefined),
    );
    setSaving(true);
    try {
      await onUpdate({ config: Object.keys(cleanConfig).length > 0 ? cleanConfig : null });
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await refreshGroups();
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium">{t("whatsapp.groups.title")}</h3>
          <p className="text-xs text-muted-foreground mt-1">{t("whatsapp.groups.description")}</p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8"
          onClick={handleRefresh}
          disabled={refreshing || loading}
        >
          {refreshing ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <RefreshCw className="h-3.5 w-3.5" />
          )}
        </Button>
      </div>

      {loading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          {t("whatsapp.groups.discovered")}...
        </div>
      ) : sortedJids.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("whatsapp.groups.noGroups")}</p>
      ) : (
        <div className="space-y-2">
          {sortedJids.map((jid) => {
            const cfg = groupConfigs[jid] ?? {};
            return (
              <div key={jid} className="rounded-lg border p-3 space-y-3">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <Users className="h-4 w-4 text-muted-foreground shrink-0" />
                    <span className="text-sm font-medium truncate">
                      {getGroupName(jid)}
                    </span>
                    {getMemberCount(jid) > 0 && (
                      <Badge variant="secondary" className="text-[10px] px-1.5 py-0 shrink-0">
                        {t("whatsapp.groups.memberCount", { count: getMemberCount(jid) })}
                      </Badge>
                    )}
                    {!isDiscovered(jid) && (
                      <Badge variant="outline" className="text-[10px] px-1.5 py-0 shrink-0">
                        {t("whatsapp.groups.configured")}
                      </Badge>
                    )}
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive shrink-0"
                    onClick={() => removeGroup(jid)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>

                <div className="grid gap-3 sm:grid-cols-[1fr_1fr_auto]">
                  <div>
                    <label className="text-[11px] text-muted-foreground uppercase tracking-wider">
                      {t("whatsapp.groups.assignAgent")}
                    </label>
                    <Select
                      value={cfg.agent_id ?? ""}
                      onValueChange={(v) => updateGroupConfig(jid, { agent_id: v || undefined })}
                    >
                      <SelectTrigger className="h-8 mt-1">
                        <SelectValue placeholder={t("whatsapp.groups.inheritAgent")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__inherit__">{t("whatsapp.groups.inheritAgent")}</SelectItem>
                        {agents.map((a) => (
                          <SelectItem key={a.id} value={a.id}>
                            {a.display_name || a.agent_key}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>

                  <div>
                    <label className="text-[11px] text-muted-foreground uppercase tracking-wider">
                      {t("whatsapp.groups.displayName")}
                    </label>
                    <Input
                      value={cfg.display_name ?? ""}
                      onChange={(e) => updateGroupConfig(jid, { display_name: e.target.value || undefined })}
                      placeholder={t("whatsapp.groups.displayNamePlaceholder")}
                      className="h-8 mt-1"
                    />
                  </div>

                  <div className="flex items-end gap-2 pb-0.5">
                    <Switch
                      checked={cfg.enabled !== false}
                      onCheckedChange={(checked) => updateGroupConfig(jid, { enabled: checked ? undefined : false })}
                    />
                    <span className="text-xs text-muted-foreground">
                      {cfg.enabled !== false ? t("whatsapp.groups.enabled") : t("whatsapp.groups.disabled")}
                    </span>
                  </div>
                </div>

                <div className="text-[11px] text-muted-foreground font-mono truncate">{jid}</div>
              </div>
            );
          })}
        </div>
      )}

      {/* Add group manually */}
      <div className="flex items-center gap-2">
        <Input
          value={newJid}
          onChange={(e) => setNewJid(e.target.value)}
          placeholder={t("whatsapp.groups.jidPlaceholder")}
          className="h-8 flex-1 text-sm"
          onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), addGroup())}
        />
        <Button type="button" variant="outline" size="sm" className="h-8" onClick={addGroup} disabled={!newJid.trim()}>
          {t("whatsapp.groups.add")}
        </Button>
      </div>

      <div className="flex items-center justify-end gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {saving ? t("whatsapp.groups.saving") : t("whatsapp.groups.save")}
        </Button>
      </div>
    </div>
  );
}
