import { useState, useEffect, useCallback } from "react";
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
import { Plus, Trash2, ChevronDown, ChevronRight, RefreshCw, AlertTriangle } from "lucide-react";
import { useWsCall } from "@/hooks/use-ws-call";
import type { ChannelContact } from "@/types/contact";
import type { AgentData } from "@/types/agent";

interface WhatsAppGroupConfigValues {
  name?: string;
  agent_id?: string;
  enabled?: boolean;
  listen_only?: boolean;
  listen_graph_id?: string;
  require_mention?: boolean;
}

interface Props {
  groups: Record<string, WhatsAppGroupConfigValues>;
  onChange: (groups: Record<string, WhatsAppGroupConfigValues>) => void;
  listContacts: (search: string, channelType?: string) => Promise<ChannelContact[]>;
  agents: AgentData[];
  instanceId: string;
}

interface RefreshedGroup {
  jid: string;
  name: string;
  participant_count: number;
}

export function WhatsAppGroupOverrides({
  groups,
  onChange,
  listContacts,
  agents,
  instanceId,
}: Props) {
  const { t } = useTranslation("channels");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [newGroupId, setNewGroupId] = useState("");
  const [knownGroups, setKnownGroups] = useState<ChannelContact[]>([]);
  const [joinedJIDs, setJoinedJIDs] = useState<Set<string>>(new Set());
  const { call: refreshGroups, loading: refreshing } = useWsCall<{ groups: RefreshedGroup[] }>("whatsapp.groups.refresh");

  const groupIds = Object.keys(groups);

  const addGroup = (id?: string) => {
    const gid = (id ?? newGroupId).trim();
    if (!gid || groups[gid]) return;
    onChange({ ...groups, [gid]: {} });
    setExpanded((prev) => ({ ...prev, [gid]: true }));
    if (!id) setNewGroupId("");
  };

  const removeGroup = (id: string) => {
    const next = { ...groups };
    delete next[id];
    onChange(next);
  };

  const updateGroup = (id: string, config: WhatsAppGroupConfigValues) => {
    onChange({ ...groups, [id]: config });
  };

  const toggle = (id: string) => {
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  // Load discovered WhatsApp groups from contacts API
  const loadKnownGroups = useCallback(async () => {
    try {
      const contacts = await listContacts("", "whatsapp");
      const groupContacts = contacts.filter(
        (c) => c.peer_kind === "group" && c.contact_type === "group",
      );
      setKnownGroups(groupContacts);
    } catch {
      /* handled by http hook */
    }
  }, [listContacts]);

  useEffect(() => {
    loadKnownGroups();
  }, [loadKnownGroups]);

  const handleRefreshGroups = useCallback(async () => {
    try {
      const result = await refreshGroups({ instance_id: instanceId });
      if (result?.groups) {
        setJoinedJIDs(new Set(result.groups.map((g) => g.jid)));
      }
      await loadKnownGroups();
    } catch {
      // error shown by useWsCall
    }
  }, [refreshGroups, instanceId, loadKnownGroups]);

  // Known groups not yet added as overrides
  const availableGroups = knownGroups.filter((g) => !groups[g.sender_id]);

  return (
    <fieldset className="rounded-md border p-3 space-y-3">
      <div className="flex items-center justify-between">
        <legend className="px-1 text-sm font-medium">
          {t("whatsappGroupOverrides.title")}
        </legend>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 gap-1 text-xs"
          onClick={handleRefreshGroups}
          disabled={refreshing}
        >
          <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`} />
          {refreshing ? t("whatsappGroupOverrides.refreshing") : t("whatsappGroupOverrides.refreshGroups")}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {t("whatsappGroupOverrides.hint")}
      </p>

      {groupIds.map((id) => {
        const group = groups[id] ?? {};
        return (
          <div key={id} className="rounded-md border p-3 space-y-3">
            <div className="flex items-center justify-between">
              <button
                type="button"
                className="flex items-center gap-1 text-sm font-medium hover:underline"
                onClick={() => toggle(id)}
              >
                {expanded[id] ? (
                  <ChevronDown className="h-4 w-4" />
                ) : (
                  <ChevronRight className="h-4 w-4" />
                )}
                <span className="text-sm">{group.name || id}</span>
                {group.name && (
                  <span className="font-mono text-xs text-muted-foreground">{id}</span>
                )}
                {joinedJIDs.size > 0 && !joinedJIDs.has(id) && (
                  <span className="inline-flex items-center gap-0.5 rounded-sm bg-yellow-100 dark:bg-yellow-900/30 px-1 py-0.5 text-[10px] font-medium text-yellow-700 dark:text-yellow-400" title={t("whatsappGroupOverrides.notJoined")}>
                    <AlertTriangle className="h-3 w-3" />
                    {t("whatsappGroupOverrides.notJoined")}
                  </span>
                )}
              </button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
                onClick={() => removeGroup(id)}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>

            {expanded[id] && (
              <div className="space-y-3 pl-2">
                {/* Group name / alias */}
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-muted-foreground">
                    {t("whatsappGroupOverrides.nameLabel")}
                  </label>
                  <Input
                    value={group.name ?? ""}
                    onChange={(e) =>
                      updateGroup(id, { ...group, name: e.target.value || undefined })
                    }
                    placeholder={t("whatsappGroupOverrides.namePlaceholder")}
                    className="h-9 text-sm"
                  />
                </div>
                {/* Agent selector */}
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-muted-foreground">
                    {t("whatsappGroupOverrides.agentLabel")}
                  </label>
                  <Select
                    value={group.agent_id ?? ""}
                    onValueChange={(val) =>
                      updateGroup(id, {
                        ...group,
                        agent_id:
                          val === "__default__" || !val ? undefined : val,
                      })
                    }
                  >
                    <SelectTrigger className="h-9">
                      <SelectValue
                        placeholder={t(
                          "whatsappGroupOverrides.selectAgent",
                        )}
                      />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__default__">
                        {t("whatsappGroupOverrides.selectAgent")}
                      </SelectItem>
                      {agents.map((a) => (
                        <SelectItem key={a.id} value={a.agent_key}>
                          {a.display_name || a.agent_key}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                {/* Require @mention override */}
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-muted-foreground">
                    {t("whatsappGroupOverrides.requireMentionLabel")}
                  </label>
                  <Select
                    value={
                      group.require_mention === undefined || group.require_mention === null
                        ? "__inherit__"
                        : group.require_mention
                          ? "true"
                          : "false"
                    }
                    onValueChange={(val) =>
                      updateGroup(id, {
                        ...group,
                        require_mention:
                          val === "__inherit__" ? undefined : val === "true",
                      })
                    }
                  >
                    <SelectTrigger className="h-9">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__inherit__">
                        {t("whatsappGroupOverrides.requireMentionInherit")}
                      </SelectItem>
                      <SelectItem value="true">
                        {t("whatsappGroupOverrides.requireMentionYes")}
                      </SelectItem>
                      <SelectItem value="false">
                        {t("whatsappGroupOverrides.requireMentionNo")}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground/70">
                    {t("whatsappGroupOverrides.requireMentionHint")}
                  </p>
                </div>
                {/* Listen-only toggle */}
                <div className="flex items-center justify-between gap-2">
                  <div className="space-y-0.5">
                    <label className="text-xs font-medium text-muted-foreground">
                      {t("whatsappGroupOverrides.listenOnlyLabel")}
                    </label>
                    <p className="text-xs text-muted-foreground/70">
                      {t("whatsappGroupOverrides.listenOnlyHint")}
                    </p>
                  </div>
                  <Switch
                    checked={group.listen_only ?? false}
                    onCheckedChange={(val) =>
                      updateGroup(id, { ...group, listen_only: val || undefined })
                    }
                  />
                </div>
                {/* Graph ID (shown when listen-only is enabled) */}
                {group.listen_only && (
                  <div className="space-y-1.5">
                    <label className="text-xs font-medium text-muted-foreground">
                      {t("whatsappGroupOverrides.graphIdLabel")}
                    </label>
                    <Input
                      value={group.listen_graph_id ?? ""}
                      onChange={(e) =>
                        updateGroup(id, {
                          ...group,
                          listen_graph_id: e.target.value || undefined,
                        })
                      }
                      placeholder={t("whatsappGroupOverrides.graphIdPlaceholder")}
                      className="h-9 text-sm"
                    />
                    <p className="text-xs text-muted-foreground/70">
                      {t("whatsappGroupOverrides.graphIdHint")}
                    </p>
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}

      {/* Discovered groups quick-add */}
      {availableGroups.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">
            {t("whatsappGroupOverrides.knownGroups")}
          </p>
          <div className="flex flex-wrap gap-1.5">
            {availableGroups.map((g) => (
              <button
                key={g.id}
                type="button"
                onClick={() => addGroup(g.sender_id)}
                className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs hover:bg-muted/50 transition-colors"
              >
                <Plus className="h-3 w-3" />
                <span>{g.display_name || g.sender_id}</span>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Manual group JID input */}
      <div className="flex items-center gap-2">
        <Input
          value={newGroupId}
          onChange={(e) => setNewGroupId(e.target.value)}
          placeholder={t("whatsappGroupOverrides.addGroupPlaceholder")}
          className="h-8 flex-1 text-sm"
          onKeyDown={(e) =>
            e.key === "Enter" && (e.preventDefault(), addGroup())
          }
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8"
          onClick={() => addGroup()}
          disabled={!newGroupId.trim()}
        >
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t("whatsappGroupOverrides.addGroup")}
        </Button>
      </div>
    </fieldset>
  );
}
