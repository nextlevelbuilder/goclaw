import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Plus, Trash2, ChevronDown, ChevronRight } from "lucide-react";
import type { ChannelContact } from "@/types/contact";
import type { AgentData } from "@/types/agent";

interface WhatsAppGroupConfigValues {
  agent_id?: string;
  enabled?: boolean;
}

interface Props {
  groups: Record<string, WhatsAppGroupConfigValues>;
  onChange: (groups: Record<string, WhatsAppGroupConfigValues>) => void;
  listContacts: (search: string, channelType?: string) => Promise<ChannelContact[]>;
  agents: AgentData[];
}

export function WhatsAppGroupOverrides({
  groups,
  onChange,
  listContacts,
  agents,
}: Props) {
  const { t } = useTranslation("channels");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [newGroupId, setNewGroupId] = useState("");
  const [knownGroups, setKnownGroups] = useState<ChannelContact[]>([]);

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

  // Known groups not yet added as overrides
  const availableGroups = knownGroups.filter((g) => !groups[g.sender_id]);

  return (
    <fieldset className="rounded-md border p-3 space-y-3">
      <legend className="px-1 text-sm font-medium">
        {t("whatsappGroupOverrides.title")}
      </legend>
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
                <span className="font-mono text-xs">{id}</span>
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
                <span className="font-mono">{g.sender_id}</span>
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
