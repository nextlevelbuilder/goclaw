import { useState, useMemo, useEffect, useRef } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { Bot, UserPlus, X, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import type { TeamMemberData } from "@/types/team";

interface TeamMembersDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  members: TeamMemberData[];
  onAddMember: (agentId: string, role?: string) => Promise<void>;
  onRemoveMember: (agentId: string) => Promise<void>;
}

const ROLE_COLORS: Record<string, string> = {
  lead: "bg-amber-500/10 text-amber-700 dark:text-amber-400 border-amber-500/30",
  reviewer: "bg-purple-500/10 text-purple-700 dark:text-purple-400 border-purple-500/30",
  member: "bg-muted text-muted-foreground",
};

export function TeamMembersDialog({
  open, onOpenChange, members, onAddMember, onRemoveMember,
}: TeamMembersDialogProps) {
  const { t } = useTranslation("teams");
  const { agents, refresh } = useAgents();
  const [showAdd, setShowAdd] = useState(false);
  const [selected, setSelected] = useState("");
  const [adding, setAdding] = useState(false);

  const didRefresh = useRef(false);
  useEffect(() => {
    if (open && !didRefresh.current) { didRefresh.current = true; refresh(); }
  }, [open, refresh]);

  // Build emoji lookup from agents' other_config
  const emojiMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) {
      const cfg = a.other_config as Record<string, unknown> | null;
      if (cfg && typeof cfg.emoji === "string" && cfg.emoji) {
        map.set(a.id, cfg.emoji);
      }
    }
    return map;
  }, [agents]);

  const memberIds = useMemo(() => new Set(members.map((m) => m.agent_id)), [members]);
  const available = useMemo(
    () => agents
      .filter((a) => a.agent_type === "predefined" && a.status === "active" && !memberIds.has(a.id))
      .map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
    [agents, memberIds],
  );

  const sorted = useMemo(
    () => [...members].sort((a, b) => {
      if (a.role === "lead" && b.role !== "lead") return -1;
      if (b.role === "lead" && a.role !== "lead") return 1;
      return (a.display_name || a.agent_key || "").localeCompare(b.display_name || b.agent_key || "");
    }),
    [members],
  );

  const [removing, setRemoving] = useState<string | null>(null);

  const handleAdd = async () => {
    if (!selected) return;
    setAdding(true);
    try {
      await onAddMember(selected, "member");
      setSelected("");
      setShowAdd(false);
    } catch {
      // toast handled by hook
    } finally {
      setAdding(false);
    }
  };

  const handleRemove = async (agentId: string) => {
    setRemoving(agentId);
    try {
      await onRemoveMember(agentId);
    } catch {
      // toast handled by hook
    } finally {
      setRemoving(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("members.title")} ({members.length})</DialogTitle>
          <DialogDescription className="sr-only">{t("members.title")}</DialogDescription>
        </DialogHeader>

        {/* Add member */}
        <div className="flex items-center justify-end">
          <Button variant="outline" size="sm" className="gap-1" onClick={() => setShowAdd(!showAdd)}>
            <UserPlus className="h-3.5 w-3.5" />
            {t("members.addMember")}
          </Button>
        </div>

        {showAdd && (
          <div className="flex gap-2">
            <div className="min-w-0 flex-1">
              <Combobox
                value={selected}
                onChange={setSelected}
                options={available}
                placeholder={t("members.searchAgents")}
              />
            </div>
            <Button size="sm" className="h-9 shrink-0 gap-1" disabled={!selected || adding} onClick={handleAdd}>
              {adding && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t("members.add")}
            </Button>
          </div>
        )}

        {/* Member list */}
        <div className="max-h-[60vh] overflow-y-auto overscroll-contain rounded-lg border">
          {sorted.map((m, i) => (
            <div
              key={m.agent_id}
              className={`group flex items-start gap-3 px-4 py-3 hover:bg-muted/30 ${i > 0 ? "border-t" : ""}`}
            >
              {emojiMap.get(m.agent_id) ? (
                <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center text-base leading-none">{emojiMap.get(m.agent_id)}</span>
              ) : (
                <Bot className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
              )}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">
                    {m.display_name || m.agent_key || m.agent_id.slice(0, 8)}
                  </span>
                  <Badge variant="outline" className={`shrink-0 text-[10px] px-1.5 py-0 ${ROLE_COLORS[m.role] ?? ""}`}>
                    {m.role}
                  </Badge>
                </div>
                {m.frontmatter && (
                  <p className="mt-1 break-words text-xs leading-relaxed text-muted-foreground">
                    {m.frontmatter}
                  </p>
                )}
              </div>
              {m.role !== "lead" && (
                <button
                  className="mt-0.5 shrink-0 cursor-pointer text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100 disabled:opacity-50"
                  onClick={() => handleRemove(m.agent_id)}
                  disabled={removing === m.agent_id}
                >
                  {removing === m.agent_id ? <Loader2 className="h-4 w-4 animate-spin" /> : <X className="h-4 w-4" />}
                </button>
              )}
            </div>
          ))}
          {members.length === 0 && (
            <div className="px-4 py-6 text-center text-sm text-muted-foreground">{t("members.noMembers")}</div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
