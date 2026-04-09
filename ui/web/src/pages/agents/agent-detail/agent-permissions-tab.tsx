import { useState, useEffect, useMemo, useCallback } from "react";
import { Plus, Trash2, Loader2, Shield, FolderOpen, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { useConfigPermissions, type ConfigPermission } from "../hooks/use-config-permissions";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { formatUserLabel } from "@/lib/format-user-label";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import type { DeliveryTarget } from "../hooks/use-agent-heartbeat";

const CONFIG_TYPES = [
  { value: "file_writer",   label: "File Writer",   descKey: "permissions.types.file_writer_desc" },
  { value: "heartbeat",     label: "Heartbeat",     descKey: "permissions.types.heartbeat_desc" },
  { value: "cron",          label: "Cron",          descKey: "permissions.types.cron_desc" },
  { value: "context_files", label: "Context Files", descKey: "permissions.types.context_files_desc" },
  { value: "*",             label: "All (*)",       descKey: "permissions.types.all_desc" },
];

function getScopeOptions(configType: string, targets: DeliveryTarget[]): ComboboxOption[] {
  // Only groups — topic-level permissions are not supported by the backend scope check
  const groupOptions = targets
    .filter((t) => t.kind === "group")
    .map((t) => ({
      value: `group:${t.channel}:${t.chatId}`,
      label: t.title ? `${t.title} (${t.chatId})` : t.chatId,
    }));

  if (configType === "file_writer") {
    return [
      { value: "group:*", label: "All Groups" },
      ...groupOptions,
      { value: "*", label: "Global (*)" },
    ];
  }
  return [
    { value: "agent", label: "Agent (DM)" },
    { value: "group:*", label: "All Groups" },
    ...groupOptions,
    { value: "*", label: "Global (*)" },
  ];
}

interface AgentPermissionsTabProps {
  agentId: string;
}

export function AgentPermissionsTab({ agentId }: AgentPermissionsTabProps) {
  const { t } = useTranslation("agents");
  const ws = useWs();
  const { permissions, loading, load, grant, revoke } = useConfigPermissions(agentId);

  const [userId, setUserId] = useState("");
  const [configType, setConfigType] = useState("file_writer");
  const [scope, setScope] = useState("group:*");
  const [permission, setPermission] = useState("allow");
  const [adding, setAdding] = useState(false);
  const [targets, setTargets] = useState<DeliveryTarget[]>([]);

  // Fetch delivery targets (groups/topics) from channel_contacts
  const loadTargets = useCallback(async () => {
    if (!agentId || !ws.isConnected) return;
    try {
      const res = await ws.call<{ targets: DeliveryTarget[] }>(
        Methods.HEARTBEAT_TARGETS, { agentId },
      );
      setTargets(res.targets ?? []);
    } catch { /* ignore — targets are optional enhancement */ }
  }, [ws, agentId]);

  useEffect(() => { loadTargets(); }, [loadTargets]);

  const scopeOptions = useMemo(
    () => getScopeOptions(configType, targets),
    [configType, targets],
  );

  // Build scope → display label lookup from targets
  const scopeLabels = useMemo(() => {
    const map = new Map<string, string>();
    map.set("group:*", t("permissions.scopes.group_all"));
    map.set("*", t("permissions.scopes.global"));
    map.set("agent", t("permissions.scopes.agent"));
    for (const tgt of targets) {
      const key = `group:${tgt.channel}:${tgt.chatId}`;
      map.set(key, tgt.title ? `${tgt.title} (${tgt.chatId})` : tgt.chatId);
    }
    return map;
  }, [targets, t]);

  // Reset scope when configType changes
  useEffect(() => {
    if (configType === "file_writer") {
      setScope("group:*");
    } else {
      setScope("agent");
    }
  }, [configType]);

  useEffect(() => { load(); }, [load]);

  const handleAdd = async () => {
    if (!userId.trim()) return;
    setAdding(true);
    await grant(scope, configType, userId.trim(), permission);
    setUserId("");
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

  // Resolve user IDs to display names
  const allPermUserIds = useMemo(
    () => [...new Set(permissions.map((p) => p.userId))],
    [permissions],
  );
  const { resolve } = useContactResolver(allPermUserIds);

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
          <UserPickerCombobox
            value={userId}
            onChange={setUserId}
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
                      <span className="text-xs font-medium text-muted-foreground">{scopeLabels.get(scopeKey) ?? scopeKey}</span>
                    </div>
                    {writers.map((p) => {
                      const label = p.metadata?.displayName || formatUserLabel(p.userId, resolve);
                      const username = p.metadata?.username ? ` @${p.metadata.username}` : "";
                      return (
                        <div key={p.id} className="flex items-center justify-between gap-2 px-3 py-2 pl-7">
                          <div className="flex items-center gap-2 min-w-0 text-sm">
                            <Badge
                              variant={p.permission === "allow" ? "success" : "destructive"}
                              className="text-2xs shrink-0"
                            >
                              {p.permission}
                            </Badge>
                            <span className="font-medium truncate">{label}</span>
                            {username && (
                              <span className="text-xs-plus text-muted-foreground shrink-0">{username}</span>
                            )}
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
                        className="text-2xs shrink-0"
                      >
                        {p.permission}
                      </Badge>
                      <span className="font-medium truncate">{formatUserLabel(p.userId, resolve)}</span>
                      <span className="text-xs-plus text-muted-foreground shrink-0">{p.configType}</span>
                      <span className="text-xs-plus text-muted-foreground shrink-0">@ {scopeLabels.get(p.scope) ?? p.scope}</span>
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
