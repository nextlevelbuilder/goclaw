import { useState, useEffect, useMemo } from "react";
import { Plus, Trash2, Loader2, Shield, FolderOpen, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { useConfigPermissions, type ConfigPermission } from "../hooks/use-config-permissions";
import { useContactSearch } from "../hooks/use-contact-search";
import type { ChannelContact } from "@/types/contact";

const CONFIG_TYPES = [
  { value: "file_writer",   label: "File Writer",   descKey: "permissions.types.file_writer_desc" },
  { value: "heartbeat",     label: "Heartbeat",     descKey: "permissions.types.heartbeat_desc" },
  { value: "cron",          label: "Cron",          descKey: "permissions.types.cron_desc" },
  { value: "context_files", label: "Context Files", descKey: "permissions.types.context_files_desc" },
  { value: "*",             label: "All (*)",       descKey: "permissions.types.all_desc" },
];

function getScopeOptions(configType: string, existingScopes: string[]): ComboboxOption[] {
  if (configType === "file_writer") {
    const dynamic = existingScopes.map((s) => ({ value: s, label: s }));
    return [
      { value: "group:*", label: "All Groups" },
      ...dynamic,
      { value: "*", label: "Global (*)" },
    ];
  }
  return [
    { value: "agent", label: "Agent (DM)" },
    { value: "group:*", label: "All Groups" },
    { value: "*", label: "Global (*)" },
  ];
}

interface AgentPermissionsTabProps {
  agentId: string;
}

export function AgentPermissionsTab({ agentId }: AgentPermissionsTabProps) {
  const { t } = useTranslation("agents");
  const { permissions, loading, load, grant, revoke } = useConfigPermissions(agentId);

  const [userId, setUserId] = useState("");
  const [configType, setConfigType] = useState("file_writer");
  const [scope, setScope] = useState("group:*");
  const [permission, setPermission] = useState("allow");
  const [adding, setAdding] = useState(false);
  const [selectedContact, setSelectedContact] = useState<ChannelContact | null>(null);

  const { contacts } = useContactSearch(userId);

  const contactOptions: ComboboxOption[] = useMemo(() =>
    contacts.map((c) => {
      const name = c.display_name || c.sender_id;
      const username = c.username ? ` @${c.username}` : "";
      const channel = c.channel_type ? ` [${c.channel_type}]` : "";
      return { value: c.sender_id, label: `${name}${username} (${c.sender_id})${channel}` };
    }),
    [contacts],
  );

  // Collect existing file_writer scopes for dynamic scope options
  const existingFileWriterScopes = useMemo(() =>
    [...new Set(
      permissions
        .filter((p) => p.configType === "file_writer")
        .map((p) => p.scope)
    )],
    [permissions],
  );

  const scopeOptions = useMemo(
    () => getScopeOptions(configType, existingFileWriterScopes),
    [configType, existingFileWriterScopes],
  );

  // Reset scope when configType changes
  useEffect(() => {
    if (configType === "file_writer") {
      setScope("group:*");
    } else {
      setScope("agent");
    }
  }, [configType]);

  useEffect(() => { load(); }, [load]);

  const handleUserChange = (val: string) => {
    setUserId(val);
    const contact = contacts.find((c) => c.sender_id === val);
    setSelectedContact(contact ?? null);
  };

  const handleAdd = async () => {
    if (!userId.trim()) return;
    setAdding(true);
    const meta =
      configType === "file_writer" && selectedContact
        ? {
            displayName: selectedContact.display_name ?? "",
            username: selectedContact.username ?? "",
          }
        : undefined;
    await grant(scope, configType, userId.trim(), permission, meta);
    setUserId("");
    setSelectedContact(null);
    setAdding(false);
  };

  // Split permissions into two sections
  const fileWriters = useMemo(
    () => permissions.filter((p) => p.configType === "file_writer"),
    [permissions],
  );
  const configPerms = useMemo(
    () => permissions.filter((p) => p.configType !== "file_writer"),
    [permissions],
  );

  // Group file_writer by scope
  const fileWritersByScope = useMemo(() => {
    const map = new Map<string, ConfigPermission[]>();
    for (const p of fileWriters) {
      const list = map.get(p.scope) ?? [];
      list.push(p);
      map.set(p.scope, list);
    }
    return map;
  }, [fileWriters]);

  const currentDescKey = CONFIG_TYPES.find((c) => c.value === configType)?.descKey ?? "";

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium flex items-center gap-2">
            <Shield className="h-4 w-4 text-amber-500" />
            {t("permissions.title")}
          </h3>
          <p className="text-xs text-muted-foreground mt-1">{t("permissions.description")}</p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 w-7 p-0 shrink-0 text-muted-foreground"
          onClick={load}
          disabled={loading}
        >
          {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
        </Button>
      </div>

      {/* Add Rule form */}
      <div className="space-y-2">
        <div className="flex flex-wrap items-end gap-2">
          <Combobox
            value={userId}
            onChange={handleUserChange}
            options={contactOptions}
            placeholder={t("permissions.userIdPlaceholder")}
            className="flex-1 min-w-[160px]"
          />
          <Select value={configType} onValueChange={setConfigType}>
            <SelectTrigger className="w-[130px] text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CONFIG_TYPES.map((o) => (
                <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Combobox
            value={scope}
            onChange={setScope}
            options={scopeOptions}
            placeholder={t("permissions.scopePlaceholder")}
            className="min-w-[140px]"
          />
          <Select value={permission} onValueChange={setPermission}>
            <SelectTrigger className="w-[90px] text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="allow">Allow</SelectItem>
              <SelectItem value="deny">Deny</SelectItem>
            </SelectContent>
          </Select>
          <Button
            size="icon"
            className="h-9 w-9 shrink-0"
            onClick={handleAdd}
            disabled={adding || !userId.trim()}
          >
            {adding ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
          </Button>
        </div>
        {currentDescKey && (
          <p className="text-xs text-muted-foreground">{t(currentDescKey)}</p>
        )}
      </div>

      {/* Rules list */}
      {loading && permissions.length === 0 ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : permissions.length === 0 ? (
        <p className="text-xs text-muted-foreground text-center py-6">{t("permissions.noRules")}</p>
      ) : (
        <div className="space-y-4">
          {/* File Writers section */}
          {fileWriters.length > 0 && (
            <div>
              <p className="text-xs font-medium text-muted-foreground mb-2">
                {t("permissions.fileWriters")} ({fileWriters.length})
              </p>
              <div className="rounded-lg border divide-y">
                {[...fileWritersByScope.entries()].map(([scopeKey, writers]) => (
                  <div key={scopeKey}>
                    <div className="flex items-center gap-1.5 px-3 py-1.5 bg-muted/40">
                      <FolderOpen className="h-3.5 w-3.5 text-muted-foreground" />
                      <span className="text-xs font-medium text-muted-foreground">{scopeKey}</span>
                    </div>
                    {writers.map((p) => {
                      const displayName = p.metadata?.displayName || p.userId;
                      const username = p.metadata?.username ? ` @${p.metadata.username}` : "";
                      return (
                        <div key={p.id} className="flex items-center justify-between gap-2 px-3 py-2 pl-7">
                          <div className="flex items-center gap-2 min-w-0 text-sm">
                            <Badge
                              variant={p.permission === "allow" ? "success" : "destructive"}
                              className="text-[10px] shrink-0"
                            >
                              {p.permission}
                            </Badge>
                            <span className="font-medium truncate">{displayName}</span>
                            {username && (
                              <span className="text-[11px] text-muted-foreground shrink-0">{username}</span>
                            )}
                            <span className="text-[11px] text-muted-foreground shrink-0 font-mono">({p.userId})</span>
                          </div>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 w-7 p-0 shrink-0 text-muted-foreground hover:text-destructive"
                            onClick={() => revoke(p.scope, p.configType, p.userId)}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      );
                    })}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Config Permissions section */}
          {configPerms.length > 0 && (
            <div>
              <p className="text-xs font-medium text-muted-foreground mb-2">
                {t("permissions.configPerms")} ({configPerms.length})
              </p>
              <div className="rounded-lg border divide-y">
                {configPerms.map((p) => (
                  <div key={p.id} className="flex items-center justify-between gap-2 px-3 py-2">
                    <div className="flex items-center gap-2 min-w-0 text-sm">
                      <Badge
                        variant={p.permission === "allow" ? "success" : "destructive"}
                        className="text-[10px] shrink-0"
                      >
                        {p.permission}
                      </Badge>
                      <span className="font-medium truncate">{p.userId}</span>
                      <span className="text-[11px] text-muted-foreground shrink-0">{p.configType}</span>
                      <span className="text-[11px] text-muted-foreground shrink-0">@ {p.scope}</span>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 shrink-0 text-muted-foreground hover:text-destructive"
                      onClick={() => revoke(p.scope, p.configType, p.userId)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
