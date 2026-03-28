import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { useHttp } from "@/hooks/use-ws";
import { useContacts } from "@/pages/contacts/hooks/use-contacts";
import { useGroups } from "@/pages/contacts/hooks/use-groups";

/** Channel types that support on-demand group fetch via API */
const REFRESHABLE_GROUP_TYPES = new Set(["slack", "feishu", "discord"]);

interface AllowFromPickerProps {
  channelType: string;
  value: string[];
  onChange: (ids: string[]) => void;
  label?: string;
  help?: string;
  /** "users" = show only contacts, "groups" = show only groups */
  mode?: "users" | "groups";
  /** Instance ID — enables on-demand group fetch for supported channels */
  instanceId?: string;
}

export function AllowFromPicker({ channelType, value, onChange, label, help, mode = "users", instanceId }: AllowFromPickerProps) {
  const { t } = useTranslation("channels");
  const http = useHttp();
  const [search, setSearch] = useState("");
  const [manualId, setManualId] = useState("");
  const [fetching, setFetching] = useState(false);

  const { contacts, loading: contactsLoading } = useContacts(mode === "users" ? { channelType, limit: 200 } : {});
  const { groups, loading: groupsLoading, refresh: refreshGroups } = useGroups(mode === "groups" ? channelType : undefined);

  const canFetchGroups = mode === "groups" && instanceId && REFRESHABLE_GROUP_TYPES.has(channelType);

  const handleFetchGroups = async () => {
    if (!instanceId || fetching) return;
    setFetching(true);
    try {
      await http.post(`/v1/channels/instances/${instanceId}/fetch-groups`);
      refreshGroups();
    } catch (err) {
      console.error("Failed to fetch groups:", err);
    } finally {
      setFetching(false);
    }
  };

  const toggle = (id: string) => {
    if (value.includes(id)) {
      onChange(value.filter((v) => v !== id));
    } else {
      onChange([...value, id]);
    }
  };

  const addManual = () => {
    const trimmed = manualId.trim();
    if (trimmed && !value.includes(trimmed)) {
      onChange([...value, trimmed]);
      setManualId("");
    }
  };

  const contactNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const c of contacts) {
      if (c.display_name) map.set(c.sender_id, c.display_name);
    }
    return map;
  }, [contacts]);

  const groupNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const g of groups) {
      if (g.group_name) map.set(g.group_id, g.group_name);
    }
    return map;
  }, [groups]);

  const resolveName = (id: string): string =>
    contactNameMap.get(id) ?? groupNameMap.get(id) ?? id;

  const lowerSearch = search.toLowerCase();
  const loading = mode === "users" ? contactsLoading : groupsLoading;

  return (
    <div className="space-y-3">
      {label && <Label>{label}</Label>}

      {/* Selected badges */}
      {value.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {value.map((id) => (
            <Badge key={id} variant="secondary" className="gap-1">
              {resolveName(id)}
              <button type="button" onClick={() => toggle(id)} className="ml-1 text-xs hover:text-destructive">
                &times;
              </button>
            </Badge>
          ))}
        </div>
      )}

      {/* Fetch groups button for channels that support on-demand refresh */}
      {canFetchGroups && (
        <Button type="button" variant="outline" size="sm" onClick={handleFetchGroups} disabled={fetching}>
          {fetching ? t("allowFromPicker.fetching", "Fetching...") : t("allowFromPicker.fetchGroups", "Fetch groups")}
        </Button>
      )}

      {loading ? (
        <p className="text-sm text-muted-foreground">{t("zalo.loading")}</p>
      ) : (
        <>
          <Input
            placeholder={t("allowFromPicker.search")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-8 text-base md:text-sm"
          />
          <div className="max-h-56 overflow-y-auto overscroll-contain rounded border p-2 space-y-1">
            {mode === "users" ? (
              (() => {
                const filtered = contacts.filter(
                  (c) =>
                    (c.display_name ?? "").toLowerCase().includes(lowerSearch) ||
                    c.sender_id.toLowerCase().includes(lowerSearch) ||
                    (c.username ?? "").toLowerCase().includes(lowerSearch),
                );
                return filtered.length > 0 ? (
                  filtered.map((c) => (
                    <label key={c.id} className="flex items-center gap-2 py-0.5 text-sm cursor-pointer hover:bg-muted/50 rounded px-1">
                      <input type="checkbox" checked={value.includes(c.sender_id)} onChange={() => toggle(c.sender_id)} />
                      <span className="truncate">{c.display_name ?? c.sender_id}</span>
                      {c.username && <span className="text-xs text-muted-foreground">@{c.username}</span>}
                      <span className="text-xs text-muted-foreground ml-auto shrink-0">{c.sender_id}</span>
                    </label>
                  ))
                ) : (
                  <p className="text-sm text-muted-foreground py-2 text-center">{t("allowFromPicker.noMatch")}</p>
                );
              })()
            ) : (
              (() => {
                const filtered = groups.filter(
                  (g) =>
                    (g.group_name ?? "").toLowerCase().includes(lowerSearch) ||
                    g.group_id.toLowerCase().includes(lowerSearch),
                );
                return filtered.length > 0 ? (
                  filtered.map((g) => (
                    <label key={g.id} className="flex items-center gap-2 py-0.5 text-sm cursor-pointer hover:bg-muted/50 rounded px-1">
                      <input type="checkbox" checked={value.includes(g.group_id)} onChange={() => toggle(g.group_id)} />
                      <span className="truncate">{g.group_name ?? g.group_id}</span>
                      <span className="text-xs text-muted-foreground ml-auto shrink-0">
                        {g.member_count > 0 ? `${g.member_count} members` : g.group_id}
                      </span>
                    </label>
                  ))
                ) : (
                  <p className="text-sm text-muted-foreground py-2 text-center">{t("allowFromPicker.noMatch")}</p>
                );
              })()
            )}
          </div>
        </>
      )}

      {/* Manual ID entry */}
      <div className="flex gap-2">
        <Input
          placeholder={t("allowFromPicker.manualPlaceholder")}
          value={manualId}
          onChange={(e) => setManualId(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addManual(); } }}
          className="h-8 text-base md:text-sm"
        />
        <Button type="button" variant="outline" size="sm" onClick={addManual} disabled={!manualId.trim()}>
          {t("allowFromPicker.add")}
        </Button>
      </div>
      {help && <p className="text-xs text-muted-foreground">{help}</p>}
    </div>
  );
}
