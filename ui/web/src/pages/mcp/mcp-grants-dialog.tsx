import { useState, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Trash2, Plus, Pencil } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn, uniqueId } from "@/lib/utils";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import type { MCPServerData, MCPAgentGrant, MCPToolInfo } from "./hooks/use-mcp";
import { ToolMultiSelect } from "./mcp-tool-multi-select";

interface MCPGrantsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server: MCPServerData;
  onGrant: (agentId: string, toolAllow?: string[], toolDeny?: string[]) => Promise<void>;
  onRevoke: (agentId: string) => Promise<void>;
  onLoadGrants: (serverId: string) => Promise<MCPAgentGrant[]>;
  onLoadTools: (serverId: string) => Promise<MCPToolInfo[]>;
}

export function MCPGrantsDialog({
  open,
  onOpenChange,
  server,
  onGrant,
  onRevoke,
  onLoadGrants,
  onLoadTools,
}: MCPGrantsDialogProps) {
  const { t } = useTranslation("mcp");
  const { agents } = useAgents();
  const portalRef = useRef<HTMLDivElement>(null);
  const [agentId, setAgentId] = useState("");
  const [toolAllow, setToolAllow] = useState<string[]>([]);
  const [toolDeny, setToolDeny] = useState<string[]>([]);
  const [grants, setGrants] = useState<MCPAgentGrant[]>([]);
  const [serverTools, setServerTools] = useState<MCPToolInfo[]>([]);
  const [editingGrantId, setEditingGrantId] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setAgentId("");
      setToolAllow([]);
      setToolDeny([]);
      setEditingGrantId(null);
      setError("");
      setLoading(true);
      Promise.all([
        onLoadGrants(server.id).catch(() => [] as MCPAgentGrant[]),
        onLoadTools(server.id).catch(() => [] as MCPToolInfo[]),
      ]).then(([existingGrants, tools]) => {
        setGrants(existingGrants);
        setServerTools(tools);
      }).finally(() => setLoading(false));
    }
  }, [open, server.id, onLoadGrants, onLoadTools]);

  const agentNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) map.set(a.id, a.display_name || a.agent_key);
    return map;
  }, [agents]);

  const clearForm = () => {
    setAgentId("");
    setToolAllow([]);
    setToolDeny([]);
    setEditingGrantId(null);
    setError("");
  };

  const selectGrant = (grant: MCPAgentGrant) => {
    setAgentId(grant.agent_id);
    setToolAllow(Array.isArray(grant.tool_allow) ? [...grant.tool_allow] : []);
    setToolDeny(Array.isArray(grant.tool_deny) ? [...grant.tool_deny] : []);
    setEditingGrantId(grant.id);
    setError("");
  };

  const isEditing = editingGrantId !== null;

  const handleGrant = async () => {
    if (!agentId) { setError(t("grants.agentRequired")); return; }
    const existing = grants.find((g) => g.agent_id === agentId);
    setLoading(true);
    setError("");
    try {
      const allow = toolAllow.length > 0 ? toolAllow : undefined;
      const deny = toolDeny.length > 0 ? toolDeny : undefined;
      await onGrant(agentId, allow, deny);
      if (existing) {
        setGrants((prev) =>
          prev.map((g) => g.agent_id === agentId ? { ...g, tool_allow: allow ?? null, tool_deny: deny ?? null } : g)
        );
      } else {
        setGrants((prev) => [
          ...prev,
          {
            id: uniqueId(),
            server_id: server.id,
            agent_id: agentId,
            enabled: true,
            tool_allow: allow ?? null,
            tool_deny: deny ?? null,
            granted_by: "",
            created_at: new Date().toISOString(),
          },
        ]);
      }
      clearForm();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("grants.failedGrant"));
    } finally {
      setLoading(false);
    }
  };

  const handleRevoke = async (grant: MCPAgentGrant) => {
    setLoading(true);
    try {
      await onRevoke(grant.agent_id);
      setGrants((prev) => prev.filter((g) => g.agent_id !== grant.agent_id));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t("grants.failedRevoke"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("grants.title", { name: server.display_name || server.name })}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          {/* Existing grants list */}
          {grants.length > 0 && (
            <div className="space-y-2">
              <Label>{t("grants.currentGrants")}</Label>
              <div className="grid gap-2">
                {grants.map((grant) => {
                  const hasAllow = Array.isArray(grant.tool_allow) && grant.tool_allow.length > 0;
                  const hasDeny = Array.isArray(grant.tool_deny) && grant.tool_deny.length > 0;
                  const isActive = editingGrantId === grant.id;
                  return (
                    <div
                      key={grant.id}
                      className={cn(
                        "rounded-md border px-3 py-2.5 cursor-pointer transition-colors",
                        isActive ? "border-ring bg-accent/50 ring-1 ring-ring/30" : "bg-muted/30 hover:bg-muted/50",
                      )}
                      onClick={() => selectGrant(grant)}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-1.5">
                            <span className="text-sm font-medium">
                              {agentNameMap.get(grant.agent_id) || grant.agent_id}
                            </span>
                            {isActive && <Pencil className="h-3 w-3 text-muted-foreground" />}
                          </div>
                          {(hasAllow || hasDeny) && (
                            <div className="mt-1.5 flex flex-col gap-1">
                              {hasAllow && (
                                <div className="flex flex-wrap items-center gap-1">
                                  <Badge variant="success" className="text-2xs px-1.5 py-0">allow</Badge>
                                  {grant.tool_allow!.map((tool) => (
                                    <Badge key={tool} variant="secondary" className="font-mono text-2xs px-1.5 py-0">{tool}</Badge>
                                  ))}
                                </div>
                              )}
                              {hasDeny && (
                                <div className="flex flex-wrap items-center gap-1">
                                  <Badge variant="destructive" className="text-2xs px-1.5 py-0">deny</Badge>
                                  {grant.tool_deny!.map((tool) => (
                                    <Badge key={tool} variant="secondary" className="font-mono text-2xs px-1.5 py-0">{tool}</Badge>
                                  ))}
                                </div>
                              )}
                            </div>
                          )}
                          {!hasAllow && !hasDeny && (
                            <p className="text-xs text-muted-foreground mt-0.5">{t("grants.allToolsAllowed")}</p>
                          )}
                        </div>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 shrink-0"
                          onClick={(e) => { e.stopPropagation(); handleRevoke(grant); }}
                          disabled={loading}
                        >
                          <Trash2 className="h-3.5 w-3.5 text-destructive" />
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Grant form (add or edit) */}
          <div className="space-y-3 rounded-md border p-3">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">
                {isEditing ? t("grants.editGrant") : t("grants.addGrant")}
              </Label>
              {isEditing && (
                <Button variant="ghost" size="sm" onClick={clearForm} className="h-6 px-2 text-xs text-muted-foreground">
                  {t("grants.cancel")}
                </Button>
              )}
            </div>
            <div className="grid gap-2">
              <Select value={agentId} onValueChange={setAgentId} disabled={isEditing}>
                <SelectTrigger>
                  <SelectValue placeholder={t("grants.selectAgent")} />
                </SelectTrigger>
                <SelectContent>
                  {agents.map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      <span>{a.display_name || a.agent_key}</span>
                      <span className="ml-2 text-xs text-muted-foreground">{a.agent_key}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <div className="grid gap-1">
                <Label className="text-xs text-muted-foreground">{t("grants.toolAllowList")}</Label>
                <ToolMultiSelect
                  value={toolAllow}
                  onChange={setToolAllow}
                  options={serverTools}
                  placeholder={t("grants.allowPlaceholder")}
                  portalContainer={portalRef}
                />
              </div>

              <div className="grid gap-1">
                <Label className="text-xs text-muted-foreground">{t("grants.toolDenyList")}</Label>
                <ToolMultiSelect
                  value={toolDeny}
                  onChange={setToolDeny}
                  options={serverTools}
                  placeholder={t("grants.denyPlaceholder")}
                  portalContainer={portalRef}
                />
              </div>
            </div>
            <Button size="sm" onClick={handleGrant} disabled={loading || !agentId} className="gap-1">
              {isEditing ? (
                <><Pencil className="h-3.5 w-3.5" /> {t("grants.update")}</>
              ) : (
                <><Plus className="h-3.5 w-3.5" /> {t("grants.grant")}</>
              )}
            </Button>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        {/* Portal target for dropdowns — inside dialog, outside overflow clipping */}
        <div ref={portalRef} className="relative" />
      </DialogContent>
    </Dialog>
  );
}
