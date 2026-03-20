import { useState, useEffect, useMemo, useRef, useLayoutEffect } from "react";
import { useTranslation } from "react-i18next";
import { createPortal } from "react-dom";
import { Trash2, Plus, X, ChevronDownIcon, Pencil } from "lucide-react";
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

  // Resolve agent display name from UUID
  const agentNameMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) {
      map.set(a.id, a.display_name || a.agent_key);
    }
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
    if (!agentId) {
      setError(t("grants.agentRequired"));
      return;
    }

    const existing = grants.find((g) => g.agent_id === agentId);

    setLoading(true);
    setError("");
    try {
      const allow = toolAllow.length > 0 ? toolAllow : undefined;
      const deny = toolDeny.length > 0 ? toolDeny : undefined;
      await onGrant(agentId, allow, deny);

      if (existing) {
        setGrants((prev) =>
          prev.map((g) =>
            g.agent_id === agentId
              ? { ...g, tool_allow: allow ?? null, tool_deny: deny ?? null }
              : g
          )
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
          {/* Existing grants */}
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
                                  <Badge variant="success" className="text-[10px] px-1.5 py-0">allow</Badge>
                                  {grant.tool_allow!.map((t) => (
                                    <Badge key={t} variant="secondary" className="font-mono text-[10px] px-1.5 py-0">{t}</Badge>
                                  ))}
                                </div>
                              )}
                              {hasDeny && (
                                <div className="flex flex-wrap items-center gap-1">
                                  <Badge variant="destructive" className="text-[10px] px-1.5 py-0">deny</Badge>
                                  {grant.tool_deny!.map((t) => (
                                    <Badge key={t} variant="secondary" className="font-mono text-[10px] px-1.5 py-0">{t}</Badge>
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
        {/* Portal target for dropdowns — inside dialog (pointer events), outside overflow (no clipping) */}
        <div ref={portalRef} className="relative" />
      </DialogContent>
    </Dialog>
  );
}

// Inline multi-select for MCP tool names with dropdown + free-text support.
function ToolMultiSelect({
  value,
  onChange,
  options,
  placeholder = "Select or type tool names...",
  portalContainer,
}: {
  value: string[];
  onChange: (value: string[]) => void;
  options: MCPToolInfo[];
  placeholder?: string;
  portalContainer?: React.RefObject<HTMLDivElement | null>;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    return options
      .filter((t) => !value.includes(t.name))
      .filter((t) => !q || t.name.toLowerCase().includes(q) || (t.description ?? "").toLowerCase().includes(q));
  }, [options, value, search]);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      const inContainer = containerRef.current?.contains(target);
      const inPortal = portalContainer?.current?.contains(target);
      if (!inContainer && !inPortal) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open, portalContainer]);

  const addTool = (name: string) => {
    if (!value.includes(name)) onChange([...value, name]);
    setSearch("");
    inputRef.current?.focus();
  };

  const removeTool = (name: string) => {
    onChange(value.filter((v) => v !== name));
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      const trimmed = search.trim().replace(/,$/, "");
      if (trimmed) addTool(trimmed);
    }
    if (e.key === "Backspace" && !search && value.length > 0) {
      removeTool(value[value.length - 1]!);
    }
  };

  // Portal dropdown: compute position relative to the portal container.
  // Can't use position:fixed because Radix Dialog's transform creates a new containing block.
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({});
  useLayoutEffect(() => {
    if (!open || filtered.length === 0 || !containerRef.current || !portalContainer?.current) return;
    const inputRect = containerRef.current.getBoundingClientRect();
    const portalRect = portalContainer.current.getBoundingClientRect();
    setDropdownStyle({
      position: "absolute",
      top: inputRect.bottom - portalRect.top + 4,
      left: inputRect.left - portalRect.left,
      width: inputRect.width,
      zIndex: 50,
    });
  }, [open, filtered.length, search, value, portalContainer]);

  return (
    <div ref={containerRef} className="relative">
      <div
        className={cn(
          "border-input dark:bg-input/30 flex min-h-9 flex-wrap items-center gap-1 rounded-md border bg-transparent px-2 py-1 text-sm shadow-xs transition-[color,box-shadow]",
          "focus-within:border-ring focus-within:ring-ring/50 focus-within:ring-2",
        )}
        onClick={() => inputRef.current?.focus()}
      >
        {value.map((name) => (
          <span
            key={name}
            className="bg-secondary text-secondary-foreground inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-xs"
          >
            {name}
            <button
              type="button"
              className="hover:text-destructive ml-0.5"
              onClick={(e) => { e.stopPropagation(); removeTool(name); }}
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        <input
          ref={inputRef}
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            if (!open) setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={handleKeyDown}
          placeholder={value.length === 0 ? placeholder : ""}
          className="placeholder:text-muted-foreground min-w-[80px] flex-1 bg-transparent py-0.5 text-base md:text-sm outline-none"
        />
        <ChevronDownIcon
          className="text-muted-foreground size-4 shrink-0 cursor-pointer opacity-50"
          onClick={() => setOpen(!open)}
        />
      </div>
      {open && filtered.length > 0 && portalContainer?.current && createPortal(
        <div
          style={dropdownStyle}
          className="bg-popover text-popover-foreground max-h-56 overflow-y-auto rounded-md border p-1 shadow-md"
        >
          {filtered.map((t) => (
            <button
              key={t.name}
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => addTool(t.name)}
              className="hover:bg-accent hover:text-accent-foreground flex w-full cursor-pointer flex-col items-start rounded-sm px-2 py-1.5 outline-hidden select-none"
            >
              <span className="truncate font-mono text-xs">{t.name}</span>
              {t.description && (
                <span className="text-muted-foreground truncate text-[11px] w-full text-left">
                  {t.description}
                </span>
              )}
            </button>
          ))}
        </div>,
        portalContainer.current,
      )}
    </div>
  );
}
