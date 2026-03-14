import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Plus, Trash2, ChevronDown, ChevronRight } from "lucide-react";
import { TelegramGroupFields, type TelegramGroupConfigValues } from "./telegram-group-fields";
import { TelegramTopicOverrides, type TelegramTopicConfigValues } from "./telegram-topic-overrides";
import type { GroupManagerGroupInfo } from "./hooks/use-channel-detail";

interface GroupConfigWithTopics extends TelegramGroupConfigValues {
  topics?: Record<string, TelegramTopicConfigValues>;
}

interface Props {
  groups: Record<string, GroupConfigWithTopics>;
  onChange: (groups: Record<string, GroupConfigWithTopics>) => void;
  /** Known groups from the system (e.g. groups with managers assigned) */
  knownGroups?: GroupManagerGroupInfo[];
}

export function TelegramGroupOverrides({ groups, onChange, knownGroups }: Props) {
  const { t } = useTranslation("channels");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [newGroupId, setNewGroupId] = useState("");

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

  const updateGroup = (id: string, config: GroupConfigWithTopics) => {
    onChange({ ...groups, [id]: config });
  };

  const updateTopics = (groupId: string, topics: Record<string, TelegramTopicConfigValues>) => {
    const group = groups[groupId] ?? {};
    const hasTopics = Object.keys(topics).length > 0;
    onChange({
      ...groups,
      [groupId]: { ...group, topics: hasTopics ? topics : undefined },
    });
  };

  const toggle = (id: string) => {
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  // Known groups not yet added as overrides
  const availableGroups = knownGroups?.filter((g) => {
    // Extract the raw group ID from "group:<channel>:<id>" format
    const rawId = extractGroupId(g.group_id);
    return !groups[rawId] && !groups[g.group_id];
  });

  return (
    <fieldset className="rounded-md border p-3 space-y-3">
      <legend className="px-1 text-sm font-medium">{t("groupOverrides.title")}</legend>
      <p className="text-xs text-muted-foreground">{t("groupOverrides.hint")}</p>

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
                {expanded[id] ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                {id === "*"
                  ? t("groupOverrides.groupWildcard")
                  : t("groupOverrides.groupLabel", { id })}
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
              <div className="space-y-4">
                <TelegramGroupFields
                  config={group}
                  onChange={(cfg) => updateGroup(id, { ...cfg, topics: group.topics })}
                  idPrefix={`grp-${id}`}
                />

                <TelegramTopicOverrides
                  topics={group.topics ?? {}}
                  onChange={(topics) => updateTopics(id, topics)}
                />
              </div>
            )}
          </div>
        );
      })}

      {/* Known groups quick-add */}
      {availableGroups && availableGroups.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">{t("groupOverrides.knownGroups")}</p>
          <div className="flex flex-wrap gap-1.5">
            {availableGroups.map((g) => {
              const rawId = extractGroupId(g.group_id);
              return (
                <button
                  key={g.group_id}
                  type="button"
                  onClick={() => addGroup(rawId)}
                  className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs hover:bg-muted/50 transition-colors"
                >
                  <Plus className="h-3 w-3" />
                  <span className="font-mono">{rawId}</span>
                  <Badge variant="secondary" className="text-[10px] px-1 py-0">
                    {g.writer_count}
                  </Badge>
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Manual group ID input */}
      <div className="flex items-center gap-2">
        <Input
          value={newGroupId}
          onChange={(e) => setNewGroupId(e.target.value)}
          placeholder={t("groupOverrides.addGroupPlaceholder")}
          className="h-8 flex-1 text-sm"
          onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), addGroup())}
        />
        <Button type="button" variant="outline" size="sm" className="h-8" onClick={() => addGroup()} disabled={!newGroupId.trim()}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t("groupOverrides.addGroup")}
        </Button>
      </div>
    </fieldset>
  );
}

/** Strips "group:<channel>:" prefix, e.g. "group:telegram:-100123" → "-100123" */
function extractGroupId(id: string): string {
  const m = id.match(/^group:[^:]+:(.+)$/);
  return m?.[1] ?? id;
}
